package vibecards

import (
	"context"
	"errors"
	"time"
)

// Transaction is the provider's recorded charge, without card credentials.
// Amount/Fee are major-unit decimals; nil Fee means an older API supplied none.
type Transaction struct {
	ID              string     `json:"id"`
	CardID          string     `json:"cardId"`
	ExternalID      string     `json:"externalId"`
	Amount          string     `json:"amount"`
	Currency        Currency   `json:"currency"`
	MerchantName    string     `json:"merchantName"`
	Status          string     `json:"status"`
	TransactionDate *time.Time `json:"transactionDate"`
	Fee             *string    `json:"fee,omitempty"`
}

type TransactionPage struct {
	Transactions []Transaction `json:"transactions"`
	PageInfo     struct {
		TotalPages int `json:"totalPages"`
		TotalCount int `json:"totalCount"`
	} `json:"pageInfo"`
}

// ListCardTransactions requires a card identity and a bounded time range. A
// consumer must authorize its subject and verify the card's assignment before
// invoking this service-key read. Never substitute a last-four search for ID.
func (c *Client) ListCardTransactions(ctx context.Context, cardID string, from, to time.Time, page int) (*TransactionPage, error) {
	if c == nil {
		return nil, ErrNotConfigured
	}
	if cardID == "" || from.IsZero() || !to.After(from) || to.Sub(from) > 370*24*time.Hour || page < 1 {
		return nil, errors.New("card ID, bounded dates and positive page required")
	}
	in := struct {
		CardID    string `json:"cardId"`
		Page      int    `json:"page"`
		PageSize  int    `json:"pageSize"`
		DateRange struct {
			StartDate time.Time `json:"startDate"`
			EndDate   time.Time `json:"endDate"`
		} `json:"dateRange"`
	}{CardID: cardID, Page: page, PageSize: 100}
	in.DateRange.StartDate, in.DateRange.EndDate = from, to
	var out TransactionPage
	if err := c.call(ctx, "/cards.v1.TransactionService/ListTransactions", in, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}
