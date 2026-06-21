---
name: theming
description: How a lattice document expresses presentation as renderer-agnostic semantic theme tokens — the closed token vocabulary, the two independent layers (document default theme + per-block override), the no-merge "side-by-side" rule, and how get_outline's document-scope `theme` boolean flags presence (not values). Grammar (the exact token enums) stays in get_schema {type: "dashboard"}.
type: guide
kind: workflow
applies_to: [get_outline, get_node, get_schema, validate_patch]
---

# Theming

A **theme** records presentation **intent** as renderer-agnostic **semantic
tokens** — never medium-specific values. A theme carries no pixels, hex colours,
fonts, or CSS; a renderer maps each semantic choice onto its own medium. This
skill covers *where* themes live, *how the two layers relate*, and *how to read
the outline's theme signal*. It does NOT transcribe the token enum values — that
grammar drifts with the schema and is `get_schema`'s job (see **Reading the
grammar** below).

## The token vocabulary (shape, not values)

Theme tokens are a **small, closed** set of optional scalar fields, each an
`enum` over a fixed meaning vocabulary (e.g. *emphasis*, *spacing*, *tone*,
*radius*, *border*, *density* — meanings, never colour literals or units). Every
token is optional; an omitted token means "inherit / renderer default". There is
no special chrome subsystem — tokens are ordinary configurable-surface fields.

For the **authoritative token list and the legal value enums**, read the
grammar — do not rely on memory or this skill (see below).

## The two layers (independent, not merged)

A theme can be expressed in exactly two places, and they are **independent
layers**:

1. **Document default theme** — the optional top-level `theme` member of the
   document. The document's base presentation choices. Addressed for edits by the
   `$theme` scope (e.g. a patch path `/$theme/<token>`).

   ```json
   {
     "manifest": { },
     "theme": { "emphasis": "high", "spacing": "cosy" },
     "root": { }
   }
   ```

2. **Per-block override** — a [block](blocks.md) wrapper's `config.theme`, any
   *subset* of tokens. A set token overrides the document default for that block;
   an omitted token inherits. **Only block wrappers carry a theme** — positional
   regions (`container`, `variable-box`) may NOT
   (`GRAMMAR_REGION_THEME_FORBIDDEN`).

   ```json
   {
     "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
     "id": "highlighted",
     "config": {
       "id": "highlighted",
       "theme": { "emphasis": "high", "tone": "accent" },
       "content": { }
     }
   }
   ```

**No merge.** The resolver validates each theme against the vocabulary and
attaches it **verbatim** — it computes no cascade and no "effective theme". The
document default lands as the resolved tree's `defaultTheme` (the default layer
only); each per-block override lands on its own block node's resolved
`config.theme`. The two are emitted **side by side**; composing them for a
renderer is a downstream consumer's job, not the resolver's.

## The outline theme flag — presence only

`get_outline`'s document-scope summary carries a **`theme` boolean**, not the
theme. It is `true` exactly when the document declares a **default** theme
(top-level `theme` member present), `false` otherwise. The **token values are
deliberately omitted** — presence only.

- `theme: true` → a default theme exists; read its actual tokens from the
  document scope (e.g. via `get_document` or by inspecting `$theme`).
- `theme: false` → no document default; any block-level overrides still resolve
  against the renderer default.

The flag says **nothing** about per-block overrides — those live on block nodes,
not in the document-scope summary. To see a block's override, drill in with
`get_node {id, nodeId}` (its `subtree` carries `config.theme`; `theme` is on the
block's surface as a settable scope).

## Reading the grammar (source layering)

Per **session-bootstrap**, a skill never re-emits schema grammar. The theme
vocabulary is defined in the theme schema and **referenced by the dashboard
envelope's `theme` member**. The theme schema is not a standalone `get_schema`
type token — fetch the envelope instead:

- **`get_schema {type: "dashboard"}`** — the authoritative grammar for the
  top-level `theme` member (and the `$theme` scope's settable token surface).
- **`get_node {id, nodeId}`** on a block wrapper — its `surface` lists `theme`
  as a settable field; the surface (not this skill) is authoritative for which
  tokens are legal to patch.

Author or edit a theme, then prove it with **`validate_patch`** before a human
commits — an out-of-vocabulary token or value is rejected by structural
validation; a theme on a non-block region is rejected
(`GRAMMAR_REGION_THEME_FORBIDDEN`).

## Cross-links

- **blocks** — the block wrapper that carries a per-block `theme` override.
- **patch-authoring** — the `$theme` scope and id-rooted pointer dialect for
  editing tokens.
- **session-bootstrap** — why token enums stay in `get_schema`, not here.
