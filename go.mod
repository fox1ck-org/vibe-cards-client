// Package vibecards is the Go client for vibe-cards.
//
// A separate repository, and a PUBLIC one, for the same reason
// vibe-proxy-client is: a client is imported by services whose CI cannot read a
// private sibling repo, and a deploy key cannot be shared across repositories.
// It carries no secrets — protocol types and HTTP paths — and nothing here can
// be used without a `vck_` key against a private endpoint.
module github.com/fox1ck-org/vibe-cards-client

go 1.25
