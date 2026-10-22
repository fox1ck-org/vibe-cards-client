package vibecards

import (
	"context"
	"errors"
	"fmt"
)

const holdingService = "/cards.v2.HoldingService/"

// TakeInput puts a card in somebody's hands.
type TakeInput struct {
	CardID string `json:"cardId"`
	// Who will hold it. Nil: the caller — or, for a service, the person
	// named with OnBehalfOf. Only an admin takes for somebody else.
	Holder *Principal `json:"holder,omitempty"`
	// Raises the card's limit for this holding, where the issuer can.
	// Nil: the limit the card was last held (or issued) with.
	Limits *Limits `json:"limits,omitempty"`
	// Where the card will work, recorded with the take.
	Usage *Usage `json:"usage,omitempty"`
	// A retry with the same id is the same take.
	RequestID string `json:"requestId,omitempty"`
}

// Take puts a card in the holder's hands. The card must be ACTIVE
// (ErrCardNotActive), held by nobody else (ErrHeldByOther), and the
// holder's own or shared with them for USE (ErrNotSharedForUse). Taking a
// card the holder already holds changes nothing and is not an error.
func (c *Client) Take(ctx context.Context, in TakeInput) (*Card, error) {
	var out cardResponse
	if err := c.call(ctx, holdingService+"Take", in, &out, false); err != nil {
		return nil, err
	}
	return out.Card, nil
}

// TakeFirstInput is "any card like this": the filter picks the candidates,
// the rest is what Take records.
type TakeFirstInput struct {
	// Narrows the candidates. Holder is forced to Nobody; Status defaults
	// to ACTIVE. Filters run with the CALLER's visibility — under
	// OnBehalfOf, the person's: their cards, their hands, what was shared
	// with them; narrow further with Owner or SharedWith. A card the holder
	// may not use is skipped, not taken.
	Filter Filter
	// Sort order of the candidates; empty: newest first.
	OrderBy   string
	Holder    *Principal
	Limits    *Limits
	Usage     *Usage
	RequestID string
	// MaxAttempts bounds how many candidates are tried; 0 means 20.
	MaxAttempts int
}

// skippable are the refusals that mean "not this card, try the next one":
// another taker won the race, the card stopped being active, it went away,
// or it is visible to the caller but not usable by the holder.
var skippable = []error{ErrHeldByOther, ErrCardNotActive, ErrNotFound, ErrNotSharedForUse}

// TakeFirst lists the cards nobody holds that match the filter and takes
// the first one it can, moving to the next when another taker won the race
// (ErrHeldByOther) or the card stopped being usable. It answers
// ErrNoCardAvailable when none could be taken; any other refusal stops it.
func (c *Client) TakeFirst(ctx context.Context, in TakeFirstInput) (*Card, error) {
	if c == nil {
		return nil, ErrNotConfigured
	}
	f := in.Filter
	f.Holder = []string{Nobody}
	if len(f.Status) == 0 {
		f.Status = []CardStatus{CardStatusActive}
	}
	budget := in.MaxAttempts
	if budget <= 0 {
		budget = 20
	}
	tried := 0
	page := Page{Size: 50}
	for {
		list, err := c.List(ctx, ListInput{Filter: f, Page: page, OrderBy: in.OrderBy})
		if err != nil {
			return nil, err
		}
		for i := range list.Cards {
			if tried == budget {
				return nil, fmt.Errorf("%w: %d candidates tried", ErrNoCardAvailable, tried)
			}
			tried++
			card, err := c.Take(ctx, TakeInput{
				CardID: list.Cards[i].ID, Holder: in.Holder, Limits: in.Limits,
				Usage: in.Usage, RequestID: in.RequestID,
			})
			if err == nil {
				return card, nil
			}
			if !isSkippable(err) {
				return nil, err
			}
		}
		if list.NextPageToken == "" {
			return nil, fmt.Errorf("%w: %d candidates tried", ErrNoCardAvailable, tried)
		}
		page.Token = list.NextPageToken
	}
}

func isSkippable(err error) bool {
	for _, s := range skippable {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}

// Return hands the card back to its owner: the holding ends, the usages
// added under it are removed, and the card's limit drops so a card nobody
// holds cannot spend even if its number leaked.
func (c *Client) Return(ctx context.Context, cardID, reason string) (*Card, error) {
	var out cardResponse
	if err := c.call(ctx, holdingService+"Return",
		map[string]string{"cardId": cardID, "reason": reason}, &out, false); err != nil {
		return nil, err
	}
	return out.Card, nil
}

// Transfer hands a held card to somebody else without it moving; the
// usages stay.
func (c *Client) Transfer(ctx context.Context, cardID string, to Principal, reason string) (*Card, error) {
	in := struct {
		CardID string    `json:"cardId"`
		To     Principal `json:"to"`
		Reason string    `json:"reason,omitempty"`
	}{cardID, to, reason}
	var out cardResponse
	if err := c.call(ctx, holdingService+"Transfer", in, &out, false); err != nil {
		return nil, err
	}
	return out.Card, nil
}

// AddUsage records where the card works. Idempotent by (card, app,
// external id): adding the same usage again updates its url and label.
func (c *Client) AddUsage(ctx context.Context, cardID string, u Usage) (*Card, error) {
	in := struct {
		CardID string `json:"cardId"`
		Usage  Usage  `json:"usage"`
	}{cardID, Usage{App: u.App, ExternalID: u.ExternalID, URL: u.URL, Label: u.Label}}
	var out cardResponse
	if err := c.call(ctx, holdingService+"AddUsage", in, &out, false); err != nil {
		return nil, err
	}
	return out.Card, nil
}

// RemoveUsage forgets one usage; the card and its other usages stay.
func (c *Client) RemoveUsage(ctx context.Context, cardID, app, externalID, reason string) (*Card, error) {
	in := map[string]string{"cardId": cardID, "app": app, "externalId": externalID, "reason": reason}
	var out cardResponse
	if err := c.call(ctx, holdingService+"RemoveUsage", in, &out, false); err != nil {
		return nil, err
	}
	return out.Card, nil
}
