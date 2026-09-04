# vibe-cards-client

Go client for [vibe-cards](https://github.com/fox1ck-org/vibe-cards) — the
platform's virtual-card service.

```go
import vibecards "github.com/fox1ck-org/vibe-cards-client"

cards := vibecards.New(os.Getenv("VIBE_CARDS_URL"), os.Getenv("VIBE_CARDS_API_KEY"))
```

`New` returns **nil** when the URL or key is empty, and every method is
nil-receiver safe: a service without the integration degrades instead of
panicking.

## What it does

Takes a card out of a pool for somebody (`DrawCard`) and hands it back
(`ReleaseClaim` for one subject, `ReleaseHolding` for the whole holding), claims a specific card for a subject (`AssignCard`), reads
what a subject holds (`ListAssignments`, `ListHoldings`), and — for a service that is about to present
a card to a payment form — exchanges a **single-use, two-minute ticket** for its
details (`CardDetailsFor`).

### Draw, don't issue

```go
out, err := cards.DrawCard(ctx, vibecards.DrawInput{
    HolderUserSub: ownerSub,                     // who will be spending
    SubjectApp:    vibecards.SubjectAppAccounts, // what it funds
    SubjectType:   vibecards.SubjectTypeAccount,
    SubjectID:     accountID.String(),
})
if out.NoStock() {
    // The pool is empty. Wait for it to be topped up — this is a supply
    // problem, not a failure of the request.
}
```

`DrawCard` hands over a card the estate already owns; `RequestCard` **issues** a
new one at the provider and costs a fee every time. Prefer the draw.

A card nobody returns is a card the estate pays to replace, so release it when
the subject is finished with it — a dead account, a replaced funding source, a
handover:

```go
_, err := cards.ReleaseClaim(ctx, assignmentID, "account died")
```

`ReleaseClaim` returns the claim your subject made and hands the card back only
when nothing else was funded from it. Reach for `ReleaseHolding` only when the
**holder** is done with the card: it revokes every claim under the holding,
including the ones another of that person's subjects is spending through.

Releasing also drops the card's limit to zero once nobody is left on it, which
is what makes a leaked number from a card sitting in stock worth nothing.

The details are never persisted by either side. They exist for the length of one
outbound request. A person has no step in which they need them: `IssuePANGrant`
refuses a JWT caller outright and requires an API key carrying the `pan:redeem`
scope.

## Why this repository is public

A client is imported by services whose CI cannot read a private sibling repo. A
GitHub deploy key is scoped to one repository and cannot be shared, and two keys
in one agent do not work — ssh offers whichever the server accepts first and the
other repository comes back "not found". `vibe-proxy-client` is public for the
same reason.

Nothing here is a secret: protocol types and HTTP paths. Reaching vibe-cards
still needs a `vck_` key against a private endpoint.

## Releasing

Tag in lockstep with the vibe-cards API it speaks to:

```
git tag v0.2.0 && git push origin v0.2.0
```
