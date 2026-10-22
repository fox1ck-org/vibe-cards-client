package vibecards

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Money is an amount in the currency's minor units (cents for USD).
type Money struct {
	Minor int64
	// ISO 4217, upper case.
	Currency string
}

// USD is an amount in US cents.
func USD(cents int64) Money { return Money{Minor: cents, Currency: "USD"} }

// String renders "12.34 USD" for two-decimal currencies, the format logs
// and journals read best.
func (m Money) String() string {
	sign, v := "", m.Minor
	if v < 0 {
		sign, v = "-", -v
	}
	return fmt.Sprintf("%s%d.%02d %s", sign, v/100, v%100, m.Currency)
}

type wireMoney struct {
	Minor    json.RawMessage `json:"minor"`
	Currency string          `json:"currency"`
}

// MarshalJSON writes minor as protojson writes an int64: a string.
func (m Money) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Minor    string `json:"minor"`
		Currency string `json:"currency"`
	}{strconv.FormatInt(m.Minor, 10), m.Currency})
}

// UnmarshalJSON reads minor as a string or a number.
func (m *Money) UnmarshalJSON(b []byte) error {
	var w wireMoney
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	m.Currency = w.Currency
	m.Minor = 0
	raw := bytes.Trim(bytes.TrimSpace(w.Minor), `"`)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	n, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return fmt.Errorf("money.minor: %w", err)
	}
	m.Minor = n
	return nil
}

// Principal is whoever acts or is acted for.
type Principal struct {
	Kind PrincipalKind `json:"kind"`
	// PERSON: the subject id; SERVICE: the key's principal id; ORG: the org id.
	ID string `json:"id"`
	// A name to show, filled by the server; ignored on input.
	Label string `json:"label,omitempty"`
}

// Person is the principal for a person's subject id (Keycloak sub).
func Person(sub string) Principal { return Principal{Kind: PrincipalPerson, ID: sub} }

// Service is the principal for a service key's principal id.
func Service(id string) Principal { return Principal{Kind: PrincipalService, ID: id} }

// Filter values with a meaning of their own.
const (
	// Me is the caller — for a service, the service itself, also under
	// OnBehalfOf.
	Me = "me"
	// Nobody, in Filter.Holder, is a card nobody holds.
	Nobody = "none"
)

// Filter is the ONE card filter, the same in List, Bulk, ListChanges and
// Spend.Query. Every field narrows; empty fields do not filter; nothing
// widens what the caller may see.
type Filter struct {
	// Cards carrying ALL of these labels.
	Labels []string `json:"labels,omitempty"`
	// Owner principal ids; Me is the caller.
	Owner []string `json:"owner,omitempty"`
	// Holder principal ids; Me is the caller; Nobody is a card nobody holds.
	Holder []string     `json:"holder,omitempty"`
	Status []CardStatus `json:"status,omitempty"`
	// Cards shared with these principals (directly, by label or as all of
	// the owner's cards).
	SharedWith []string `json:"sharedWith,omitempty"`
	// Cards changed at or after this instant.
	UpdatedSince *time.Time `json:"updatedSince,omitempty"`
	// Free text over last four, card name, owner's and holder's names.
	Query            string   `json:"query,omitempty"`
	CardIDs          []string `json:"cardIds,omitempty"`
	IssuerAccountIDs []string `json:"issuerAccountIds,omitempty"`
}

// Page asks for one page of a list. Zero values are the server's defaults.
type Page struct {
	Size  int32  `json:"pageSize,omitempty"`
	Token string `json:"pageToken,omitempty"`
}

type pageResponse struct {
	NextPageToken string `json:"nextPageToken"`
	TotalCount    int32  `json:"totalCount"`
}

// Limits are what the ISSUER holds. A day, a week, a country are rules.
type Limits struct {
	// Nil: no limit at the issuer.
	Total *Money `json:"total,omitempty"`
	// Only where the issuer holds per-transaction limits.
	PerTxn *Money `json:"perTxn,omitempty"`
}

