package vibecards

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNilClientAndMissingCredential(t *testing.T) {
	var c *Client
	if _, err := c.Get(context.Background(), "x"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("nil client: %v", err)
	}
	if _, err := c.TakeFirst(context.Background(), TakeFirstInput{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("nil client TakeFirst: %v", err)
	}
	if New("", "vck_x") != nil {
		t.Fatal("New without a URL must be nil")
	}
	keyless := New("http://127.0.0.1:1", "")
	if _, err := keyless.Get(context.Background(), "x"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("no credential: %v", err)
	}
}

// The wire as vibe-cards writes it: numeric enums (its JSON codec), int64 as
// strings, every field emitted, unset messages as null.
const cardJSON = `{
  "id": "c1", "orgId": "", "last4": "4242", "name": "uk-1", "bin": "424242",
  "network": "visa", "currency": "USD", "status": 2, "statusNote": "",
  "owner": {"kind": 1, "id": "sub-owner", "label": "Owner"},
  "holder": {"holder": {"kind": "KIND_PERSON", "id": "sub-1", "label": ""},
             "takenBy": {"kind": 2, "id": "api-key:svc", "label": "svc"},
             "from": "2026-10-22T10:00:00Z", "to": null},
  "labels": ["uk"], "limits": {"total": {"minor": "50000", "currency": "USD"}, "perTxn": null},
  "spentTotal": {"minor": "1250", "currency": "USD"}, "spentToday": {"minor": 250, "currency": "USD"},
  "usages": [{"app": "vibe-accounts", "externalId": "acc-1", "url": "", "label": "",
              "addedBy": {"kind": 2, "id": "api-key:svc", "label": ""}, "addedAt": "2026-10-22T10:00:01Z",
              "onBehalfOf": {"kind": 1, "id": "sub-1", "label": ""}}],
  "createdAt": "2026-10-01T00:00:00Z", "updatedAt": "2026-10-22T10:00:01Z",
  "capabilities": ["pause", "three_ds"], "lastHolding": null, "unknownFutureField": 7,
  "status2": "CARD_STATUS_WHATEVER"
}`

func TestCardDecodesTheServersWire(t *testing.T) {
	var c Card
	if err := json.Unmarshal([]byte(cardJSON), &c); err != nil {
		t.Fatal(err)
	}
	if c.Status != CardStatusActive || c.Owner.Kind != PrincipalPerson || !c.HeldBy("sub-1") ||
		c.Holder.TakenBy.Kind != PrincipalService || c.Holder.To != nil {
		t.Fatalf("card: %+v", c)
	}
	if c.Limits.Total == nil || c.Limits.Total.Minor != 50000 || c.Limits.PerTxn != nil {
		t.Fatalf("limits: %+v", c.Limits)
	}
	if c.SpentTotal.Minor != 1250 || c.SpentToday.Minor != 250 || c.DailyLimit != nil {
		t.Fatalf("money: %+v %+v", c.SpentTotal, c.SpentToday)
	}
	u := c.UsageOf("vibe-accounts", "acc-1")
	if u == nil || u.OnBehalfOf == nil || u.OnBehalfOf.ID != "sub-1" || !c.Can("three_ds") || c.Can("number") {
		t.Fatalf("usage/caps: %+v", c)
	}
}

func TestEnumsAndMoney(t *testing.T) {
	var s CardStatus
	for in, want := range map[string]CardStatus{
		`4`: CardStatusQuarantine, `"CARD_STATUS_CLOSED"`: CardStatusClosed,
		`"CARD_STATUS_NEW_ONE"`: CardStatusUnspecified, `null`: CardStatusUnspecified, `9`: CardStatus(9),
	} {
		if err := json.Unmarshal([]byte(in), &s); err != nil || s != want {
			t.Fatalf("%s → %v, %v", in, s, err)
		}
	}
	if CardStatusPaused.String() != "CARD_STATUS_PAUSED" || CardStatus(9).String() != "9" {
		t.Fatal("String")
	}
	raw, _ := json.Marshal(USD(-1505))
	if string(raw) != `{"minor":"-1505","currency":"USD"}` || USD(-1505).String() != "-15.05 USD" {
		t.Fatalf("money: %s %s", raw, USD(-1505))
	}
	var m Money
	if err := json.Unmarshal([]byte(`{"minor":"x","currency":"USD"}`), &m); err == nil {
		t.Fatal("a non-number minor must not decode")
	}
}

// recorder is a tiny Connect JSON server: one handler per procedure.
type recorder struct {
	mu       sync.Mutex
	headers  []http.Header
	bodies   map[string][]string
	handlers map[string]func(body string) (int, string)
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	b, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	r.headers = append(r.headers, req.Header.Clone())
	if r.bodies == nil {
		r.bodies = map[string][]string{}
	}
	r.bodies[req.URL.Path] = append(r.bodies[req.URL.Path], string(b))
	h := r.handlers[req.URL.Path]
	r.mu.Unlock()
	if h == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	status, out := h(string(b))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, out)
}

