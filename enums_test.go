package vibecards

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// vibe-cards writes enums as numbers. Before these types existed, every read
// that carried a card failed to decode — DrawCard answered "cannot unmarshal
// number into Go struct field Holding.holding.cardStatus of type string" — and
// the account console could not order a card at all.
func TestEnumsDecodeNumbersNamesAndWords(t *testing.T) {
	var s CardStatus
	for in, want := range map[string]CardStatus{
		`2`: CardStatusActive, `0`: CardStatusUnspecified, `5`: CardStatusClosed,
		`"CARD_STATUS_PAUSED"`: CardStatusPaused, `"blocked"`: CardStatusBlocked,
		`"CARD_STATUS_UNSPECIFIED"`: CardStatusUnspecified, `null`: CardStatusUnspecified,
		`9`: CardStatus("9"),
	} {
		if err := json.Unmarshal([]byte(in), &s); err != nil || s != want {
			t.Fatalf("card status %s: got %q (%v), want %q", in, s, err, want)
		}
	}
	var c Currency
	for in, want := range map[string]Currency{`1`: CurrencyUSD, `"CURRENCY_EUR"`: CurrencyEUR, `"usd"`: CurrencyUSD, `0`: ""} {
		if err := json.Unmarshal([]byte(in), &c); err != nil || c != want {
			t.Fatalf("currency %s: got %q (%v), want %q", in, c, err, want)
		}
	}
	var o DrawOutcome
	for in, want := range map[string]DrawOutcome{`3`: OutcomeNoStock, `"DRAW_OUTCOME_DRAWN"`: OutcomeDrawn, `2`: OutcomeReused} {
		if err := json.Unmarshal([]byte(in), &o); err != nil || o != want {
			t.Fatalf("outcome %s: got %q (%v), want %q", in, o, err, want)
		}
	}
	if err := json.Unmarshal([]byte(`true`), &s); err == nil {
		t.Fatal("a boolean is not an enum and must be refused")
	}
}

func TestDrawCardDecodesTheServersNumericEnums(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"outcome":1,
			"holding":{"id":"h1","cardId":"c1","limitCents":"5000","cardStatus":2,"claims":[]},
			"assignment":{"id":"a1","cardId":"c1","status":"reserved","cardStatus":2,"cardCurrency":1}}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "vck_test").DrawCard(context.Background(), DrawInput{HolderUserSub: "sub-1"})
	if err != nil {
		t.Fatalf("numeric enums must decode: %v", err)
	}
	if got.NoStock() || got.Reused() || got.Outcome != OutcomeDrawn {
		t.Fatalf("outcome not decoded: %q", got.Outcome)
	}
	if got.Holding == nil || !got.Holding.CardStatus.Usable() {
		t.Fatalf("holding status not decoded: %+v", got.Holding)
	}
	if got.Assignment == nil || got.Assignment.CardStatus != CardStatusActive || got.Assignment.CardCurrency != CurrencyUSD {
		t.Fatalf("assignment enums not decoded: %+v", got.Assignment)
	}
}
