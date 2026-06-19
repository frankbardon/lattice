# Blocks & the Tree Grammar

A Lattice document is not a free-form tree of arbitrary nodes. Beyond per-item
schema validation, the resolver enforces a **tree grammar**: a small set of rules
about *where each kind of node may appear*. Those rules turn on two primitives
this page describes together:

- the **block wrapper** — the mandatory, single-layer wrapper that carries every
  cross-cutting per-block concern (a stable id, an optional
  [theme](theme.md) override, a title, a visibility flag) and holds exactly one
  content leaf, and
- **positional regions** — the layout-only types (`container`, `variable-box`)
  marked with the schema-level `positional` keyword, which position children and
  carry no chrome of their own.

The grammar pass (`internal/resolver/grammar.go`) runs **once** over the
assembled resolved tree, **fail-fast**: the first violation stops the walk and is
returned as a `CodedError` naming the offending `path`.

## The block wrapper

The **block** (`schemas/items/block.schema.json`,
`https://lattice.dev/schemas/items/block/1.0.0`) is a wrapper item type —
**distinct from `container`**. A container *groups and arranges* a grid of
children; a block *arranges nothing*. It wraps a single inner content item and
applies its per-block concerns to that one leaf.

A block holds its inner item in **`config.content`**, not in the document
`children` array:

```json
{
  "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
  "id": "fruits-block",
  "config": {
    "id": "fruits-block",
    "title": "Fruits",
    "visibility": true,
    "theme": { "emphasis": "high", "tone": "accent" },
    "content": {
      "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
      "id": "fruits",
      "config": { "title": "Fruits", "columns": [ ... ], "rows": [ ... ] }
    }
  }
}
```

The block's own concerns are:

| Field | Required | Meaning |
| --- | --- | --- |
| `id` | **yes** | Stable, machine-readable anchor for the block, unique within the document. It is what a [configurator](configurator.md) target or a [JSON Patch changeset](changesets.md) addresses the block by. Distinct from an instance's optional layout-local `id`. |
| `content` | **yes** | The single inner content item the block wraps — one instance node referencing an item-type schema via `$ref`. **Exactly one.** |
| `title` | no | Optional human heading/label rendered with the content. Tunable at runtime (it is on the block's [configurable surface](configurable.md)). |
| `visibility` | no | Whether the block (and its content) is shown; defaults to `true`. Tunable at runtime. |
| `theme` | no | An optional per-block [theme](theme.md) **override**, drawn from the shared theme vocabulary. A *partial* subset is allowed; an out-of-vocabulary token is rejected by config validation. |

### How a block resolves

The resolver emits the wrapper and its inner content as **two separate nodes**:

- The **wrapper node** carries only its own concerns. The `content` field is
  *lifted out* of the wrapper's resolved config, so a consumer reads the inner
  item once, as a node — never duplicated inside the wrapper.
- The **inner content leaf** is resolved **identically to how it would resolve
  unwrapped** — same scoped variable environment, same interpolation, same config
  validation — and emitted as the wrapper's single child in `children`.

So in the resolved tree a block appears as a node with exactly one child: the
content it wraps.

Two wrapper invariants are guarded fail-fast (defense-in-depth over the schema):

- `WRAPPER_ID_MISSING` — the block's `id` is absent or whitespace-only.
- `WRAPPER_CHILD_COUNT_INVALID` — the block does not wrap exactly one content
  item (its `content` is absent, null, or not a single instance object).

The block's `theme` override **rides through ordinary config validation** (the
block schema `$ref`s the theme schema) and is then attached **verbatim**. The
resolver performs **no** cascade, merge, or effective-theme computation — see
[Theme](theme.md#no-merge-side-by-side-layers).

## Positional regions and the `positional` marker

A **positional region** is a layout-only type: it positions its children and
carries no chrome or theme of its own. A type is a region when its schema sets the
top-level **`positional: true`** marker. Two regions ship today:

- **`container`** — groups arbitrary items on a relative-weight grid
  (`config.grid`). See [Document Structure](document-structure.md) and the
  [Schema Catalog](catalog.md#container-itemscontainer100).
- **`variable-box`** (`schemas/items/variable-box.schema.json`,
  `.../items/variable-box/1.0.0`) — the dedicated home for the **variable
  widgets** (text-input, slider, select, …). Like a container it is layout-only,
  but it holds its widget children **directly** — they are **not** individually
  block-wrapped, because the box (not a per-widget wrapper) supplies their grouped
  presentation downstream. Its only surface is a layout-only `arrangement`
  (`stacked` | `inline`) — the analogue of a container's grid, with no chrome.

The marker is the **single source of truth** for which types are legal regions.
The grammar pass reads it via the catalog (`ResolvedType.IsPositional`) rather
than any hardcoded type list, so **adding a new positional type makes it a legal
root/region child with no change to the grammar code**.

## The grammar rules

The grammar pass enforces these structural rules over the assembled tree:

| Rule | Violation code |
| --- | --- |
| **Root holds only positional regions.** The document `root` accepts only region types (marker-driven). A content leaf or a block wrapper directly under root fails. | `GRAMMAR_ROOT_CHILD_INVALID` |
| **A container holds nested regions or block wrappers.** A container may nest other positional regions *or* hold block wrappers — but a **bare content leaf** under a container fails: content must be block-wrapped. | `GRAMMAR_REGION_CHILD_INVALID` |
| **A variable-box holds variable widgets, directly.** Every child must be a variable widget held directly — not wrapped, not a nested region. | `GRAMMAR_VARIABLE_BOX_CHILD_INVALID` |
| **A block never re-wraps a block.** A block holds exactly one content leaf, and that leaf may not itself be a block wrapper. (The exactly-one count is guarded by the block pass; this rule adds the no-recursion check.) | `GRAMMAR_WRAPPER_NESTED` |
| **A positional region carries no theme.** Regions are layout-only; only block wrappers carry chrome. A `theme` on a region node is rejected. | `GRAMMAR_REGION_THEME_FORBIDDEN` |

Each error names the offending `path` (and, where relevant, the offending
`type`) in its details. See the [Error Codes](../reference/error-codes.md)
reference.

> **Why the wrapper is mandatory and flat.** Cross-cutting concerns (identity,
> theme, visibility, title) live in *one* place — the block — rather than being
> scattered across every leaf item type. The single flat layer keeps the model
> simple: a content leaf is wrapped exactly once, a region groups wrappers, and
> the resolver stays dumb — it validates the shape and attaches the concerns, and
> a downstream builder owns mixing, rendering, AI, and persistence.

## A worked shape

`examples/minimal-dashboard.json` is the canonical legal shape: a root container
holds a body region whose two-column grid places two **block-wrapped** static
tables. `examples/themed-dashboard.json` adds the three theme/grammar constructs
side by side — a document default theme, a per-wrapper override on one block, and
a `variable-box` holding a widget directly. See
[Examples](../reference/examples.md).
