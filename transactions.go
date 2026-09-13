package vibecards

import (
	"context"
	"errors"
	"time"
)

// Transaction is the provider's recorded charge, without card credentials.
// Amount/Fee are major-unit decimals; nil Fee means an older API supplied none.
type Transaction struct {
	ID              string            `json:"id"`
	CardID          string            `json:"cardId"`
	ExternalID      string            `json:"externalId"`
	Amount          string            `json:"amount"`
	Currency        Currency          `json:"currency"`
	MerchantName    string            `json:"merchantName"`
	Status          TransactionStatus `json:"status"`
	TransactionDate *time.Time        `json:"transactionDate"`
	Fee             *string           `json:"fee,omitempty"`
}

type TransactionPage struct {
	Transactions []Transaction `json:"transactions"`
	PageInfo     *struct {
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
	if out.PageInfo == nil || len(out.Transactions) > 100 {
		return nil, errors.New("incomplete transaction page")
	}
	for _, tx := range out.Transactions {
		if tx.ID == "" || tx.CardID != cardID || tx.TransactionDate == nil {
			return nil, errors.New("invalid transaction page")
		}
	}
	return &out, nil
}

// TransactionStatus accepts both numeric and named Connect protobuf enums.
type TransactionStatus string

func (s *TransactionStatus) UnmarshalJSON(b []byte) error {
	v, err := decodeEnum(b, "TRANSACTION_STATUS_", map[int64]string{0: "", 1: "pending", 2: "completed", 3: "declined", 4: "refunded"})
	if err == nil {
		*s = TransactionStatus(v)
	}
	return err
}