func reasonBody(reason, code string) string {
	b := errorInfoBytes(reason, "cards.v2", nil)
	raw, _ := json.Marshal(map[string]any{"code": code, "message": reason,
		"details": []map[string]string{{"type": "google.rpc.ErrorInfo", "value": base64Std(b)}}})
	return string(raw)
}

func TestHeadersCarryTokenAndActor(t *testing.T) {
	rec := &recorder{handlers: map[string]func(string) (int, string){
		"/cards.v2.CardService/Get": func(string) (int, string) { return 200, `{"card":{"id":"c1"}}` },
	}}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	c := New(srv.URL+"/", "vck_service")

	ctx := OnBehalfOf(context.Background(), "sub-1", "person@example.test")
	if _, err := c.Get(ctx, "c1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(WithToken(context.Background(), "jwt-of-person"), "c1"); err != nil {
		t.Fatal(err)
	}
	h := rec.headers
	if h[0].Get("Authorization") != "Bearer vck_service" || h[0].Get(HeaderActorSub) != "sub-1" ||
		h[0].Get(HeaderActorEmail) != "person@example.test" || h[0].Get("Content-Type") != "application/json" {
		t.Fatalf("first: %v", h[0])
	}
	if h[1].Get("Authorization") != "Bearer jwt-of-person" || h[1].Get(HeaderActorSub) != "" {
		t.Fatalf("second: %v", h[1])
	}
}

func TestTakeFirstSkipsCardsSomebodyElseGot(t *testing.T) {
	var takes []string
	rec := &recorder{}
	rec.handlers = map[string]func(string) (int, string){
		"/cards.v2.CardService/List": func(body string) (int, string) {
			var in struct {
				Filter Filter `json:"filter"`
				Page   Page   `json:"page"`
			}
			_ = json.Unmarshal([]byte(body), &in)
			if len(in.Filter.Holder) != 1 || in.Filter.Holder[0] != Nobody ||
				len(in.Filter.Status) != 1 || in.Filter.Status[0] != CardStatusActive ||
				len(in.Filter.Labels) != 1 || in.Filter.Labels[0] != "uk" {
				return 400, `{"code":"invalid_argument","message":"bad filter"}`
			}
			if in.Page.Token == "" {
				return 200, `{"cards":[{"id":"a"},{"id":"b"}],"page":{"nextPageToken":"p2"}}`
			}
			return 200, `{"cards":[{"id":"c"}],"page":{"nextPageToken":""}}`
		},
		"/cards.v2.HoldingService/Take": func(body string) (int, string) {
			var in TakeInput
			_ = json.Unmarshal([]byte(body), &in)
			takes = append(takes, in.CardID)
			switch in.CardID {
			case "a":
				return 400, reasonBody("held_by_other", "failed_precondition")
			case "b":
				return 400, reasonBody("card_not_active", "failed_precondition")
			}
			return 200, `{"card":{"id":"` + in.CardID + `","holder":{"holder":{"kind":1,"id":"sub-1"}}}}`
		},
	}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	c := New(srv.URL, "vck_service")

	card, err := c.TakeFirst(context.Background(), TakeFirstInput{
		Filter: Filter{Labels: []string{"uk"}, Holder: []string{"ignored"}},
		Usage:  &Usage{App: "vibe-accounts", ExternalID: "acc-1"},
	})
	if err != nil || card.ID != "c" || !card.HeldBy("sub-1") || strings.Join(takes, ",") != "a,b,c" {
		t.Fatalf("card %+v, err %v, takes %v", card, err, takes)
	}

	takes = nil
	_, err = c.TakeFirst(context.Background(), TakeFirstInput{Filter: Filter{Labels: []string{"uk"}}, MaxAttempts: 2})
	if !errors.Is(err, ErrNoCardAvailable) || len(takes) != 2 {
		t.Fatalf("budget: %v %v", err, takes)
	}

	// A refusal that is not about the card stops the search.
	rec.handlers["/cards.v2.HoldingService/Take"] = func(string) (int, string) {
		return 403, reasonBody("not_allowed", "permission_denied")
	}
	if _, err := c.TakeFirst(context.Background(), TakeFirstInput{Filter: Filter{Labels: []string{"uk"}}}); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("stop: %v", err)
	}
}

