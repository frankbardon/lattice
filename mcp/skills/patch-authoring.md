---
name: patch-authoring
description: How to shape an id-rooted RFC 6902 JSON Patch against a lattice dashboard — the id-rooted pointer dialect ($-scopes and item ids), the field-vs-structural-vs-metadata split and its gating (field edits gated by the lattice_get_node surface; structural and metadata edits ungated by the surface and validated by re-resolve, planned via lattice_get_outline), and a runnable example patch. Pairs with authoring-loop (the procedure) and lattice_get_schema (the grammar).
type: guide
kind: workflow
applies_to: [lattice_validate_patch, lattice_get_node, lattice_get_outline, lattice_get_schema]
---

# Patch authoring

How to construct the `ops` array `lattice_validate_patch` (and the eventual
`POST /api/patch`) consumes: an **RFC 6902 JSON Patch** — an ordered list of
`{op, path, value?, from?}` operations — whose pointers are **id-rooted**. This
skill covers the pointer dialect and the field-vs-structural split. It does NOT
restate any item type's field grammar — that is `lattice_get_schema`'s job (and it drifts
per server/type). For the surrounding procedure, read **authoring-loop**.

One mechanism edits every scope: an item's config, the manifest, variables,
connections, the theme, or the tree structure. There is no second edit language.

## Id-rooted pointers

A changeset pointer's **leading segment** is an *address* the apply layer
resolves; the remainder after it is literal RFC 6901.

- **`$`-scope leads** address the five document scopes: `$manifest`,
  `$variables`, `$connections`, `$theme`, `$root`. The `$`-prefix is recognized
  before any id lookup, so a scope keyword can never collide with an item id.
- **Any other lead is an item `id`** — the node's stable instance id (from
  `lattice_get_outline`), resolved to that node's physical location. Address a node's
  config field as `/<id>/config/<field>`.
- **Nested config dots into the pointer**: a surface key `grid.gap` is patched at
  `/<id>/config/grid/gap`.
- **Block content** lives physically at the wrapper's `config/content`, but the
  inner item is addressable by **its own id** — prefer `/<inner-id>/config/...`
  over `/<wrapper-id>/config/content/config/...`.
- **Structure** uses the `children` array: append with the RFC 6901 end token
  `/<container-id>/children/-`, address an existing slot positionally
  `/<container-id>/children/N`, and remove a whole node by id `/<item-id>`.
- **Move/copy** name the source by `from` and the destination by `path`, both
  id-rooted. A same-parent reorder is a `move` between two slots of one
  `children` array.
- **Metadata** is the `metadata` envelope sibling (not config): edit the whole
  object at `/<id>/metadata` or one key at `/<id>/metadata/<key>`; the document
  root's metadata is `/$root/metadata[/<key>]`.

The server is **stateless** — `lattice_validate_patch` takes the FULL cumulative patch
every call. Ops apply sequentially (order matters), and **positional pointers
resolve against the ORIGINAL tree** (the id index is built once, not recomputed
between ops). When removing several siblings of one parent positionally, author
the removes highest-index-first; removing/appending by stable `id` or `children/-`
sidesteps the issue entirely.

## Field edits vs structural edits (the gating split)

Every op is routed to **exactly one** of two guardrails by what it addresses.
This split decides which tool you consult to know an op is legal.

### Field edits — gated by the `lattice_get_node` surface

A field-level edit (rooted at an item's `config`, or at a settable document scope
like `/$manifest/<field>` or `/$theme/<token>`) may only touch a path the
node's/scope's **configurable surface** exposes.

- **Consult `lattice_get_node {id, nodeId}`**: its `surface` is the flat `[{key, type}]`
  list of legal field paths and their types. An op whose path is not on the
  surface is rejected `CONFIG_OVERRIDE_FIELD_UNKNOWN`; a value of the wrong type
  is rejected `CONFIG_OVERRIDE_VALUE_INVALID`. For a block wrapper the surface is
  the *content* item's fields.
- The surface (not this skill) is authoritative for which fields exist and their
  types — call `lattice_get_node`; for the full grammar of a value, call `lattice_get_schema`.

### Structural edits — NOT surface-gated; plan via `lattice_get_outline`

