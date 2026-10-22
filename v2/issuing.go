package vibecards

import "context"

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
