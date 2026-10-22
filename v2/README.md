# vibe-cards-client/v2

Go client for the **cards.v2** API of
[vibe-cards](https://github.com/fox1ck-org/vibe-cards). Standard library only.

```go
import vibecards "github.com/fox1ck-org/vibe-cards-client/v2"

cards := vibecards.New(os.Getenv("VIBE_CARDS_URL"), os.Getenv("VIBE_CARDS_API_KEY"))
```

`New` returns **nil** without a URL and every method is nil-receiver safe
(`ErrNotConfigured`), so a service without the integration degrades instead of
panicking.

The repository root contains no Go module. Import this `/v2` module.

## The model in one paragraph

A card has an owner, at most one holder, labels, and usages (where it works).
There are no pools and no "free cards": you **Take** a card that is yours or
shared with you for USE, **AddUsage** to say where it works, **Return** it when
done. "Any card labelled uk" is `List{labels: [uk], holder: [none]}` and a
Take of the one picked — `TakeFirst` does that and moves on when somebody else
won the race.

## A service, for a person

```go
// The person who pressed the button. Requires the key's `on_behalf` scope;
// without it vibe-cards ignores the header and the service acts as itself.
ctx = vibecards.OnBehalfOf(ctx, buyerSub, buyerEmail)
usage := vibecards.Usage{App: "vibe-accounts", ExternalID: accountID, URL: accountURL, Label: "UK-01"}

card, err := cards.TakeFirst(ctx, vibecards.TakeFirstInput{
    Filter: vibecards.Filter{Labels: []string{"uk", "google"}, SharedWith: []string{buyerSub}},
    Holder: &vibecards.Principal{Kind: vibecards.PrincipalPerson, ID: buyerSub},
    Limits: &vibecards.Limits{Total: ptr(vibecards.USD(30000))},
    Usage:  &usage, // recorded with the take
})
switch {
case errors.Is(err, vibecards.ErrNoCardAvailable):
    // Nothing matching is free: issue one (cards.Issue — costs a fee) or wait.
case err != nil:
    return err
}

// Present it: a single-use ticket, redeemed at once. Never store the result.
since := time.Now()
details, err := cards.CardDetails(ctx, card.ID, &usage)
switch {
case errors.Is(err, vibecards.ErrCardUnreadable):
    // The issuer hides its details; the card is quarantined. Return it, take another.
case errors.Is(err, vibecards.ErrNoHolderProfile):
    // Configuration: no holder name/address resolves. Retrying will not help.
}
submitPaymentForm(details)

code, err := cards.WaitThreeDS(ctx, card.ID, since, 2*time.Minute) // nil, nil: nothing arrived

// Done with it (the account died, funding replaced):
_, err = cards.Return(ctx, card.ID, "account died")
```

Under `OnBehalfOf` the service **is that person** to vibe-cards (since
vibe-cards К1-3b): it sees and may use what the person sees and may use, with
the key's own scopes on top (`pan:redeem`, taking for somebody else), never an
admin's reach; `Me` in a filter or principal is the person too. Without
`OnBehalfOf` a service key sees only what was shared with the service itself —
usually nothing — so name the person on every call that selects cards.

## Find a card by where it works

No need to keep card ids: the usage you added is the key.

```go
list, err := cards.List(ctx, vibecards.ListInput{Filter: vibecards.Filter{
    UsageApp: "vibe-fb", UsageExternalID: adAccountID, // or UsageApp alone: every card working for you
}})
```

A removed usage (`RemoveUsage`, or `Return`, which drops the holding's usages)
no longer selects its card. `UsageExternalID` without `UsageApp` is refused.

## Why a card is stopped, what replaced it, how old it is

`Card.StatusReason` says who stopped a card and who can start it again:
`USER_PAUSED` (a person; Resume), `PLATFORM_HOLD` (a person, but the issuer
cannot stop cards — it still spends there), `RULE_PAUSED` (a rule's breach;
lifts by itself), `ISSUER_BLOCKED` (the issuer's own block — nothing here lifts
it, so replace the card: `card.IssuerBlocked()`), `QUARANTINE`, `REPLACED`.
A reissued card is CLOSED with `ReplacedBy` (id, last4, when linked); its
successor carries `Replaces`. `SyncedAt` is when the issuer's record of the
card was last read — "issuer data N ago".

## Errors

Every refusal is a `*vibecards.Error` carrying the server's
`google.rpc.ErrorInfo.reason`. Branch with `errors.Is` — never on the message:

| reason | error | what to do |
|---|---|---|
| `held_by_other` | `ErrHeldByOther` | someone else has it — take another |
| `card_not_active` | `ErrCardNotActive` | paused / quarantined / closed |
| `not_shared_for_use` | `ErrNotSharedForUse` | the holder may only VIEW it |
| `card_unreadable` | `ErrCardUnreadable` | quarantined — return, take another |
| `no_holder_profile` | `ErrNoHolderProfile` | configuration, not transient |
| `grant_spent` | `ErrGrantSpent` | ticket expired or already redeemed |
| `in_progress` | `ErrInProgress` | retry with the **same** request id |
| `issuer_unsupported` | `ErrIssuerUnsupported` | `err.(*Error).MissingCapability()` names it |

…and one `Err*` for every other `cards.v2.ErrorReason` (`ReasonOf(err)` gives
the raw reason). `ErrUnauthenticated` / `ErrPermissionDenied` match by Connect
code.

## A backend for a person's screen

```go
ctx = vibecards.WithToken(ctx, personAccessToken) // the caller's own token
list, err := cards.List(ctx, vibecards.ListInput{
    Filter: vibecards.Filter{Holder: []string{vibecards.Me}}, OrderBy: "-spent_today",
})
n, err := cards.RevealNumber(ctx, cardID, "support ticket 1234") // people only, on the record
```

A vibe-desk token (`azp = vibe-desk`, admitted by `READER_ACCEPTED_AZP`) opens
the reads only — `List`, `Get`, `ListChanges`; everything that changes a card
answers `ErrUnauthenticated` to it.

## Bulk over a whole organisation

Up to 1000 selected cards `Bulk` applies the action and answers the outcome.
Beyond that it answers at once with an operation to follow:

```go
res, err := cards.Bulk(ctx, vibecards.BulkInput{Filter: vibecards.Filter{Labels: []string{"uk"}}, Action: vibecards.BulkPause})
if res.Background() {
    for {
        op, err := cards.GetBulkOperation(ctx, res.OperationID) // starter or admin only
        if err != nil || op.State.Finished() { break }
        time.Sleep(5 * time.Second) // op.Done of op.Total
    }
}
```

## What is covered

Card (Issue, Get, List, Update, Pause, Resume, Close, Bulk, GetBulkOperation,
RevealNumber, ListChanges), Holding (Take, TakeFirst, Return, Transfer,
AddUsage, RemoveUsage), Share (Share, Unshare, ListShares), Pan (IssueGrant,
Redeem, CardDetails, WaitThreeDS), Spend (Query), Issuing (TopUp — money to a
person's account at the issuer; raising one card's limit is `Update` with
`Limits`). Labels, rules and the rest of the issuing settings are the
console's business and are not wrapped.

## Conformance

`conformance/` is a separate module (connect-go and the generated code never
reach a consumer). It holds a copy of vibe-cards' `proto/cards/v2`, Go code
generated from it with **pinned** buf plugins, and a test that runs this client
against those handlers with a strict JSON decoder, under both vibe-cards'
production codec and canonical protojson. It also fails when the proto gains
an `ErrorReason` the client has no error for.

Refresh after a cards.v2 change (from the vibe-cards checkout at the new commit):

```sh
cp ../../../finance/vibe-cards/proto/cards/v2/*.proto conformance/proto/cards/v2/
cd conformance/proto && buf generate && cd .. && go test ./...
```

The copy is of vibe-cards #115 (К1-3c: status_reason, replaced_by/replaces, synced_at).

## Releasing

Tag in lockstep with the vibe-cards API it speaks to. The module lives in the
`v2/` major-version subdirectory, so its tags have no prefix:

```sh
git tag v2.2.0 && git push origin v2.2.0
```

## Issuer references

`ListAccounts(ctx)` reads the organisation's own issuer connections (kind, name,
balance and capabilities), without credentials. It needs organisation-wide
issuing read access. `Card.IssuerAccountID` joins a card to an account.

`Transaction.IssuerReference` is the issuer's stable operation identifier,
available with OWN issuing read access and empty in PLATFORM mode or when
unknown. Keep it distinct from `Transaction.ID`, the card service's identifier.

Canonical service-account delegation uses `v2.WithToken(ctx, saToken)` and
`v2.WithOnBehalf(ctx, orgID, canonicalPersonID)`. An empty person means org-scoped
system work; the server still enforces the service's permissions. Resolve
historical issuer-qualified subjects through the directory first. No role or
email is supplied. The explicit delegation helper selected last replaces the
other protocol's headers. Existing `OnBehalfOf` remains the legacy service-key
protocol; enable canonical delegation only after the producer supports it.
