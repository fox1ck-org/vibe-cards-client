package vibecards

import (
	"context"
	"time"
)

const spendService = "/cards.v2.SpendService/"

// TimeRange is [From, To).
type TimeRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// SpendQuery is one "how much, where, by whom" question over the cards a
// filter selects. Without GroupBy the operations themselves come back,
// paged.
type SpendQuery struct {
	Filter Filter    `json:"filter"`
	Range  TimeRange `json:"range"`
	// Empty: every status. The statuses select the operations FIRST; every
	// total, group and page is computed over what they kept.
	TxStatus        []TxStatus `json:"txStatus,omitempty"`
	GroupBy         GroupBy    `json:"groupBy,omitempty"`
	Bucket          Bucket     `json:"bucket,omitempty"`
	ComparePrevious bool       `json:"comparePrevious,omitempty"`
	Page            Page       `json:"page"`
}

// SpendTotals are the sums over what a query selected.
type SpendTotals struct {
	// completed + pending − refunded.
	Spent        Money `json:"spent"`
	IssuerFees   Money `json:"issuerFees"`
	PlatformFees Money `json:"platformFees"`
	Operations   int32 `json:"operations"`
	Refunds      int32 `json:"refunds"`
	Declines     int32 `json:"declines"`
	Cards        int32 `json:"cards"`
}

// SpendGroup is one row of a grouped query.
type SpendGroup struct {
	Key           string        `json:"key"`
	Label         string        `json:"label"`
	Totals        SpendTotals   `json:"totals"`
	PrincipalKind PrincipalKind `json:"principalKind"`
}

// SpendPoint is one bucket of a series.
type SpendPoint struct {
	At    time.Time `json:"at"`
	Spent Money     `json:"spent"`
}

// Transaction is one operation on a card.
type Transaction struct {
	ID string `json:"id"`
	// The issuer's stable operation reference. Empty in PLATFORM mode.
	IssuerReference string `json:"issuerReference"`
	CardID          string `json:"cardId"`
	Amount          Money  `json:"amount"`
	// What the issuer charged, as it reported.
	IssuerFee Money `json:"issuerFee"`
	// What its fee terms say it should have charged; nil: terms unknown.
	ExpectedIssuerFee *Money    `json:"expectedIssuerFee"`
	PlatformFee       Money     `json:"platformFee"`
	Merchant          string    `json:"merchant"`
	Descriptor        string    `json:"descriptor"`
	MCC               string    `json:"mcc"`
	Country           string    `json:"country"`
	Status            TxStatus  `json:"status"`
	At                time.Time `json:"at"`
}

// SpendReport answers a SpendQuery.
type SpendReport struct {
	Totals       SpendTotals
	Previous     *SpendTotals
	Groups       []SpendGroup
	Series       []SpendPoint
	Transactions []Transaction
	// Of Transactions; empty on the last page.
	NextPageToken string
	TotalCount    int32
}

// Spend runs one query. Only cards the caller can see are counted.
func (c *Client) Spend(ctx context.Context, q SpendQuery) (*SpendReport, error) {
	var out struct {
		Totals       SpendTotals   `json:"totals"`
		Previous     *SpendTotals  `json:"previous"`
		Groups       []SpendGroup  `json:"groups"`
		Series       []SpendPoint  `json:"series"`
		Transactions []Transaction `json:"transactions"`
		Page         pageResponse  `json:"page"`
	}
	if err := c.call(ctx, spendService+"Query", q, &out, false); err != nil {
		return nil, err
	}
	return &SpendReport{Totals: out.Totals, Previous: out.Previous, Groups: out.Groups, Series: out.Series,
		Transactions: out.Transactions, NextPageToken: out.Page.NextPageToken, TotalCount: out.Page.TotalCount}, nil
}
