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

The v1 client (`github.com/fox1ck-org/vibe-cards-client`, cards.v1: pools,
draw, assignments) lives at the repository root until every consumer has
moved; then cards.v1 is deleted.

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

`Me` in a filter or principal is always the **caller** — under `OnBehalfOf`
still the service — so name the person explicitly.

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

## What is covered

Card (Issue, Get, List, Update, Pause, Resume, Close, Bulk, RevealNumber,
ListChanges), Holding (Take, TakeFirst, Return, Transfer, AddUsage,
RemoveUsage), Share (Share, Unshare, ListShares), Pan (IssueGrant, Redeem,
CardDetails, WaitThreeDS), Spend (Query). Labels, rules and issuing
settings are the console's business and are not wrapped.

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

The copy is of vibe-cards `cc285c8` (#111).

## Releasing

Tag in lockstep with the vibe-cards API it speaks to. The module lives in the
`v2/` major-version subdirectory, so its tags have no prefix:

```sh
git tag v2.0.0 && git push origin v2.0.0
```
