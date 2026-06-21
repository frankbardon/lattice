---
name: placement-grid
description: Grid placement semantics — how get_outline's compact placement summary (e.g. "col 2+1, row 1+1") maps to the verbatim placement config (colStart/colSpan/rowStart/rowSpan), how a container's relative-weight grid works, and how to place or move a node via patch. Pairs with get_outline (the summary), get_node (verbatim config), patch-authoring (structural edits), and get_schema (grammar).
type: guide
kind: workflow
applies_to: [get_outline, get_node, validate_patch, get_schema]
---

# Placement & the grid

A **container** is a positional region: it arranges its children on a
**relative-weight grid** (`config.grid`), and each child places itself with explicit
1-indexed coordinates in its own `placement`. This skill covers the grid model, how
to read `get_outline`'s placement **summary**, and how to place or move a node. It
does NOT restate the grid/placement grammar — call `get_schema`.

## The container grid

A container's `config.grid` declares **unitless track weights** plus a gap:

```json
{ "grid": { "columns": [1, 1], "rows": [1], "gap": 1 } }
```

- `columns` / `rows` are **relative weights**, not pixels or CSS units. The
  resolver normalizes each axis to fractions summing to 1 (so `[1, 1]` → two equal
  columns; `[2, 1]` → a 2/3 + 1/3 split). The grid is renderer-agnostic — no CSS
  keywords, no absolute units anywhere.
- `gap` is the inter-track spacing (also unitless).

## Child placement: coordinates

Each child carries a `placement` with four **1-indexed** keys, each defaulting to
`1`:

| Key | Meaning |
|---|---|
| `colStart` | first column track the child occupies (1-indexed) |
| `colSpan` | number of column tracks it spans |
| `rowStart` | first row track (1-indexed) |
| `rowSpan` | number of row tracks it spans |

```json
{ "placement": { "colStart": 2, "colSpan": 1, "rowStart": 1, "rowSpan": 1 } }
```

The resolver validates each placement against the grid bounds:
`LAYOUT_PLACEMENT_INVALID` (malformed) / `LAYOUT_PLACEMENT_OUT_OF_BOUNDS` (off the
declared tracks).

## The placement summary ↔ config mapping

`get_outline` reports each placed node's position as a **compact, lossy summary**
(NOT the verbatim config object) — read it as **`<start>+<span>`** per axis:

```
col 2+1, row 1+1
   │ │      │ │
   │ └ colSpan=1   └ rowSpan=1
   └ colStart=2    rowStart=1
```

So `"col 2+1, row 1+1"` ⇔ `{colStart: 2, colSpan: 1, rowStart: 1, rowSpan: 1}`.
Edge forms you may see:

- Only a start present → just the number (e.g. `"col 2"` ⇔ `colStart: 2`).
- Only a span present → `"?+<span>"` (start unknown/defaulted).
- Placement present but not in the recognized grid shape → `"placed (<keys>)"`,
  listing the keys without echoing values.
- No placement at all → the summary is omitted (the node is unplaced; coordinates
  default to `1`).

The summary is for **navigation**; for the exact placement object to edit, call
`get_node` (the `subtree` carries the verbatim `placement`). The grid itself
(`config.grid`) is a normal field on the container's surface.

## Placing and moving nodes

Placement edits split the same way every edit does (see **patch-authoring**):

- **Set/adjust a placement coordinate** — a **field** edit on the node's config-
  level placement. Address it id-rooted, e.g. `/<node-id>/placement/colStart`, and
  confirm the leaf is legal before patching. To resize the grid itself, edit the
  container's `config.grid` (e.g. `/<container-id>/config/grid/columns`).
- **Place a *new* node** — a **structural** `add` into the parent container's
  `children` array (`/<container-id>/children/-`), with `placement` set on the added
  node. Plan the target from `get_outline`; a bare content leaf must be
  block-wrapped first (see **blocks**).
- **Move a node** — a structural `move` naming `from` and `path`, both id-rooted.
  A **same-parent reorder** (a `move` between two slots of one `children` array)
  **keeps** placement. A **cross-parent move strips placement** (stale parent-grid
  coordinates are dropped), so re-set the node's `placement` for the new grid after
  moving.

Structural edits are **not** surface-gated — the mutated tree is re-resolved, so an
out-of-bounds placement or a grammar break (e.g. an unwrapped leaf) rejects the
whole patch. Prove every placement change with `validate_patch` before the human
commits.

## Cross-links

- **patch-authoring** — id-rooted pointers, field-vs-structural gating, move/copy
  rules, the runnable structural example.
- **blocks** — content leaves must be block-wrapped before placing under a container.
- **variables** — placing a variable-box region and its widgets.
- **session-bootstrap** — source layering (grid/placement grammar lives in `get_schema`).
