// Package conformance runs the dependency-free v2 client against Connect
// handlers generated from a copy of vibe-cards' proto/cards/v2, so a field
// name, an enum or an error shape the client gets wrong fails here rather
// than in a consumer.
//
// The fake server decodes requests STRICTLY (an unknown field is an error),
// and runs twice: with vibe-cards' production codec (numeric enums, every
// field emitted) and with canonical protojson (enum names, unset fields
// omitted). The model behind it is deliberately small — the real rules live
// in vibe-cards and are tested there (internal/contract); what is tested
// here is the wire.
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	vibecards "github.com/fox1ck-org/vibe-cards-client/v2"
	pb "github.com/fox1ck-org/vibe-cards-client/v2/conformance/gen/cards/v2"
	"github.com/fox1ck-org/vibe-cards-client/v2/conformance/gen/cards/v2/cardsv2connect"
)

// jsonCodec is vibe-cards' internal/infra/codec.NumericEnumJSON with
// DiscardUnknown turned OFF: the production server would silently drop a
// misspelt request field, and that is exactly what this test must catch.
type jsonCodec struct{ numeric bool }

func (jsonCodec) Name() string { return "json" }

func (c jsonCodec) Marshal(v any) ([]byte, error) {
	return protojson.MarshalOptions{UseEnumNumbers: c.numeric, EmitUnpopulated: c.numeric}.Marshal(v.(proto.Message))
}

func (jsonCodec) Unmarshal(data []byte, v any) error {
	return protojson.UnmarshalOptions{DiscardUnknown: false}.Unmarshal(data, v.(proto.Message))
}

// reasonErr is vibe-cards' handler.reasonError: the code plus a
// google.rpc.ErrorInfo whose reason is the lower-snake ErrorReason name.
func reasonErr(code connect.Code, r pb.ErrorReason, meta map[string]string) error {
	e := connect.NewError(code, fmt.Errorf("refused: %s", r))
	d, err := connect.NewErrorDetail(&errdetails.ErrorInfo{Reason: reasonWord(r), Domain: "cards.v2", Metadata: meta})
	if err != nil {
		panic(err)
	}
	e.AddDetail(d)
	return e
}

func reasonWord(r pb.ErrorReason) string {
	return strings.ToLower(strings.TrimPrefix(r.String(), "ERROR_REASON_"))
}

type caller struct {
	sub     string
	service bool
	actor   string
}

type ctxKey struct{}

// auth mirrors vibe-cards' interceptor in what the client relies on: a
// Bearer credential, and X-Actor-* believed only from a service key with
// the on_behalf scope.
func auth(onBehalfKeys map[string]bool) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			tok, ok := strings.CutPrefix(req.Header().Get("Authorization"), "Bearer ")
			if !ok || tok == "" {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("no credential"))
			}
			c := caller{sub: tok}
			if strings.HasPrefix(tok, "vck_") {
				c.sub, c.service = "api-key:"+strings.TrimPrefix(tok, "vck_"), true
				if onBehalfKeys[tok] {
					c.actor = req.Header().Get("X-Actor-Sub")
				}
			}
			return next(context.WithValue(ctx, ctxKey{}, c), req)
		}
	}
}

func callerOf(ctx context.Context) caller { return ctx.Value(ctxKey{}).(caller) }

func person(id string) *pb.Principal { return &pb.Principal{Kind: pb.Principal_KIND_PERSON, Id: id} }

// fake is the small model: cards, PAN tickets, 3DS codes.
type fake struct {
	cardsv2connect.UnimplementedCardServiceHandler
	cardsv2connect.UnimplementedHoldingServiceHandler
	cardsv2connect.UnimplementedPanServiceHandler
	cardsv2connect.UnimplementedShareServiceHandler
	cardsv2connect.UnimplementedSpendServiceHandler
	cardsv2connect.UnimplementedIssuingServiceHandler

	mu     sync.Mutex
	cards  []*pb.Card
	grants map[string]string // token → card id
	codes  map[string][]*pb.WaitThreeDSResponse
	// raceTake: the next Take of this card finds somebody else got it first.
	raceTake map[string]string
	// reason, when set, is answered by Get for the drift test.
	reason pb.ErrorReason
}

func (f *fake) find(id string) (*pb.Card, error) {
	for _, c := range f.cards {
		if c.GetId() == id {
			return c, nil
		}
	}
	return nil, reasonErr(connect.CodeNotFound, pb.ErrorReason_ERROR_REASON_NOT_FOUND, nil)
}

