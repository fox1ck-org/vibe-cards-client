package vibecards

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// vibe-cards serialises protobuf enums as NUMBERS: its JSON codec is tuned for
// a frontend that treats enum fields that way. So on the wire a card's status
// arrives as `2`, not `"CARD_STATUS_ACTIVE"`, and decoding it into a plain
// string broke every read that carried a card — DrawCard, GetAssignment,
// ListAssignments — with "cannot unmarshal number into Go struct field".
//
// The enum types below accept the number, the proto constant name and the bare
// word, and always yield the bare word, so a consumer compares against the
// constants and never sees the wire form. Each numbering mirrors the server's
// proto, which is the source of truth for the order: appending is safe,
// renumbering is not.

// decodeEnum turns one JSON enum value into its bare word. An unknown number is
// kept as its digits rather than dropped: a value the server added after this
// client was built is still a value, and a consumer that logs it can see it.
func decodeEnum(b []byte, prefix string, byNumber map[int64]string) (string, error) {
	if string(b) == "null" {
		return "", nil
	}
	if b[0] != '"' {
		var n json.Number
		if err := json.Unmarshal(b, &n); err != nil {
			return "", err
		}
		i, err := n.Int64()
		if err != nil {
			return "", err
		}
		if v, ok := byNumber[i]; ok {
			return v, nil
		}
		return strconv.FormatInt(i, 10), nil
	}
	var raw string
	if err := json.Unmarshal(b, &raw); err != nil {
		return "", err
	}
	raw = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(raw), prefix))
	if raw == "unspecified" {
		raw = ""
	}
	return raw, nil
}

// CardStatus is the card's lifecycle as vibe-cards reports it on a claim or a
// holding: what the holder may expect the card to do right now.
type CardStatus string

const (
	CardStatusUnspecified CardStatus = ""
	CardStatusCreating    CardStatus = "creating"
	CardStatusActive      CardStatus = "active"
	CardStatusPaused      CardStatus = "paused"
	CardStatusBlocked     CardStatus = "blocked"
	CardStatusClosed      CardStatus = "closed"
)

var cardStatusByNumber = map[int64]string{
	0: "", 1: "creating", 2: "active", 3: "paused", 4: "blocked", 5: "closed",
}

// Usable reports whether the card can be charged right now.
func (s CardStatus) Usable() bool { return s == CardStatusActive }

func (s *CardStatus) UnmarshalJSON(b []byte) error {
	v, err := decodeEnum(b, "CARD_STATUS_", cardStatusByNumber)
	if err != nil {
		return fmt.Errorf("card status: %w", err)
	}
	*s = CardStatus(v)
	return nil
}

// Currency is the card's currency as an ISO-4217 code, "" when unspecified.
type Currency string

const (
	CurrencyUnspecified Currency = ""
	CurrencyUSD         Currency = "USD"
	CurrencyEUR         Currency = "EUR"
)

var currencyByNumber = map[int64]string{0: "", 1: "USD", 2: "EUR"}

func (c *Currency) UnmarshalJSON(b []byte) error {
	v, err := decodeEnum(b, "CURRENCY_", currencyByNumber)
	if err != nil {
		return fmt.Errorf("currency: %w", err)
	}
	*c = Currency(strings.ToUpper(v))
	return nil
}

// DrawOutcome is how a draw ended.
type DrawOutcome string

const (
	// OutcomeDrawn — a card came out of the pool's stock.
	OutcomeDrawn DrawOutcome = "drawn"
	// OutcomeReused — the subject already held one. Nothing was drawn and
	// nothing changed; the existing holding is returned.
	OutcomeReused DrawOutcome = "reused"
	// OutcomeNoStock — the pool is empty. NOT an error: the caller may draw,
	// there is just nothing to hand out right now.
	OutcomeNoStock DrawOutcome = "no_stock"
)

var drawOutcomeByNumber = map[int64]string{0: "", 1: "drawn", 2: "reused", 3: "no_stock"}

func (o *DrawOutcome) UnmarshalJSON(b []byte) error {
	v, err := decodeEnum(b, "DRAW_OUTCOME_", drawOutcomeByNumber)
	if err != nil {
		return fmt.Errorf("draw outcome: %w", err)
	}
	*o = DrawOutcome(v)
	return nil
}
