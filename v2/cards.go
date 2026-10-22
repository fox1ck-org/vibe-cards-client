package vibecards

import (
	"context"
	"time"
)

const cardService = "/cards.v2.CardService/"

type cardResponse struct {
	Card *Card `json:"card"`
}

// IssueInput buys new cards. Issuing costs a fee every time; prefer taking a
// card the organisation already has.
type IssueInput struct {
	// One press of the button: a retry with the same id answers the cards of
	// the first attempt (or ErrInProgress), never more cards. Required.
	RequestID string `json:"requestId"`
	// Whose the cards will be; nil: the caller (or the person OnBehalfOf).
	Owner  *Principal `json:"owner,omitempty"`
	Count  int32      `json:"count"`
	Limits *Limits    `json:"limits,omitempty"`
	Name   string     `json:"name,omitempty"`
	Labels []string   `json:"labels,omitempty"`
	// ISO 4217; empty: the issuer's default.
	Currency string `json:"currency,omitempty"`
	// OWN issue mode only.
	BIN             string `json:"bin,omitempty"`
	IssuerAccountID string `json:"issuerAccountId,omitempty"`
	HolderProfileID string `json:"holderProfileId,omitempty"`
	// Put the cards straight into the owner's hands.
	Take bool `json:"take,omitempty"`
}

// Issue buys `Count` new cards. A card the issuer builds slowly comes back
// CREATING and turns ACTIVE by itself.
func (c *Client) Issue(ctx context.Context, in IssueInput) ([]Card, error) {
	if in.Count == 0 {
		in.Count = 1
	}
	var out struct {
		Cards []Card `json:"cards"`
	}
	if err := c.call(ctx, cardService+"Issue", in, &out, false); err != nil {
		return nil, err
	}
	return out.Cards, nil
}

// Get reads one card.
func (c *Client) Get(ctx context.Context, cardID string) (*Card, error) {
	var out cardResponse
	if err := c.call(ctx, cardService+"Get", map[string]string{"id": cardID}, &out, false); err != nil {
		return nil, err
	}
	return out.Card, nil
}

// ListInput selects cards.
type ListInput struct {
	Filter Filter `json:"filter"`
	Page   Page   `json:"page"`
	// "created_at", "updated_at", "spent_today", "spent_total", "last4";
	// a leading "-" sorts descending. Empty: newest first.
	OrderBy string `json:"orderBy,omitempty"`
}

// CardList is one page of cards.
type CardList struct {
	Cards []Card
	// Empty on the last page.
	NextPageToken string
	TotalCount    int32
	// Distinct holders of a card the filter selects, over the whole result.
	HolderCount int32
}

// List reads one page of the cards the filter selects among those the
// caller can see.
func (c *Client) List(ctx context.Context, in ListInput) (*CardList, error) {
	var out struct {
		Cards       []Card       `json:"cards"`
		Page        pageResponse `json:"page"`
		HolderCount int32        `json:"holderCount"`
	}
	if err := c.call(ctx, cardService+"List", in, &out, false); err != nil {
		return nil, err
	}
	return &CardList{Cards: out.Cards, NextPageToken: out.Page.NextPageToken,
		TotalCount: out.Page.TotalCount, HolderCount: out.HolderCount}, nil
}

// UpdateInput changes what a card's owner (or an admin) may change. Nil
// fields stay as they are.
type UpdateInput struct {
	ID           string   `json:"id"`
	Name         *string  `json:"name,omitempty"`
	Limits       *Limits  `json:"limits,omitempty"`
	AddLabels    []string `json:"addLabels,omitempty"`
	RemoveLabels []string `json:"removeLabels,omitempty"`
	// "" clears the card's own choice.
	HolderProfileID *string `json:"holderProfileId,omitempty"`
}

// Update changes a card. A limit the issuer cannot hold is refused with
// ErrIssuerUnsupported, not ignored.
func (c *Client) Update(ctx context.Context, in UpdateInput) (*Card, error) {
	var out cardResponse
	if err := c.call(ctx, cardService+"Update", in, &out, false); err != nil {
		return nil, err
	}
	return out.Card, nil
}

// Pause stops a card. When the issuer cannot, the platform holds it and
// Card.StatusNote says the issuer still lets it spend.
func (c *Client) Pause(ctx context.Context, cardID, reason string) (*Card, error) {
	var out cardResponse
	if err := c.call(ctx, cardService+"Pause", map[string]string{"id": cardID, "reason": reason}, &out, false); err != nil {
		return nil, err
	}
	return out.Card, nil
}

// Resume lifts a pause, a hold or — for an admin — a quarantine.
func (c *Client) Resume(ctx context.Context, cardID string) (*Card, error) {
	var out cardResponse
	if err := c.call(ctx, cardService+"Resume", map[string]string{"id": cardID}, &out, false); err != nil {
		return nil, err
	}
	return out.Card, nil
}