func TestWaitThreeDSNothingArrivedIsNotAnError(t *testing.T) {
	rec := &recorder{handlers: map[string]func(string) (int, string){
		"/cards.v2.PanService/WaitThreeDS": func(string) (int, string) {
			return 200, `{"code":"","deliveredAt":null,"merchant":""}`
		},
	}}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	since := time.Date(2026, 10, 22, 10, 0, 0, 0, time.FixedZone("Kyiv", 3*3600))
	code, err := New(srv.URL, "k").WaitThreeDS(context.Background(), "c1", since, 30*time.Second)
	if err != nil || code != nil {
		t.Fatalf("%v %v", code, err)
	}
	body := rec.bodies["/cards.v2.PanService/WaitThreeDS"][0]
	if body != `{"cardId":"c1","since":"2026-10-22T07:00:00Z","waitSeconds":30}` {
		t.Fatalf("body %s", body)
	}
}

func TestCanonicalOnBehalfHeaders(t *testing.T) {
	rec := &recorder{handlers: map[string]func(string) (int, string){"/cards.v2.CardService/Get": func(string) (int, string) { return 200, `{"card":{"id":"c1"}}` }}}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	c := New(srv.URL, "")
	ctx := WithToken(OnBehalfOf(context.Background(), "old-sub", "old@example.test"), "sa-token")
	ctx = WithOnBehalf(ctx, "org", "canonical-sub")
	if _, err := c.Get(ctx, "c1"); err != nil {
		t.Fatal(err)
	}
	h := rec.headers[0]
	if h.Get("Authorization") != "Bearer sa-token" || h.Get(HeaderOnBehalfOrg) != "org" || h.Get(HeaderOnBehalfSub) != "canonical-sub" || h.Get(HeaderActorSub) != "" || h.Get(HeaderActorEmail) != "" {
		t.Fatalf("canonical headers: %v", h)
	}
	if _, err := c.Get(WithOnBehalf(ctx, "org", ""), "c1"); err != nil {
		t.Fatal(err)
	}
	if rec.headers[1].Get(HeaderOnBehalfSub) != "" || rec.headers[1].Get(HeaderOnBehalfOrg) != "org" {
		t.Fatal("org-only service work changed")
	}
	if _, err := c.Get(OnBehalfOf(ctx, "legacy", ""), "c1"); err != nil {
		t.Fatal(err)
	}
	if rec.headers[2].Get(HeaderOnBehalfOrg) != "" || rec.headers[2].Get(HeaderActorSub) != "legacy" {
		t.Fatal("mixed protocols")
	}
	if _, err := c.Get(WithOnBehalf(ctx, "", ""), "c1"); err == nil {
		t.Fatal("empty org accepted")
	}
	if len(rec.headers) != 3 {
		t.Fatal("invalid delegation reached server")
	}
}
