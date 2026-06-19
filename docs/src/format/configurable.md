# Configurable Surfaces

An item type declares which of its config fields are **runtime-configurable** —
editable by a viewer or an external override — through a schema-level
`configurable` keyword. Together those entries form the item type's
**configurable surface**: the honest, machine-readable list of knobs a
configurator may expose and the [override system](variables.md#runtime-inputs)
may set, without a human hand-maintaining a parallel list.

The surface is **declarative data on the item-type schema**, not per-instance
config. Every instance of a type shares the same surface. The resolver validates
each declaration fail-fast and attaches the validated surface to the resolved
instance, so downstream consumers read it without re-parsing the schema.

## The `configurable` keyword

`configurable` is a top-level keyword on an item-type schema — a sibling of
`type` and `properties`, not a config property. It maps each
runtime-configurable config **field** to a descriptor:

```json
{
  "type": "object",
  "additionalProperties": false,
  "configurable": {
    "label": {
      "type": "string",
      "label": "Label",
      "rendering": "text-input",
      "constraints": {
        "description": "The human label rendered beside the control."
      }
    },
    "disabled": {
      "type": "boolean",
      "label": "Disabled",
      "rendering": "toggle"
    }
  },
  "properties": {
    "label": { "type": "string" },
    "disabled": { "type": "boolean" }
  }
}
```

Each descriptor carries:

- **`type` (required).** The field's value type, drawn from the
  [variable type set](variables.md): `string`, `number`, `integer`, `boolean`,
  `enum`, or `array`. This is the type an editor or override deals in — not
  necessarily the raw JSON Schema type of the underlying property (see
  [Object-shaped fields](#object-shaped-fields)).
- **`label` (optional).** A human label for the field, shown by a configurator.
- **`constraints` (optional).** An **opaque** object the resolver passes through
  verbatim. Use it to carry editing hints — an `enum` of allowed values, a
  `description`, the shape of nested sub-`fields` — for downstream tools. The
  resolver does not interpret it.
- **`rendering` (optional).** A preferred widget item-type to edit this field
  with, e.g. `text-input` or `toggle`. It must name a
  [registered widget](widgets.md), so a configurator can auto-pick a control.

## Validation rules

On every resolve the resolver validates the `configurable` declaration of each
node's item type, fail-fast, reporting `CONFIGURABLE_SURFACE_INVALID` (with the
offending `path`, `type`, and `field` in the error details) when:

- a declared **field name is not a real config property** of the item type
  (it is absent from the schema's `properties`), or
- a field's **`type` is not one of the variable type set**, or
- a **`rendering` hint names a widget** the catalog does not know.

A type that declares no `configurable` keyword simply has an empty surface — it
is not an error.

## Constraints: top-level fields only

The mechanism validates field names against the item type's **top-level**
schema properties only, and field `type`s against the **variable type set**
(`string` / `number` / `integer` / `boolean` / `enum` / `array` — no nested
`object`). So a surface is declared only on **top-level config fields** carrying
those types. A nested object property cannot be surfaced as an `object`; instead
it is surfaced under the closest variable type and its inner shape is described
in `constraints`.

### Object-shaped fields

When a meaningful editable field is itself an object — the
[`container`](catalog.md#container-itemscontainer100) `grid`, or the
[`form`](forms.md) `layout` — it is declared with the closest variable type
(`array`) and its sub-fields are spelled out in `constraints.fields` for the
configurator to walk:

```json
"configurable": {
  "layout": {
    "type": "array",
    "label": "Form layout",
    "constraints": {
      "description": "The form's layout: mode plus columns/rows/gap.",
      "fields": {
        "mode": { "type": "string", "enum": ["flow", "grid"] },
        "columns": { "description": "Flow: column count. Grid: weight array." }
      }
    }
  }
}
```

## What declares a surface

Every shipped item type that has runtime-tunable presentation declares one:

- All **13 [widgets](widgets.md)** surface their shared `label` and `disabled`,
  plus family-specific fields — `placeholder` (string family and `tag-input`),
  `min` / `max` / `step` (number family), and `sort` (enum and option-array
  families).
- The **[`form`](forms.md)** container surfaces its `layout`.
- The **[`container`](catalog.md#container-itemscontainer100)** surfaces its
  `grid`, and the **[`table`](catalog.md#table-itemstable100)** surfaces its
  `title`, `columns`, and `query`.

## What reads a surface

The resolver attaches the validated surface to each resolved instance, in
sorted field order, so downstream layers read it directly:

- A **[configurator](configurator.md)** auto-generates an editor from the
  surface — one control per field, picked by the `rendering` hint.
- The **override system** knows exactly which fields it may set at runtime.
- A **[JSON Patch changeset](changesets.md)** uses the surface as its
  **guardrail** — it enumerates the only paths a patch may legally touch.

Because the surface is derived from the schema and validated on every resolve, a
declared surface can never drift out of sync with the properties the item type
actually accepts.