func (f *fake) Get(_ context.Context, req *connect.Request[pb.GetRequest]) (*connect.Response[pb.GetResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reason != pb.ErrorReason_ERROR_REASON_UNSPECIFIED {
		return nil, reasonErr(connect.CodeFailedPrecondition, f.reason, map[string]string{"capability": "pause"})
	}
	c, err := f.find(req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetResponse{Card: c}), nil
}

func (f *fake) List(_ context.Context, req *connect.Request[pb.ListRequest]) (*connect.Response[pb.ListResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	flt := req.Msg.GetFilter()
	var sel []*pb.Card
	for _, c := range f.cards {
		if !hasAll(c.GetLabels(), flt.GetLabels()) {
			continue
		}
		if len(flt.GetStatus()) > 0 && !slices.Contains(flt.GetStatus(), c.GetStatus()) {
			continue
		}
		if len(flt.GetHolder()) > 0 {
			who := "none"
			if c.Holder != nil {
				who = c.GetHolder().GetHolder().GetId()
			}
			if !slices.Contains(flt.GetHolder(), who) {
				continue
			}
		}
		sel = append(sel, c)
	}
	size := int(req.Msg.GetPage().GetPageSize())
	if size == 0 {
		size = 100
	}
	from, _ := strconv.Atoi(req.Msg.GetPage().GetPageToken())
	to := min(from+size, len(sel))
	next := ""
	if to < len(sel) {
		next = strconv.Itoa(to)
	}
	return connect.NewResponse(&pb.ListResponse{Cards: sel[from:to],
		Page: &pb.PageResponse{NextPageToken: next, TotalCount: int32(len(sel))}}), nil
}

func hasAll(have, want []string) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}
	return true
}

func (f *fake) Take(ctx context.Context, req *connect.Request[pb.TakeRequest]) (*connect.Response[pb.TakeResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := f.find(req.Msg.GetCardId())
	if err != nil {
		return nil, err
	}
	if other, ok := f.raceTake[c.GetId()]; ok {
		delete(f.raceTake, c.GetId())
		c.Holder = &pb.Holding{Holder: person(other), TakenBy: person(other), From: timestamppb.Now()}
	}
	if c.GetStatus() != pb.CardStatus_CARD_STATUS_ACTIVE {
		return nil, reasonErr(connect.CodeFailedPrecondition, pb.ErrorReason_ERROR_REASON_CARD_NOT_ACTIVE, nil)
	}
	who := callerOf(ctx)
	holder := req.Msg.GetHolder().GetId()
	if holder == "" {
		holder = who.actor
	}
	if holder == "" {
		holder = who.sub
	}
	if c.Holder != nil && c.GetHolder().GetHolder().GetId() != holder {
		return nil, reasonErr(connect.CodeFailedPrecondition, pb.ErrorReason_ERROR_REASON_HELD_BY_OTHER, nil)
	}
	if c.Holder == nil {
		c.Holder = &pb.Holding{Holder: person(holder), TakenBy: &pb.Principal{Kind: pb.Principal_KIND_SERVICE, Id: who.sub},
			From: timestamppb.Now()}
	}
	if l := req.Msg.GetLimits(); l != nil {
		c.Limits = l
	}
	if u := req.Msg.GetUsage(); u != nil {
		f.addUsage(c, u, who)
	}
	return connect.NewResponse(&pb.TakeResponse{Card: c}), nil
}

func (f *fake) addUsage(c *pb.Card, u *pb.Usage, who caller) {
	u = proto.Clone(u).(*pb.Usage)
	u.AddedBy = &pb.Principal{Kind: pb.Principal_KIND_SERVICE, Id: who.sub}
	u.AddedAt = timestamppb.Now()
	if who.actor != "" {
		u.OnBehalfOf = person(who.actor)
	}
	c.Usages = slices.DeleteFunc(c.Usages, func(x *pb.Usage) bool {
		return x.GetApp() == u.GetApp() && x.GetExternalId() == u.GetExternalId()
	})
	c.Usages = append(c.Usages, u)
}

func (f *fake) AddUsage(ctx context.Context, req *connect.Request[pb.AddUsageRequest]) (*connect.Response[pb.AddUsageResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := f.find(req.Msg.GetCardId())
	if err != nil {
		return nil, err
	}
	f.addUsage(c, req.Msg.GetUsage(), callerOf(ctx))
	return connect.NewResponse(&pb.AddUsageResponse{Card: c}), nil
}

func (f *fake) Return(_ context.Context, req *connect.Request[pb.ReturnRequest]) (*connect.Response[pb.ReturnResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := f.find(req.Msg.GetCardId())
	if err != nil {
		return nil, err
	}
	if c.Holder != nil {
		last := c.Holder
		last.To = timestamppb.Now()
		c.LastHolding, c.Holder, c.Usages = last, nil, nil
	}
	return connect.NewResponse(&pb.ReturnResponse{Card: c}), nil
}

func (f *fake) IssueGrant(ctx context.Context, req *connect.Request[pb.IssueGrantRequest]) (*connect.Response[pb.IssueGrantResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	who := callerOf(ctx)
	if !who.service {
		return nil, reasonErr(connect.CodePermissionDenied, pb.ErrorReason_ERROR_REASON_NOT_ALLOWED, nil)
	}
	c, err := f.find(req.Msg.GetCardId())
	if err != nil {
		return nil, err
	}
	h := c.GetHolder().GetHolder().GetId()
	if h == "" || (h != who.sub && h != who.actor) {
		return nil, reasonErr(connect.CodePermissionDenied, pb.ErrorReason_ERROR_REASON_NOT_SHARED_FOR_USE, nil)
	}
	if req.Msg.GetUsage().GetApp() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the grant names no usage"))
	}
	tok := fmt.Sprintf("grant-%d", len(f.grants)+1)
	f.grants[tok] = c.GetId()
	return connect.NewResponse(&pb.IssueGrantResponse{Token: tok,
		ExpiresAt: timestamppb.New(time.Now().Add(2 * time.Minute))}), nil
}

func (f *fake) Redeem(_ context.Context, req *connect.Request[pb.RedeemRequest]) (*connect.Response[pb.RedeemResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.grants[req.Msg.GetToken()]
	if !ok {
		return nil, reasonErr(connect.CodeNotFound, pb.ErrorReason_ERROR_REASON_GRANT_SPENT, nil)
	}
	delete(f.grants, req.Msg.GetToken())
	c, err := f.find(id)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RedeemResponse{Number: c.GetBin() + "000000" + c.GetLast4(), ExpMonth: "01",
		ExpYear: "2030", Cvv: "123", Holder: &pb.HolderData{Name: "JOHN DOE", Country: "GB", Zip: "E1 6AN"}}), nil
}

func (f *fake) WaitThreeDS(_ context.Context, req *connect.Request[pb.WaitThreeDSRequest]) (*connect.Response[pb.WaitThreeDSResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.Msg.GetWaitSeconds() != 5 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("wait %d", req.Msg.GetWaitSeconds()))
	}
	since := req.Msg.GetSince().AsTime()
	for _, code := range f.codes[req.Msg.GetCardId()] {
		if code.GetDeliveredAt().AsTime().After(since) {
			return connect.NewResponse(code), nil
		}
	}
	return connect.NewResponse(&pb.WaitThreeDSResponse{}), nil
}

func (f *fake) Share(ctx context.Context, req *connect.Request[pb.ShareRequest]) (*connect.Response[pb.ShareResponse], error) {
	out := make([]*pb.Share, 0, len(req.Msg.GetShares()))
	for i, s := range req.Msg.GetShares() {
		s = proto.Clone(s).(*pb.Share)
		if s.GetTarget() == nil || s.GetRight() == pb.Right_RIGHT_UNSPECIFIED {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("target and right"))
		}
		s.Id = fmt.Sprintf("share-%d", i+1)
		s.SharedBy = person(callerOf(ctx).sub)
		s.CreatedAt = timestamppb.Now()
		out = append(out, s)
	}
	return connect.NewResponse(&pb.ShareResponse{Shares: out}), nil
}

func (f *fake) Query(_ context.Context, req *connect.Request[pb.QueryRequest]) (*connect.Response[pb.QueryResponse], error) {
	r := req.Msg.GetRange()
	if r.GetFrom().AsTime().IsZero() || !r.GetTo().AsTime().After(r.GetFrom().AsTime()) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("range"))
	}
	if !slices.Equal(req.Msg.GetTxStatus(), []pb.TxStatus{pb.TxStatus_TX_STATUS_COMPLETED}) ||
		!slices.Equal(req.Msg.GetFilter().GetCardIds(), []string{"d"}) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("filter"))
	}
	usd := func(m int64) *pb.Money { return &pb.Money{Minor: m, Currency: "USD"} }
	return connect.NewResponse(&pb.QueryResponse{
		Totals: &pb.SpendTotals{Spent: usd(123456789012), Operations: 1},
		Transactions: []*pb.Transaction{{Id: "t1", CardId: "d", IssuerReference: "issuer-reference-fixture", Amount: usd(123456789012), IssuerFee: usd(0),
			PlatformFee: usd(0), Merchant: "Google", Status: pb.TxStatus_TX_STATUS_COMPLETED, At: r.GetFrom()}},
		Page: &pb.PageResponse{TotalCount: 1},
	}), nil
}

