package vibecards

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A nil client is the "vibe-cards is not configured here" state. It must answer,
// not panic: a consumer without the integration should degrade, not crash.
func TestUnconfiguredClientIsUsableAndSaysSo(t *testing.T) {
	if c := New("", "key"); c != nil {
		t.Fatal("an empty base URL must yield a nil client")
	}
	if c := New("http://x", ""); c != nil {
		t.Fatal("an empty API key must yield a nil client")
	}

	var c *Client
	if _, err := c.AssignCard(context.Background(), AssignInput{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("nil client should answer ErrNotConfigured, got %v", err)
	}
	if _, err := c.CardDetailsFor(context.Background(), "id"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("nil client should answer ErrNotConfigured, got %v", err)
	}
}

func TestAssignCardSpeaksConnectUnaryJSON(t *testing.T) {
	var gotPath, gotAuth, gotType string
	var gotBody AssignInput

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotType = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(Assignment{ID: "a1", Status: StatusReserved, CardLastFour: "4417"})
	}))
	defer srv.Close()

	c := New(srv.URL, "vck_test")
	got, err := c.AssignCard(context.Background(), AssignInput{
		CardID: "c1", SubjectApp: SubjectAppAccounts, SubjectType: SubjectTypeAccount, SubjectID: "acc-9",
	})
	if err != nil {
		t.Fatalf("assign: %v", err)
	}

	if gotPath != "/cards.v1.AssignmentService/AssignCard" {
		t.Fatalf("Connect unary posts to /<package>.<Service>/<Method>, got %q", gotPath)
	}
	if gotAuth != "Bearer vck_test" {
		t.Fatalf("vck_ keys ride on Authorization, got %q", gotAuth)
	}
	if gotType != "application/json" {
		t.Fatalf("content type should be application/json, got %q", gotType)
	}
	if gotBody.SubjectID != "acc-9" || gotBody.SubjectApp != SubjectAppAccounts {
		t.Fatalf("request body did not round-trip: %+v", gotBody)
	}
	if !got.Live() || got.CardLastFour != "4417" {
		t.Fatalf("unexpected assignment: %+v", got)
	}
}

// The card's last four comes back; nothing that can be spent does. If this ever
// starts failing because Assignment gained a number field, that is the bug.
func TestAssignmentCarriesNoSpendableCardData(t *testing.T) {
	b, err := json.Marshal(Assignment{CardLastFour: "4417"})
	if err != nil {
		t.Fatal(err)
	}
	var asMap map[string]any
	if err := json.Unmarshal(b, &asMap); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"cardNumber", "cvv", "expirationMonth", "expirationYear"} {
		if _, present := asMap[forbidden]; present {
			t.Errorf("Assignment must not carry %q — it exists to name a card, not to reveal one", forbidden)
		}
	}
}

func TestCardDetailsForMintsAndRedeemsInOneGo(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/cards.v1.AssignmentService/IssuePANGrant":
			_ = json.NewEncoder(w).Encode(Grant{Token: "vckg_abc", ExpiresAt: time.Now().Add(2 * time.Minute)})
		case "/cards.v1.AssignmentService/RedeemPANGrant":
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in["token"] != "vckg_abc" {
				t.Errorf("redeem should present the minted token, got %q", in["token"])
			}
			_ = json.NewEncoder(w).Encode(CardDetails{CardNumber: "4111111111111111", BillingCity: "Kyiv"})
		default:
			t.Errorf("unexpected call %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	d, err := New(srv.URL, "vck_test").CardDetailsFor(context.Background(), "a1")
	if err != nil {
		t.Fatalf("card details: %v", err)
	}
	if d.CardNumber == "" || d.BillingCity != "Kyiv" {
		t.Fatalf("details did not round-trip: %+v", d)
	}
	if len(calls) != 2 || calls[0] != "/cards.v1.AssignmentService/IssuePANGrant" {
		t.Fatalf("the ticket must be minted immediately before it is redeemed, got %v", calls)
	}
}

func TestNamedErrorsAreDistinguishable(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{"missing billing profile", 400,
			`{"code":"failed_precondition","message":"card has no billing profile and no default is set"}`,
			ErrNoBillingProfile},
		{"spent ticket", 404,
			`{"code":"not_found","message":"pan grant is expired or already used"}`,
			ErrGrantExpired},
		{"closed card", 400,
			`{"code":"failed_precondition","message":"card is not bindable: card is closed"}`,
			ErrNotBindable},
		{"key without the scope", 403,
			`{"code":"permission_denied","message":"forbidden: key lacks the pan:redeem scope"}`,
			ErrForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			_, err := New(srv.URL, "k").IssueGrant(context.Background(), "a1")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}

// No code delivered is an answer, not a failure: a BIN whose 3DS mode is `auto`
// never delivers one because the issuer confirms on its own.
func TestNoThreeDSCodeIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	code, err := New(srv.URL, "k").WaitForThreeDS(context.Background(), "a1", time.Now(), 5*time.Second)
	if err != nil {
		t.Fatalf("an empty wait must not be an error: %v", err)
	}
	if code != nil {
		t.Fatalf("expected no code, got %+v", code)
	}
}

func TestWaitForThreeDSPassesSinceSoStaleCodesCannotConfirm(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"code":"123456"}`))
	}))
	defer srv.Close()

	since := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	code, err := New(srv.URL, "k").WaitForThreeDS(context.Background(), "a1", since, 30*time.Second)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code == nil || code.Code != "123456" {
		t.Fatalf("expected the delivered code, got %+v", code)
	}
	if got["since"] != since.Format(time.RFC3339Nano) {
		t.Fatalf("since must reach the server — it is what stops an earlier attempt's code "+
			"confirming this one; got %v", got["since"])
	}
	if got["waitSeconds"].(float64) != 30 {
		t.Fatalf("waitSeconds should be forwarded, got %v", got["waitSeconds"])
	}
}
