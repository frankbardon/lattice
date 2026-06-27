---
name: variables
description: How lattice variables work — declaring typed variables at document or container scope, the two reference forms (${name} string template and {"$var": "name"} typed binding), tree-scoped shadowing, the variable-box region + widgets that set them, and how values flow into resolved config. Pairs with lattice_get_outline (in-scope names), lattice_get_node (consumer config), and lattice_get_schema (declaration grammar).
type: guide
kind: workflow
applies_to: [lattice_get_outline, lattice_get_node, lattice_validate_patch, lattice_get_schema]
---

# Variables

Variables let a document declare named, typed values once and reference them from
item config. References are substituted **before** config is validated, so the
item schema sees concrete, typed values. This skill covers declaration scope, the
two reference forms, shadowing, and the variable-box. It does NOT restate the
declaration grammar — call `lattice_get_schema` for that.

## Declaring

A declaration is `{ name, type, default?|expr?, options? }`:

```json
{ "name": "window", "type": "integer", "default": 24 }
```

- `type` ∈ `string | number | integer | boolean | enum | array | object`.
- `object` backs a **structured** value — a keyed map the item's own config schema
  shapes (e.g. a container's `grid`, a panel `spec`). The variable type only checks
  "it is an object"; the authoritative field-by-field structure is the item-type
  config schema's re-validation, so a structured configurable surface declares its
  sub-fields there, not here.
- `default` and `expr` are **mutually exclusive**; a bare `{name, type}` is a valid
  value-less declaration.
- `options` is **required for `enum`, forbidden otherwise**.
- `expr` is a computed expression (evaluated against in-scope vars, coerced to
  `type`); it flows into the **same value slot** a `default` would, so consumers
  treat computed and literal variables identically.

Declarations appear at **document scope** (top-level `variables` array) or on **any
container instance** (its `variables` array). Bad shapes fail fast:
`VAR_DECLARATION_INVALID`, `VAR_OPTIONS_INVALID`, `VAR_TYPE`, `VAR_CYCLE`,
`VAR_EXPR`.

## The two reference forms

References live inside an item's `config`:

**String template — `${name}`.** Every `${name}` in a string is replaced by the
value rendered as text; the surrounding string is preserved, so the result is
always a string. Integral numbers render with no trailing `.0`.

```json
{ "title": "Metrics for ${region}" }   →   { "title": "Metrics for us-east" }
```

**Typed binding — `{ "$var": "name" }`.** An object that is *exactly*
`{"$var": "name"}` (one key, string value) is replaced **wholesale** by the
variable's value, **preserving its JSON type** — an integer stays an integer, an
array stays an array. Any object not exactly this shape is ordinary data.

```json
{ "hours": { "$var": "window" } }   →   { "hours": 24 }
```

Maps and slices are walked recursively. A reference to a name not visible at the
node fails fast with `VAR_UNDEFINED` (it names the offending path + reference form).
Pick the binding form when you need to preserve a non-string type; pick the template
form to splice a value into prose.

## Scope: document root vs nested, and shadowing

The resolver computes, per node, the set of variables **visible at that node** by
layering declarations from the document root **down** to the node:

```
item-local  →  ancestor containers  →  document
(nearest declaration wins — an inner decl SHADOWS an outer one of the same name)
```

- A document-scope variable is visible to **every** node unless shadowed.
- A declaration on a container is visible only to **that container's subtree**.
- Sibling subtrees are independent — a declaration in one branch never leaks into
  another.

Each resolved node carries its visible environment as `varEnv`, and each entry
records `declaredAt` (the path of the declaring node). To see **which names are in
scope at the document root**, call `lattice_get_outline` — its `document.variables` is
exactly that sorted name list. To inspect a consumer node's interpolated config,
call `lattice_get_node`.

## Runtime inputs: widgets + the variable-box

A **settable** variable (literal/default-backed, not `expr`) can be driven at
resolve time by a **widget** — a leaf item that sets the single variable named by
its `variable` config key. Changing the control sets that variable's override and
re-resolves, so dependent `${var}` / `$var` consumers update live. (Computed `expr`
variables are never overridden, but a computed chain reading an overridden literal
**recomputes** against the runtime value.)

Widgets live in a **`variable-box`** — a layout-only region that holds its widget
children **directly**, NOT block-wrapped (unlike content leaves; see **blocks**).
Its only surface is a layout `arrangement` (`stacked | inline`). A variable-box may
hold **only** variable widgets, directly — a wrapped child or nested region fails
`GRAMMAR_VARIABLE_BOX_CHILD_INVALID`. (Widgets may also live inside a `form`
content leaf; the box is the standalone home.) A widget bound to a variable whose
declared type its family forbids fails `WIDGET_TYPE_MISMATCH`.

## How values flow into resolved output

1. The resolver builds each node's `varEnv` (scope walk + shadowing + `expr` eval).
2. Runtime overrides (widget settings, `?name=value` query params) replace settable
   defaults.
3. Config interpolation runs: `${name}` and `{"$var": "name"}` are substituted
   against the node's `varEnv`.
4. The substituted config is schema-validated, then emitted in the resolved tree.

So a variable change re-runs the whole chain; this is what lets a runtime input
drive a `${var}` consumer (directly, or through an `expr`).

## Cross-links

- **blocks** — the wrapper/content model variables resolve *into* (content leaves);
  variable-box vs block-wrapping.
- **placement-grid** — placing a variable-box and its widgets in a grid.
- **patch-authoring** — editing a variable scope via `/$variables/...` id-rooted ops.
- **session-bootstrap** — source layering (declaration grammar lives in `lattice_get_schema`).
