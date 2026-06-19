# Variables

Variables let a document declare named, typed values once and reference them
from item configuration. They can be static defaults, computed expressions, or
runtime inputs.

## Declaring variables

A variable **declaration** has the shape `{ name, type, default?|expr?, options? }`:

```json
{ "name": "window", "type": "integer", "default": 24 }
```

| Field | Notes |
| --- | --- |
| `name` | Identifier, unique within its declaring scope. |
| `type` | One of `string`, `number`, `integer`, `boolean`, `enum`, `array`. |
| `default` | Optional literal value, validated against `type`. **Mutually exclusive with `expr`.** |
| `expr` | Optional computed expression. **Mutually exclusive with `default`.** |
| `options` | The permitted value set; **required for `enum`, forbidden otherwise**. |

A bare `{ name, type }` with neither `default` nor `expr` is a valid,
value-less declaration. Declaring both `default` and `expr`, or `options` on a
non-enum type, or an `enum` without options, fails fast
(`VAR_DECLARATION_INVALID` / `VAR_OPTIONS_INVALID`).

Declarations appear at **document scope** (the top-level `variables` array) or on
any **container instance** (its `variables` array).

## Types

Values arrive as decoded JSON. `integer` requires a number with no fractional
part; `number` accepts any JSON number; `enum` requires a string that is one of
the declared `options`. A `default` (or a runtime override) that does not match
its declared type fails fast with `VAR_TYPE` (or `VAR_OPTIONS_INVALID` for an
out-of-set enum value).

## Tree scoping & shadowing

The resolver walks the item tree and, for every node, computes the set of
variables **visible at that node** by layering declarations from the document
root down to the node itself. A declaration on an inner node **shadows** an
outer one of the same name:

```
item-local  →  ancestor containers  →  document
(nearest declaration wins)
```

Each visible variable records **where it was declared** (`declaredAt`, the
resolved-tree path of the owning node). This per-node environment is attached to
every node in the resolved tree as `varEnv`, and because each entry carries its
`declaredAt`, the full variable→node visibility mapping is recoverable from the
tree alone. (Using that mapping for partial re-resolution is deferred — see
[Out of Scope](../reference/out-of-scope.md).)

Sibling subtrees are independent: extending the environment always returns a new
environment, so a declaration in one branch never leaks into another.

## Interpolation: `$var` and `${}`

Variable references inside an item's `config` are substituted **before** the
config is validated, so the item-type schema sees concrete, typed values rather
than raw references. There are two forms:

### Typed binding — `{ "$var": "name" }`

An object that is *exactly* `{ "$var": "name" }` (one key, string value) is
replaced **wholesale** by the named variable's value, **preserving its JSON
type**. An integer variable stays an integer, an array stays an array:

```json
{ "hours": { "$var": "window" } }   →   { "hours": 24 }
```

Any object that is not exactly this shape is treated as ordinary data, not a
binding.

### String template — `${name}`

Every `${name}` occurrence inside a string value is replaced by the named
variable's value rendered as text. The surrounding string is preserved, so the
result is always a string. Integral numbers render without a trailing `.0`
(e.g. `24`, not `24.000000`):

```json
{ "title": "Metrics for ${region}" }   →   { "title": "Metrics for us-east" }
```

Maps and slices are walked recursively. A reference to a name not visible at the
node fails fast with `VAR_UNDEFINED`, naming the offending instance path and the
reference form.

Interpolation is the same reusable mechanism used for connection **query**
parameters — see [Connections](connections.md).

## Computed variables — `expr`

A declaration may carry an `expr` string instead of a `default`. The expression
is evaluated against the in-scope variables, and its result is coerced and
validated to the declared type, then flows into the **same value slot** a
default would. Interpolation and dependency tracking therefore treat computed
and literal variables identically.

```json
[
  { "name": "window",     "type": "integer", "default": 24 },
  { "name": "windowDays", "type": "integer", "expr": "window / 24" },
  { "name": "heading",    "type": "string",
    "expr": "\"Metrics (last \" + string(windowDays) + \"d)\"" }
]
```

Within a scope, computed declarations are layered **after** the literals and
resolved in **dependency order**, so an expression may reference inherited
variables, sibling literals, and other computed variables. A dependency cycle
fails fast with `VAR_CYCLE`; a compile/eval failure is `VAR_EXPR`; a result that
does not match the declared type is `VAR_TYPE`.

> **Expression syntax is intentionally generic in this spec.** Expressions are
> evaluated with the [`expr-lang/expr`](https://expr-lang.org/) engine, which
> supports arithmetic, string concatenation, comparisons, and built-in
> conversion functions such as `string(...)`. **Pinning the exact supported
> subset is deferred future work** (see
> [Out of Scope](../reference/out-of-scope.md)); treat the examples as
> illustrative rather than as a normative grammar.

## Runtime inputs

Variables can be set at resolution time by **runtime overrides**. An override
replaces the *effective* value of a **settable** variable — one backed by a
literal/default, not a computed `expr`. Computed variables are never
overridable and keep their evaluated value. Crucially, because a computed
variable reads the same value slot an override writes, a computed chain that
depends on an overridden literal **recomputes against the runtime value** — this
is what lets a runtime input drive a `${var}` consumer through an `expr`.

Overrides come from two sources, both wired by `lattice serve`:

- **Widgets.** A [widget](widgets.md) is a leaf item that **sets a single
  variable** named by its `variable` config key. There are 13 widgets across
  five families — string (`text-input`, `textarea`), number (`number-field`,
  `slider`, `stepper`), boolean (`toggle`, `checkbox`), enum (`select`,
  `radio-group`, `segmented`), and array (`multiselect`, `checkbox-group`,
  `tag-input`). A widget may only bind a variable whose declared type its family
  permits, otherwise the resolver reports `WIDGET_TYPE_MISMATCH`. Changing the
  control sets that variable's runtime override and re-resolves the document, so
  dependent `${var}` / `$var` consumers update live. The widget only declares the
  binding and its presentation; the variable itself is declared in the
  document/container `variables` and supplies the effective default (override >
  default). `select` is the canonical single-choice control, replacing the
  retired `dropdown` item.
- **URL query parameters.** `serve` reads `?name=value` parameters as overrides
  for the initial render. Because query params arrive as text, a value targeting
  a non-string variable is parsed to the declared type before validation; a
  value that cannot be parsed or fails the type/enum check fails fast with the
  same `VAR_TYPE` / `VAR_OPTIONS_INVALID` codes a bad default would.

An override for an undeclared name is a no-op. A `nil`/empty override set leaves
every variable at its declared default, so the resolved-tree contract is
identical to a plain `resolve`.
