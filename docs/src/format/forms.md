# Forms & Widget Placement

[Widgets](widgets.md) are leaf item types that each set a single variable. This
page covers the two ways to arrange them in a document:

- A **`form`** container groups widgets and lays them out compactly, so a set of
  controls does not each consume a whole grid cell.
- A **standalone widget** placed directly in a normal
  [`container`](catalog.md#container-itemscontainer100) grid cell — because a
  widget is an ordinary item instance, it can occupy a cell exactly like a table
  does.

## Standalone widgets

A widget is a normal instance. Dropping one straight into a `container`'s grid —
with its own 1-indexed `placement` — just works; no `form` wrapper is required:

```json
{
  "$ref": "https://lattice.dev/schemas/items/textarea/1.0.0",
  "id": "note-input",
  "placement": { "colStart": 1, "rowStart": 3 },
  "config": { "label": "Note", "variable": "note" }
}
```

The container normalizes this widget's placement into its
[layout block](document-structure.md#the-resolved-tree) alongside every other
child, and the widget binds its variable through the usual
[binding contract](widgets.md#the-binding-contract). Use a standalone widget when
a single control sits naturally beside other panels; reach for a `form` when you
have a cluster of controls that should pack together.

## The `form` container

`form` (`.../items/form/1.0.0`) is, like `container`, **structurally special**:
it is the other item type permitted to carry `children`. Unlike `container`, a
form may **only** contain variable widgets — a non-widget child fails fast with
`LAYOUT_FORM_CHILD_INVALID`, naming the offending child path. A form keeps a set
of controls together as one compact unit.

A form picks one of two **layout modes** via the `layout.mode` discriminator.
Flow and grid are two modes of the one `form` type, not two types.

| | `flow` (default) | `grid` |
| --- | --- | --- |
| Arrangement | Compact label+control cells, filled row-major then wrapped | A weighted grid identical in shape to `container`'s |
| `layout.columns` | A unitless **integer** count (1–12) | An **array** of relative column-track weights |
| `layout.rows` / `layout.gap` | Not used | Relative row-track weights / relative gap |
| Child `placement` | None — widgets carry no placement | Each child carries an explicit 1-indexed `placement` |

No CSS keywords or absolute units appear anywhere: flow `columns` is a plain
count, and grid weights are unitless relative weights normalized to fractions
summing to 1 per axis (exactly as for `container`).

The form instance itself still carries a `placement` describing where it sits in
its **parent** container's grid — the modes above govern only how the form
arranges its own children.

### Flow mode

Flow is the default — omit `layout`, or set `layout.mode` to `"flow"`. Each child
becomes a compact label+control cell; cells fill left-to-right across `columns`
(default 1) and wrap to the next row. Widgets carry **no** placement.

```json
{
  "$ref": "https://lattice.dev/schemas/items/form/1.0.0",
  "id": "general-form",
  "placement": { "colStart": 1, "colSpan": 2, "rowStart": 1 },
  "config": {
    "layout": { "mode": "flow", "columns": 2 }
  },
  "children": [
    {
      "$ref": "https://lattice.dev/schemas/items/text-input/1.0.0",
      "id": "title-input",
      "config": { "label": "Panel title", "variable": "title" }
    },
    {
      "$ref": "https://lattice.dev/schemas/items/select/1.0.0",
      "id": "region-select",
      "config": {
        "label": "Region",
        "variable": "region",
        "options": [{ "value": "us", "label": "United States" }]
      }
    },
    {
      "$ref": "https://lattice.dev/schemas/items/toggle/1.0.0",
      "id": "live-toggle",
      "config": { "label": "Live updates", "variable": "live" }
    }
  ]
}
```

The resolver attaches a normalized `flow` block to the form node: `mode`, the
resolved `columns` count, and a `cells` list giving each child its 1-indexed
`column`/`row`. With `columns: 2` and three children, the third wraps to row 2:

```json
"flow": {
  "mode": "flow",
  "columns": 2,
  "cells": [
    { "column": 1, "row": 1 },
    { "column": 2, "row": 1 },
    { "column": 1, "row": 2 }
  ]
}
```

A `columns` count outside the schema's `[1, 12]` range fails config validation
(`RESOLVE_CONFIG_INVALID`).

### Grid mode

Set `layout.mode` to `"grid"` to arrange the form's widgets on a weighted grid
identical in shape to a `container`'s. The `layout` object then carries
`columns` / `rows` track-weight arrays and a relative `gap`, and **each child**
declares an explicit 1-indexed `placement`:

```json
{
  "$ref": "https://lattice.dev/schemas/items/form/1.0.0",
  "id": "thresholds-form",
  "placement": { "colStart": 1, "colSpan": 2, "rowStart": 2 },
  "config": {
    "layout": { "mode": "grid", "columns": [1, 1], "rows": [1], "gap": 1 }
  },
  "children": [
    {
      "$ref": "https://lattice.dev/schemas/items/slider/1.0.0",
      "id": "threshold-slider",
      "placement": { "colStart": 1, "rowStart": 1 },
      "config": { "label": "Alert threshold", "variable": "threshold" }
    },
    {
      "$ref": "https://lattice.dev/schemas/items/stepper/1.0.0",
      "id": "window-stepper",
      "placement": { "colStart": 2, "rowStart": 1 },
      "config": { "label": "Window", "variable": "window" }
    }
  ]
}
```

Grid mode reuses the **exact same** grid path as `container`: tracks normalize to
fractions summing to 1, and each child's placement is validated against the grid
bounds, so an out-of-bounds or non-positive placement fails with the same
`LAYOUT_PLACEMENT_OUT_OF_BOUNDS` / `LAYOUT_PLACEMENT_INVALID` codes a container
would emit. The resolver attaches a normalized `layout` block to the form node
(the same shape container nodes carry), not a `flow` block.

## Error codes

| Code | When |
| --- | --- |
| `LAYOUT_FORM_CHILD_INVALID` | A `form` contains a non-widget child (a container, a table, …). |
| `LAYOUT_FORM_COLUMNS_INVALID` | A form's `layout` field has an unexpected type (a contract backstop; the schema bounds the integer range). |
| `LAYOUT_PLACEMENT_OUT_OF_BOUNDS` | A grid-mode form child's placement falls outside the form's grid (shared with `container`). |
| `LAYOUT_PLACEMENT_INVALID` | A grid-mode form child's placement is malformed, e.g. a non-positive span (shared with `container`). |

## Worked example

[`examples/form-dashboard.json`](../reference/examples.md) exercises a flow-mode
form, a grid-mode form, **and** a standalone widget in one document, with a table
consuming every bound variable through `$var` typed bindings and `${}` string
templates.
