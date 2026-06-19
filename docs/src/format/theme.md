# Theme & Semantic Tokens

A **theme** expresses presentation **choices** as renderer-agnostic **semantic
tokens**. Each token is a closed, enum-constrained vocabulary of *meaning* —
never a medium-specific value. A theme carries **no pixels, hex colours, fonts,
CSS, or any HTML/medium detail**; a renderer maps each semantic choice onto
whatever concrete styling its medium uses.

The theme vocabulary is defined once, in
`schemas/theme/theme.schema.json`
(`https://lattice.dev/schemas/theme/1.0.0`), and referenced wherever a theme may
be expressed:

- the **document default theme** (E2-S2), and
- a **[block](catalog.md) wrapper's per-block `theme` override** (E2-S3).

Tokens are ordinary scalar fields drawn from the
[variable type set](variables.md) (each an `enum` over a fixed value list), so a
theme is fully describable through the
[configurable-surface mechanism](configurable.md) — there is **no special chrome
subsystem and no group tags**.

## The token vocabulary

The vocabulary is intentionally **small** and structured so it can grow without
breaking existing themes: a `base` group of cross-cutting tokens
(`$defs/baseTokens`) that any future per-type token group composes alongside,
rather than redefining. Every token is optional; an omitted token means "inherit
/ renderer default".

| Token | Values | Meaning |
| --- | --- | --- |
| `emphasis` | `none` · `low` · `high` | How prominent the element is relative to its surroundings. |
| `spacing` | `compact` · `cosy` · `roomy` | Relative internal/outer breathing room. |
| `density` | `comfortable` · `compact` | How tightly repeated/listed content is packed. |
| `tone` | `neutral` · `accent` · `positive` · `caution` · `critical` | Semantic colour *intent*, by meaning — no colour value. |
| `radius` | `none` · `subtle` · `rounded` | How softened the corners of a surface are. |
| `border` | `none` · `hairline` · `standard` | How present a separating edge is. |

Every value is **enum-constrained** and medium-agnostic — there are no units
(no `px`), no colour literals (no hex), and nothing HTML/CSS-specific. A renderer
is free to map, say, `spacing: roomy` to whatever concrete distance suits its
medium.

## Extending the vocabulary

New tokens are added by extending `$defs/baseTokens` (for further cross-cutting
choices) or by composing an additional per-type token group alongside it. Because
each token is an independent optional `enum` field, adding one never invalidates a
theme that omits it. Bumping the schema's `$id` semver follows the usual
[per-schema versioning](catalog.md) convention.
