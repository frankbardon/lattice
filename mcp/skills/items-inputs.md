---
name: items-inputs
description: The runtime-input item family — the 13 variable widgets (text-input, textarea, number-field, slider, stepper, toggle, checkbox, select, radio-group, segmented, multiselect, checkbox-group, tag-input) that each SET one variable, plus the variable-box region that holds them. Grouped by binding shape (string / numeric / boolean / single-select enum / multi-value array) with the widget↔variable type-compatibility contract and "pick this when" guidance. Pairs with variables (the binding target), items-forms (grouping widgets), and lattice_get_schema (the per-type field grammar).
type: reference
kind: items
applies_to: [lattice_get_schema, lattice_get_node, lattice_get_outline, lattice_validate_patch]
covers: [text-input, textarea, number-field, slider, stepper, toggle, checkbox, select, radio-group, segmented, multiselect, checkbox-group, tag-input, variable-box]
---

# Items: the input family

This is a per-item **reference** for the runtime-input family: **13 variable
widgets** plus the **`variable-box`** region that houses them. A **widget** is a
leaf item that **sets exactly one variable** — it carries no children and no
content; its whole job is to drive a variable's runtime override so dependent
`${var}` / `$var` consumers re-resolve live. This skill groups the widgets by
**binding shape**, states the one contract they all share, and says *which to
reach for* — it does **not** list any type's fields. For the field grammar of any
type call **`lattice_get_schema`** (the type name); schemas drift per server, so any copy
here would rot (see **session-bootstrap** → source layering).

## The binding contract (every widget)

A widget binds a variable through a required **`variable`** config key naming the
single variable it sets. Two rules the resolver enforces fail-fast:

- **Visible in scope.** `variable` must name a variable declared at document
  scope or on an ancestor container, visible where the widget sits — else
  `VAR_UNDEFINED`. Scope and shadowing are covered in **variables**.
- **Type-compatible.** Each widget belongs to a **family** keyed to the variable
  *type* it may bind. Binding a variable whose declared `type` the family forbids
  fails `WIDGET_TYPE_MISMATCH` (it names the path, widget, variable, and type).

Every widget here is a built-in member of the **widget** behavior category (a leaf
that sets one variable); `variable-box` is a **region**. A downstream server can
publish its own widget type into this family by keyword, inheriting the same
binding contract — see **custom-item-types**.

The **variable owns the value**: the widget declares only the binding and its
presentation; the variable's declaration (in a `variables` array) supplies the
authoritative default, and at resolve time *override beats default*. A widget's
own `default` config (where present) is presentation-only — what's shown before
the viewer interacts — never the resolution-time value. The shared presentation
floor (`label`, `description`, `disabled`, and the family-typed `default`) is
common across families; per-type extras live in `lattice_get_schema`.

## The five families (pick by variable type)

Choose the family by the **type of the variable** you're driving, then the widget
by presentation. The variable type is the hard constraint; the rest is taste.

| Family | Binds variable type | Widgets |
|---|---|---|
| **String** | `string` | `text-input`, `textarea` |
| **Number** | `number` *or* `integer` | `number-field`, `slider`, `stepper` |
| **Boolean** | `boolean` | `toggle`, `checkbox` |
| **Enum** (single-select) | `enum` | `select`, `radio-group`, `segmented` |
| **Array** (multi-value) | `array` | `multiselect`, `checkbox-group`, `tag-input` |

### String — `text-input`, `textarea`

Free-text controls binding a `string` variable.

- **`text-input`** — single-line. Pick for a short value: a label, a name, an id.
- **`textarea`** — multi-line. Pick for a longer body: a note, a description.

Both add an optional `placeholder` (see `lattice_get_schema`).

### Number — `number-field`, `slider`, `stepper`

Numeric controls binding a `number` **or** `integer` variable.

- **`number-field`** — free-entry box. Pick for an exact, unbounded figure.
- **`slider`** — a draggable track. Pick for a bounded magnitude tuned by feel
  (give it `min`/`max` so the track has ends).
- **`stepper`** — −/+ buttons around a value. Pick for small integer nudges
  (a count, a window size) where ±1 steps suit.

All three accept an optional `min` / `max` / `step` range. The resolver rejects an
**inverted range** (`min` > `max`) or a non-positive `step` with
`RESOLVE_CONFIG_INVALID`, naming the field — a cross-field check JSON Schema can't
express. Confirm the exact fields with `lattice_get_schema`.

### Boolean — `toggle`, `checkbox`

True/false controls binding a `boolean` variable. No type-specific fields beyond
the shared floor.

- **`toggle`** — an on/off switch. Pick for a setting/mode ("Live updates").
- **`checkbox`** — a single checkbox. Pick for an opt-in / acknowledgement.

> A single `checkbox` (boolean) is distinct from `checkbox-group` (array) — same
> word, different families. Don't conflate them.

### Enum (single-select) — `select`, `radio-group`, `segmented`

