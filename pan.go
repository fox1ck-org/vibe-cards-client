package vibecards

import (
	"context"
	"time"
)

// CardDetails is the instrument and the address it is registered to —
// everything a payment form asks for, and nothing more.
//
// It is returned to the caller and kept nowhere. Treat it the way vibe-fb's own
// domain.CardDetails is treated: alive for the length of one outbound request,
// never persisted, never logged. The field names deliberately mirror that
// struct so the mapping is field for field with nothing in between.
type CardDetails struct {
	CardNumber      string `json:"cardNumber"`
	ExpirationMonth string `json:"expirationMonth"`
	ExpirationYear  string `json:"expirationYear"`
	CVV             string `json:"cvv"`
	BillingName     string `json:"billingName"`
	BillingAddress  string `json:"billingAddress"`
	BillingCity     string `json:"billingCity"`
	BillingState    string `json:"billingState"`
	BillingZip      string `json:"billingZip"`
	BillingCountry  string `json:"billingCountry"`
}

// Grant is a one-shot ticket for a card's details.
type Grant struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// IssueGrant mints a ticket for one assignment's card.
//
// Requires an API key carrying the `pan:redeem` scope; vibe-cards refuses a JWT
// caller outright. Mint it INSIDE the step that redeems it — its two-minute
// life is the whole safety margin, and a grant minted before a wait spends that
// margin doing nothing.
func (c *Client) IssueGrant(ctx context.Context, assignmentID string) (*Grant, error) {
	var out Grant
	if err := c.call(ctx, "/cards.v1.AssignmentService/IssuePANGrant",
		map[string]string{"assignmentId": assignmentID}, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

// RedeemGrant exchanges a ticket for the card's details, exactly once.
//
// A second attempt with the same token returns ErrGrantExpired, which is also
// what an unknown or expired token returns — vibe-cards does not distinguish
// them, so a prober learns nothing from the answer.
func (c *Client) RedeemGrant(ctx context.Context, token string) (*CardDetails, error) {
	var out CardDetails
	if err := c.call(ctx, "/cards.v1.AssignmentService/RedeemPANGrant",
		map[string]string{"token": token}, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

// CardDetailsFor is IssueGrant followed immediately by RedeemGrant.
//
// This is the shape callers should reach for: the two calls exist separately so
// the ticket can be audited, not so a caller can hold one. Keeping them
// adjacent is what makes the short TTL a safety margin rather than an obstacle.
func (c *Client) CardDetailsFor(ctx context.Context, assignmentID string) (*CardDetails, error) {
	g, err := c.IssueGrant(ctx, assignmentID)
	if err != nil {
		return nil, err
	}
	return c.RedeemGrant(ctx, g.Token)
}

// ThreeDSCode is a confirmation code delivered by Brocard's webhook.
type ThreeDSCode struct {
	Code        string    `json:"code"`
	DeliveredAt time.Time `json:"deliveredAt"`
}

// WaitForThreeDS blocks until a confirmation code arrives for the assignment's
// card, or until the wait runs out.
//
// `since` is load-bearing: it discards codes delivered before this attempt
// began, so a code from an earlier try cannot confirm this one. Pass the moment
// immediately BEFORE triggering the authorization.
//
// A nil result with a nil error means nothing arrived — an answer, not a
// failure. For a BIN whose 3DS mode is `auto` no code is ever delivered,
// because the issuer confirms on its own, and that is the normal case.
func (c *Client) WaitForThreeDS(ctx context.Context, assignmentID string, since time.Time, wait time.Duration) (*ThreeDSCode, error) {
	in := map[string]any{
		"assignmentId": assignmentID,
		"since":        since.UTC().Format(time.RFC3339Nano),
		"waitSeconds":  int(wait.Seconds()),
	}
	var out ThreeDSCode
	if err := c.call(ctx, "/cards.v1.AssignmentService/WaitForThreeDSCode", in, &out, true); err != nil {
		return nil, err
	}
	if out.Code == "" {
		return nil, nil
	}
	return &out, nil
}
