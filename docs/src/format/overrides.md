# Runtime Overrides

A resolved dashboard is not frozen. At **resolution time** a caller may supply an
**override set** — a flat map of address → value — that adjusts the document
before the tree is assembled. Both `lattice resolve` (via `ResolveWithValues`)
and `lattice serve` (per request) accept the same override set, so the runtime
model is identical whether you re-resolve from the CLI or from the served page.

Overrides are **ephemeral**: they apply only to that single resolution. The
document on disk is never mutated, and an empty override set yields exactly the
same resolved tree as a plain `resolve`.

## Two override kinds, one addressable map

The override set is a single `map[string]any` whose **key is an address**. The
address shape selects what the value targets:

| Address | Targets | Example |
| --- | --- | --- |
| `name` (bare) | a **variable** named `name` | `region` → `"eu"` |
| `<node-id>.<field>` | a node's **config field** | `summary.title` → `"Pinned"` |

A key with no `.` is a **variable override**; a key of the form
`<node-id>.<field>` is a **config override**. The two kinds share one map and are
routed by address — neither the CLI nor the server has to separate them.

### Variable overrides

A variable override replaces the *effective* value of a **settable** variable —
see [Runtime inputs](variables.md#runtime-inputs) for the full rules. In short:
the value flows through the variable model (override > default), computed
variables recompute against it, and dependent `${var}` / `$var` consumers update
live. An override for an undeclared name is a no-op; a value that fails the
variable's type / enum check fails fast with the usual `VAR_TYPE` /
`VAR_OPTIONS_INVALID` codes.

### Config overrides

A config override sets one **config field** of a single resolved node, addressed
by `<node-id>.<field>`. It is applied **after interpolation**, so it overwrites
whatever an interpolated `${var}` produced for that field, and is validated
against the node's [configurable surface](configurable.md) — only a field the
item type declares `configurable` may be overridden. Failures are fail-fast:

- An address whose `<field>` is not on the node's configurable surface fails with
  `CONFIG_OVERRIDE_FIELD_UNKNOWN`.
- A value that violates the field's declared type / constraints fails with
  `CONFIG_OVERRIDE_VALUE_INVALID`.

Like every override, a config override is **ephemeral** — it mutates only the
resolved instance for that resolution, never the document.

## Precedence

Within a single resolution the layers compose top-down, each later layer winning
for the value it touches:

1. **Declared defaults** — the variable `default` and the node's authored
   `config`.
2. **Variable overrides** — replace settable variables; computed variables
   recompute against the new values; `${var}` / `$var` consumers see the result.
3. **Config overrides** — applied last, after interpolation, so a
   `<node-id>.<field>` override **wins over** whatever the interpolated config
   produced for that field.

So for a field whose value came from a `${var}` template, a variable override
changes it *through* interpolation, while a config override on that same field
pins it *regardless* of interpolation — the config override is the more specific,
later-applied layer.

## Supplying overrides through `serve`

`lattice serve` collects the unified override set from two transports and routes
both kinds — variable and config — into the same map:

- **URL query parameters.** Each `?address=value` becomes one override. A bare
  name (`?region=eu`) is a variable override; a dotted name
  (`?summary.title=Pinned`) is a config override. Query values arrive as text and
  are coerced to the target's declared type before validation.
- **The `/api/resolve` endpoint.** The served page POSTs a JSON object of the
  current overrides on every re-resolve. Interactive [widget](widgets.md)
  controls post their bound variable as a variable override on change; a client
  may also include `<node-id>.<field>` keys to drive config overrides.

For example, serving `examples/widgets-dashboard.json` and requesting

```
GET /?region=eu&summary.title=Pinned
```

resolves the document with `region` overridden to `eu` (a variable override that
flows through the `${region}` template into the summary table) **and** the
`summary` table's `title` config field pinned to `Pinned` (a config override that
wins over the interpolated `"${label} — ${region}"` title). Both adjustments are
ephemeral to that request.
