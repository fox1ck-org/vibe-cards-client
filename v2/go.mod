// The cards.v2 client. Its own module, with the /v2 major-version suffix,
// beside the v1 client at the repository root: services still on cards.v1
// keep importing github.com/fox1ck-org/vibe-cards-client until they move.
//
// It depends on the standard library only, as v1 does — a consumer
// importing it inherits nothing. The conformance test that runs it against a
// real Connect server lives in ./conformance, a separate module, so its
// generated code and connect-go never reach a consumer's module graph.
module github.com/fox1ck-org/vibe-cards-client/v2

go 1.25
