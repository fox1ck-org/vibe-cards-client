// Conformance: the v2 client against a real Connect server built from a copy
// of vibe-cards' proto/cards/v2. A separate module so that connect-go,
// protobuf and the generated code never enter a consumer's module graph —
// the client module itself depends on the standard library only.
module github.com/fox1ck-org/vibe-cards-client/v2/conformance

go 1.26.0

require (
	buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go v1.36.12-20260825204119-511051f7f437.2
	connectrpc.com/connect v1.21.1
	github.com/fox1ck-org/vibe-cards-client/v2 v2.0.0
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260803160001-6ac0973c030d
	google.golang.org/protobuf v1.36.12
)

replace github.com/fox1ck-org/vibe-cards-client/v2 => ../
