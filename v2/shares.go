package vibecards

import (
	"context"
	"time"
)

const shareService = "/cards.v2.ShareService/"

// Share lets a principal see (VIEW) or use (USE) a card, every card with a
// label, or every card of an owner. Exactly one of CardID, Label and
// AllOfOwner is set.
type Share struct {
	ID string `json:"id,omitempty"`

	CardID string `json:"cardId,omitempty"`
	Label  string `json:"label,omitempty"`
	// An owner's principal id: "all of this owner's cards" — what an
	// assistant is given, with RightUse.
	AllOfOwner string `json:"allOfOwner,omitempty"`

	With  Principal `json:"with"`
	Right Right     `json:"right"`

	// Filled by the server.
	SharedBy  *Principal `json:"sharedBy,omitempty"`
	CreatedAt *time.Time `json:"createdAt,omitempty"`
}

// Share creates shares. Sharing what is already shared updates its right.
func (c *Client) Share(ctx context.Context, shares ...Share) ([]Share, error) {
	in := struct {
		Shares []Share `json:"shares"`
	}{make([]Share, len(shares))}
	for i, s := range shares {
		in.Shares[i] = Share{CardID: s.CardID, Label: s.Label, AllOfOwner: s.AllOfOwner, With: s.With, Right: s.Right}
	}
	var out struct {
		Shares []Share `json:"shares"`
	}
	if err := c.call(ctx, shareService+"Share", in, &out, false); err != nil {
		return nil, err
	}
	return out.Shares, nil
}

// Unshare removes shares by id and answers how many went.
func (c *Client) Unshare(ctx context.Context, shareIDs ...string) (int32, error) {
	in := struct {
		ShareIDs []string `json:"shareIds"`
	}{shareIDs}
	var out struct {
		Removed int32 `json:"removed"`
	}
	if err := c.call(ctx, shareService+"Unshare", in, &out, false); err != nil {
		return 0, err
	}
	return out.Removed, nil
}

// ListSharesInput narrows the shares read; empty reads every share the
// caller can see.
type ListSharesInput struct {
	CardID  string `json:"cardId,omitempty"`
	WithID  string `json:"withId,omitempty"`
	OwnerID string `json:"ownerId,omitempty"`
	Page    Page   `json:"page"`
}

// ShareList is one page of shares.
type ShareList struct {
	Shares        []Share
	NextPageToken string
	TotalCount    int32
}

// ListShares reads one page of shares.
func (c *Client) ListShares(ctx context.Context, in ListSharesInput) (*ShareList, error) {
	var out struct {
		Shares []Share      `json:"shares"`
		Page   pageResponse `json:"page"`
	}
	if err := c.call(ctx, shareService+"ListShares", in, &out, false); err != nil {
		return nil, err
	}
	return &ShareList{Shares: out.Shares, NextPageToken: out.Page.NextPageToken, TotalCount: out.Page.TotalCount}, nil
}