// Holding is who has a card and since when.
type Holding struct {
	Holder Principal `json:"holder"`
	// Who put the card in the holder's hands.
	TakenBy Principal `json:"takenBy"`
	From    time.Time `json:"from"`
	// Nil while the card is held.
	To *time.Time `json:"to,omitempty"`
}

// Usage is where a card works. App, ExternalID, URL and Label come from the
// client and mean nothing to vibe-cards; the rest is filled by the server.
type Usage struct {
	// The client application, as it names itself ("vibe-accounts").
	App string `json:"app"`
	// The client's own id of the thing the card pays for.
	ExternalID string `json:"externalId"`
	// A link to open that thing in the client.
	URL string `json:"url,omitempty"`
	// What to call it ("Google Ads · 812-…-4410").
	Label string `json:"label,omitempty"`

	AddedBy *Principal `json:"addedBy,omitempty"`
	AddedAt *time.Time `json:"addedAt,omitempty"`
	// The holder the usage was added for, when somebody else added it.
	OnBehalfOf *Principal `json:"onBehalfOf,omitempty"`
}

// Card is a card as everyone sees it. It never carries the number.
type Card struct {
	ID       string `json:"id"`
	OrgID    string `json:"orgId"`
	Last4    string `json:"last4"`
	Name     string `json:"name"`
	BIN      string `json:"bin"`
	Network  string `json:"network"`
	Currency string `json:"currency"`

	Status CardStatus `json:"status"`
	// Why the status is what it is, in words.
	StatusNote string    `json:"statusNote"`
	Owner      Principal `json:"owner"`
	// Who has the card now; nil: nobody.
	Holder *Holding `json:"holder"`
	Labels []string `json:"labels"`
	Limits Limits   `json:"limits"`

	SpentTotal Money `json:"spentTotal"`
	// completed + pending − refunded since local midnight in the
	// organisation's report time zone.
	SpentToday Money `json:"spentToday"`
	// The tightest per-card daily limit rule reaching the card; nil: none.
	DailyLimit *Money `json:"dailyLimit"`

	// Empty in the PLATFORM issue mode.
	IssuerAccountID *string `json:"issuerAccountId"`
	Usages          []Usage `json:"usages"`
	// What the card's issuer can do: "pause", "close", "limit_total",
	// "limit_per_txn", "rename", "number", "three_ds".
	Capabilities []string `json:"capabilities"`
	// The most recent closed holding; nil when nobody ever held it.
	LastHolding *Holding `json:"lastHolding"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Held reports whether somebody holds the card now.
func (c *Card) Held() bool { return c != nil && c.Holder != nil && c.Holder.Holder.ID != "" }

// HeldBy reports whether the principal id holds the card now.
func (c *Card) HeldBy(id string) bool { return c.Held() && c.Holder.Holder.ID == id }

// Can reports whether the card's issuer has a capability.
func (c *Card) Can(capability string) bool {
	if c == nil {
		return false
	}
	for _, s := range c.Capabilities {
		if s == capability {
			return true
		}
	}
	return false
}

// UsageOf is the card's usage for (app, externalID), or nil.
func (c *Card) UsageOf(app, externalID string) *Usage {
	if c == nil {
		return nil
	}
	for i := range c.Usages {
		if c.Usages[i].App == app && c.Usages[i].ExternalID == externalID {
			return &c.Usages[i]
		}
	}
	return nil
}

// HolderData is the cardholder name and address a merchant that checks the
// address (AVS) is given.
type HolderData struct {
	Name   string `json:"name"`
	Street string `json:"street"`
	City   string `json:"city"`
	State  string `json:"state"`
	Zip    string `json:"zip"`
	// ISO 3166-1 alpha-2.
	Country string `json:"country"`
}

// CardChange is one entry of a card's journal.
type CardChange struct {
	ID     string `json:"id"`
	CardID string `json:"cardId"`
	// "card.issued", "holding.taken", "usage.added", "number.granted", …
	// A new kind is an addition; show unknown kinds as text.
	Kind  string    `json:"kind"`
	Actor Principal `json:"actor"`
	// The person a service acted for, when it said.
	OnBehalfOf *Principal `json:"onBehalfOf"`
	Summary    string     `json:"summary"`
	At         time.Time  `json:"at"`
}
