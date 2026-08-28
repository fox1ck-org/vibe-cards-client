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

Claims a card for a subject (`RequestCard`, `AssignCard`), reads the claims a
subject holds (`ListAssignments`), and — for a service that is about to present
a card to a payment form — exchanges a **single-use, two-minute ticket** for its
details (`CardDetailsFor`).

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
