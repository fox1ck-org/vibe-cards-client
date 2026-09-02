package vibecards

import (
	"context"
	"time"
)

// Subject apps and types, as vibe-cards spells them. A consumer should use
// these constants rather than literals so a rename breaks the build.
const (
	SubjectAppAccounts = "vibe-accounts"
	SubjectAppFB       = "vibe-fb"

	SubjectTypeAccount   = "account"
	SubjectTypeAdAccount = "ad_account"

	PurposeFBFunding = "fb_funding"
)

// Assignment statuses.
const (
	StatusReserved = "reserved"
	StatusActive   = "active"
	StatusFailed   = "failed"
	StatusRevoked  = "revoked"
)

// Assignment is one card held by one subject for one purpose.
//
// It carries enough of the card to draw a row and nothing that could be used to
// spend: the point of an assignment is to say WHICH card without revealing it.
type Assignment struct {
	ID          string `json:"id"`
	CardID      string `json:"cardId"`
	SubjectApp  string `json:"subjectApp"`
	SubjectType string `json:"subjectType"`
	SubjectID   string `json:"subjectId"`
	Purpose     string `json:"purpose"`
	Status      string `json:"status"`
	// ExternalRef is what the far end calls the funding source it created —
	// a facebook_funding_id. Empty until provisioning succeeds.
	ExternalRef string `json:"externalRef"`

	AssignedBy       string `json:"assignedBy"`
	AssignedByEmail  string `json:"assignedByEmail"`
	Note             string `json:"note"`
	LastErrorCode    string `json:"lastErrorCode"`
	LastErrorMessage string `json:"lastErrorMessage"`

	ReserveExpiresAt *time.Time `json:"reserveExpiresAt,omitempty"`
	CreatedAt        *time.Time `json:"createdAt,omitempty"`
	ActivatedAt      *time.Time `json:"activatedAt,omitempty"`
	RevokedAt        *time.Time `json:"revokedAt,omitempty"`
	RevokeReason     string     `json:"revokeReason"`

	// HoldingID names the holding this claim was made under — who had the card
	// at the time. Empty for claims made before the pool existed.
	HoldingID string `json:"holdingId"`

	CardLastFour string     `json:"cardLastFour"`
	CardName     string     `json:"cardName"`
	CardStatus   CardStatus `json:"cardStatus"`
	CardCurrency Currency   `json:"cardCurrency"`
}

// Live reports whether the claim still holds its card.
func (a *Assignment) Live() bool {
	return a != nil && (a.Status == StatusReserved || a.Status == StatusActive)
}

// RequestCardInput asks for a card on terms, rather than for a specific card.
//
// This is the demand side, and the call a consumer preparing an account should
// reach for: it does not need to know which provider to issue from, or that
// there is such a thing as a provider.
type RequestCardInput struct {
	SubjectApp  string `json:"subjectApp"`
	SubjectType string `json:"subjectType"`
	SubjectID   string `json:"subjectId"`
	// Empty defaults to PurposeFBFunding.
	Purpose string `json:"purpose,omitempty"`

	// LimitCents of 0 issues without a hard cap — the security rules in
	// vibe-cards are what bound the spend then.
	LimitCents int64 `json:"limitCents,omitempty"`
	// Empty defaults to USD.
	Currency     string `json:"currency,omitempty"`
	PreferredBIN string `json:"preferredBin,omitempty"`
	// Empty uses the estate default. A card with neither cannot be bound, and
	// the call refuses rather than issuing one that cannot be used.
	BillingProfileID string `json:"billingProfileId,omitempty"`
	Note             string `json:"note,omitempty"`
	// OwnerSub is whose card this is, as a Keycloak sub. REQUIRED.
	//
	// It is what vibe-finance bills the spend to, and it decides which Brocard
	// user the card is drawn against. A service issuing on somebody's behalf
	// must say whose it is: a card owned by the service attributes a buyer's
	// charges to nobody, and reconciliation reports them as a residual with no
	// payee. vibe-cards refuses the request without it.
	OwnerSub string `json:"ownerSub"`
}

