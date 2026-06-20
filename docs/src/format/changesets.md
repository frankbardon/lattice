# Changesets (JSON Patch)

Every edit to a document — at **any** scope — is expressed as a **JSON Patch
([RFC 6902](https://www.rfc-editor.org/rfc/rfc6902))** document. This is the
single, universal changeset mechanism for the format: there is no second edit
language, no scope-specific mutation API, and no bespoke diff shape. Whether a
change retunes one item's config, rewrites the manifest, adds a variable, swaps a
connection, flips a theme token, or restructures the root region, it is the same
artifact — an ordered array of JSON Patch operations against the document.

> **Implemented.** A changeset can now be applied: `lattice` loads a stored
> document, applies a changeset to it under the configurable-surface and tree-grammar
> guardrails, re-resolves the result for full validation, and persists it — all
> atomically. The Go entry point is `changeset.ApplyChangeset`; the CLI is
> [`lattice patch`](#applying-a-changeset-lattice-patch). Two pieces remain
> out of scope (see [What is deferred](#what-is-deferred)): an **HTTP write
> endpoint** (`serve` stays read-only) and a **database storage backend**.

## Why one mechanism

A document is a single JSON tree: a manifest, a variable set, connections, a
default theme, and a root region of nested items. JSON Patch already addresses
*any* location in such a tree by [JSON Pointer
(RFC 6901)](https://www.rfc-editor.org/rfc/rfc6901) and already defines the six
operations needed to mutate one — `add`, `remove`, `replace`, `move`, `copy`,
`test`. Adopting it wholesale means:

- **Uniformity.** The same operation vocabulary edits an item's config field, a
  document scope, or the whole root. A consumer learns one changeset format, not
  one per scope.
- **Addressability.** A JSON Pointer names exactly the node and field a change
  touches, which is precisely what a [guardrail](#the-two-guardrails) needs in
  order to decide legality.
- **Ordering and atomicity.** A patch is an ordered list; `test` operations let a
  changeset assert preconditions. The format inherits this for free.

## Uniform across every scope

The changeset format does not vary by what it edits. The same JSON Patch shape
applies to all of these scopes:

| Scope         | Example pointer target                  |
| ------------- | --------------------------------------- |
| **item**      | a node's `config` field                 |
| **manifest**  | the document `title` / `description`    |
| **variables** | the document variable set               |
| **connections** | the document connections              |
| **theme**     | a [theme token](theme.md)               |
| **root**      | the resolved root region                |

These line up one-for-one with the addresses the rest of the format already
speaks: an item is addressed by its stable `id`, and the five document scopes are
the reserved `$`-keywords (`$manifest`, `$variables`, `$connections`, `$theme`,
`$root`) introduced for
[reserved document-scope targets](configurator.md#reserved-document-scope-targets).
A changeset is just the *write* counterpart to those *read/target* addresses,
spelled in JSON Pointer.

A minimal config edit and a minimal theme edit are the same kind of document:

```json
[
  { "op": "replace", "path": "/<item>/config/title", "value": "Pinned" }
]
```

```json
[
  { "op": "replace", "path": "/$theme/density", "value": "compact" }
]
```

## The id-rooted pointer dialect

A changeset pointer's **leading segment** is not a literal physical path — it is
an *address* the apply layer resolves, exactly the way a
[configurator `target`](configurator.md) or a
[config override](overrides.md#config-overrides) addresses a node. The remainder
after the leading segment is a literal [RFC 6901](https://www.rfc-editor.org/rfc/rfc6901)
JSON Pointer.

- **Leading segment → physical base.** A leading segment beginning with `$` is a
  reserved [document scope](configurator.md#reserved-document-scope-targets) and
  routes to that scope's physical member (`$manifest` → `/manifest`, `$variables`
  → `/variables`, `$connections` → `/connections`, `$theme` → `/theme`, `$root` →
  `/root`); the `$`-prefix is recognized *before* any id lookup, so a reserved
  keyword can never collide with an item id. Any other leading segment is an item
  `id`, resolved through an index of every id-carrying node to that node's
  physical pointer in the on-disk tree.
- **Block content is `/config/content`, not a child slot.** A
  [block wrapper](blocks-and-grammar.md#the-block-wrapper)'s single inner content
  item lives physically at the wrapper's `config.content` — a config field, not a
  `children` slot. So to edit a wrapped leaf's config you address
  `/<wrapper-id>/config/content/config/<field>` (or, more usually, address the
  inner item directly by *its own* id). The id index descends into `config/content`
  precisely so the inner item is addressable by its own id.
- **Structure is `children/-` and `children/N`.** Only a container or form carries
  a `children` array. Append a child with the RFC 6901 end-of-array token —
  `/<container-id>/children/-` — and address an existing child slot positionally —
  `/<container-id>/children/0`. Remove a whole item by addressing it as a whole:
  `/<item-id>`.
- **Move uses `from` + `path`.** A `move` (or `copy`) names the relocated node by
  `from` (an id-rooted pointer to the source) and its destination by `path`. Both
  ends are translated. Reordering a child within one parent is a `move` whose
  `from` and `path` are slots of the same `children` array.

```json
[
  { "op": "add", "path": "/main-grid/children/-",
    "value": { "id": "kpi-block", "$ref": "block@1.0.0",
               "config": { "content": { "id": "kpi", "$ref": "metric@1.0.0", "config": {} } } } },
  { "op": "move", "from": "/old-block", "path": "/main-grid/children/0" },
  { "op": "remove", "path": "/stale-block" }
]
```

> **Gotcha — id-rooted pointers resolve against the *original* tree.** The id
> index is built once, up front, from the document as loaded — it is *not*
> recomputed between operations. So a positional pointer like `/<id>/children/2`
> always means "index 2 in the original array," even if an earlier operation in
> the same changeset removed an earlier sibling. When you remove several siblings
> of one parent in a single changeset, **author the removes in descending
> physical-index order** (highest index first) so each removal does not shift the
> index of a sibling a later operation still refers to. Removing or appending by a
> stable item `id` (`/<item-id>`, `children/-`) sidesteps this entirely.

## The two guardrails

A changeset is not free to touch anything. Each operation is routed to **exactly
one** of two guardrails, decided by what it addresses:

### Field and nested edits → the configurable surface

A field-level edit — anything rooted at an item's `config` or at a settable
document scope — may only touch a path the
[configurable surface](configurable.md) actually exposes. This is the same
surface that gates an [ephemeral runtime override](overrides.md): the resolver
computes it from the schema on every resolve, so the set of legal patch paths can
never drift out of sync with what an item type or scope accepts.

- For an **item** edit (`/<id>/config/<field>`), the surface is the item type's
  `configurable` declaration. A target that resolves to a field *not* on that
  surface is rejected with `CONFIG_OVERRIDE_FIELD_UNKNOWN` — exactly as a config
  override of an unsurfaced field fails. The value is type-checked against the
  surface field's declared type (and enum options), rejected with
  `CONFIG_OVERRIDE_VALUE_INVALID`.
- For a **document scope** edit (`/$manifest/<field>`, `/$theme/<token>`), the
  surface is the scope's entry in the document schema's
  [`documentScopes`](configurator.md#document-scope-surfaces) keyword. `$manifest`
  surfaces `title` / `description`; `$theme` surfaces the theme tokens. A path the
  scope does not surface is out of bounds.

**Nested config edits** are supported through the surface's nested declaration.
A `configurable` key that itself carries a dot — e.g. `"grid.gap"` — declares an
explicit sub-path into a nested config object. A changeset that addresses the
matching nested path, `/<id>/config/grid/gap`, has its pointer remainder joined
with "." into the dotted surface key (`grid.gap`) and is checked against that
nested entry. A nested edit is legal **only** if the surface declares the dotted
key; addressing a nested path the surface does not enumerate is off-surface, the
same `CONFIG_OVERRIDE_FIELD_UNKNOWN`. (Document scopes surface top-level fields
only, so a multi-segment scope path simply never matches.)

### Structural edits → tree grammar + re-resolve

A structural edit — an insert or delete in a `children` array, a remove by item
id, or a `move`/`copy` that relocates a node — cannot be surface-gated: the
`$root` configurable surface is intentionally **empty**, so there is no surface
field to match against. Instead, structure is validated by **re-resolving the
mutated document**: the full two-pass resolver runs again, so the
[tree grammar](blocks-and-grammar.md#the-grammar-rules) (root holds only
positional regions, a container holds regions or block wrappers, a bare leaf must
be wrapped, …), every item-type schema, referential integrity, and
variable/interpolation validity are all re-checked "for free." A mutated tree
that violates any rule rejects the whole changeset.

Re-resolve catches almost everything, but it cannot catch a **missing or
duplicate instance `id`** — the resolver's id index is last-wins, so a second
node reusing an id is silently shadowed rather than rejected. Because the `id` is
the stable address every changeset pointer roots on, a structural `add` is
checked *before* apply: its value must carry its own non-empty, document-unique
`id` (`CHANGESET_STRUCTURAL_ID_INVALID`).

Two structural specifics:

- **Emptied regions are legal.** Removing the last child of a container leaves an
  empty `children` array; the grammar permits it, so a changeset may legally empty
  a region.
- **Cross-parent move strips placement.** A node's
  [`placement`](forms.md#widget-placement) is expressed in its *immediate parent*
  container's grid coordinates. A `move` that carries a node to a **different**
  parent would carry stale coordinates that the new grid's bounds reject. The
  apply layer therefore strips the `placement` from any cross-parent move, so the
  node falls back to the default first cell (which fits any grid). A **same-parent
  reorder keeps** its placement — the grid is unchanged, so the author's explicit
  coordinates are still valid and are preserved.

## Preconditions: `test` ops and the revision precondition

A changeset has two independent levers for optimistic concurrency, so a stale
edit cannot clobber a document that changed underneath it.

- **RFC 6902 `test` ops.** A `test` operation asserts that a given path holds an
  expected value; the standard applier evaluates it during apply, and a mismatch
  aborts the whole changeset (`PATCH_APPLY_FAILED`) with nothing persisted. Use a
  `test` to make a changeset's legality depend on the current value of the very
  field it edits.
- **The revision precondition.** A caller may supply an opaque *expected
  revision* — `changeset.WithExpectedRevision(token)` in Go, `--expect-revision`
  on the CLI. The pipeline re-reads the store's **current** revision immediately
  before the write (as close to the write as possible, to minimize the race
  window) and rejects with `CHANGESET_REVISION_CONFLICT` if it no longer matches.
  The conflict code is distinct so a caller can retry: reload, re-derive the
  changeset against the new bytes, re-apply. The revision token is opaque and
  compared verbatim — a git commit hash for the git backend, a content hash for
  the filesystem backend (both
  [storage backends](../reference/storage.md) expose it). If the configured store
  cannot report a revision but an expected one was supplied, the apply fails with
  `CHANGESET_REVISION_UNSUPPORTED` rather than silently skipping the check. Omit
  the precondition for single-writer behavior.

## Canonical serialization

After a changeset is applied, the mutated document is re-serialized
**canonically**: object keys are emitted in sorted order with a fixed two-space
indent. This makes the on-disk form deterministic — the same logical document
always produces identical bytes.

The practical consequence is a **one-time reflow**. The first changeset applied
to a hand-authored document may rewrite key order and indentation across the
whole file (a large, noisy diff), because it brings the document into canonical
form. Every subsequent changeset then produces a **minimal, stable diff** that
touches only the bytes the edit actually changed. A no-op changeset applied to an
already-canonical document round-trips to identical bytes.

## Applying a changeset (`lattice patch`)

The `lattice patch` command applies a changeset to a stored document end to end:

```console
lattice patch <id> --changeset edit.json
```

- **`<id>`** is the document's **manifest id**, not a filesystem path. Unlike
  `resolve`/`serve`, `patch` always operates through a storage backend.
- **`--changeset <path>`** is the changeset file — an id-rooted JSON Patch array.
  The special path `-` reads the changeset from **stdin**, so a changeset can be
  piped in.
- **`--store fs|git`** and **`--root <dir>`** select the backend, the same seam
  `resolve`/`serve` use (defaults: `fs`, the working directory). For the git
  backend, the persisting `Save` is a commit.
- **`--expect-revision <token>`** is the optional optimistic-concurrency
  precondition described above; omit it for no precondition.
- **`--schemas <dir>`** points at the dashboard schema and item-type catalog.

The command exits non-zero on any coded error, reported through the shared CLI
error path (a `{code, message, details}` JSON envelope under the global `--json`
flag), and nothing is persisted on failure.

### The `ApplyChangeset` Go entry point

The CLI is a thin wrapper over the single reusable Go entry point, which every
touchpoint shares:

```go
func ApplyChangeset(
    store storage.Store,
    res DocumentResolver,
    id string,
    cs *changeset.Changeset,
    opts ...changeset.ApplyOption,
) (*changeset.ApplyResult, error)
```

It runs the whole pipeline — `Store.Load(id)` → resolve the current bytes (to get
the surfaces the field-edit guardrail checks against) → apply under the
guardrails and canonically re-marshal → **re-resolve** the mutated bytes (the
structural/schema/referential guardrail) → check the revision precondition →
`Store.Save`. It is **atomic**: on any error at any step the apply is rejected and
the store is never touched, so the stored document is left byte-for-byte
unchanged. The store is written **exactly once**, only on full success.
`*resolver.Resolver` satisfies the `DocumentResolver` capability via
`ResolveBytesWithValues`. `ApplyResult` returns the persisted bytes and their
already-computed resolved tree.

## Relationship to runtime overrides

A changeset and a [runtime override](overrides.md) share the configurable surface
but differ in lifetime:

| | [Runtime override](overrides.md) | JSON Patch changeset |
| --- | --- | --- |
| **Lifetime** | ephemeral — one resolution | durable — a persisted edit |
| **Shape** | flat `address → value` map | ordered RFC 6902 operation list |
| **Scope** | variables and config fields | every scope, uniformly |
| **Field guardrail** | configurable surface | configurable surface |
| **Structural edits** | not applicable | tree grammar + re-resolve |

An override answers "what should this one resolution see?" A changeset answers
"what edit should be recorded against the document?" Field edits in both are
gated by the same surface, and the changeset reuses the override pass's coded
errors so a field-level violation reads the same either way.

## What is deferred

The apply pipeline is implemented. Two adjacent capabilities are still
**explicitly out of scope** (see [Out of Scope](../reference/out-of-scope.md)):

- **An HTTP write endpoint.** The `serve` command stays **read-only** — it
  re-resolves and renders, but exposes no write path. A changeset is applied
  through the Go `ApplyChangeset` entry point or the `lattice patch` CLI, not over
  HTTP.
- **A database storage backend.** Apply persists through the existing filesystem
  and git [storage backends](../reference/storage.md). A database-backed `Store`
  is future work — the `Store` contract admits one, but none ships.

When either is implemented, this page and the relevant reference chapter should be
updated rather than left stale.
