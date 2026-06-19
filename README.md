# Lattice

A declarative **JSON format for describing dashboards**, plus `dashspec` — a Go
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

Produces the CLI at `bin/dashspec`. The build is **pure Go** — `CGO_ENABLED=0`
is enforced globally so no C-toolchain dependency can enter the build graph.
Requires Go 1.26+.

## Run

`dashspec` has two subcommands.

### `resolve`

Validate a document and print its resolved tree as JSON:

```sh
dashspec resolve examples/minimal-dashboard.json
```

Flags: `--schemas <dir>` (catalog directory, default `schemas`) and the global
`--json` (emit failing errors as a `{code,message,details}` envelope). Documents
that reference a secret need it set in the environment, e.g.:

```sh
METRICS_API_TOKEN=xyz dashspec resolve examples/kitchen-sink-dashboard.json
```

The secret value never appears in the output.

### `serve`

Serve a document over HTTP — an HTML structural sketch plus a JSON
resolved-tree endpoint, re-resolved per request with AlpineJS-driven runtime
inputs (dropdown selections and `?var=value` URL params):

```sh
dashspec serve examples/dropdown-dashboard.json   # http://localhost:8080
```

Flags: `--schemas <dir>` and `--port <n>` (default 8080).

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
