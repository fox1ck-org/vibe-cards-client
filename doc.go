// Package vibecards is the typed door into vibe-cards for other Go services.
//
// It is its own repository, and a public one, for exactly the reason
// vibe-proxy-client is: a client is imported by services whose CI cannot read a
// private sibling repo. A GitHub deploy key is scoped to one repository and
// cannot be shared, and two keys in one agent do not work — ssh offers whichever
// the server accepts first and the other repo comes back "not found". Making
// the client public removes the credential from the problem entirely.
//
// It carries no secrets: protocol types and HTTP paths. Nothing here reaches
// vibe-cards without a `vck_` API key against a private endpoint.
//
// The platform rule that a client lives WITH its service still holds in spirit:
// this repository is released in lockstep with vibe-cards, and a consumer
// pinning an old tag is choosing to lag, not drifting by accident.
//
// There are two ways to get a card, and they mean different things:
//
//   - DrawCard takes one out of a pool the estate already owns and hands it
//     back with ReleaseClaim when the subject is done (ReleaseHolding is the
//     holder's door and revokes every claim on the card). It costs nothing, so
//     it does not have to be gated on somebody agreeing to spend.
//   - RequestCard ISSUES one at the provider, which costs a fee every time.
//     That is why automatic issuing is switched off in production. Prefer
//     DrawCard; reach for RequestCard only where a new card is genuinely the
//     point.
//
// A card that nobody releases is a card the estate pays to replace, so the
// release is not optional politeness: a dead account, a replaced funding
// source or a handover should all give the card back.
//
// Two things it deliberately does NOT do:
//
//   - It does not model a Facebook ad account, or an anti-detect account, or
//     anything else a subject might be. A subject is three opaque strings.
//   - It does not hold card details. RedeemPANGrant returns them to the caller
//     and keeps no copy; they are meant to live for the length of one outbound
//     request and then be forgotten.
//
// Every method is nil-receiver safe and a nil client is what New returns when
// the base URL or key is empty, so a consumer that has not been configured for
// vibe-cards degrades instead of panicking — the same contract vibe-accounts'
// proxy reader offers.
package vibecards