A structural edit (insert/delete in a `children` array, remove by id, or
move/copy) **cannot** be surface-gated — the `$root` surface is intentionally
empty. Instead the mutated document is **re-resolved**: the full resolver re-runs,
so the tree grammar (root holds regions; a container holds regions or block
wrappers; a bare leaf must be block-wrapped; …), every schema, referential
integrity, and variable validity are all re-checked. A mutated tree that breaks a
rule rejects the whole patch.

- **Consult `lattice_get_outline {id}`** to plan structure — where a child goes, what to
  move/remove. The surface does NOT list structural moves.
- A structural `add` is checked *before* apply: its value must carry a non-empty,
  document-unique `id` (`CHANGESET_STRUCTURAL_ID_INVALID`) — re-resolve cannot
  catch a duplicate id (last-wins), so this is enforced up front.
- A cross-parent `move` **strips placement** (stale parent-grid coordinates); a
  same-parent reorder keeps it.

### Metadata edits — NOT surface-gated; validated by re-resolve

An edit of the `metadata` envelope sibling (`/<id>/metadata`, `/<id>/metadata/<key>`,
or `/$root/metadata[/<key>]`) is **not** a config-field edit, so it bypasses the
surface gate the same way a structural edit does — `metadata` never appears on a
`lattice_get_node` surface. add / replace / remove all apply, for the whole object or one
key. Legality is enforced by **re-resolve**, not the surface: only the document
root, containers, and block wrappers may carry metadata (an ineligible target —
a leaf or widget — rejects the patch), and every value must be a scalar (a
non-scalar value rejects it). Plan eligible targets from `lattice_get_outline` (the node
skeleton); do not look for metadata on the surface.

The `placement` envelope sibling (`/<id>/placement`, `/<id>/placement/colStart`,
…) is gated the same way — **not** surface-gated; re-resolve validates the
coordinates against the parent grid (`LAYOUT_PLACEMENT_*`). See **placement-grid**.

In short: **field edit → check `lattice_get_node` surface; structural, metadata, or
placement edit → plan from `lattice_get_outline`, let re-resolve validate.**

## Runnable example

Against the shipped example dashboard **`example-grids`**
(`examples/grids-dashboard.json`): a `body` container holds a block-wrapped
`sidebar` table and a `panel` container; `panel` holds block-wrapped `main` and
`footer` tables. This cumulative patch does one of each kind: a **field** edit
(retitle the `sidebar` table — `title` is on its surface), a **structural add**
(append a new block-wrapped table to `body`), and a **structural remove** (drop
the `footer-block` by id). Send it whole to `lattice_validate_patch {id: "example-grids",
ops: [...]}`.

```json
[
  { "op": "replace", "path": "/sidebar/config/title", "value": "Links" },
  { "op": "add", "path": "/body/children/-",
    "value": {
      "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
      "id": "notes-block",
      "config": {
        "id": "notes-block",
        "content": {
          "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
          "id": "notes",
          "config": {
            "title": "Notes",
            "columns": [{ "header": "Note" }],
            "rows": [["first"]]
          }
        }
      }
    }
  },
  { "op": "remove", "path": "/footer-block" }
]
```

Why each op is valid:

- **`replace /sidebar/config/title`** — `sidebar` is the content table,
  addressable by its own id; `title` is on its configurable surface (confirm with
  `lattice_get_node {id: "example-grids", nodeId: "sidebar"}`), and the value is a string.
- **`add /body/children/-`** — `body` is a container, so it carries a `children`
  array; `children/-` appends. The added node is a block wrapper around a leaf
  table (a bare leaf must be wrapped), and both wrapper and inner item carry
  unique ids (`notes-block`, `notes`). Re-resolve validates the grammar.
- **`remove /footer-block`** — removes the whole block by its id. Emptying a
  region would be legal too; here `panel` still holds `main-block`.

Run it through `lattice_validate_patch`; on `ok: true`, read the `preview` and keep
`baseRevision` for the human's `POST /api/patch` commit (see **authoring-loop**).

## Cross-links

- **authoring-loop** — the full read→simulate→commit procedure this patch fits into.
- **session-bootstrap** — the source-layering doctrine (why grammar stays in `lattice_get_schema`).
