# Configurators

A **configurator** is a leaf item type that renders an editor for **another item
in the same document** — its *target*. Instead of hand-authoring a form of
controls, you point a configurator at a target by that target's stable instance
`id`, and the resolver builds the editor for you from the target's
[configurable surface](configurable.md). The generated controls drive
[config overrides](overrides.md#config-overrides) that re-resolve the target
ephemerally, so a viewer can retune one item live without the document ever
being mutated on disk.

A configurator never has children.

## Declaring a configurator

A configurator instance `$ref`s the configurator item type and carries just two
config fields:

```json
{
  "$ref": "https://lattice.dev/schemas/items/configurator/1.0.0",
  "id": "summary-configurator",
  "config": {
    "target": "summary",
    "title": "Configure the summary table"
  }
}
```

- **`target` (required).** The stable instance `id` of the item this
  configurator edits. It must reference an item declared **in the same
  document** that carries an `id`. Most items omit `id`; a configurator's target
  is the first thing that makes a stable `id` *required* on the item it points
  at.
- **`title` (optional).** A heading rendered above the generated editor. It is
  itself a configurable field, so it can be retuned at runtime like any other.

That is the entire authored surface of a configurator — there is no per-field
form to write. The editor is derived purely from the target.

### Target validation

On every resolve the configurator pass builds a tree-wide `id` index once, then
validates each configurator's `target` against it, fail-fast:

- `CONFIGURATOR_TARGET_MISSING_ID` — the `target` reference is empty or
  whitespace-only, so there is no id to look up.
- `CONFIGURATOR_TARGET_NOT_FOUND` — the `target` is a well-formed id but **no
  item in the document declares it**; the reference dangles.

Both errors name the offending configurator's `path` in their details.

## The auto-generated form

When the target resolves, the configurator pass reads the target's validated
[configurable surface](configurable.md) and generates **one control per surface
field**, in surface (sorted field) order. For each field it picks:

- the field's preferred **`rendering`** widget when the surface declares one, else
- the **canonical widget** for the field's value type — `string` → `text-input`,
  `number` / `integer` → `number-field`, `boolean` → `toggle`, `enum` →
  `select`, `array` → `multiselect`.

The controls are laid out with the same **flow layout** a
[`form`](forms.md) uses, so a renderer arranges them exactly like an authored
form. The whole editor is attached to the resolved configurator node as its
`generated` form: a `target` id, the list of `widgets`, and the `flow` layout.
Each generated widget records the `widget` item type that renders it, the
target `field` it edits, the field's value `type`, a human `label`, and the
field's opaque `constraints` (option sets, `min`/`max`, nested sub-`fields`)
passed through verbatim for the renderer to honor.

Because the form is derived from the surface and regenerated on every resolve,
the editor can never drift out of sync with what the target actually accepts. A
configurator that targets a surface-less item yields an empty (but present)
form, so a renderer can always tell a resolved configurator from an unresolved
one.

> **Composite fields are one control each.** The configurable surface is
> top-level-only, so a table's `columns` array or a form's `layout` is a
> *single* surface entry — and the configurator generates a single control for
> the whole field, not one per sub-field. The field's inner shape travels in
> `constraints` for the renderer; per-sub-field editing is out of scope.

## The ephemeral mutation model

Each generated widget carries the **override address** it drives:
`<target-id>.<field>`. When a viewer changes a control, the renderer posts a
[config override](overrides.md#config-overrides) keyed by that address, and the
document re-resolves with the override applied. As with every
[runtime override](overrides.md):

- The override is **ephemeral** — it adjusts only the target's resolved instance
  for that one resolution; the document on disk is never touched.
- It is applied **after interpolation** and validated against the target's
  configurable surface. A field not on the surface fails
  `CONFIG_OVERRIDE_FIELD_UNKNOWN`; a value violating the field's declared type or
  constraints fails `CONFIG_OVERRIDE_VALUE_INVALID`.

So a configurator is the authoring-side counterpart to the override system: the
[configurable surface](configurable.md) declares *which* fields are tunable, the
configurator *renders the editor* for them, and a
[config override](overrides.md#config-overrides) is *what a change posts*. The
served page wires this loop end to end — see
[Supplying overrides through `serve`](overrides.md#supplying-overrides-through-serve).

## Worked example

`examples/configurator-dashboard.json` places a `summary` table beside a
configurator that targets it by id. The table surfaces `title`, `columns`, and
`query`, so the resolver generates a three-control editor — a `text-input` for
the title and a control for each composite field — each bound to
`summary.<field>`. See [Examples](../reference/examples.md).
