---
name: blocks
description: The block wrapper + content model — a block wraps exactly one content leaf and delegates its editable knobs to it; how get_node resolves block-vs-content and surfaces the CONTENT item's fields; and how to address a block (and its content) in an id-rooted patch. Pairs with patch-authoring (the pointer dialect) and get_schema (the grammar).
type: guide
kind: workflow
applies_to: [get_node, get_outline, validate_patch, get_schema]
---

# Blocks

A lattice document's content leaves (table, markdown, heading, image, …) never
sit bare in the tree: each is wrapped in **exactly one block**. The block is the
single, flat layer carrying every cross-cutting per-leaf concern — a stable `id`,
an optional per-block `theme` override, a `title`, a `visibility` flag — so those
concerns live in *one* place instead of being scattered across every leaf type.
This skill covers the wrapper↔content split, how `get_node` resolves it, and how
to address a block in a patch. It does NOT restate the block or content grammar —
that is `get_schema`'s job (and it drifts per server/type).

## Wrapper + content: exactly one

A block (`block` item type) holds its inner leaf in **`config.content`**, NOT in
the document `children` array. It wraps **exactly one** content item:

```json
{
  "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
  "id": "fruits-block",
  "config": {
    "id": "fruits-block",
    "title": "Fruits",
    "content": {
      "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
      "id": "fruits",
      "config": { "title": "Fruits", "columns": [ ... ], "rows": [ ... ] }
    }
  }
}
```

Invariants the resolver enforces fail-fast:

- A block's `id` must be present (non-whitespace) — else `WRAPPER_ID_MISSING`.
- A block must wrap **exactly one** content item — `content` absent, null, or not
  a single instance object is `WRAPPER_CHILD_COUNT_INVALID`.
- A block **never re-wraps a block** — the inner leaf may not itself be a block
  (`GRAMMAR_WRAPPER_NESTED`).

The wrapper carries chrome; the leaf carries the actual content. Where blocks are
*required* (a bare content leaf under a container/root must be block-wrapped) is
tree-grammar territory — see **placement-grid** and `get_outline`.

> A **variable-box** region is the exception: it holds its widget children
> **directly**, NOT block-wrapped (see **variables**). Blocks wrap *content*
> leaves; variable-boxes group *widgets*.

## How get_node resolves block-vs-content

`get_node {id, nodeId}` resolves the wrapper/content split **server-side**. When
`nodeId` names a **block wrapper**:

- **`subtree`** is the **whole block** — the wrapper plus its `config/content`
  (the exact shape a raw patch edits).
- **`surface`** (the flat `[{key, type}]` editable field list) is the **CONTENT
  item's** fields, NOT the wrapper's. A block delegates its editable knobs to what
  it wraps, so the surface reflects the leaf.

So you address one node (`fruits-block`) and `get_node` hands you both the
wrapper's stored shape and the content's editable surface in one call. The surface
gates **field** edits only; structural edits are planned from `get_outline` (see
**patch-authoring**).

## Addressing a block in a patch

Pointers are **id-rooted** (full dialect: **patch-authoring**). The content leaf
has its own `id`, and that is the preferred address:

- **Edit a content field** — address the **inner item by its own id**:
  `/<content-id>/config/<field>`. Prefer `/fruits/config/title` over the
  physical `/fruits-block/config/content/config/title`. Both resolve to the same
  place; the inner-id form is shorter and survives reshaping.
- **Edit a wrapper concern** (title, visibility, theme) — address the **wrapper
  id**: `/fruits-block/config/title`, `/fruits-block/config/visibility`.
- **Replace the whole block** (e.g. a structural `add` of a new block) — the
  value is the full block object above: wrapper `$ref` + `id` + `config` with a
  unique inner-leaf `id`. A bare leaf cannot be added under a container; wrap it.
- **Remove a block** — `remove /<wrapper-id>` drops the wrapper and its content
  together.

Confirm a field is legal by checking the `get_node` surface for the node; confirm
the value's grammar with `get_schema`. Then prove the whole patch with
`validate_patch` before the human commits via `POST /api/patch`.

## Cross-links

- **patch-authoring** — the id-rooted pointer dialect, field-vs-structural gating,
  and the runnable block-add example.
- **variables** — how `${var}` flows into a content leaf's config; the variable-box
  (widgets held directly, not block-wrapped).
- **placement-grid** — where blocks are required, and how a block places in a grid.
- **session-bootstrap** — source layering (why grammar stays in `get_schema`).