func newServer(t *testing.T, numeric bool) (*fake, *vibecards.Client) {
	t.Helper()
	usd := func(m int64) *pb.Money { return &pb.Money{Minor: m, Currency: "USD"} }
	card := func(id string, status pb.CardStatus, labels ...string) *pb.Card {
		return &pb.Card{Id: id, Last4: "42" + id + id, Bin: "424242", Network: "visa", Currency: "USD", Status: status,
			Owner: person("sub-owner"), Labels: labels, Limits: &pb.Limits{Total: usd(50000)},
			SpentTotal: usd(0), SpentToday: usd(0), Capabilities: []string{"pause", "three_ds"},
			CreatedAt: timestamppb.Now(), UpdatedAt: timestamppb.Now()}
	}
	f := &fake{
		cards: []*pb.Card{
			card("a", pb.CardStatus_CARD_STATUS_ACTIVE, "uk", "google"),
			card("b", pb.CardStatus_CARD_STATUS_PAUSED, "uk", "google"),
			card("c", pb.CardStatus_CARD_STATUS_ACTIVE, "de", "google"),
			card("d", pb.CardStatus_CARD_STATUS_ACTIVE, "uk", "google"),
		},
		grants:   map[string]string{},
		codes:    map[string][]*pb.WaitThreeDSResponse{},
		raceTake: map[string]string{"a": "sub-other"},
	}
	opts := []connect.HandlerOption{
		connect.WithInterceptors(auth(map[string]bool{"vck_accounts": true})),
		connect.WithCodec(jsonCodec{numeric: numeric}),
	}
	mux := http.NewServeMux()
	mux.Handle(cardsv2connect.NewCardServiceHandler(f, opts...))
	mux.Handle(cardsv2connect.NewHoldingServiceHandler(f, opts...))
	mux.Handle(cardsv2connect.NewPanServiceHandler(f, opts...))
	mux.Handle(cardsv2connect.NewShareServiceHandler(f, opts...))
	mux.Handle(cardsv2connect.NewSpendServiceHandler(f, opts...))
	mux.Handle(cardsv2connect.NewIssuingServiceHandler(f, opts...))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return f, vibecards.New(srv.URL, "vck_accounts")
}

