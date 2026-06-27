---
name: items-forms
description: The form-composition item family — `form` (a widget-only container that groups variable widgets in a flow or grid layout) and `configurator` (a leaf that auto-generates an editor for another item's configurable surface, targeting it by id or a reserved document scope). How each composes typed inputs and how their bindings/overrides flow. Pairs with items-inputs (the widgets a form groups), variables (the override model), and lattice_get_schema (the per-type field grammar + configurable surface).
type: reference
kind: items
applies_to: [lattice_get_schema, lattice_get_node, lattice_get_outline, lattice_validate_patch]
covers: [form, configurator]
---

# Items: the form family

This is a per-item **reference** for the two **form-composition** types —
**`form`** and **`configurator`**. Both assemble *typed inputs* into one unit, but
from opposite directions: a `form` is **authored** (you place widgets inside it),
a `configurator` is **generated** (it derives its controls from another item's
surface). This skill covers what each is *for* and how its bindings/overrides
flow — it does **not** list their fields. For the field grammar of either type
call **`lattice_get_schema`** (`form`, `configurator`); the schema drifts per server, so a
copy here would rot (see **session-bootstrap** → source layering).

## The two types at a glance

| Type | Composes inputs by… | Children? | Drives | Binding |
|---|---|---|---|---|
| `form` | **grouping authored widgets** | yes — widgets only | each widget sets its variable | variable override (re-resolve) |
| `configurator` | **generating controls from a target's surface** | no (leaf) | a target item's tunable fields | config override (ephemeral, `target.field`) |

## `form` — group authored widgets

`form` is, like `container`, a positional **region** — it holds and positions
children. Under the hood it is exactly `role: region` with a flow layout and a
*widgets-only* child policy: the same behavior category as `container` and
`variable-box`, differing only in those two attributes (this is the "form
collapse" — `form` is no longer a name-special case, just another region). A form
therefore may hold **only variable widgets** — a non-widget child (a container, a
table, …) fails fast with `LAYOUT_FORM_CHILD_INVALID`, naming the offending child.
A downstream server can publish its own region/wrapper/widget types into these
families by keyword — see **custom-item-types**. A form keeps a cluster of
controls together as one compact unit; the form instance itself carries a
`placement` for where it sits in its **parent** container's grid.

Each child widget binds its own variable through the ordinary widget contract
(see **items-inputs** / **variables**) — the form **groups** widgets, it does not
change how they bind. Submitting a control sets that one variable's runtime
override and re-resolves, exactly as a standalone widget would.

A form picks one of two **layout modes** via a `layout.mode` discriminator — flow
and grid are two modes of the one `form` type, not two types:

- **`flow`** (default — omit `layout`, or set `mode: "flow"`). Compact
  label+control cells fill row-major across an integer `columns` count and wrap.
  Widgets carry **no** `placement`. The resolver attaches a normalized `flow`
  block (resolved `columns` + per-child `cells`).
- **`grid`** (`mode: "grid"`). A weighted grid **identical in shape to a
  `container`'s** — `layout` carries `columns`/`rows` track-weight arrays + `gap`,
  and **each child** declares an explicit 1-indexed `placement`. It reuses the
  exact `container` grid path, so an out-of-bounds/malformed placement fails the
  same `LAYOUT_PLACEMENT_OUT_OF_BOUNDS` / `LAYOUT_PLACEMENT_INVALID` codes.

No CSS units anywhere: flow `columns` is a plain count (schema-bounded `[1,12]`);
grid weights are unitless relative weights. For the exact `layout` grammar call
`lattice_get_schema form` — and see **placement-grid** for the grid mechanics grid-mode
shares with `container`.

> **`form` vs `variable-box`.** Both group widgets directly (neither
> block-wraps). Reach for `form` when you want flow/grid layout *of the controls
> themselves* and label+control packing; reach for `variable-box` (see
> **items-inputs** / **variables**) for the dedicated, grouped-styling widget
> region with just a `stacked`/`inline` `arrangement`. A single control beside
> other panels needs neither — drop it straight in a `container` cell.

**Pick `form` when** you have a *set* of authored controls that should pack
together with a chosen layout — a settings panel, a filter bar, a parameter
block.

## `configurator` — generate an editor from a target

A `configurator` is a leaf (no children) that renders an editor for **another
item in the same document** — its *target* — or for a reserved document scope.
Instead of hand-authoring widgets, you point at a target and the resolver builds
the controls from that target's **configurable surface** — the schema-level list
of an item type's runtime-tunable fields (a `configurable` keyword on the type
schema; inspect it via `lattice_get_schema <type>`). Its authored config is just two
fields — a required `target` and an optional `title` (for the grammar,
`lattice_get_schema configurator`).

**Targeting.** `target` is either:

- the stable instance **`id`** of an item declared in the same document — the
  first thing that makes a stable `id` *required* on the item it points at; or
- a reserved **`$`-prefixed scope keyword** — `$manifest`, `$variables`,
  `$theme`, `$connections`, `$root` — routed to a document-level surface, never
  looked up as an item id (so it can't collide with an item named `theme`).

Target validation is fail-fast: `CONFIGURATOR_TARGET_MISSING_ID` (empty/blank
target), `CONFIGURATOR_TARGET_NOT_FOUND` (an id that no item declares — a dangling
reference), or `CONFIGURATOR_TARGET_SCOPE_UNKNOWN` (a `$`-keyword naming no known
scope).

**Generation.** On every resolve the configurator reads the target's validated
configurable surface and emits **one control per surface field**, in sorted field
order, picking the field's `rendering` hint or the canonical widget for its value
type (`string` → `text-input`, `number`/`integer` → `number-field`, `boolean` →
`toggle`, `enum` → `select`, `array` → `multiselect`). The controls use the same
**flow layout** a `form` uses. The whole editor is attached to the resolved node
as its `generated` form (`target`, `widgets`, `flow`). Because it's regenerated
from the surface each resolve, it can **never drift** from what the target
accepts. A surface-less target yields a present-but-**empty** form.

> **Composite fields are one control each.** The surface is top-level-only, so a
> table's `columns` or a form's `layout` is a *single* control; the inner shape
> travels in the field's `constraints` for the renderer. No per-sub-field editing.

**Submission — ephemeral config override.** A configurator does **not** bind a
variable; each generated control drives a **config override** (see **variables**) keyed
`target-id.field`. A change posts that override and the document re-resolves with
it applied **after interpolation**, validated against the target's surface — a
field off the surface fails `CONFIG_OVERRIDE_FIELD_UNKNOWN`, a bad value
`CONFIG_OVERRIDE_VALUE_INVALID`. The override is **ephemeral**: it adjusts only
that one resolution; the document on disk is never touched. (The durable
counterpart is a JSON Patch changeset, gated by the same surface — see
**patch-authoring**.) This is the key contrast with a `form`, whose widgets drive
*variable* overrides.

**Pick `configurator` when** you want to let a viewer retune an existing item (or
a document scope) live, without hand-authoring a control per field — and you're
willing to make that item carry a stable `id`. Reach for a `form` instead when the
controls drive *document variables* rather than one item's config.

## Inline example references

- **`examples/form-dashboard.json`** — a flow-mode form, a grid-mode form, **and**
  a standalone widget in one document, with a table consuming every bound variable
  via `$var` / `${}`. The grounding fixture for `form`.
- **`examples/configurator-dashboard.json`** — a `summary` table beside a
  configurator that targets it by id; the table surfaces `title`, `columns`,
  `query`, so the resolver generates a three-control editor bound to
  `summary.<field>`. The grounding fixture for an item-targeting `configurator`.
- **`examples/theme-configurator-dashboard.json`** — a configurator pointed at the
  reserved `$theme` scope, generating controls for the theme tokens; the
  document-scope-target case.

## Cross-links

- **items-inputs** — the widgets a `form` groups, the binding contract, and
  `variable-box` (the *other* widget home contrasted with `form`).
- **lattice_get_schema** — the `configurable` keyword on a type schema (the surface a
  `configurator` generates its editor from; it enumerates a target's tunable
  fields).
- **variables** — the variable-override model a form's widgets use, and the
  config-override model a configurator posts.
- **placement-grid** — the grid mechanics grid-mode `form` shares with `container`.
- **patch-authoring** — the durable counterpart to a configurator's ephemeral
  override (a changeset gated by the same surface).
- **custom-item-types** — `form` as a `region` behavior, and publishing your own
  region/widget types by keyword.
- **session-bootstrap** — source layering: why per-type field grammar stays in
  `lattice_get_schema`.
