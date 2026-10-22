// Package vibecards is the Go client for the cards.v2 API of vibe-cards.
//
// cards.v2 is the card model as a person thinks about it: a card, whose it
// is, who has it now, who it is shared with, where it is used. There are no
// pools, no "free cards", no draw and no assignment any more:
//
//   - Take puts a card in somebody's hands, Return gives it back, Transfer
//     hands it on. "Any card labelled uk" is List with that filter and
//     holder "none", then Take of the one picked — TakeFirst does exactly
//     that and moves to the next card when another taker won the race.
//   - AddUsage records where the card works (four opaque strings the client
//     chooses: app, external id, url, label); RemoveUsage forgets one.
//   - A SERVICE presents a card with a single-use ticket: IssueGrant, then
//     Redeem at once (CardDetails does both). A 3DS code is WaitThreeDS.
//   - Share gives a person VIEW or USE of a card, of a label, or of all of
//     an owner's cards.
//
// Refusals are *Error values carrying the server's google.rpc.ErrorInfo
// reason. Branch with errors.Is against the Err* reasons (ErrHeldByOther,
// ErrCardNotActive, ErrCardUnreadable, ErrNoHolderProfile, …) — never on the
// message text.
//
// A service that acts for a person says so per call with OnBehalfOf; a
// service forwarding a person's own token (a BFF) uses WithToken. Both are
// context values, so one Client serves every caller.
//
// Like v1, the package has no dependencies beyond the standard library: it
// speaks Connect's unary JSON protocol directly, so importing it adds
// nothing to a consumer's module graph. The conformance module next to it
// proves the wire format against handlers generated from the same proto.
//
// Every method is nil-receiver safe and answers ErrNotConfigured on a nil
// Client, which is what New returns without a base URL — a service with no
// card integration degrades instead of panicking.
package vibecards