func codecs(t *testing.T, fn func(t *testing.T, numeric bool)) {
	for _, numeric := range []bool{true, false} {
		name := "protojson"
		if numeric {
			name = "vibe-cards-codec"
		}
		t.Run(name, func(t *testing.T) { fn(t, numeric) })
	}
}

// TestTakeUsePresentConfirmReturn is the path a service walks for a person:
// any free card labelled uk+google → where it works → its details → its
// 3DS code → back to its owner.
func TestTakeUsePresentConfirmReturn(t *testing.T) {
	codecs(t, func(t *testing.T, numeric bool) {
		f, client := newServer(t, numeric)
		ctx := vibecards.OnBehalfOf(context.Background(), "sub-buyer", "buyer@example.test")
		usage := vibecards.Usage{App: "vibe-accounts", ExternalID: "acc-1", URL: "https://accounts.example.test/acc-1", Label: "UK-01"}

		// a: somebody else wins the race; b: paused; c: wrong label → d.
		card, err := client.TakeFirst(ctx, vibecards.TakeFirstInput{
			Filter: vibecards.Filter{Labels: []string{"uk", "google"}},
			Limits: &vibecards.Limits{Total: ptr(vibecards.USD(30000))},
		})
		if err != nil {
			t.Fatalf("TakeFirst: %v", err)
		}
		if card.ID != "d" || !card.HeldBy("sub-buyer") || card.Holder.TakenBy.Kind != vibecards.PrincipalService ||
			card.Limits.Total.Minor != 30000 || card.Status != vibecards.CardStatusActive || card.Owner.ID != "sub-owner" {
			t.Fatalf("took %+v", card)
		}

		card, err = client.AddUsage(ctx, card.ID, usage)
		if err != nil {
			t.Fatalf("AddUsage: %v", err)
		}
		u := card.UsageOf("vibe-accounts", "acc-1")
		if u == nil || u.Label != "UK-01" || u.OnBehalfOf == nil || u.OnBehalfOf.ID != "sub-buyer" ||
			u.AddedBy == nil || u.AddedBy.ID != "api-key:accounts" || u.AddedAt == nil {
			t.Fatalf("usage %+v", u)
		}

		g, err := client.IssueGrant(ctx, card.ID, &usage)
		if err != nil {
			t.Fatalf("IssueGrant: %v", err)
		}
		if g.Token == "" || time.Until(g.ExpiresAt) <= 0 {
			t.Fatalf("grant %+v", g)
		}
		d, err := client.Redeem(ctx, g.Token)
		if err != nil {
			t.Fatalf("Redeem: %v", err)
		}
		if d.Number != "42424200000042dd" || d.CVV != "123" || d.Holder.Country != "GB" || d.ExpYear != "2030" {
			t.Fatalf("details %+v", d)
		}
		if _, err := client.Redeem(ctx, g.Token); !errors.Is(err, vibecards.ErrGrantSpent) {
			t.Fatalf("second redeem: %v", err)
		}
		if _, err := client.CardDetails(ctx, card.ID, &usage); err != nil {
			t.Fatalf("CardDetails: %v", err)
		}

		before := time.Now()
		f.mu.Lock()
		f.codes["d"] = append(f.codes["d"], &pb.WaitThreeDSResponse{Code: "777111",
			DeliveredAt: timestamppb.New(before.Add(time.Second)), Merchant: "Google Payments"})
		f.mu.Unlock()
		code, err := client.WaitThreeDS(ctx, card.ID, before, 5*time.Second)
		if err != nil || code == nil || code.Code != "777111" || code.Merchant != "Google Payments" || code.DeliveredAt.IsZero() {
			t.Fatalf("3ds %+v %v", code, err)
		}
		code, err = client.WaitThreeDS(ctx, card.ID, before.Add(time.Minute), 5*time.Second)
		if err != nil || code != nil {
			t.Fatalf("no code must be nil, nil: %+v %v", code, err)
		}

		card, err = client.Return(ctx, card.ID, "account died")
		if err != nil {
			t.Fatalf("Return: %v", err)
		}
		if card.Held() || len(card.Usages) != 0 || card.LastHolding == nil || card.LastHolding.To == nil ||
			card.LastHolding.Holder.ID != "sub-buyer" {
			t.Fatalf("returned %+v", card)
		}

		// A person's own token: a ticket is for services only.
		if _, err := client.IssueGrant(vibecards.WithToken(ctx, "person-jwt"), card.ID, &usage); !errors.Is(err, vibecards.ErrNotAllowed) {
			t.Fatalf("person grant: %v", err)
		}
		if _, err := client.Take(ctx, vibecards.TakeInput{CardID: "nope"}); !errors.Is(err, vibecards.ErrNotFound) {
			t.Fatalf("unknown card: %v", err)
		}
	})
}

