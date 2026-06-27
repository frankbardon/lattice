---
name: custom-item-types
description: How a downstream consumer publishes a FIRST-CLASS custom item type — no fork, no Go — by tagging its `.schema.json` with the `latticeBehavior` keyword. Explains the three behavior roles (region, wrapper, widget), what each is FOR, and when to pick it, so a custom type joins the built-in catalog and resolves under the same grammar. The built-ins (container/variable-box/form are regions, block is a wrapper, the inputs are widgets) are just the first citizens of this scheme. Pairs with items-layout / items-inputs / items-forms (the built-in families per role), patch-authoring (editing instances), and lattice_get_schema (the live grammar, including each type's own latticeBehavior).
type: guide
kind: workflow
applies_to: [lattice_get_schema, lattice_list_schemas, lattice_get_manifest, lattice_validate_patch]
covers: [latticeBehavior, region, wrapper, widget]
---

# Custom item types

How to add a **first-class** custom item type to a lattice server **without
forking it and without writing Go**. You ship one item-type `.schema.json`
through the same pluggable schema `fs.FS` the built-ins load from, and you tag it
with the **`latticeBehavior`** keyword. That keyword is the contract that makes
the resolver treat your type exactly like a built-in: it joins the catalog
`lattice_get_manifest` reports, `lattice_list_schemas` lists it, `lattice_get_schema` returns it, and it
resolves and validates under the *same* tree grammar — no name-string special
cases anywhere.

This skill teaches the **vocabulary and intent** of `latticeBehavior` — the three
roles and *when to pick each*. It does **not** restate any type's field grammar.
For the actual fields/types/enums of a type — *including the concrete shape of its
`latticeBehavior` block* — call **`lattice_get_schema {type}`**; that grammar drifts per
server and per type, so any copy here would rot (see **session-bootstrap** →
source layering).

## What `latticeBehavior` does

Every behavior the resolver gives a built-in — *this type holds children, this one
wraps a single inner item, this one sets a variable* — is selected by the
`latticeBehavior` keyword on its schema, **not** by the type's name. The built-in
types are simply the first types tagged this way. Tag your own type with the same
keyword and it gains the identical behavior; the resolver never checks the literal
type token.

The keyword names one **role** plus the attributes that role needs. There are
exactly **three roles**, and picking the right one is the whole decision.

| Role | The type's job | Pick it when your type… |
|---|---|---|
| `region` | Holds and positions children | groups other items and lays them out |
| `wrapper` | Wraps exactly one inner instance | carries per-item chrome around a single child |
| `widget` | Sets exactly one variable | is a runtime input control |

A type with a malformed `latticeBehavior` is rejected **fast, at catalog-index
time** (before any document resolves) with **`SCHEMA_BEHAVIOR_INVALID`**, naming
the offending schema — so a bad role or a missing/contradictory attribute fails
when the catalog loads, not deep in a resolve.

## `role: region` — hold and position children

A **region** is a positional container: it owns a `children` set and arranges
them. Reach for it when your custom type's job is to *group and place* other
items — a panel, a band, a split, a dedicated input area.

Two attributes shape a region (read their exact form via `lattice_get_schema`):

- A **child-policy** attribute decides *what kind of children* the region admits —
  either nested regions and block **wrappers**, or **widgets** only. This is what
  the tree grammar enforces fail-fast: a child the policy forbids is rejected on
  the resolved tree, the same way it is for a built-in region.
- A **layout** attribute decides *how* children are placed — on a weighted grid,
  in a wrapping flow, or with no layout of its own.

The built-in regions are just three points in this space: **`container`** (admits
regions/wrappers, grid layout), **`variable-box`** (admits widgets, no layout),
and **`form`** (admits widgets, flow layout). See **items-layout** for
container, **items-inputs** for variable-box, and **items-forms** for form. Your
custom region picks its own policy + layout and slots in beside them.

## `role: wrapper` — wrap one inner instance

A **wrapper** carries cross-cutting, per-item concerns around **exactly one**
inner instance, held in a single named **content field** (named by an attribute on
the behavior — read it via `lattice_get_schema`). Reach for it when your type's job is to
*decorate a single child* (an id, a title, visibility, a theme override) rather
than to hold many.

The sole built-in wrapper is **`block`** — see **items-layout** and **blocks**
for the wrapper↔content split and how `lattice_get_node` surfaces the *inner* item's
editable fields against the wrapper node. A custom wrapper inherits the same
guards (one inner instance; never re-wrap another wrapper) automatically.

## `role: widget` — set one variable

A **widget** is a leaf input that **sets exactly one variable**, driving its
runtime override so dependent `${var}` / `$var` consumers re-resolve live. Reach
for it when your type is a *control* — a new kind of picker, entry, or switch.

A widget's behavior carries:

- a **binds** attribute: the variable **type(s)** this widget is allowed to drive.
  This *is* the type-compatibility contract — binding a variable whose declared
  type the list forbids fails `WIDGET_TYPE_MISMATCH`. Your custom widget declares
  which variable types it accepts and gets the same enforcement.
- optional **gate** attributes that opt the widget into extra resolver checks — an
  option-set requirement (the control needs a bounded option set) and a numeric
  range check (reject an inverted min/max). Turn these on only if your control
  needs them.

The 13 built-in widgets (grouped by binding shape) and the regions that house them
are catalogued in **items-inputs**; **variables** owns the binding target itself.

## Publishing one (the loop)

1. **Author the schema.** Write `<your-type>.schema.json` like any item type, and
   add a `latticeBehavior` block: choose the role, fill its attributes. For the
   exact JSON shape of that block and of the surrounding schema, mirror a built-in
   of the same role via **`lattice_get_schema`** (e.g. read `container` for a region) —
   do not transcribe fields from this skill.
2. **Drop it in the schema `fs.FS`** the server loads (the same source the
   built-ins come from). No Go, no registration, no rebuild of lattice.
3. **Confirm it landed.** `lattice_get_manifest` / `lattice_list_schemas` should now list your
   type; `lattice_get_schema {your-type}` should return it. If the server rejected it at
   index time, the `SCHEMA_BEHAVIOR_INVALID` error names the schema and the
   problem — fix the role/attributes and reload.
4. **Author and simulate an instance.** Build a document/patch using your type and
   run **`lattice_validate_patch`** — it resolves under the same grammar as any built-in,
   so the role's guarantees (child policy, wrapper count, widget type-match) are
   enforced on the simulated tree. Iterate to green before a human commits via
   `POST /api/patch`. The edit loop itself is **authoring-loop**.

Because a custom type is *just another catalog entry*, every other skill applies
to it unchanged: place its instances per **placement-grid**, edit them per
**patch-authoring**, theme them per **theming**.

## Cross-links

- **items-layout** — the built-in `region` (`container`) and `wrapper` (`block`).
- **items-inputs** — the built-in `widget` family and the `variable-box` region.
- **items-forms** — `form`, the flow-layout `region` that groups widgets.
- **variables** — the binding target a `widget` sets and how an override
  re-resolves the tree.
- **patch-authoring** / **authoring-loop** — building and simulating instances of
  any type, custom or built-in.
- **session-bootstrap** — source layering: why each type's real grammar (and its
  own `latticeBehavior` shape) lives in `lattice_get_schema`, not here.
