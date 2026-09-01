package vibecards

import (
	"context"
	"time"
)

// Draw outcomes, as vibe-cards spells them.
const (
	// OutcomeDrawn — a card came out of the pool's stock.
	OutcomeDrawn = "DRAW_OUTCOME_DRAWN"
	// OutcomeReused — the subject already held one. Nothing was drawn and
	// nothing was spent.
	OutcomeReused = "DRAW_OUTCOME_REUSED"
	// OutcomeNoStock — the pool is empty. NOT an error: the caller may draw,
	// the pool exists, and there is nothing free. Hold and try later; the
	// pool's own top-up target is what fixes it.
	OutcomeNoStock = "DRAW_OUTCOME_NO_STOCK"
)

// Holding is one person holding one card for a stretch of time.
//
// A claim says "this card funds that ad account"; a holding says "this person
// has this card". One holding carries several claims — the account claim and
// the ad-account claim on the same card are one physical use — which is why
// a card is released by releasing the HOLDING, not the claims.
type Holding struct {
	ID            string `json:"id"`
	CardID        string `json:"cardId"`
	GroupID       string `json:"groupId"`
	HolderUserSub string `json:"holderUserSub"`
	HolderEmail   string `json:"holderEmail"`
	// Exclusive reports whether this holding has the card to itself.
	Exclusive bool `json:"exclusive"`
	// LimitCents is the hard ceiling the card was raised to for this holding.
	// It is the blast radius: a pooled card has no personal balance behind it
	// to run out of.
	LimitCents int64  `json:"limitCents,string"`
	DrawnBy    string `json:"drawnBy"`
	Note       string `json:"note"`

	AcquiredAt    *time.Time `json:"acquiredAt,omitempty"`
	ReleasedAt    *time.Time `json:"releasedAt,omitempty"`
	ReleasedBy    string     `json:"releasedBy"`
	ReleaseReason string     `json:"releaseReason"`

	CardLastFour string `json:"cardLastFour"`
	CardName     string `json:"cardName"`
	CardStatus   string `json:"cardStatus"`
	GroupName    string `json:"groupName"`
	// HolderLabel is the holder in words — the member roster's name or e-mail
	// when vibe-cards knows them, else the e-mail recorded at draw time.
	HolderLabel string `json:"holderLabel"`
	// Claims are the live claims made under this holding.
	Claims []HoldingClaim `json:"claims"`
}

// HoldingClaim is a live claim seen from its holding — enough to say "this
// card funds account X in vibe-accounts".
type HoldingClaim struct {
	AssignmentID string `json:"assignmentId"`
	SubjectApp   string `json:"subjectApp"`
	SubjectType  string `json:"subjectType"`
	SubjectID    string `json:"subjectId"`
	Purpose      string `json:"purpose"`
	Status       string `json:"status"`
	ExternalRef  string `json:"externalRef"`
}

// Live reports whether the holding still occupies a seat on the card.
func (h *Holding) Live() bool { return h != nil && h.ReleasedAt == nil }

// DrawInput asks for a card out of a pool.
//
// This is what a consumer preparing an account should reach for. Unlike
// RequestCard it does not issue: it hands over a card the estate already owns
// and takes it back when the subject is done, which is why it costs nothing and
// does not have to be gated on somebody agreeing to spend.
type DrawInput struct {
	// GroupID may be empty when the caller is granted exactly one pool.
	GroupID string `json:"groupId,omitempty"`
	// HolderUserSub is the Keycloak sub of whoever will be spending. Required:
	// a card held by nobody puts spend on a payee that does not exist.
	HolderUserSub string `json:"holderUserSub"`
	HolderEmail   string `json:"holderEmail,omitempty"`
	// LimitCents lowers the group's default for this holding. Raising it is not
	// a caller's decision.
	LimitCents int64  `json:"limitCents,omitempty,string"`
	Note       string `json:"note,omitempty"`

	// What to claim the card for. Leave empty to take a card without pointing
	// it at anything yet.
	SubjectApp  string `json:"subjectApp,omitempty"`
	SubjectType string `json:"subjectType,omitempty"`
	SubjectID   string `json:"subjectId,omitempty"`
	Purpose     string `json:"purpose,omitempty"`
}

// DrawResult is what the pool did.
type DrawResult struct {
	Holding    *Holding    `json:"holding"`
	Assignment *Assignment `json:"assignment"`
	Outcome    string      `json:"outcome"`
}

// NoStock reports whether the pool had nothing free. Callers should hold rather
// than fail: an empty pool is a supply problem, not a caller's mistake.
func (r *DrawResult) NoStock() bool { return r != nil && r.Outcome == OutcomeNoStock }

// Reused reports that the subject already held a card and nothing was drawn.
func (r *DrawResult) Reused() bool { return r != nil && r.Outcome == OutcomeReused }

// DrawCard takes a card out of a pool for one person, optionally claiming it
// for a subject in the same call.
//
// Idempotent by (subject, purpose) when a subject is given: a retried request
// finds the claim it already made rather than taking a second card.
func (c *Client) DrawCard(ctx context.Context, in DrawInput) (*DrawResult, error) {
	var out DrawResult
	if err := c.call(ctx, "/cards.v1.AssignmentService/DrawCard", in, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReleaseHolding hands a card back to its pool: the holding ends, the claims
// made under it are revoked, and — once nobody else is on the card — its limit
// drops to zero, so a card sitting in stock cannot spend even if its number
// leaked.
//
// Call this when a subject is finished with its card: an account that died, an
// ad account whose funding was replaced, a handover to somebody else. A card
// nobody returns is a card the estate pays to replace.
func (c *Client) ReleaseHolding(ctx context.Context, id, reason string) (*Holding, error) {
	var out Holding
	if err := c.call(ctx, "/cards.v1.AssignmentService/ReleaseHolding",
		map[string]string{"id": id, "reason": reason}, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

// HoldingListInput narrows a holdings listing. All fields are optional.
type HoldingListInput struct {
	CardID        string `json:"cardId,omitempty"`
	GroupID       string `json:"groupId,omitempty"`
	HolderUserSub string `json:"holderUserSub,omitempty"`
	LiveOnly      bool   `json:"liveOnly,omitempty"`
}

// ListHoldings answers "what is this person holding" and "who is on this card".
func (c *Client) ListHoldings(ctx context.Context, in HoldingListInput) ([]Holding, error) {
	var out struct {
		Holdings []Holding `json:"holdings"`
	}
	if err := c.call(ctx, "/cards.v1.AssignmentService/ListHoldings", in, &out, false); err != nil {
		return nil, err
	}
	return out.Holdings, nil
}