func TestShareAndSpendWire(t *testing.T) {
	codecs(t, func(t *testing.T, numeric bool) {
		_, client := newServer(t, numeric)
		ctx := context.Background()
		shares, err := client.Share(ctx, vibecards.Share{AllOfOwner: "sub-buyer", With: vibecards.Person("sub-assistant"),
			Right: vibecards.RightUse}, vibecards.Share{Label: "uk", With: vibecards.Person("sub-viewer"), Right: vibecards.RightView})
		if err != nil {
			t.Fatalf("Share: %v", err)
		}
		if len(shares) != 2 || shares[0].ID == "" || shares[0].AllOfOwner != "sub-buyer" || shares[0].Right != vibecards.RightUse ||
			shares[1].Label != "uk" || shares[1].With.Kind != vibecards.PrincipalPerson || shares[1].SharedBy == nil {
			t.Fatalf("shares %+v", shares)
		}

		from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		rep, err := client.Spend(ctx, vibecards.SpendQuery{
			Filter:   vibecards.Filter{CardIDs: []string{"d"}},
			Range:    vibecards.TimeRange{From: from, To: from.AddDate(0, 1, 0)},
			TxStatus: []vibecards.TxStatus{vibecards.TxStatusCompleted},
		})
		if err != nil {
			t.Fatalf("Spend: %v", err)
		}
		if rep.Totals.Spent.Minor != 123456789012 || len(rep.Transactions) != 1 ||
			rep.Transactions[0].Status != vibecards.TxStatusCompleted || !rep.Transactions[0].At.Equal(from) ||
			rep.Transactions[0].ExpectedIssuerFee != nil || rep.Transactions[0].IssuerReference != "issuer-reference-fixture" || rep.TotalCount != 1 {
			t.Fatalf("report %+v", rep)
		}
	})
}

// sentinels is every reason the client names. The drift test below fails
// when the proto gains a reason this map (and so the client) lacks.
var sentinels = map[vibecards.Reason]error{
	vibecards.ReasonNotFound:            vibecards.ErrNotFound,
	vibecards.ReasonNotAllowed:          vibecards.ErrNotAllowed,
	vibecards.ReasonLimitReached:        vibecards.ErrLimitReached,
	vibecards.ReasonInsufficientBalance: vibecards.ErrInsufficientBalance,
	vibecards.ReasonIssuerUnsupported:   vibecards.ErrIssuerUnsupported,
	vibecards.ReasonIssuerFailed:        vibecards.ErrIssuerFailed,
	vibecards.ReasonCardNotActive:       vibecards.ErrCardNotActive,
	vibecards.ReasonHeldByOther:         vibecards.ErrHeldByOther,
	vibecards.ReasonNotSharedForUse:     vibecards.ErrNotSharedForUse,
	vibecards.ReasonCooldown:            vibecards.ErrCooldown,
	vibecards.ReasonGrantSpent:          vibecards.ErrGrantSpent,
	vibecards.ReasonCardUnreadable:      vibecards.ErrCardUnreadable,
	vibecards.ReasonInProgress:          vibecards.ErrInProgress,
	vibecards.ReasonNotInPlan:           vibecards.ErrNotInPlan,
	vibecards.ReasonTopUpDisabled:       vibecards.ErrTopUpDisabled,
	vibecards.ReasonLabelExists:         vibecards.ErrLabelExists,
	vibecards.ReasonRuleInvalid:         vibecards.ErrRuleInvalid,
	vibecards.ReasonNoHolderProfile:     vibecards.ErrNoHolderProfile,
	vibecards.ReasonProfileInUse:        vibecards.ErrProfileInUse,
}

// TestEveryProtoReasonHasAnError sends each cards.v2.ErrorReason through a
// real connect-go error detail and checks the client names it.
func TestEveryProtoReasonHasAnError(t *testing.T) {
	f, client := newServer(t, true)
	for n := range pb.ErrorReason_name {
		r := pb.ErrorReason(n)
		if r == pb.ErrorReason_ERROR_REASON_UNSPECIFIED {
			continue
		}
		t.Run(r.String(), func(t *testing.T) {
			want, ok := sentinels[vibecards.Reason(reasonWord(r))]
			if !ok {
				t.Fatalf("the client has no error for %s — add Reason and Err constants", r)
			}
			f.mu.Lock()
			f.reason = r
			f.mu.Unlock()
			_, err := client.Get(context.Background(), "d")
			if !errors.Is(err, want) || vibecards.ReasonOf(err) != vibecards.Reason(reasonWord(r)) {
				t.Fatalf("%s: got %v", r, err)
			}
			var e *vibecards.Error
			if !errors.As(err, &e) || e.Code != "failed_precondition" || e.Domain != "cards.v2" || e.MissingCapability() != "pause" {
				t.Fatalf("%s: %#v", r, err)
			}
		})
	}
	if len(sentinels) != len(pb.ErrorReason_name)-1 {
		t.Fatalf("client names %d reasons, proto has %d", len(sentinels), len(pb.ErrorReason_name)-1)
	}
}

// TestRequestsAreStrictProtoJSON re-encodes what the client sends through
// the generated types: a field the proto does not know fails the decode.
func TestRequestsAreStrictProtoJSON(t *testing.T) {
	_, client := newServer(t, true)
	_, err := client.Take(context.Background(), vibecards.TakeInput{CardID: "d", RequestID: "r-1",
		Holder: ptr(vibecards.Person("sub-1")), Usage: &vibecards.Usage{App: "x", ExternalID: "y"}})
	if err != nil {
		t.Fatalf("Take with every field: %v", err)
	}
	// The same server refuses an unknown field, so the check above is real.
	raw, _ := json.Marshal(map[string]any{"cardId": "d", "nonsense": 1})
	var m pb.TakeRequest
	if err := (jsonCodec{numeric: true}).Unmarshal(raw, &m); err == nil {
		t.Fatal("the strict codec accepted an unknown field")
	}
}

