# Lattice schema catalog

JSON Schema (draft 2020-12) definitions for Lattice dashboard documents and item
types.

## Base URL

All schema `$id`s use the stable base URL:

```
https://lattice.dev/schemas/
```

The URL is an identifier namespace, not a live fetch target — the resolver loads
schemas from this local catalog and keys them by `$id`. Item-type schemas embed a
semver in the path so multiple versions can coexist:

```
https://lattice.dev/schemas/<name>/<major>.<minor>.<patch>
```

## Layout

- `dashboard.schema.json` — top-level document schema (`https://lattice.dev/schemas/dashboard/1.0.0`).
  Defines the `manifest` and the recursive `root` item instance. An instance node
  is `{ "$ref": <item-type uri>, "config", "placement", "children"? }`.
  `children` is permitted structurally on any node; the rule that only containers
  may have children is enforced by the resolver (E1-S4), not by this schema.
- `items/` — item-type schemas referenced by instance `$ref`s.
  - `container.schema.json` (`.../items/container/1.0.0`) — a **positional
    region** (see the `positional` marker below); groups children on a
    relative-weight grid. E2-S1 formalizes relative-weight tracks + placement.
  - `variable-box.schema.json` (`.../items/variable-box/1.0.0`) — a **positional
    region** dedicated to holding the **variable widgets** (text-input, slider,
    select, …). Like `container` it is layout-only and carries no chrome/theme;
    it is distinguished from a container by its type identity and provides the
    grouped downstream styling for the variable widgets it holds. Its
    variable-widget children are held DIRECTLY — they are NOT individually
    `block`-wrapped (the box, not a per-widget wrapper, supplies their grouped
    presentation). It declares a single layout-only `arrangement`
    (`stacked`|`inline`) surface — the analogue of a container's grid. The
    container/variable-box-children grammar is enforced by the resolver (E3-S2).
  - `block.schema.json` (`.../items/block/1.0.0`) — the mandatory **wrapper**,
    DISTINCT from `container`: it wraps EXACTLY ONE inner content item (held in
    `config.content`, not the document `children` array) and carries the
    cross-cutting per-block concerns applied uniformly to whatever it wraps — a
    required stable `id` (the patch/configurator anchor), an optional `theme`
    override (`$ref`s the theme schema; validated against the token vocabulary,
    attached verbatim with NO merge), a human `title`, and a `visibility` flag.
    It groups no grid and arranges nothing. `title`/`visibility` are a configurable
    surface. The resolver emits the wrapper and its single inner content as
    SEPARATE nodes (inner = the wrapper's one child); the wrapper invariants
    (required `id`, exactly-one content) and the no-wrapper-in-wrapper rule are
    enforced by the resolver (E1-S2 / E3-S2), not structurally here.
  - `table.schema.json` (`.../items/table/1.0.0`) — a tabular leaf type. It may
    render static columns/rows or bind to a connection by `connectionId` (E4-S2).
    It declares an `expectedResult` keyword — the result-shape contract (E4-S3):
    a JSON Schema fragment describing the rows a bound connection is expected to
    return (here, an array of non-empty row objects whose cells are non-null
    scalars). `expectedResult` is a schema-level keyword, NOT instance config; it
    is ignored by config validation and read by the resolver, which requires a
    well-formed contract on any bound item and validates inline `static`
    connection data against it.
  - `select.schema.json` / `radio-group.schema.json` / `segmented.schema.json`
    (`.../items/{select,radio-group,segmented}/1.0.0`) — the enum widget family
    (E1-S3). Each is a runtime-input leaf binding an `enum` variable to a fixed
    `{value,label?}` option set with optional `sort` ordering. `select` renders
    as a single-choice `<select>` menu (the canonical single-choice runtime
    control), `radio-group` as a radio column, `segmented` as a button
    row. Changing the selection re-resolves the document; the chosen value
    becomes the variable's runtime override (override > default), so dependent
    `${var}`/`$var` consumers update live. The resolver enforces widget↔variable
    type compatibility (enum widget bound to a non-enum variable →
    `WIDGET_TYPE_MISMATCH`).
- `theme/` — the renderer-agnostic theme vocabulary.
  - `theme.schema.json` (`.../theme/1.0.0`) — presentation choices expressed as
    closed, enum-constrained **semantic tokens** (e.g. `emphasis: none|low|high`,
    `spacing: compact|cosy|roomy`). No px, no hex, nothing HTML/CSS/medium-specific.
    Tokens are ordinary `enum` fields compatible with the configurable-surface
    mechanism. Referenced by the document default theme (E2-S2) and the block
    wrapper's `theme` override (E2-S3); kept small and structured (base tokens +
    room for per-type extension).
- `connections/` — connection (data source) type schemas, referenced by
  document-scoped connection instances (`{ id, $ref, config, secretRefs? }`).
  Loaded into the same catalog as item types and validated the same way by the
  resolver (E4-S1). Connections are declared and validated only — never dialed.
  - `http.schema.json` (`.../connections/http/1.0.0`) — a query-style backend
    (endpoint + request shape; credentials via `secretRefs`, never inlined).
  - `static.schema.json` (`.../connections/static/1.0.0`) — an inline data
    source whose rows live in `config`; lets the result-shape contract (E4-S3)
    be exercised without a real backend.

## Schema-level keywords

Item-type schemas may carry top-level keywords that are NOT instance config and
NOT standard JSON Schema validation — they are read by the resolver/catalog
(captured by the parser as unknown keywords). Existing examples: `configurable`
(the runtime-configurable surface), `expectedResult` (the result-shape contract).

- `positional` (boolean) — designates a type as a **layout-only positional
  region**: a node that only positions children and carries no chrome/theme of
  its own. `container` and `variable-box` set `positional: true`. The marker is
  the **single source of truth** for which types are legal positional regions —
  the grammar pass (E3-S2) reads it via the catalog (`Catalog.IsPositional` /
  `ResolvedType.IsPositional`) rather than any hardcoded type list, so adding a
  new type with the marker makes it a legal root/container child WITHOUT any
  validation-code change. Positional region schemas declare no chrome/theme
  fields, only their own layout-only surface (e.g. `container`'s `grid`,
  `variable-box`'s `arrangement`).

## Examples

Hand-written conforming documents live in `../examples/` and serve as downstream
fixtures.
