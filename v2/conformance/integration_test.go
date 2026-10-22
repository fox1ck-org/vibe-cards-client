package conformance

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	cards "github.com/fox1ck-org/vibe-cards-client/v2"
	pb "github.com/fox1ck-org/vibe-cards-client/v2/conformance/gen/cards/v2"
	"github.com/fox1ck-org/vibe-cards-client/v2/conformance/gen/cards/v2/cardsv2connect"
)

type integrationFixture struct {
	cardsv2connect.UnimplementedIntegrationServiceHandler
}

func (integrationFixture) BeginRead(context.Context, *connect.Request[pb.BeginReadRequest]) (*connect.Response[pb.BeginReadResponse], error) {
	return connect.NewResponse(&pb.BeginReadResponse{Token: "opaque-guard", ExpiresAt: timestamppb.New(time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC))}), nil
}
func (integrationFixture) ValidateRead(_ context.Context, r *connect.Request[pb.ValidateReadRequest]) (*connect.Response[pb.ValidateReadResponse], error) {
	switch r.Msg.Token {
	case "changed":
		return nil, reasonErr(connect.CodeAborted, pb.ErrorReason_ERROR_REASON_READ_CHANGED, nil)
	case "expired":
		return nil, reasonErr(connect.CodeAborted, pb.ErrorReason_ERROR_REASON_READ_EXPIRED, nil)
	}
	return connect.NewResponse(&pb.ValidateReadResponse{}), nil
}
func (integrationFixture) ListChanges(_ context.Context, r *connect.Request[pb.ListIntegrationChangesRequest]) (*connect.Response[pb.ListIntegrationChangesResponse], error) {
	if r.Msg.Since == "revoked" {
		return nil, reasonErr(connect.CodeFailedPrecondition, pb.ErrorReason_ERROR_REASON_CURSOR_INVALIDATED, nil)
	}
	if r.Msg.Limit != 17 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("limit lost"))
	}
	return connect.NewResponse(&pb.ListIntegrationChangesResponse{NextCursor: "opaque-next", Events: []*pb.ProductEvent{{Id: "stable-event", Type: "cards.transaction.v1", Version: 1, OrgId: "org", Source: "cards", At: timestamppb.New(time.Date(2026, 10, 22, 11, 0, 0, 0, time.UTC)), Data: &pb.ProductChange{ResourceId: "transaction", CardId: "card", Operation: "updated"}}}}), nil
}
func TestIntegrationContract(t *testing.T) {
	for _, numeric := range []bool{false, true} {
		t.Run(map[bool]string{false: "named", true: "numeric"}[numeric], func(t *testing.T) {
			mux := http.NewServeMux()
			mux.Handle(cardsv2connect.NewIntegrationServiceHandler(integrationFixture{}, connect.WithCodec(jsonCodec{numeric: numeric})))
			srv := httptest.NewServer(mux)
			defer srv.Close()
			client := cards.New(srv.URL, "fixture-key")
			ctx := context.Background()
			g, err := client.BeginRead(ctx)
			if err != nil || g.Token != "opaque-guard" || g.ExpiresAt.Hour() != 12 {
				t.Fatalf("begin: %#v %v", g, err)
			}
			if err = client.ValidateRead(ctx, g.Token); err != nil {
				t.Fatal(err)
			}
			if err = client.ValidateRead(ctx, "changed"); !errors.Is(err, cards.ErrReadChanged) {
				t.Fatalf("changed: %v", err)
			}
			if err = client.ValidateRead(ctx, "expired"); !errors.Is(err, cards.ErrReadExpired) {
				t.Fatalf("expired: %v", err)
			}
			page, err := client.ListProductChanges(ctx, "", 17)
			if err != nil {
				t.Fatal(err)
			}
			if page.NextCursor != "opaque-next" || len(page.Events) != 1 || page.Events[0].Data.ResourceID != "transaction" || page.Events[0].Data.CardID != "card" || page.Events[0].Version != 1 || page.Events[0].At.Hour() != 11 {
				t.Fatalf("changes: %#v", page)
			}
			if _, err = client.ListProductChanges(ctx, "revoked", 17); !errors.Is(err, cards.ErrCursorInvalidated) {
				t.Fatalf("revoked: %v", err)
			}
		})
	}
}