func ptr[T any](v T) *T { return &v }

func (f *fake) RevealNumber(_ context.Context, req *connect.Request[pb.RevealNumberRequest]) (*connect.Response[pb.RevealNumberResponse], error) {
	if req.Msg.GetCardId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("card_id"))
	}
	return connect.NewResponse(&pb.RevealNumberResponse{Number: "4242424242424242", ExpMonth: "01", ExpYear: "2030",
		Cvv: "123", Holder: &pb.HolderData{Name: "JOHN DOE"}, HiddenByIssuer: []string{"cvv"},
		VisibleUntil: timestamppb.New(time.Date(2026, 10, 22, 10, 0, 30, 0, time.UTC))}), nil
}

func (f *fake) ListChanges(_ context.Context, req *connect.Request[pb.ListChangesRequest]) (*connect.Response[pb.ListChangesResponse], error) {
	if req.Msg.GetFilter().GetUpdatedSince() == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("since"))
	}
	return connect.NewResponse(&pb.ListChangesResponse{
		Changes: []*pb.CardChange{{Id: "ch1", CardId: "d", Kind: "number.granted",
			Actor: &pb.Principal{Kind: pb.Principal_KIND_SERVICE, Id: "api-key:accounts"}, OnBehalfOf: person("sub-buyer"),
			Summary: "ticket", At: req.Msg.GetFilter().GetUpdatedSince()}},
		Page: &pb.PageResponse{NextPageToken: "n", TotalCount: 9}}), nil
}

func (f *fake) Bulk(_ context.Context, req *connect.Request[pb.BulkRequest]) (*connect.Response[pb.BulkResponse], error) {
	if req.Msg.GetAction() != pb.BulkAction_BULK_ACTION_PAUSE {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("bulk"))
	}
	// The whole organisation's "uk" cards: more than one call applies.
	if slices.Equal(req.Msg.GetFilter().GetLabels(), []string{"uk"}) {
		return connect.NewResponse(&pb.BulkResponse{OperationId: "op-1", Selected: 4400}), nil
	}
	if !slices.Equal(req.Msg.GetFilter().GetOwner(), []string{"me"}) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("bulk"))
	}
	return connect.NewResponse(&pb.BulkResponse{Succeeded: 3, Failed: 1, Selected: 4,
		Failures: []*pb.BulkFailure{{CardId: "b", Reason: "issuer_unsupported", Message: "cannot pause"}}}), nil
}

func (f *fake) GetBulkOperation(_ context.Context, req *connect.Request[pb.GetBulkOperationRequest]) (*connect.Response[pb.GetBulkOperationResponse], error) {
	if req.Msg.GetOperationId() != "op-1" {
		return nil, reasonErr(connect.CodeNotFound, pb.ErrorReason_ERROR_REASON_NOT_FOUND, nil)
	}
	return connect.NewResponse(&pb.GetBulkOperationResponse{Operation: &pb.BulkOperation{
		Id: "op-1", Action: pb.BulkAction_BULK_ACTION_PAUSE, State: pb.BulkOperationState_BULK_OPERATION_STATE_RUNNING,
		Total: 4400, Done: 1025, Succeeded: 1024, Failed: 1,
		Failures:  []*pb.BulkFailure{{CardId: "b", Reason: "not_allowed", Message: "not yours"}},
		StartedBy: person("sub-admin"), StartedAt: timestamppb.New(time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)),
	}}), nil
}

func (f *fake) TopUp(_ context.Context, req *connect.Request[pb.TopUpRequest]) (*connect.Response[pb.TopUpResponse], error) {
	m := req.Msg
	if m.GetPerson().GetId() != "sub-buyer" || m.GetAmount().GetMinor() != 5000 || m.GetRequestId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("top-up"))
	}
	return connect.NewResponse(&pb.TopUpResponse{Balance: &pb.Money{Minor: 12500, Currency: "USD"}}), nil
}

func (f *fake) ListShares(_ context.Context, req *connect.Request[pb.ListSharesRequest]) (*connect.Response[pb.ListSharesResponse], error) {
	return connect.NewResponse(&pb.ListSharesResponse{
		Shares: []*pb.Share{{Id: "s1", Target: &pb.Share_CardId{CardId: req.Msg.GetCardId()}, With: person(req.Msg.GetWithId()),
			Right: pb.Right_RIGHT_VIEW}},
		Page: &pb.PageResponse{TotalCount: 1}}), nil
}

