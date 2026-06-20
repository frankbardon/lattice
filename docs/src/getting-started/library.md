# Using lattice as a library

Beyond the `lattice` binary, the resolver and write pipeline are usable directly
from another Go module. The **entire public surface is a single package**,
`github.com/frankbardon/lattice/service` — a transport-agnostic facade over the
resolver, the changeset (RFC 6902) write pipeline, and the storage backends.

Everything else lives under `internal/` and is not importable by Go's
internal-package rule. An external module programs against `service.*` (plus the
root [`errors`](../reference/error-codes.md) package) and never names an
`internal/...` path. The CLI, and the future WASM and MCP frontends, are each a
thin adapter over this same facade.

## Install

```sh
go get github.com/frankbardon/lattice@v0.4.0
```

```go
import "github.com/frankbardon/lattice/service"
```

## Two ways to construct a Service

Both return the same wired facade and expose the same methods.

| Constructor | Use when |
| --- | --- |
| `service.Open(Options{Backend, Root, Schemas})` | Batteries-included, real filesystem. Wires a store rooted at `Root` and a resolver over the `Schemas` `fs.FS`. |
| `service.New(store, res)` | Injection. Wires an already-built `Store` and `*Resolver` without touching the filesystem — pair with `NewStore` / `NewResolver`. |

`Options.Schemas` is a stdlib `fs.FS`: an `os.DirFS("schemas")` on a real disk,
or an `embed.FS` whose top level holds `dashboard.schema.json` and the item-type
catalog. Documents are addressed by their `manifest.id`, so a document with id
`example-minimal` lives at `<Root>/example-minimal.json`.

## Open, Resolve, Patch

```go
svc, err := service.Open(service.Options{
	Backend: service.BackendFS,  // or service.BackendGit for write history
	Root:    ".",                // directory of <id>.json documents
	Schemas: os.DirFS("schemas"),
})
if err != nil {
	// every failure is an *errors.CodedError with a stable Code + Details map
}

// Resolve loads ./example-minimal.json by manifest.id and runs the two-pass
// resolver. The second argument is the runtime override map (nil applies none).
tree, err := svc.Resolve("example-minimal", nil)

// A validated RFC 6902 edit: ParseChangeset returns an opaque handle; Patch runs
// the atomic apply -> validate -> re-resolve -> save pipeline and persists only
// on full success. Pointers are id-rooted ("/<node-id>/config/..."), with
// document scopes under a "$"-prefixed token ("/$manifest", "/$theme").
cs, err := svc.ParseChangeset([]byte(
	`[{"op":"replace","path":"/$manifest/title","value":"Renamed"}]`))
res, err := svc.Patch("example-minimal", cs)
```

## The verb set

A `*Service` built either way exposes:

- **Read** — `Resolve(id, overrides)` and `ResolveBytes(b, src, overrides)` run
  the two-pass resolver (store-addressed vs. in-memory); `Load(id)`, `List()`,
  `Exists(id)` are byte-level store reads.
- **Write** — `ParseChangeset(b)` → opaque `*Changeset`; `Patch(id, cs, opts...)`
  applies it atomically (pass `WithExpectedRevision(rev)` for an
  optimistic-concurrency precondition); `Save(document)` writes **unvalidated**
  whole bytes; `Delete(id)` removes a document.
- **History** — `History(id)` and `LoadAt(id, rev)` read version history (git
  backend only); `Revision(id)` returns the current token to pair with
  `WithExpectedRevision`. These are capability-gated: a backend that lacks the
  capability is rejected with `STORAGE_CAPABILITY_UNSUPPORTED` rather than
  degrading silently.

## Injection path (custom store / embedded schemas)

When the store or schema catalog should not come from the OS filesystem — an
in-memory store, a custom `Store` implementation, or schemas baked in via
`embed.FS` — build the pieces with the low-level builders and wire them with
`New`. This is the path the future WASM and MCP frontends use.

```go
//go:embed schemas
var schemaFS embed.FS

res, err := service.NewResolver(schemaFS)  // fs.FS: os.DirFS or embed.FS
store, err := service.NewStore(service.BackendFS, afero.NewMemMapFs(), "docs")

svc := service.New(store, res)             // same verb set as Open returns
```

`NewStore(backend, fs, root)` takes an `afero.Fs`, so any afero-backed
filesystem works; `NewResolver(schemas)` takes a stdlib `fs.FS`.

## Compile-checked examples

Both flows above exist as Go `Example` functions in `service/example_test.go`,
so the documented usage is verified against the real signatures by
`go test ./service/...` and cannot drift silently.