// RequestCard issues a card and claims it for the subject in one call.
//
// Idempotent by (subject, purpose): a retried request returns the claim it
// already made rather than issuing a second card. `reused` reports which
// happened — the caller does not need it to be correct, but a retry that
// silently costs an issue fee is worth being able to see.
func (c *Client) RequestCard(ctx context.Context, in RequestCardInput) (a *Assignment, reused bool, err error) {
	var out struct {
		Assignment Assignment `json:"assignment"`
		Reused     bool       `json:"reused"`
	}
	if err := c.call(ctx, "/cards.v1.AssignmentService/RequestCard", in, &out, false); err != nil {
		return nil, false, err
	}
	return &out.Assignment, out.Reused, nil
}

// AssignInput claims a card for a subject.
type AssignInput struct {
	CardID      string `json:"cardId"`
	SubjectApp  string `json:"subjectApp"`
	SubjectType string `json:"subjectType"`
	SubjectID   string `json:"subjectId"`
	// Empty defaults to PurposeFBFunding.
	Purpose string `json:"purpose,omitempty"`
	Note    string `json:"note,omitempty"`
}

// AssignCard claims a card for a subject.
//
// Idempotent by (card, subject, purpose): a retried call — a workflow step that
// ran twice, a pod that died after the write — returns the existing claim
// rather than failing. Callers may therefore retry freely.
func (c *Client) AssignCard(ctx context.Context, in AssignInput) (*Assignment, error) {
	var out Assignment
	if err := c.call(ctx, "/cards.v1.AssignmentService/AssignCard", in, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAssignment reads one claim.
func (c *Client) GetAssignment(ctx context.Context, id string) (*Assignment, error) {
	var out Assignment
	if err := c.call(ctx, "/cards.v1.AssignmentService/GetAssignment",
		map[string]string{"id": id}, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListInput narrows a listing. All fields are optional.
type ListInput struct {
	CardID     string `json:"cardId,omitempty"`
	SubjectApp string `json:"subjectApp,omitempty"`
	SubjectID  string `json:"subjectId,omitempty"`
	Purpose    string `json:"purpose,omitempty"`
	// LiveOnly keeps reserved and active claims, dropping the history.
	LiveOnly bool `json:"liveOnly,omitempty"`
}

// ListAssignments answers "what is this subject holding" and "who holds this card".
func (c *Client) ListAssignments(ctx context.Context, in ListInput) ([]Assignment, error) {
	var out struct {
		Assignments []Assignment `json:"assignments"`
	}
	if err := c.call(ctx, "/cards.v1.AssignmentService/ListAssignments", in, &out, false); err != nil {
		return nil, err
	}
	return out.Assignments, nil
}

// RevokeAssignment takes a card back. The row is kept, not deleted: "whose card
// was this in March" is the question asked after money goes missing.
func (c *Client) RevokeAssignment(ctx context.Context, id, reason string) (*Assignment, error) {
	var out Assignment
	if err := c.call(ctx, "/cards.v1.AssignmentService/RevokeAssignment",
		map[string]string{"id": id, "reason": reason}, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

// MarkProvisioned reports that the far end accepted the card, and what it calls
// the funding source it created.
func (c *Client) MarkProvisioned(ctx context.Context, id, externalRef string) (*Assignment, error) {
	var out Assignment
	if err := c.call(ctx, "/cards.v1.AssignmentService/MarkAssignmentProvisioned",
		map[string]string{"id": id, "externalRef": externalRef}, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

// MarkFailed reports that the far end refused, and why. Recording the refusal
// is what turns "the card never got bound" from an absence into an answer.
func (c *Client) MarkFailed(ctx context.Context, id, code, message string) (*Assignment, error) {
	var out Assignment
	if err := c.call(ctx, "/cards.v1.AssignmentService/MarkAssignmentFailed",
		map[string]string{"id": id, "errorCode": code, "errorMessage": message}, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}
