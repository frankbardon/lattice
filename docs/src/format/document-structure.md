# Document Structure

A dashboard document is a single JSON object validated against
`schemas/dashboard.schema.json`
(`$id: https://lattice.dev/schemas/dashboard/1.0.0`). It has two required
members and two optional ones:

```json
{
  "manifest": { ... },
  "root": { ... },
  "variables": [ ... ],
  "theme": { ... },
  "connections": [ ... ]
}
```

No other top-level keys are allowed (`additionalProperties: false`).

## `manifest` (required)

Document-level metadata.

| Field | Required | Notes |
| --- | --- | --- |
| `formatVersion` | yes | Semver of the document format, e.g. `"1.0.0"` (pattern `^\d+\.\d+\.\d+$`). |
| `id` | yes | Stable machine-readable identifier for the dashboard. |
| `title` | yes | Human-readable title. |
| `description` | no | Longer-form description. |
| `author` | no | Author or owner. |

The manifest is passed through **verbatim** into the resolved tree.

## `root` (required)

The root **item instance**. Every item in a dashboard — including the root — is
an instance node of this shape:

```json
{
  "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
  "id": "root",
  "config": { ... },
  "placement": { ... },
  "variables": [ ... ],
  "children": [ ... ]
}
```

| Field | Required | Notes |
| --- | --- | --- |
| `$ref` | yes | URI of the item-type schema this node instantiates. |
| `id` | no | Instance-local identifier, unique within the document. |
| `config` | no | Per-instance configuration; its shape is defined by the referenced item type and is opaque at the document level. |
| `placement` | no | Layout hints interpreted by the parent container's grid. |
| `variables` | no | Variable declarations scoped to this node and its descendants (meaningful on containers). |
| `children` | no | Child instances. |

The root is conventionally a `container`, and under the tree grammar (below) it
must be a **positional region** type.

### The tree grammar

`children` is structurally permitted on **any** instance by the schema, but where
each kind of node may actually appear is governed by the **tree grammar**, a
fail-fast resolver pass over the assembled tree. In short: the **root** holds only
positional regions; a **container** holds nested regions or **block wrappers** (a
bare content leaf must be wrapped in a block); a **variable-box** holds variable
widgets directly; a **block** wraps exactly one content leaf and never re-wraps a
block; and a region carries no theme. The full set of rules, the
[block wrapper](blocks-and-grammar.md), the `positional` marker, and the grammar
error codes are documented on the
[Blocks & the Tree Grammar](blocks-and-grammar.md) page.

The **container** is a positional region: it arranges its children on a
relative-weight grid (`config.grid`) — unitless column and row track weights plus
a gap — and children place themselves with explicit, 1-indexed `placement`
coordinates (`colStart`, `colSpan`, `rowStart`, `rowSpan`, each defaulting to 1).
The resolver normalizes the tracks to fractions summing to 1 per axis and
validates each placement against the grid bounds (`LAYOUT_PLACEMENT_INVALID`,
`LAYOUT_PLACEMENT_OUT_OF_BOUNDS`). No CSS keywords or absolute units appear
anywhere — the grid is renderer-agnostic.

## `variables` (optional)

An array of document-scope [variable declarations](variables.md). They are
visible to every node unless shadowed by a same-named declaration on a
descendant container.

## `theme` (optional)

The document-scope **default [theme](theme.md)** — the document's base
presentation choices, drawn from the shared semantic-token vocabulary. It is the
*default layer*; per-block `theme` overrides are emitted side by side and never
merged into it. See [Theme](theme.md#no-merge-side-by-side-layers).

## `connections` (optional)

An array of document-scoped [connection instances](connections.md) — data
sources that items bind to by id. Connections are declared and validated only;
they are never dialed.

## The resolved tree

`lattice resolve` emits a **resolved tree** whose shape is a stable,
JSON-tagged contract (additive changes only). Its top-level members are:

```json
{
  "manifest": { ... },
  "root": { ... resolved instance ... },
  "defaultTheme": { ... },
  "connections": [ ... resolved connections ... ]
}
```

`defaultTheme` is the document's [default theme](theme.md) (the default layer
only — per-block overrides ride on their own block nodes, never merged here);
omitted when the document declares no theme.

A **resolved instance** records more than the source node, because resolution
has already validated and computed several things:

| Field | Meaning |
| --- | --- |
| `id` | Copied from the source instance (omitted if none). |
| `type` | The resolved type identity: the raw `ref`, the canonical `id`, and the parsed `name`/`version`. |
| `container` | `true` when the resolved type is a container. Surfaced so consumers need not re-derive it. |
| `config` | The **interpolated**, schema-validated config (variable references already substituted). |
| `placement` | Verbatim placement hints. |
| `layout` | For containers only: the normalized grid (fractional track sizes + each child's validated placement). |
| `children` | Resolved child instances, in document order. |
| `varEnv` | The variable environment **visible at this node** (see [Variables](variables.md)). |
| `binding` | For bound items only: the resolved data binding (see [Connections](connections.md)). |

Because the resolved tree is fully validated, downstream consumers (a renderer,
the `serve` inspector, a future dependency tracker) may assume every node is
structurally valid and type-checked.
