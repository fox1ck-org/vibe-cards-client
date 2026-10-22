package vibecards

import (
	"context"
	"time"
)

const panService = "/cards.v2.PanService/"

// Grant is a single-use, short-lived ticket for one card's details.
type Grant struct {
	// Shown exactly once; vibe-cards stores only its hash.
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// CardDetails is everything a payment form asks for, and nothing more.
//
// It is returned to the caller and kept nowhere: alive for the length of
// one outbound request, never persisted, never logged.
type CardDetails struct {
	Number   string     `json:"number"`
	ExpMonth string     `json:"expMonth"`
	ExpYear  string     `json:"expYear"`
	CVV      string     `json:"cvv"`
	Holder   HolderData `json:"holder"`
}

// IssueGrant mints a ticket for one card. SERVICES ONLY: an API key with
// the PAN scope; a person's token is refused (ErrNotAllowed). The card must
// be ACTIVE and held by the service or by the person it acts for
// (OnBehalfOf) — or theirs, or shared with them for USE. A card the issuer
// last showed without details is refused with ErrCardUnreadable before a
// ticket is spent on it.
//
// usage says where the details are going to be presented; it is recorded
// with the grant. Mint the ticket INSIDE the step that redeems it.
func (c *Client) IssueGrant(ctx context.Context, cardID string, usage *Usage) (*Grant, error) {
	in := struct {
		CardID string `json:"cardId"`
		Usage  *Usage `json:"usage,omitempty"`
	}{CardID: cardID}
	if usage != nil {
		in.Usage = &Usage{App: usage.App, ExternalID: usage.ExternalID, URL: usage.URL, Label: usage.Label}
	}
	var out Grant
	if err := c.call(ctx, panService+"IssueGrant", in, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

// Redeem exchanges a ticket for the card's details, exactly once
// (ErrGrantSpent after, and for an unknown or expired ticket). A page
// without a usable number, expiry or CVV quarantines the card and answers
// ErrCardUnreadable: return the card and take another.
func (c *Client) Redeem(ctx context.Context, token string) (*CardDetails, error) {
	var out CardDetails
	if err := c.call(ctx, panService+"Redeem", map[string]string{"token": token}, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

// CardDetails is IssueGrant followed at once by Redeem — the shape to reach
// for. The two calls exist separately so the ticket is on the record, not
// so a caller can hold one.
func (c *Client) CardDetails(ctx context.Context, cardID string, usage *Usage) (*CardDetails, error) {
	g, err := c.IssueGrant(ctx, cardID, usage)
	if err != nil {
		return nil, err
	}
	return c.Redeem(ctx, g.Token)
}

// ThreeDSCode is a confirmation code the issuer delivered for a card.
type ThreeDSCode struct {
	Code        string    `json:"code"`
	DeliveredAt time.Time `json:"deliveredAt"`
	// Who asked for it, when the issuer says.
	Merchant string `json:"merchant"`
}

// WaitThreeDS returns the first 3DS code for the card delivered after
// `since`, waiting up to `wait` (the server clamps it; 0 is its maximum).
//
// `since` is load-bearing: pass the moment immediately BEFORE triggering the
// authorization, so a code from an earlier attempt cannot confirm this one.
// A code is handed out once.
//
// A nil code with a nil error means nothing arrived in time — an answer,
// not a failure. An issuer whose cards need no 3DS answers at once.
func (c *Client) WaitThreeDS(ctx context.Context, cardID string, since time.Time, wait time.Duration) (*ThreeDSCode, error) {
	in := struct {
		CardID      string    `json:"cardId"`
		Since       time.Time `json:"since"`
		WaitSeconds int32     `json:"waitSeconds,omitempty"`
	}{cardID, since.UTC(), int32(wait / time.Second)}
	var out ThreeDSCode
	if err := c.call(ctx, panService+"WaitThreeDS", in, &out, true); err != nil {
		return nil, err
	}
	if out.Code == "" {
		return nil, nil
	}
	return &out, nil
}