// Close closes a card for good.
func (c *Client) Close(ctx context.Context, cardID, reason string) (*Card, error) {
	var out cardResponse
	if err := c.call(ctx, cardService+"Close", map[string]string{"id": cardID, "reason": reason}, &out, false); err != nil {
		return nil, err
	}
	return out.Card, nil
}

// BulkInput applies one action to the cards a filter selects. An empty
// filter is refused — it would be every card.
type BulkInput struct {
	Filter Filter     `json:"filter"`
	Action BulkAction `json:"action"`
	Limits *Limits    `json:"limits,omitempty"`
	Labels []string   `json:"labels,omitempty"`
	Reason string     `json:"reason,omitempty"`
}

// BulkResult counts what happened; one card's refusal does not stop the
// rest. When the filter selected more cards than one call applies (1000),
// the cards are worked through in the background: OperationID is set,
// Succeeded and Failed are 0, and GetBulkOperation follows it.
type BulkResult struct {
	Succeeded int32         `json:"succeeded"`
	Failed    int32         `json:"failed"`
	Failures  []BulkFailure `json:"failures"`
	// Set when the bulk runs in the background.
	OperationID string `json:"operationId"`
	// How many cards the filter selected.
	Selected int32 `json:"selected"`
}

// Background reports whether the bulk runs in the background.
func (r *BulkResult) Background() bool { return r != nil && r.OperationID != "" }

// BulkFailure is one card's refusal.
type BulkFailure struct {
	CardID  string `json:"cardId"`
	Reason  Reason `json:"reason"`
	Message string `json:"message"`
}

// Bulk applies one action to many cards, each checked as if it were alone.
func (c *Client) Bulk(ctx context.Context, in BulkInput) (*BulkResult, error) {
	var out BulkResult
	if err := c.call(ctx, cardService+"Bulk", in, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

// BulkOperation is a Bulk running in the background.
type BulkOperation struct {
	ID     string             `json:"id"`
	Action BulkAction         `json:"action"`
	State  BulkOperationState `json:"state"`
	// The cards selected when it started.
	Total int32 `json:"total"`
	// Tried so far: Succeeded + Failed.
	Done      int32 `json:"done"`
	Succeeded int32 `json:"succeeded"`
	Failed    int32 `json:"failed"`
	// The first refusals (at most 200); Failed counts them all.
	Failures   []BulkFailure `json:"failures"`
	StartedBy  Principal     `json:"startedBy"`
	StartedAt  time.Time     `json:"startedAt"`
	FinishedAt *time.Time    `json:"finishedAt,omitempty"`
}

// GetBulkOperation follows a background Bulk. Only whoever started it (the
// person, under OnBehalfOf) or an admin reads it; anybody else gets
// ErrNotFound.
func (c *Client) GetBulkOperation(ctx context.Context, operationID string) (*BulkOperation, error) {
	var out struct {
		Operation *BulkOperation `json:"operation"`
	}
	if err := c.call(ctx, cardService+"GetBulkOperation", map[string]string{"operationId": operationID}, &out, false); err != nil {
		return nil, err
	}
	return out.Operation, nil
}

// RevealedNumber is a card's number shown to a PERSON, on the record.
type RevealedNumber struct {
	Number   string     `json:"number"`
	ExpMonth string     `json:"expMonth"`
	ExpYear  string     `json:"expYear"`
	CVV      string     `json:"cvv"`
	Holder   HolderData `json:"holder"`
	// What the issuer's page did not show ("exp", "cvv").
	HiddenByIssuer []string `json:"hiddenByIssuer"`
	// Hide the number after this instant.
	VisibleUntil time.Time `json:"visibleUntil"`
}

// RevealNumber shows a card's number to a person (WithToken with their
// token). A service never reads a number this way — it uses CardDetails.
// The reason is required and written to the journal.
func (c *Client) RevealNumber(ctx context.Context, cardID, reason string) (*RevealedNumber, error) {
	var out RevealedNumber
	if err := c.call(ctx, cardService+"RevealNumber",
		map[string]string{"cardId": cardID, "reason": reason}, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListChangesInput reads the journal: with Filter.CardIDs one card's
// history, with Filter.UpdatedSince the feed a puller keeps fresh with.
type ListChangesInput struct {
	Filter Filter `json:"filter"`
	Page   Page   `json:"page"`
}

// ChangeList is one page of the journal.
type ChangeList struct {
	Changes       []CardChange
	NextPageToken string
	TotalCount    int32
}

// ListChanges reads one page of the journal.
func (c *Client) ListChanges(ctx context.Context, in ListChangesInput) (*ChangeList, error) {
	var out struct {
		Changes []CardChange `json:"changes"`
		Page    pageResponse `json:"page"`
	}
	if err := c.call(ctx, cardService+"ListChanges", in, &out, false); err != nil {
		return nil, err
	}
	return &ChangeList{Changes: out.Changes, NextPageToken: out.Page.NextPageToken, TotalCount: out.Page.TotalCount}, nil
}
