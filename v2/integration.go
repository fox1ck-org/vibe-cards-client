package vibecards

import (
	"context"
	"time"
)

const integrationService = "/cards.v2.IntegrationService/"

// ReadGuard validates one complete collection across ordinary paginated RPCs.
// It grants no access. Validate only after the last page/metadata read; never
// publish aggregates or write a ledger before it succeeds. On ErrReadChanged or
// ErrReadExpired discard the entire collection and retry a bounded number of times.
type ReadGuard struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (c *Client) BeginRead(ctx context.Context) (*ReadGuard, error) {
	var out ReadGuard
	if err := c.call(ctx, integrationService+"BeginRead", struct{}{}, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}
func (c *Client) ValidateRead(ctx context.Context, token string) error {
	return c.call(ctx, integrationService+"ValidateRead", struct {
		Token string `json:"token"`
	}{token}, &struct{}{}, false)
}

// ProductChange is an identity-only notification. Obtain details with the
// ordinary authorized API. Operation is created, updated or deleted.
type ProductChange struct {
	ResourceID string `json:"resourceId"`
	CardID     string `json:"cardId"`
	Operation  string `json:"operation"`
}
type ProductEvent struct {
	ID      string        `json:"id"`
	Type    string        `json:"type"`
	Version int32         `json:"version"`
	OrgID   string        `json:"orgId"`
	Source  string        `json:"source"`
	At      time.Time     `json:"at"`
	Data    ProductChange `json:"data"`
}
type ProductChanges struct {
	Events     []ProductEvent `json:"events"`
	NextCursor string         `json:"nextCursor"`
}

// ListProductChanges polls the retained integration journal. ListChanges remains
// the UI's card audit timeline. Save NextCursor even for an empty filtered page;
// ErrCursorInvalidated requires a complete bootstrap. Cursors are caller-bound
// and expire after seven unused days. Event IDs survive delivery retries.
func (c *Client) ListProductChanges(ctx context.Context, since string, limit int32) (*ProductChanges, error) {
	var out ProductChanges
	in := struct {
		Since string `json:"since"`
		Limit int32  `json:"limit"`
	}{since, limit}
	if err := c.call(ctx, integrationService+"ListChanges", in, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}