// TestEveryMethodSpeaksTheProto: the responses above decode into the
// client's types, and every request the client can build — with every field
// set — passes the strict decoder (an unimplemented method still decodes its
// request first, so "unimplemented" means the request was well-formed).
func TestEveryMethodSpeaksTheProto(t *testing.T) {
	codecs(t, func(t *testing.T, numeric bool) {
		_, client := newServer(t, numeric)
		ctx := context.Background()
		since := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

		rn, err := client.RevealNumber(WithPerson(ctx), "d")
		if err != nil || rn.Number == "" || rn.HiddenByIssuer[0] != "cvv" || rn.VisibleUntil.Second() != 30 || rn.Holder.Name != "JOHN DOE" {
			t.Fatalf("RevealNumber %+v %v", rn, err)
		}
		ch, err := client.ListChanges(ctx, vibecards.ListChangesInput{Filter: vibecards.Filter{UpdatedSince: &since}})
		if err != nil || len(ch.Changes) != 1 || ch.Changes[0].OnBehalfOf.ID != "sub-buyer" || !ch.Changes[0].At.Equal(since) ||
			ch.Changes[0].Actor.Kind != vibecards.PrincipalService || ch.NextPageToken != "n" || ch.TotalCount != 9 {
			t.Fatalf("ListChanges %+v %v", ch, err)
		}
		br, err := client.Bulk(ctx, vibecards.BulkInput{Filter: vibecards.Filter{Owner: []string{vibecards.Me}},
			Action: vibecards.BulkPause, Reason: "freeze all mine"})
		if err != nil || br.Succeeded != 3 || br.Selected != 4 || br.Background() || br.Failures[0].Reason != vibecards.ReasonIssuerUnsupported {
			t.Fatalf("Bulk %+v %v", br, err)
		}
		bg, err := client.Bulk(ctx, vibecards.BulkInput{Filter: vibecards.Filter{Labels: []string{"uk"}}, Action: vibecards.BulkPause})
		if err != nil || !bg.Background() || bg.OperationID != "op-1" || bg.Selected != 4400 || bg.Succeeded != 0 {
			t.Fatalf("Bulk in the background %+v %v", bg, err)
		}
		op, err := client.GetBulkOperation(ctx, bg.OperationID)
		if err != nil || op.State != vibecards.BulkOperationRunning || op.State.Finished() || op.Total != 4400 || op.Done != 1025 ||
			op.Failures[0].Reason != vibecards.ReasonNotAllowed || op.StartedBy.ID != "sub-admin" || op.StartedAt.Hour() != 9 || op.FinishedAt != nil {
			t.Fatalf("GetBulkOperation %+v %v", op, err)
		}
		if _, err := client.GetBulkOperation(ctx, "op-2"); !errors.Is(err, vibecards.ErrNotFound) {
			t.Fatalf("an unknown operation: %v", err)
		}
		bal, err := client.TopUp(ctx, vibecards.TopUpInput{Person: vibecards.Person("sub-buyer"), IssuerAccountID: "acc",
			Amount: vibecards.USD(5000), RequestID: "r-1"})
		if err != nil || bal == nil || bal.Minor != 12500 || bal.Currency != "USD" {
			t.Fatalf("TopUp %+v %v", bal, err)
		}
		sl, err := client.ListShares(ctx, vibecards.ListSharesInput{CardID: "d", WithID: "sub-viewer", OwnerID: "sub-owner",
			Page: vibecards.Page{Size: 10, Token: "t"}})
		if err != nil || sl.Shares[0].CardID != "d" || sl.Shares[0].Right != vibecards.RightView || sl.TotalCount != 1 {
			t.Fatalf("ListShares %+v %v", sl, err)
		}

		usd := ptr(vibecards.USD(1000))
		full := vibecards.Filter{Labels: []string{"uk"}, Owner: []string{"me"}, Holder: []string{"none"},
			Status: []vibecards.CardStatus{vibecards.CardStatusActive}, SharedWith: []string{"sub"}, UpdatedSince: &since,
			Query: "42", CardIDs: []string{"d"}, IssuerAccountIDs: []string{"acc"},
			UsageApp: "vibe-fb", UsageExternalID: "act_1"}
		limits := &vibecards.Limits{Total: usd, PerTxn: usd}
		calls := map[string]func() error{
			"Issue": func() error {
				_, err := client.Issue(ctx, vibecards.IssueInput{RequestID: "r", Owner: ptr(vibecards.Person("sub")), Count: 2,
					Limits: limits, Name: "n", Labels: []string{"uk"}, Currency: "USD", BIN: "424242",
					IssuerAccountID: "acc", HolderProfileID: "hp", Take: true})
				return err
			},
			"Update": func() error {
				_, err := client.Update(ctx, vibecards.UpdateInput{ID: "d", Name: ptr("n"), Limits: limits,
					AddLabels: []string{"a"}, RemoveLabels: []string{"b"}, HolderProfileID: ptr("")})
				return err
			},
			"Pause":       func() error { _, err := client.Pause(ctx, "d", "r"); return err },
			"Resume":      func() error { _, err := client.Resume(ctx, "d"); return err },
			"Close":       func() error { _, err := client.Close(ctx, "d", "r"); return err },
			"Transfer":    func() error { _, err := client.Transfer(ctx, "d", vibecards.Person("sub-2"), "r"); return err },
			"RemoveUsage": func() error { _, err := client.RemoveUsage(ctx, "d", "app", "ext", "r"); return err },
			"Unshare":     func() error { _, err := client.Unshare(ctx, "s1", "s2"); return err },
			"List": func() error {
				_, err := client.List(ctx, vibecards.ListInput{Filter: full, Page: vibecards.Page{Size: 5, Token: "0"}, OrderBy: "-spent_today"})
				return err
			},
			"Spend": func() error {
				_, err := client.Spend(ctx, vibecards.SpendQuery{Filter: full, Range: vibecards.TimeRange{From: since, To: since.Add(time.Hour)},
					TxStatus: []vibecards.TxStatus{vibecards.TxStatusDeclined}, GroupBy: vibecards.GroupByLabel,
					Bucket: vibecards.BucketHour, ComparePrevious: true, Page: vibecards.Page{Size: 1}})
				return err
			},
		}
		for name, call := range calls {
			err := call()
			var e *vibecards.Error
			if err != nil && (!errors.As(err, &e) || e.Code == "invalid_argument" && strings.Contains(e.Message, "unmarshal")) {
				t.Errorf("%s: the request did not decode: %v", name, err)
			}
		}
	})
}

