---
name: items-layout
description: The layout item family — `container` (the relative-weight grid region that groups and places children) and `block` (the mandatory single-leaf wrapper that carries per-block chrome). How layout nests under root, how containers place children, and how a block wraps exactly one content leaf. Pairs with placement-grid (the grid/coordinates), blocks (the wrapper↔content split), and get_schema (the per-type field grammar).
type: reference
kind: items
applies_to: [get_schema, get_node, get_outline, validate_patch]
covers: [container, block]
---

# Items: the layout family

This is a per-item **reference** for the two layout-structural item types —
**`container`** and **`block`**. They build the document skeleton: containers
position things on a grid, blocks wrap content leaves so they may sit in that
grid. This skill covers what each is *for*, how they nest, and the gotchas — it
does **not** list their fields. For the field grammar of either type call
**`get_schema`** (`container`, `block`); the schema drifts per server, so any copy
here would rot (see **session-bootstrap** → source layering).

## The two types at a glance

| Type | Role | Holds children in | Carries chrome? |
|---|---|---|---|
| `container` | Positional region — groups items and arranges them on a relative-weight grid | `children` array | No (layout-only) |
| `block` | Mandatory wrapper — wraps **exactly one** content leaf, carries per-block concerns | `config.content` (one instance) | Yes (id, title, visibility, theme) |

A `container` *arranges*; a `block` *arranges nothing* — it holds one leaf and
applies cross-cutting concerns to it. They are deliberately distinct: keep them
unconflated.

## How layout nests (the tree shape)

The document `root` and every region are **containers** (or another positional
region). The legal nesting, top-down:

```
root (container)
└─ body (container)            ← regions nest inside regions
   ├─ block ─ content leaf     ← a region holds block-wrapped leaves
   └─ block ─ content leaf
```

Two grammar rules govern this (enforced fail-fast on the resolved tree):

- **Root holds only positional regions.** A block or a bare content leaf directly
  under `root` fails `GRAMMAR_ROOT_CHILD_INVALID`.
- **A container holds nested regions *or* block wrappers** — never a bare content
  leaf. An unwrapped leaf under a container fails `GRAMMAR_REGION_CHILD_INVALID`.

So a content leaf reaches the grid only by being block-wrapped first. See
**blocks** for the wrapper requirement and **placement-grid** for the rules in
full.

## `container` — the grid region

A container groups its `children` and places each on a **relative-weight grid**
declared in `config.grid` (unitless `columns`/`rows` track weights + a `gap` —
no CSS units, no pixels). Each child positions itself with explicit 1-indexed
`placement` coordinates. A container carries **no theme** of its own — a `theme`
on a region fails `GRAMMAR_REGION_THEME_FORBIDDEN`; chrome lives on blocks.

The grid model, the `colStart/colSpan/rowStart/rowSpan` placement keys, and how
`get_outline`'s compact `"col 2+1, row 1+1"` summary maps to the verbatim config
are all covered in **placement-grid** — read it before placing or moving nodes.
For the exact grid/placement field grammar, call `get_schema container`.

**Pick `container` when** you need to group siblings and lay them out — a body
region, a sidebar + main split, a panel of stacked rows, or a nested subgrid.
Containers nest arbitrarily: a container child of a container declares its own
subgrid independent of its parent.

> Note: `container` is not the only positional region. `variable-box` is also a
> region, but it holds *variable widgets* directly (not block-wrapped) and is
> covered by **variables**, not here.

## `block` — the content wrapper

A `block` wraps **exactly one** inner content item, held in **`config.content`**
(not in a `children` array). It is the single, flat layer carrying every
per-leaf cross-cutting concern — a required stable `id`, an optional `title`, a
`visibility` flag, and an optional per-block `theme` override — so those concerns
live in one place instead of being duplicated across every leaf type. The
resolver emits the wrapper and its inner leaf as **two separate nodes** (the
content lifted out into the wrapper's single child).

Invariants the resolver guards fail-fast:

- A block's `id` must be present (non-whitespace) — else `WRAPPER_ID_MISSING`.
- A block wraps **exactly one** content item — `content` absent, null, or not a
  single instance object is `WRAPPER_CHILD_COUNT_INVALID`.
- A block **never re-wraps a block** — the inner leaf may not itself be a block
  (`GRAMMAR_WRAPPER_NESTED`).

**Pick `block` when** you are placing any content leaf (`markdown`, `heading`,
`image`, `table`, …) under a container — wrapping is **mandatory**, not optional.
You also reach for the block layer when you want to set a leaf's `title`, toggle
its `visibility`, or attach a `theme` override: those are wrapper concerns, not
leaf concerns. The full wrapper↔content split, how `get_node` surfaces the
*content's* editable fields against the wrapper node, and how to address each side
in a patch live in **blocks**. For the wrapper's own field grammar, call
`get_schema block`.

## Inline example reference

`examples/minimal-dashboard.json` is the canonical legal shape and the best
grounding for this family: a root **container** holds a `body` **container** whose
two-column grid (`"columns": [1, 1]`) places two **block-wrapped** static tables,
each block carrying its stable `id` and a `placement`. `examples/grids-dashboard.json`
extends it with a nested panel container declaring its own subgrid — read it when
you need arbitrary grid nesting.

## Cross-links

- **blocks** — the wrapper↔content split, how `get_node` resolves a block, and how
  to address a block (and its inner leaf) in an id-rooted patch.
- **placement-grid** — the relative-weight grid, the 1-indexed placement
  coordinates, the `get_outline` placement summary, and place/move via patch.
- **items-content** — the content leaves (`markdown`, `heading`, `image`) a block
  wraps.
- **variables** — `variable-box`, the *other* positional region (widgets held
  directly, not block-wrapped).
- **session-bootstrap** — source layering: why the per-type field grammar stays in
  `get_schema`.
