package vibecards

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNilClientAnswersOnTheDrawSurfaceToo(t *testing.T) {
	var c *Client
	if _, err := c.DrawCard(context.Background(), DrawInput{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("nil client should answer ErrNotConfigured, got %v", err)
	}
	if _, err := c.ReleaseHolding(context.Background(), "id", "reason"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("nil client should answer ErrNotConfigured, got %v", err)
	}
	if _, err := c.ListHoldings(context.Background(), HoldingListInput{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("nil client should answer ErrNotConfigured, got %v", err)
	}
}

// An empty pool is an ANSWER, not an error. A consumer that treated it as a
// failure would climb a retry ladder against a supply problem, and — worse —
// the flows that used to issue a card on demand would look broken rather than
// waiting.
func TestNoStockIsAnAnswerNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"outcome":"DRAW_OUTCOME_NO_STOCK"}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "vck_test").DrawCard(context.Background(), DrawInput{HolderUserSub: "sub-1"})
	if err != nil {
		t.Fatalf("an empty pool must not be an error: %v", err)
	}
	if !got.NoStock() {
		t.Fatalf("want NoStock, got outcome %q", got.Outcome)
	}
	if got.Holding != nil {
		t.Fatal("no stock means no holding came back")
	}
}

// protojson renders int64 as a STRING, so a limit read as a plain number would
// be silently zero — and a zero limit is exactly the value that means "this
// card may spend the whole treasury" once the estate is pooled.
func TestLimitCentsSurvivesProtojsonStringEncoding(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"outcome":"DRAW_OUTCOME_DRAWN","holding":{"id":"h1","limitCents":"50000","cardLastFour":"4417"}}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "vck_test").DrawCard(context.Background(), DrawInput{
		HolderUserSub: "sub-1", LimitCents: 25000,
	})
	if err != nil {
		t.Fatalf("draw: %v", err)
	}
	if got.Holding == nil || got.Holding.LimitCents != 50000 {
		t.Fatalf("want 50000 cents back, got %+v", got.Holding)
	}
	// And the request has to speak the same dialect on the way out.
	if gotBody["limitCents"] != "25000" {
		t.Fatalf("limitCents must go out as a string, got %#v", gotBody["limitCents"])
	}
}

func TestReleaseHoldingSendsIdAndReason(t *testing.T) {
	var gotPath string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		// releasedAt is what makes it released; the server always stamps it.
		_, _ = w.Write([]byte(`{"id":"h1","releasedAt":"2026-09-01T14:00:00Z","releaseReason":"account died"}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "vck_test").ReleaseHolding(context.Background(), "h1", "account died")
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if gotPath != "/cards.v1.AssignmentService/ReleaseHolding" {
		t.Fatalf("wrong path: %s", gotPath)
	}
	if gotBody["id"] != "h1" || gotBody["reason"] != "account died" {
		t.Fatalf("wrong body: %#v", gotBody)
	}
	if got.Live() {
		t.Fatal("a released holding must not read as live")
	}
}

// The consumer's door, and the reason it exists: releasing the HOLDING revokes
// every claim under it, including the one another of that person's accounts is
// spending through. A subject that is finished releases its own claim.
func TestReleaseClaimSendsIdAndReason(t *testing.T) {
	var gotPath string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"id":"a1","status":"revoked","revokeReason":"account retired"}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "vck_test").ReleaseClaim(context.Background(), "a1", "account retired")
	if err != nil {
		t.Fatalf("release the claim: %v", err)
	}
	if gotPath != "/cards.v1.AssignmentService/ReleaseClaim" {
		t.Fatalf("wrong path: %s", gotPath)
	}
	if gotBody["id"] != "a1" || gotBody["reason"] != "account retired" {
		t.Fatalf("wrong body: %#v", gotBody)
	}
	if got.Live() {
		t.Fatal("a revoked claim must not read as live")
	}
}

func TestNilClientAnswersOnReleaseClaim(t *testing.T) {
	var c *Client
	if _, err := c.ReleaseClaim(context.Background(), "a1", "reason"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
}