Single-choice controls binding an `enum` variable to a fixed option set. All
three **require** an `options` set (`{ value, label? }`, ≥1) and accept an
optional `sort`. The bound `enum` variable carries its **own** authoritative
`options` (the permitted value set — see **variables**); the widget's `options`
declare what is *shown* and in what order.

- **`select`** — a `<select>` menu. The canonical single-choice control; default
  pick, and scales to long option lists. (Replaces the retired `dropdown`.)
- **`radio-group`** — a column of mutually-exclusive radios. Pick for a small set
  where all options should be visible at once.
- **`segmented`** — a horizontal button row. Pick for a very small set (2–4) read
  as a toggle bar.

### Array (multi-value) — `multiselect`, `checkbox-group`, `tag-input`

Multi-value controls binding an `array` variable; the selected/entered values
flow through the override path **as an array**.

- **`multiselect`** — a multi-choice menu. Pick for picking several from a bounded
  set, especially a long one.
- **`checkbox-group`** — independent checkboxes, one per option. Pick for a short
  bounded set where every option should be visible.
- **`tag-input`** — freeform tag/chip entry. Pick when the viewer types
  **arbitrary** values rather than choosing from a fixed set.

`multiselect` and `checkbox-group` are **option-constrained** — they require a
bounded `options` set (same `{ value, label? }` + `sort` shape as the enum
family); an absent/empty set fails `RESOLVE_CONFIG_INVALID`. `tag-input` is
**freeform** — it declares **no** `options` (it takes a `placeholder` instead).
Note the asymmetry vs enum: an `array` variable declaration carries no `options`
of its own, so the bounded set — when required — lives on the widget. Confirm
each type's option/placeholder fields with `lattice_get_schema`.

## `variable-box` — where widgets live

A **`variable-box`** is a **positional region** (like `container`, layout-only,
**no chrome/theme** of its own) dedicated to holding variable widgets. Its widget
children are held **directly** — they are **NOT** block-wrapped (the box, not a
per-widget block, supplies their grouped presentation). Its only surface is an
`arrangement` (`stacked` — default, one widget per row — or `inline` — a single
row); for that field call `lattice_get_schema variable-box`.

A variable-box may hold **only** variable widgets, directly — a block-wrapped or
nested-region child fails `GRAMMAR_VARIABLE_BOX_CHILD_INVALID`. It is the
**standalone** home for widgets; a `form` content leaf is the other (see
**items-forms**). A widget may also sit alone directly in a normal `container`
grid cell with its own `placement` — no box required — when a single control
belongs beside other panels. Reach for the variable-box (or a `form`) when a
*cluster* of controls should pack together as a unit.

## Pick this when… (cheat sheet)

| You're driving a… | Variable type | Reach for |
|---|---|---|
| short text value | `string` | `text-input` |
| long text body | `string` | `textarea` |
| exact number | `number`/`integer` | `number-field` |
| bounded magnitude by feel | `number`/`integer` | `slider` |
| small integer nudged ±1 | `integer` | `stepper` |
| on/off setting | `boolean` | `toggle` |
| single opt-in | `boolean` | `checkbox` |
| one of many (long list) | `enum` | `select` |
| one of a few (all visible) | `enum` | `radio-group` |
| one of 2–4 (button bar) | `enum` | `segmented` |
| several from a bounded set | `array` | `multiselect` |
| several, short set all visible | `array` | `checkbox-group` |
| arbitrary typed-in values | `array` | `tag-input` |

The variable type is the constraint a `WIDGET_TYPE_MISMATCH` enforces; the rest is
presentation. Confirm required/optional fields per type with `lattice_get_schema` — never
guess them from this table.

## Inline example references

- **`examples/widgets-dashboard.json`** — one widget per family (a `text-input`,
  a `slider`, a `toggle`, a `select`, a `multiselect`, a freeform `tag-input`),
  each bound to a matching variable and consumed by a table via `$var` typed
  bindings and `${}` string templates. The grounding fixture for the families.
- **`examples/dropdown-dashboard.json`** — a `select` inside a `variable-box`,
  driving a variable that a table consumes; the canonical single-widget-in-a-box
  shape (and the `dropdown` → `select` replacement).
- **`examples/binding-dashboard.json`** — the *consumer* side: how a variable a
  widget sets reaches config via `$var` / `${}` (no widgets itself, but the other
  half of the loop).

## Cross-links

- **variables** — the binding target: declaring the typed variable, `${name}` vs
  `{ "$var": "name" }`, scope/shadowing, and how an override re-resolves the tree.
- **items-forms** — the `form` container (the *other* home for widgets) and the
  `configurator` that auto-generates widgets from a target's surface.
- **items-layout** — `container` (a standalone widget's grid cell) and how regions
  nest; `variable-box` is the positional region introduced there.
- **placement-grid** — placing a `variable-box` (or a standalone widget) on a grid.
- **custom-item-types** — publishing your own widget type by keyword, inheriting
  the same `binds` type-compatibility contract.
- **session-bootstrap** — source layering: why per-type field grammar stays in
  `lattice_get_schema`.
