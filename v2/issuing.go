package vibecards

import (
	"context"
	"time"
)

const issuingService = "/cards.v2.IssuingService/"

// TopUpInput moves money to a person's account at the issuer.
type TopUpInput struct {
	// Whose account at the issuer.
	Person Principal `json:"person"`
	// Empty: the organisation's default issuer account.
	IssuerAccountID string `json:"issuerAccountId,omitempty"`
	Amount          Money  `json:"amount"`
	// One press of the button, kept across retries of that press. Required.
	RequestID string `json:"requestId"`
}

// TopUp moves money to a person's account at the issuer, when the
// organisation's top-up module allows it (ErrTopUpDisabled otherwise). It
// answers the person's balance at the issuer after the top-up (nil when the
// issuer did not say).
//
// Raising one CARD's spending limit is not a top-up: that is Update with
// Limits (vibe-desk's «Пополнить» on a held card is exactly that).
func (c *Client) TopUp(ctx context.Context, in TopUpInput) (*Money, error) {
	var out struct {
		Balance *Money `json:"balance"`
	}
	if err := c.call(ctx, issuingService+"TopUp", in, &out, false); err != nil {
		return nil, err
	}
	return out.Balance, nil
}

// IssuerAccount is an organisation's own issuer connection. It contains no
// credentials. Accounts are hidden in PLATFORM mode.
type IssuerAccount struct {
	ID           string      `json:"id"`
	Owner        IssuerOwner `json:"owner"`
	Kind         string      `json:"kind"`
	Capabilities []string    `json:"capabilities"`
	Balance      *Money      `json:"balance"`
	IsDefault    bool        `json:"isDefault"`
	Name         string      `json:"name"`
	CardCount    int32       `json:"cardCount"`
	RunsOutAt    *time.Time  `json:"runsOutAt"`
}

// ListAccounts returns the organisation's own issuer accounts. It requires
// permission to read organisation-wide issuing settings; it never returns secrets.
func (c *Client) ListAccounts(ctx context.Context) ([]IssuerAccount, error) {
	var out struct {
		Accounts []IssuerAccount `json:"accounts"`
	}
	if err := c.call(ctx, issuingService+"ListAccounts", struct{}{}, &out, false); err != nil {
		return nil, err
	}
	return out.Accounts, nil
}
