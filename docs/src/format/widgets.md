# Widgets

A **widget** is a leaf item type that **sets a single variable**. Each widget
carries a `variable` config key naming the one variable it drives. Changing the
control records that variable's [runtime override](variables.md#runtime-inputs)
and re-resolves the document, so dependent `${var}` / `$var` consumers update
live.

Widgets are grouped into **families** by the variable type they bind. The
resolver enforces **widget↔variable type compatibility**: a widget may only bind
a variable whose declared `type` its family permits. A mismatch fails fast with
`WIDGET_TYPE_MISMATCH`. A widget never has children — only the
[`container`](catalog.md#container-itemscontainer100) and
[`form`](forms.md#the-form-container) item types may.

A widget is an ordinary item instance: place one directly in a `container` grid
cell, or group several inside a [`form`](forms.md). See
[Forms & Widget Placement](forms.md) for both arrangements.

## Families at a glance

| Family | Binds variable type | Widgets |
| --- | --- | --- |
| String | `string` | `text-input`, `textarea` |
| Number | `number` or `integer` | `number-field`, `slider`, `stepper` |
| Boolean | `boolean` | `toggle`, `checkbox` |
| Enum | `enum` | `select`, `radio-group`, `segmented` |
| Array | `array` | `multiselect`, `checkbox-group`, `tag-input` |

There are 13 widgets across these five families.

## The binding contract

Every widget shares the same binding shape and rules:

- **`variable` (required).** The name of the variable the widget sets. It must
  be a variable **visible in the widget's scope** — declared at document scope
  or on an ancestor [container](catalog.md#container-itemscontainer100). A widget
  binding a name not visible in scope fails with `VAR_UNDEFINED`.
- **Type compatibility.** The bound variable's declared `type` must be one the
  widget's family permits (see the table above). Otherwise the resolver reports
  `WIDGET_TYPE_MISMATCH`, naming the offending instance path, the widget, the
  variable, and its declared type.
- **The variable owns the value.** The widget only declares the binding and its
  presentation; the variable itself is declared in the document/container
  `variables` array and supplies the effective default. At resolution time the
  override-over-default rule applies (override > default) — see
  [Variables — Runtime inputs](variables.md#runtime-inputs).
- **Leaf only.** A widget never carries `children`.

### Shared presentation config

Across the families, widgets share a small presentation floor:

| Field | Type | Notes |
| --- | --- | --- |
| `label` | string | Optional label rendered with the control. |
| `description` | string | Optional help text rendered beside or beneath the control. |
| `disabled` | boolean | When true the control is read-only. |
| `default` | (family-typed) | Optional **widget-local** default shown before the viewer interacts. Presentation only — the variable's declared default remains the authoritative resolution-time value. Not present on the option-set array widgets or `tag-input`. |

Individual families add their own type-specific config, documented below.

## String family — `text-input`, `textarea`

Free-text controls that bind a `string` variable.

- **`text-input`** — a single-line field.
- **`textarea`** — a multi-line field.

Both accept the shared `label` / `description` / `disabled` / `default` plus:

| Field | Type | Notes |
| --- | --- | --- |
| `placeholder` | string | Optional placeholder shown in the empty field. |

```json
{
  "$ref": "https://lattice.dev/schemas/items/text-input/1.0.0",
  "id": "label-input",
  "config": {
    "label": "Panel label",
    "variable": "label",
    "placeholder": "Overview"
  }
}
```

## Number family — `number-field`, `slider`, `stepper`

Numeric controls that bind a `number` **or** `integer` variable.

- **`number-field`** — a free-entry number input.
- **`slider`** — a draggable track (benefits from `min`/`max` to bound it).
- **`stepper`** — increment / decrement buttons around a value.

All three accept the shared config plus an optional range:

| Field | Type | Notes |
| --- | --- | --- |
| `min` | number | Optional inclusive lower bound. Must not exceed `max`. |
| `max` | number | Optional inclusive upper bound. Must not be less than `min`. |
| `step` | number | Optional increment. Must be a positive number. |

The resolver rejects an **inverted range** (`min` > `max`) or a non-positive
`step` with `RESOLVE_CONFIG_INVALID`, naming the offending field. (JSON Schema
already guarantees each value's type and the positive-step bound; the cross-field
`min`/`max` relationship is the resolver's check.)

```json
{
  "$ref": "https://lattice.dev/schemas/items/slider/1.0.0",
  "id": "threshold-slider",
  "config": {
    "label": "Alert threshold",
    "variable": "threshold",
    "min": 0,
    "max": 100,
    "step": 5
  }
}
```

## Boolean family — `toggle`, `checkbox`

True/false controls that bind a `boolean` variable.

- **`toggle`** — an on/off switch.
- **`checkbox`** — a checkbox.

Both accept only the shared `label` / `description` / `disabled` / `default`
config — no type-specific fields.

```json
{
  "$ref": "https://lattice.dev/schemas/items/toggle/1.0.0",
  "id": "live-toggle",
  "config": { "label": "Live updates", "variable": "live" }
}
```

## Enum family — `select`, `radio-group`, `segmented`

Single-choice controls that bind an `enum` variable to a fixed option set.

- **`select`** — a single-choice `<select>` menu (the canonical runtime-input
  control, and the replacement for the retired `dropdown` item).
- **`radio-group`** — a column of mutually-exclusive radio buttons.
- **`segmented`** — a horizontal button row (suits small option sets).

All three **require** an `options` set in addition to the shared config:

| Field | Type | Notes |
| --- | --- | --- |
| `options` | array of `{ value, label? }` | **Required.** The selectable options in display order. At least one. `label` defaults to `value` when omitted. |
| `sort` | `declared` \| `label` \| `value` | Optional display ordering. `declared` (default) keeps the listed order; `label` sorts ascending by display label (falling back to value); `value` sorts ascending by value. |

The bound variable is declared with `type: "enum"` and its own `options`
(the authoritative permitted value set — see
[Variables — Types](variables.md#types)). The widget's `options` declare what is
shown and how it is ordered.

```json
{
  "$ref": "https://lattice.dev/schemas/items/select/1.0.0",
  "id": "region-select",
  "config": {
    "label": "Region",
    "variable": "region",
    "options": [
      { "value": "us", "label": "United States" },
      { "value": "eu", "label": "Europe" },
      { "value": "apac", "label": "Asia-Pacific" }
    ]
  }
}
```

## Array family — `multiselect`, `checkbox-group`, `tag-input`

Multi-value controls that bind an `array` variable. The selected (or entered)
values flow through the override path **as an array**.

- **`multiselect`** — a multi-choice menu.
- **`checkbox-group`** — a set of independent checkboxes, one per option.
- **`tag-input`** — a freeform tag/chip entry field.

`multiselect` and `checkbox-group` are **option-constrained**: they require a
bounded `{ value, label? }` option set, sharing the enum family's option shape
and `sort` ordering:

| Field | Type | Notes |
| --- | --- | --- |
| `options` | array of `{ value, label? }` | The values the bound array may contain, in display order. An **absent or empty** set fails with `RESOLVE_CONFIG_INVALID`. |
| `sort` | `declared` \| `label` \| `value` | Optional display ordering, as for the enum family. |

`tag-input` is **freeform**: it declares no `options` (the viewer types
arbitrary values that accumulate as tags) and instead accepts:

| Field | Type | Notes |
| --- | --- | --- |
| `placeholder` | string | Optional placeholder shown in the empty entry field. |

Unlike the enum family, an `array` variable declaration carries **no**
`options` of its own — the bounded set, when required, lives on the widget.

```json
{
  "$ref": "https://lattice.dev/schemas/items/multiselect/1.0.0",
  "id": "metrics-multiselect",
  "config": {
    "label": "Metrics",
    "variable": "metrics",
    "options": [
      { "value": "latency", "label": "Latency" },
      { "value": "errors", "label": "Error rate" },
      { "value": "throughput", "label": "Throughput" }
    ],
    "sort": "label"
  }
}
```

## Error codes

| Code | When |
| --- | --- |
| `WIDGET_TYPE_MISMATCH` | The bound variable's declared type is not one the widget's family permits. |
| `VAR_UNDEFINED` | The widget binds a name not visible in its scope. |
| `RESOLVE_CONFIG_INVALID` | A number widget has an inverted range or non-positive step, or an option-constrained array widget has no options. |

## Worked example

[`examples/widgets-dashboard.json`](../reference/examples.md) exercises one
widget per family — a `text-input`, a `slider`, a `toggle`, a `select`, a
`multiselect`, and a freeform `tag-input` — each bound to a matching variable and
consumed by a table through `$var` typed bindings and `${}` string templates.
