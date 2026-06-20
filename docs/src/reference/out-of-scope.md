# Out of Scope

This effort delivers the **dashboard format, the schema catalog, and the
`lattice` resolver**. Several capabilities are deliberately **not** part of it.
They are listed here so future contributors know the boundaries of what the
shipped binary actually does — and do not assume a behavior the spec does not
promise.

## Not implemented in this effort

- **Live data fetch.** Connections are *declared and validated only*. `lattice`
  never dials an `http` connection, opens a socket, or makes a network request.
  The only real data check is validating a `static` connection's inline rows
  against the result-shape contract.
- **Visual rendering.** There is no chart, table, or widget rendering engine.
  The `serve` command produces a minimal **structural sketch** plus a JSON
  resolved-tree endpoint — enough to inspect structure, not to display a finished
  dashboard.
- **Styling.** No CSS, themes, colors, fonts, or visual design. The container
  grid is expressed purely in relative, unitless track weights; mapping that to
  pixels or CSS is a renderer's job, not the spec's.
- **Refresh / polling.** Nothing re-fetches or auto-updates data on a timer. The
  only update mechanism is `serve` re-resolving the whole document on each
  request.
- **Partial / incremental re-resolution.** Every resolve processes the entire
  document. The resolved tree records each variable's `declaredAt` so a future
  dependency tracker *could* scope re-resolution to affected nodes, but that
  optimization is deferred — today resolution is always whole-document.
- **A pinned `expr-lang` syntax.** Computed variables use the `expr-lang/expr`
  engine, but the exact supported subset is intentionally left **generic** in
  this spec. Pinning a normative grammar (which operators, functions, and forms
  are guaranteed) is deferred future work; treat the expression examples as
  illustrative, not as a stable contract.
- **An HTTP write endpoint.** The
  [JSON Patch apply→save pipeline](../format/changesets.md) is implemented and
  reachable through the Go `changeset.ApplyChangeset` entry point and the
  [`lattice patch`](../format/changesets.md#applying-a-changeset-lattice-patch)
  CLI. But `serve` stays **read-only** — it re-resolves and renders, and exposes
  no write path over HTTP. A network-facing apply endpoint is future work.
- **A database storage adapter.** Apply persists through the filesystem and git
  [storage backends](storage.md). A database-backed `Store` is future work — the
  `Store` contract is designed to admit one, but none ships.

## What downstream consumers can rely on

- The **resolved-tree shape** is a stable, JSON-tagged contract; changes are
  additive and backward-compatible.
- A resolved tree is **fully validated** (both passes passed), so consumers may
  assume every node is structurally valid and type-checked.
- The resolved tree is **secret-value-free by construction**.

When any of the deferred items above is implemented, this page (and the relevant
format chapter) should be updated rather than left stale.
