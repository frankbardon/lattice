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

- the **document default theme**, declared as the optional top-level `theme`
  member of the document, and
- a **[block](blocks-and-grammar.md#the-block-wrapper) wrapper's per-block
  `theme` override**, declared in the wrapper's `config.theme`.

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

## The two layers

A theme can be expressed in exactly two places, and they are independent layers:

- **Document default theme.** The optional top-level `theme` member of the
  document carries the document's base presentation choices. Its tokens are
  constrained to the vocabulary by structural validation, so a consumer may treat
  every present token as valid.

  ```json
  {
    "manifest": { ... },
    "theme": { "emphasis": "high", "spacing": "cosy" },
    "root": { ... }
  }
  ```

- **Per-block override.** A [block wrapper](blocks-and-grammar.md#the-block-wrapper)
  may carry a `config.theme` override — any *subset* of tokens. A set token
  overrides the corresponding document-default choice for that block; an omitted
  token inherits. Positional regions (`container`, `variable-box`) carry **no**
  theme — only block wrappers do, enforced by the grammar pass
  (`GRAMMAR_REGION_THEME_FORBIDDEN`).

  ```json
  {
    "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
    "id": "highlighted",
    "config": {
      "id": "highlighted",
      "theme": { "emphasis": "high", "tone": "accent" },
      "content": { ... }
    }
  }
  ```

## No merge: side-by-side layers

The resolver stays **dumb** about themes. It validates each theme against the
vocabulary and attaches it **verbatim**, but it performs **no cascade, merge, or
"effective theme" computation**:

- the document default lands on the resolved tree as `defaultTheme` (the **default
  layer only**), and
- each per-block override lands on its own block node's resolved `config.theme`.

The two are emitted **side by side**. Composing the cascade — deciding how a
block's partial override layers over the document default for a given renderer —
is a **downstream consumer's job**, not the resolver's. There is no computed
effective theme in the resolved output. This is what keeps the format
renderer-agnostic: the resolver records *intent* as semantic tokens, and a
builder owns mixing and rendering.

## Extending the vocabulary

New tokens are added by extending `$defs/baseTokens` (for further cross-cutting
choices) or by composing an additional per-type token group alongside it. Because
each token is an independent optional `enum` field, adding one never invalidates a
theme that omits it. Bumping the schema's `$id` semver follows the usual
[per-schema versioning](catalog.md) convention.
