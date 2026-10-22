# vibe-cards-client

Go client for [vibe-cards](https://github.com/fox1ck-org/vibe-cards), using the
cards.v2 API. The supported module lives in [`v2/`](v2/README.md) and depends
only on the Go standard library.

```go
import vibecards "github.com/fox1ck-org/vibe-cards-client/v2"

cards := vibecards.New(os.Getenv("VIBE_CARDS_URL"), os.Getenv("VIBE_CARDS_API_KEY"))
```

See the [client guide](v2/README.md) for taking and returning cards, usages,
sharing, PAN tickets, 3DS, and typed errors. The repository root contains no Go
module; run Go commands from `v2/` or `v2/conformance/`.

## Checks

```sh
cd v2
go vet ./...
go test ./...
cd conformance
go test ./...
```

Conformance tests run against generated cards.v2 Connect handlers in a separate
module, so protobuf and Connect dependencies never enter consumers' module graphs.

## Releases

Keep the `v2/` major-version directory and publish root tags `v2.X.Y` (not
`v2/v2.X.Y`). Existing published tags are immutable. The retired root client
is available only through historical tags; current consumers import `/v2`.

## Why this repository is public

Services can download this contract in CI without access to a private sibling
repository. It contains protocol types and HTTP paths, never credentials.
The service still requires authentication.

## Deployment configuration

`DefaultBaseURL` now names a local endpoint (`http://localhost:8080`), removing
private deployment topology from the public client. Configure the service URL
explicitly. `New` still returns nil for an empty URL. Existing release tags and
history are unchanged.

The additive integration methods coordinate complete paginated reads: call
`BeginRead`, collect all pages and related metadata, then `ValidateRead` before
publishing aggregates or committing a ledger. On `ErrReadChanged` or
`ErrReadExpired`, discard the entire collection and retry a bounded number of
times. Tokens never grant access and remain bound to the same caller/authority.

`ListProductChanges` reads the retained product journal, separate from
`ListChanges` (the UI card audit timeline). Save its cursor even for an empty
filtered page. `ErrCursorInvalidated` requires a complete bootstrap. The four
identity-only event types carry stable event IDs for deduplication; obtain current
resource details through the ordinary authorized API.
