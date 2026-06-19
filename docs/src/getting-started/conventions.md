# Conventions

Lattice mirrors the engineering conventions of its sibling project
[pulse](https://github.com/frankbardon/pulse). pulse is a convention source
only — it is not a runtime dependency. The conventions below apply to the
codebase; document authors mainly care about the format chapters, but these
explain the shape of the tooling.

## Language & build

- **Go 1.26**, module path `github.com/frankbardon/lattice`.
- **Pure Go.** `CGO_ENABLED=0` is exported globally by the Makefile, making the
  no-C-toolchain rule a build contract rather than a convention.
- The binary is built to `bin/lattice` via `make build` (`-trimpath`, stripped).

## Tooling

- **Lint** is `go vet` plus [staticcheck](https://staticcheck.dev/); there is no
  golangci-lint. `make lint` runs both.
- **Tests** use the standard library `testing` package only — no testify or
  other assertion libraries. Tests are `func TestXxx(t *testing.T)` using
  `t.Error*` / `t.Fatal*`, table-driven where it helps. Benchmarks live in
  `*_bench_test.go` and run via `make bench` (not part of `make test`).
- **Configuration is via CLI flags**, never config files.

## Code style

- Lowercase, single or compound package names; short receiver names.
- Enums are typed string constants: `type X string` with
  `const X_FOO X = "X_FOO"`.
- Every serialized type carries JSON struct tags — the **serialized form is the
  contract**, not the Go field names.

## Error handling — coded errors

All failures are `CodedError` values carrying a typed `Code`, a message,
optional structured `Details`, and an optional wrapped cause. Codes are grouped
by domain (`RESOLVE_*`, `SCHEMA_*`, `VAR_*`, `CONNECTION_*`, `LAYOUT_*`,
`SERVE_*`). `CodedError` implements `json.Marshaler`, which is what powers the
`--json` output. Resolution is **fail-fast**: the first coded error is returned.
The full list is in [Error Codes](../reference/error-codes.md).

## Schema-catalog conventions

- All schema `$id`s share the stable base `https://lattice.dev/schemas/`. This
  base is an **identifier namespace, not a live fetch target** — the resolver
  loads schemas from the local catalog and keys them by `$id`.
- Item-type and connection-type schemas embed a **semver in the path**, so
  multiple versions can coexist:
  `https://lattice.dev/schemas/<name>/<major>.<minor>.<patch>`.

These are detailed in [Schema Catalog](../format/catalog.md).

## Documentation

This book is an [mdBook](https://rust-lang.github.io/mdBook/). Build it with
`mdbook build docs` (output goes to `docs/book/`, which is gitignored). The live
site is published from `docs/src/` to
<https://frankbardon.github.io/lattice/>.
