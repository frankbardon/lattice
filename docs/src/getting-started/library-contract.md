# Library API Contract

This page is the reference companion to [Using as a Library](library.md). That
page walks through *how* to construct and drive a `Service`; this one states the
two non-obvious rules a public caller must understand to use the surface
correctly, plus the stability intent behind it.

## The public surface

The **entire** public Go surface of lattice is the single package
`github.com/frankbardon/lattice/service`, plus the root
[`errors`](../reference/error-codes.md) package. Everything else lives under
`internal/` and is not importable by Go's internal-package rule. This is the
**v0.4.0 contract**: `service` is *the* public seam, the cores stay `internal/`
so their shapes remain free to change, and each transport (the CLI, and the
planned WASM and MCP frontends) is a thin adapter over this one facade.

## Rule 1 — Boundary types are opaque handles

The boundary types `service` re-exports — `ResolvedTree`, `Changeset`,
`ApplyResult`, `ApplyOption`, `Store`, `RevisionedStore`, `VersionedStore`,
`Revision`, `Resolver`, `OverrideSet`, `Backend` — are Go **type aliases** (`=`)
of their `internal/...` definitions, not new named types. A `service.ResolvedTree`
*is* the `resolver.ResolvedTree` the cores produce, so no conversion happens at
the seam.

What an alias gives you and what it does not:

- You **can name and read** these types through `service.*` — declare variables of
  them, range over `[]service.Revision`, read fields like `Revision.Hash` or
  `ResolvedTree.Manifest`.
- You **cannot construct** the ones carrying unexported fields (`Resolver`, the
  `Store` implementations, `Service` itself) by building a struct literal —
  Go forbids setting unexported fields from outside the defining package.

Instead, you obtain values of these types only from the provided constructors and
methods:

| To get a… | Call |
| --- | --- |
| `*Service` | `Open(Options{...})` or `New(store, res)` |
| `*Resolver` | `NewResolver(schemas)` |
| `Store` | `NewStore(backend, fs, root)` |
| `*Changeset` | `svc.ParseChangeset(b)` |
| `ResolvedTree` | `svc.Resolve(...)` / `svc.ResolveBytes(...)` |
| `ApplyResult` | `svc.Patch(...)` |
| `ApplyOption` | `WithExpectedRevision(rev)` |

Why it is built this way: keeping construction under the facade keeps the
invariants those values carry — structural and config validation, apply
atomicity, byte-faithful storage — under the facade's control too. A caller can
never hand a half-built `Resolver` or an unwired `Service` to a method, because it
cannot build one. The boundary types behave as **opaque handles**: name them,
read them, pass them back to the facade — but mint them only through the
constructors above.

## Rule 2 — Capabilities are flat, probed by error code

Version history and the current-revision token are **optional store
capabilities**, and they are *not* uniform across backends. Rather than expose a
separate capability-checking API, `service` makes them flat methods on `*Service`
that return a coded error when the wired backend lacks the capability.

### Capability matrix

| Method | Capability needed | `BackendFS` | `BackendGit` |
| --- | --- | --- | --- |
| `History(id)` | `VersionedStore` | ✗ | ✓ |
| `LoadAt(id, rev)` | `VersionedStore` | ✗ | ✓ |
| `Revision(id)` | `RevisionedStore` | ✓ | ✓ |

The filesystem backend derives a content hash for `Revision`, so the
optimistic-concurrency precondition works on both backends; only read-side
*history* (`History` / `LoadAt`) is git-only. A custom or injected `Store` may
implement neither.

### Probing a capability

There is no `Supports(...)` call. A caller probes by **calling the method and
checking the error code**: when the backend lacks the capability, the method
returns an `*errors.CodedError` with code
[`STORAGE_CAPABILITY_UNSUPPORTED`](../reference/error-codes.md#storage_--whole-document-persistence)
(carrying `Details["id"]`) rather than degrading silently.

```go
import "github.com/frankbardon/lattice/errors"

revs, err := svc.History("example-minimal")
switch {
case err == nil:
	// backend supports history; revs is usable (newest-first)
case errors.HasCode(err, errors.STORAGE_CAPABILITY_UNSUPPORTED):
	// the wired backend is not a VersionedStore (e.g. BackendFS) —
	// fall back, or surface "history unavailable" to the user
default:
	// a real failure (e.g. STORAGE_NOT_FOUND for an unknown id)
}
```

`errors.HasCode(err, code)` is the supported way to branch on a code; the same
pattern applies to `LoadAt` and `Revision`.

### Optimistic concurrency

`Revision(id)` returns the **opaque** current-revision token (compare-only —
never parse it). Pass it to a `Patch` via `WithExpectedRevision(rev)` to make the
write conditional: the apply re-reads the store's current revision immediately
before saving and, on mismatch, rejects the whole changeset with code
[`CHANGESET_REVISION_CONFLICT`](../reference/error-codes.md#changeset_-patch_--the-json-patch-write-pipeline)
— nothing is persisted, so you can reload, re-derive the changeset against the
new bytes, and retry.

```go
rev, err := svc.Revision("example-minimal")          // current token
// ... build cs ...
res, err := svc.Patch("example-minimal", cs, service.WithExpectedRevision(rev))
if errors.HasCode(err, errors.CHANGESET_REVISION_CONFLICT) {
	// the document changed since you read it — reload and retry
}
```

(If you supply `WithExpectedRevision` to a store that does not implement
`RevisionedStore` at all, the apply is rejected with
`CHANGESET_REVISION_UNSUPPORTED` rather than ignoring the precondition you asked
for.)