// WithPerson authenticates as a person (any non-vck_ bearer is one here).
func WithPerson(ctx context.Context) context.Context { return vibecards.WithToken(ctx, "person-jwt") }

// TestCardSaysWhyWhatReplacedItAndHowOld: what v1 told vibe-accounts and v2
// must too — the reason a card is stopped (an issuer's block is replaced, a
// person's pause is not), the successor of a reissued card and the card it
// came from, and when the issuer's numbers were last read. In both codecs
// an unset link or time decodes as nil, never as a zero value.
func TestCardSaysWhyWhatReplacedItAndHowOld(t *testing.T) {
	codecs(t, func(t *testing.T, numeric bool) {
		f, client := newServer(t, numeric)
		ctx := context.Background()
		linked := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
		synced := time.Date(2026, 10, 22, 9, 15, 0, 0, time.UTC)

		f.mu.Lock()
		b, _ := f.find("b")
		b.StatusReason = pb.CardStatusReason_CARD_STATUS_REASON_ISSUER_BLOCKED
		b.SyncedAt = timestamppb.New(synced)
		c, _ := f.find("c")
		c.Status = pb.CardStatus_CARD_STATUS_CLOSED
		c.StatusReason = pb.CardStatusReason_CARD_STATUS_REASON_REPLACED
		c.ReplacedBy = &pb.CardLink{CardId: "d", Last4: "42dd", At: timestamppb.New(linked)}
		d, _ := f.find("d")
		d.Replaces = &pb.CardLink{CardId: "c", Last4: "42cc", At: timestamppb.New(linked)}
		f.mu.Unlock()

		blocked, err := client.Get(ctx, "b")
		if err != nil || !blocked.IssuerBlocked() || blocked.StatusReason != vibecards.CardStatusReasonIssuerBlocked ||
			blocked.SyncedAt == nil || !blocked.SyncedAt.Equal(synced) || blocked.ReplacedBy != nil || blocked.Replaces != nil {
			t.Fatalf("issuer-blocked card %+v %v", blocked, err)
		}
		old, err := client.Get(ctx, "c")
		if err != nil || old.StatusReason != vibecards.CardStatusReasonReplaced || old.IssuerBlocked() ||
			old.ReplacedBy == nil || old.ReplacedBy.CardID != "d" || old.ReplacedBy.Last4 != "42dd" ||
			old.ReplacedBy.At == nil || !old.ReplacedBy.At.Equal(linked) {
			t.Fatalf("replaced card %+v %v", old, err)
		}
		succ, err := client.Get(ctx, "d")
		if err != nil || succ.Replaces == nil || succ.Replaces.CardID != "c" || succ.StatusReason != vibecards.CardStatusReasonUnspecified ||
			succ.SyncedAt != nil {
			t.Fatalf("successor %+v %v", succ, err)
		}
		for n, name := range pb.CardStatusReason_name {
			if got := vibecards.CardStatusReason(n).String(); got != name {
				t.Errorf("CardStatusReason %d: the client names it %q, the proto %q", n, got, name)
			}
		}
	})
}

func (f *fake) ListAccounts(context.Context, *connect.Request[pb.ListAccountsRequest]) (*connect.Response[pb.ListAccountsResponse], error) {
	return connect.NewResponse(&pb.ListAccountsResponse{Accounts: []*pb.IssuerAccount{{Id: "issuer-fixture", Owner: pb.IssuerAccount_OWNER_ORG, Kind: "brocard", Name: "Own account", Capabilities: []string{"issue"}, Balance: &pb.Money{Minor: 321, Currency: "USD"}, CardCount: 3, IsDefault: true}}}), nil
}
func TestListAccountsWire(t *testing.T) {
	codecs(t, func(t *testing.T, numeric bool) {
		_, client := newServer(t, numeric)
		accounts, err := client.ListAccounts(context.Background())
		if err != nil || len(accounts) != 1 {
			t.Fatalf("accounts %+v, error %v", accounts, err)
		}
		a := accounts[0]
		if a.ID != "issuer-fixture" || a.Owner != vibecards.IssuerOwnerOrg || a.Kind != "brocard" || a.Name != "Own account" || !a.IsDefault || a.CardCount != 3 || a.Balance == nil || a.Balance.Minor != 321 || a.RunsOutAt != nil || !slices.Equal(a.Capabilities, []string{"issue"}) {
			t.Fatalf("account %+v", a)
		}
	})
}
