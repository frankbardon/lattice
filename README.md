# Lattice

A declarative **JSON format for describing dashboards**, plus `lattice` — a Go
tool that loads, validates, and **resolves** those documents into a flat,
renderer-agnostic tree.

A dashboard document is a single JSON file: a `manifest`, a recursive `root`
item tree, optional document-scoped `variables`, and optional document-scoped
`connections` (data sources). Item types and connection types are described by
versioned JSON Schemas in a local catalog; each node in the document is an
**instance** of one of those types, referenced by `$ref`.

> **Scope.** This repository delivers the **format, the schema catalog, and the
> resolver**. It deliberately does **not** dial connections, render real charts,
> apply styling, or refresh data. See the spec's
> [Out of Scope](https://frankbardon.github.io/lattice/reference/out-of-scope.html)
> page for the precise boundary.

## Build

```sh
make build
```

Produces the CLI at `bin/lattice`. The build is **pure Go** — `CGO_ENABLED=0`
is enforced globally so no C-toolchain dependency can enter the build graph.
Requires Go 1.26+.

## Run

`lattice` has three subcommands.

### `resolve`

Validate a document and print its resolved tree as JSON:

```sh
lattice resolve examples/minimal-dashboard.json
```

Flags: `--schemas <dir>` (catalog directory, default `schemas`) and the global
`--json` (emit failing errors as a `{code,message,details}` envelope). Documents
that reference a secret need it set in the environment, e.g.:

```sh
METRICS_API_TOKEN=xyz lattice resolve examples/kitchen-sink-dashboard.json
```

The secret value never appears in the output.

### `serve`

Serve a document over HTTP — an HTML structural sketch plus a JSON
resolved-tree endpoint, re-resolved per request with AlpineJS-driven runtime
inputs (dropdown selections and `?var=value` URL params):

```sh
lattice serve examples/dropdown-dashboard.json   # http://localhost:8080
```

Flags: `--schemas <dir>` and `--port <n>` (default 8080).

When run in **backend mode** (`--store`/`--root`, addressing a document by its
manifest id) the server also exposes a write endpoint:

```
POST /api/patch   {"id": "<id>", "ops": [<RFC 6902 id-rooted JSON Patch>], "expectedRevision": "<optional>"}
```

It commits the changeset through the atomic apply→validate→save pipeline and
returns `{"revision": "<new>", "result": <resolved tree>}`. A stale
`expectedRevision` yields `409` (`CHANGESET_REVISION_CONFLICT`); an unknown id
`404`; a malformed/off-surface/invalid changeset `422` — each as a coded-error
JSON envelope. The route is disabled (read-only server) in path mode.

> **Security:** the HTTP server, including `POST /api/patch`, has **no
> authentication or authorization**. It assumes a localhost/trusted deployment.
> Do not expose it on an untrusted network — any caller could mutate stored
> documents. This is a known, accepted gap.

### `mcp`

Run lattice as an [MCP](https://modelcontextprotocol.io/) server over **stdio**,
exposing its read and dry-run capabilities as tools an MCP host (a coding
assistant, an agent) can call:

```sh
lattice mcp --store fs --root ./dashboards --schemas schemas
```

Flags: `--store`/`--root` (backend selection, as for `resolve`/`serve`) and
`--schemas <dir>`. The server advertises seven tools — `list_dashboards`,
`get_outline`, `get_node`, `get_document`, `list_schemas`, `get_schema`, and
`validate_patch` — all **read or dry-run only**. The model navigates a document,
drills into a node or fetches a type schema, and **simulates** an edit with
`validate_patch` (the same apply→validate pipeline as a real write, minus the
save). It **never persists**: a validated patch is committed separately by a
human through the `POST /api/patch` endpoint above, passing the `baseRevision`
`validate_patch` returned as `expectedRevision`.

> **Security:** like the HTTP write endpoint, MCP mode has **no authentication**
> and assumes a localhost/trusted deployment — a known, accepted gap.

The full tool reference, host-config snippet, and the propose-then-commit
walkthrough are in the spec's
[MCP Mode](https://frankbardon.github.io/lattice/reference/mcp.html) page.

## Using lattice as a library

Lattice's entire public Go surface is the single package
`github.com/frankbardon/lattice/service` — a transport-agnostic facade over the
resolver, the changeset write pipeline, and the storage backends. Everything
else lives under `internal/` and is not importable; an external module programs
against `service.*` (plus the root `errors` package) and never names an
`internal/...` path.

Two rules govern this surface — boundary types are opaque handles (name and read
them, but construct only via the facade's constructors), and store capabilities
(`History` / `LoadAt` / `Revision`) are probed by error code rather than a
feature flag. Both, plus the FS-vs-git capability matrix, are spelled out in the
[Library API Contract](docs/src/getting-started/library-contract.md).

Add the module and import the facade:

```sh
go get github.com/frankbardon/lattice@v0.4.0
```

```go
import "github.com/frankbardon/lattice/service"
```

### Open + Resolve + Patch (filesystem)

The batteries-included `Open` constructor wires a filesystem-backed store rooted
at a directory plus a resolver over a schema-catalog `fs.FS`. Documents are
addressed by their `manifest.id`, so a document with id `example-minimal` lives
at `<Root>/example-minimal.json`.

```go
package main

import (
	"fmt"
	"os"

	"github.com/frankbardon/lattice/service"
)

func main() {
	svc, err := service.Open(service.Options{
		Backend: service.BackendFS,        // or service.BackendGit for write history
		Root:    ".",                       // directory of <id>.json documents
		Schemas: os.DirFS("schemas"),       // holds dashboard.schema.json + the catalog
	})
	if err != nil {
		panic(err)
	}

	// Resolve loads ./example-minimal.json by manifest.id and runs the two-pass
	// resolver. The second argument is the runtime override map (nil applies none).
	tree, err := svc.Resolve("example-minimal", nil)
	if err != nil {
		panic(err)
	}
	fmt.Println("resolved", tree.Manifest["id"])

	// Apply a validated RFC 6902 edit. ParseChangeset returns an opaque handle;
	// Patch runs the atomic apply -> validate -> re-resolve -> save pipeline and
	// persists only on full success. Pointers are id-rooted ("/<node-id>/config/...")
	// with document scopes under a "$"-prefixed token ("/$manifest", "/$theme").
	cs, err := svc.ParseChangeset([]byte(
		`[{"op":"replace","path":"/$manifest/title","value":"Renamed"}]`))
	if err != nil {
		panic(err)
	}
	if _, err := svc.Patch("example-minimal", cs); err != nil {
		panic(err)
	}
}
```

Every failure path returns an `*errors.CodedError`
(`github.com/frankbardon/lattice/errors`) carrying a stable `Code`, a message,
and a `Details` map — type-assert or use `errors.HasCode` to branch on it.

### Injection path (custom store / embedded schemas)

When the store or schema catalog should not come from the OS filesystem — an
in-memory store, a custom `Store` implementation, or schemas baked in via
`embed.FS` — build the pieces yourself with the low-level builders and wire them
with `New` instead of `Open`. This is the path the future WASM and MCP frontends
use, where the document and catalog live in memory rather than on disk.

```go
//go:embed schemas
var schemaFS embed.FS

// A resolver over embedded schemas (the embed.FS sub-tree whose top level holds
// dashboard.schema.json), and any Store you like — here an in-memory one.
res, err := service.NewResolver(schemaFS)        // fs.FS: os.DirFS or embed.FS
store, err := service.NewStore(service.BackendFS, afero.NewMemMapFs(), "docs")

svc := service.New(store, res)                   // same verb set as Open returns
```

`NewStore(backend, fs, root)` takes an `afero.Fs`, so any afero-backed
filesystem (in-memory, OS, read-only overlay) works; `NewResolver(schemas)`
takes a stdlib `fs.FS`. A `*Service` built either way exposes the same methods:
reads (`Resolve`, `ResolveBytes`, `Load`, `List`, `Exists`), writes
(`ParseChangeset`, `Patch`, `Save`, `Delete`), and git-only history
(`History`, `LoadAt`, `Revision`).

A compile-checked version of both examples lives in
[`service/example_test.go`](service/example_test.go) (Go `Example` functions),
so the documented usage cannot drift from the real signatures.

## Conventions

Lattice mirrors the engineering conventions of its sibling project
[pulse](https://github.com/frankbardon/pulse) (a convention source, not a
dependency):

- **Go 1.26**, pure Go (`CGO_ENABLED=0`), module `github.com/frankbardon/lattice`.
- **Lint:** `go vet` + [staticcheck](https://staticcheck.dev/) via `make lint`.
- **Tests:** standard-library `testing` only — no testify. `make test`.
- **Errors:** a `CodedError` model with domain-prefixed typed codes
  (`RESOLVE_*`, `SCHEMA_*`, `VAR_*`, `CONNECTION_*`, `LAYOUT_*`, `SERVE_*`);
  fail-fast resolution returns the first error. `--json` emits them as JSON.
- **Config via CLI flags**, never config files.

Makefile targets: `make build` (default), `test`, `cover`, `fmt`, `vet`, `lint`,
`bench`, and `docs` / `docs-serve` / `docs-clean` for this book.

## Documentation

The full specification — document structure, the typed-schema/instance pattern,
the schema catalog, variable semantics, connection and secret handling, examples,
error codes, and the out-of-scope list — is published as an
[mdBook](https://rust-lang.github.io/mdBook/):

**<https://frankbardon.github.io/lattice/>**

The source lives in `docs/src/`; build locally with `make docs` (output goes to
`docs/book/`, which is gitignored). Worked example documents live in `examples/`,
and the schema catalog lives in `schemas/`.

## Status

In development. Built with the [Flow](https://github.com/frankbardon) planning
workflow.
