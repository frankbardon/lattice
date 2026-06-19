# Schema Catalog

The schema catalog is the set of JSON Schema (draft 2020-12) definitions the
resolver loads and keys by `$id`. It lives under `schemas/` and is described by
`schemas/README.md`.

## Layout

```
schemas/
├── dashboard.schema.json        # top-level document schema
├── theme/
│   └── theme.schema.json        # the semantic-token theme vocabulary
├── items/                       # item-type schemas (instance $ref targets)
│   ├── container.schema.json    # positional region (grid)
│   ├── variable-box.schema.json # positional region (variable widgets)
│   ├── block.schema.json        # mandatory single-leaf wrapper
│   ├── table.schema.json
│   ├── text-input.schema.json   # string-family widget
│   ├── slider.schema.json       # number-family widget
│   ├── toggle.schema.json       # boolean-family widget
│   ├── select.schema.json       # enum-family widget
│   └── multiselect.schema.json  # array-family widget
└── connections/                 # connection-type schemas
    ├── http.schema.json
    └── static.schema.json
```

The full widget catalog (13 item types across five families) is documented on
the [Widgets](widgets.md) page; only a representative item per family is shown
above.

The resolver scans the catalog directory (default `schemas/`, overridable with
`--schemas`) recursively for `*.schema.json` files and indexes them by `$id`.

## `dashboard.schema.json`

The top-level document schema
(`https://lattice.dev/schemas/dashboard/1.0.0`). It defines the `manifest`, the
recursive `instance` shape used for `root` and every node beneath it, the
`varDeclaration` shape, and the `connection` instance shape. See
[Document Structure](document-structure.md).

## `items/` — item types

Each item type is referenced by an instance `$ref`.

### `container` (`.../items/container/1.0.0`)

A **positional region** (see the [`positional` marker](#schema-level-keywords)):
it groups children and arranges them on a relative-weight grid. `config.grid`
holds unitless `columns` and `rows` track weight lists (normalized by the
resolver to fractions summing to 1 per axis) and a relative `gap`. An axis with
no track list is a single implicit full-size track. Children place themselves
with explicit 1-indexed `placement` coordinates. No CSS units or keywords appear.

### `variable-box` (`.../items/variable-box/1.0.0`)

A **positional region** dedicated to holding the **variable widgets**. Like
`container` it is layout-only and carries no chrome/theme; it holds its widget
children **directly** (they are not block-wrapped). Its only surface is a
layout-only `arrangement` (`stacked` | `inline`). See
[Blocks & the Tree Grammar](blocks-and-grammar.md#positional-regions-and-the-positional-marker).

### `block` (`.../items/block/1.0.0`)

The mandatory **wrapper** — *distinct from `container`* — that wraps **exactly
one** inner content item (held in `config.content`) and carries the cross-cutting
per-block concerns: a required stable `id`, an optional [theme](theme.md)
override, a `title`, and a `visibility` flag. The resolver emits the wrapper and
its inner content as separate nodes. See
[Blocks & the Tree Grammar](blocks-and-grammar.md#the-block-wrapper).

### `form` (`.../items/form/1.0.0`)

The other structurally special type: like `container` it may carry `children`,
but it may only contain variable widgets. A form groups widgets compactly via one
of two layout modes (`flow` or `grid`) selected by `layout.mode`. See
[Forms & Widget Placement](forms.md).

### `table` (`.../items/table/1.0.0`)

A tabular leaf type. It can render static `columns`/`rows`, or bind to a
connection via `connectionId` and carry a `query`. It declares the
`expectedResult` schema-level keyword — the
[result-shape contract](connections.md#result-shape-contract) describing the
rows a bound connection is expected to return.

### `configurator` (`.../items/configurator/1.0.0`)

A leaf type that renders an editor for **another item in the same document** — its
`target`, referenced by that item's stable instance `id`. The resolver validates
the target reference and auto-generates an editor form from the target's
[configurable surface](configurable.md); each control drives an ephemeral
[config override](overrides.md#config-overrides). See
[Configurators](configurator.md).

### Variable widgets

Beyond `container` and `table`, the catalog ships **13 variable widgets** — leaf
item types that each **set a single variable**. They are grouped into five
families by the variable type they bind:

| Family | Binds | Widgets |
| --- | --- | --- |
| String | `string` | `text-input`, `textarea` |
| Number | `number` / `integer` | `number-field`, `slider`, `stepper` |
| Boolean | `boolean` | `toggle`, `checkbox` |
| Enum | `enum` | `select`, `radio-group`, `segmented` |
| Array | `array` | `multiselect`, `checkbox-group`, `tag-input` |

Each widget carries a `variable` config key naming the variable it drives;
changing the control sets that variable's runtime override and re-resolves the
document. The resolver enforces widget↔variable type compatibility — a widget
bound to a variable of an incompatible declared type fails
`WIDGET_TYPE_MISMATCH`. `select` is the canonical single-choice control and the
replacement for the retired `dropdown` item.

Per-widget config, the binding contract, and the type-compatibility rules are
documented on the [Widgets](widgets.md) page. See also
[Variables — Runtime inputs](variables.md#runtime-inputs).

## `connections/` — connection types

Connection types are referenced by document-scoped connection instances
(`{ id, $ref, config, secretRefs? }`). They are loaded into the same catalog as
item types and validated the same way. Connections are **declared and validated
only — never dialed**.

### `http` (`.../connections/http/1.0.0`)

A query-style backend: an endpoint `url`, optional `method` (`GET`/`POST`),
static `headers`, and static `query` params. Credentials are referenced
indirectly via the connection's `secretRefs` or `{ "$secret": ... }` config
values — never inlined.

### `static` (`.../connections/static/1.0.0`)

An inline data source: `rows` are embedded directly in `config` (objects mapping
column name to a JSON-scalar cell, nulls allowed), with an optional explicit
`columns` ordering. It exists so the result-shape contract can be exercised
without a real backend.

## `theme/` — the theme vocabulary

`theme.schema.json` (`.../theme/1.0.0`) defines the renderer-agnostic
[theme](theme.md): presentation choices expressed purely as closed,
enum-constrained **semantic tokens** (`emphasis`, `spacing`, `density`, `tone`,
`radius`, `border`) — no px, no hex, nothing medium-specific. It is referenced by
the document default theme and a block wrapper's `theme` override.

## Schema-level keywords

Item-type schemas may carry top-level keywords that are **not** instance config
and **not** standard JSON Schema validation — they are read by the
resolver/catalog. Examples: `configurable` (the
[configurable surface](configurable.md)), `expectedResult` (the
[result-shape contract](connections.md#result-shape-contract)), and:

- **`positional`** (boolean) — designates a type as a **layout-only positional
  region**: a node that only positions children and carries no chrome/theme.
  `container` and `variable-box` set `positional: true`. The marker is the
  **single source of truth** for which types are legal root/region children — the
  [tree grammar](blocks-and-grammar.md#the-grammar-rules) reads it via the catalog
  rather than a hardcoded type list, so a new type with the marker becomes a legal
  region child with no validation-code change.

## Examples are fixtures

Hand-written conforming documents live in `examples/` and double as downstream
test fixtures. See [Examples](../reference/examples.md).
