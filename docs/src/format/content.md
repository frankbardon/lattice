# Content Item Types

A **content item** is a leaf item type that carries page-like content — prose, a
section heading, or an image — rather than data or a control. Three content types
ship in the catalog:

| Type | Carries | Required fields | Optional fields |
| --- | --- | --- | --- |
| [`markdown`](#markdown-itemsmarkdown100) | A block of prose | `source` | — |
| [`heading`](#heading-itemsheading100) | A section heading | `text`, `level` | — |
| [`image`](#image-itemsimage100) | An image reference | `src` | `alt`, `caption` |

Together they let a dashboard read like an article. A content leaf never has
children, and — like every leaf — it is an ordinary item instance
(`{ "$ref", "id", "config" }`). See
[Typed Schemas & Instances](typed-schemas.md) for the instance shape.

## Content leaves must be block-wrapped

A content type is **non-positional**: it is neither a layout region nor a widget,
so the [tree grammar](blocks-and-grammar.md#the-grammar-rules) treats it as a
content leaf that must sit inside a [`block`](catalog.md#block-itemsblock100)
wrapper. A bare content leaf placed directly under a container (or under the
document `root`) is rejected with
[`GRAMMAR_REGION_CHILD_INVALID`](../reference/error-codes.md), naming the
offending instance path.

Wrapping the leaf in a `block` satisfies the grammar and lets the block apply its
per-block concerns — a stable `id`, an optional [theme](theme.md) override, a
`title`, and a `visibility` flag — to the single leaf it carries. The wrapper and
its inner content resolve as two separate nodes; see
[Blocks & the Tree Grammar](blocks-and-grammar.md#the-block-wrapper).

A minimal legal placement (container → block → content leaf):

```json
{
  "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
  "id": "intro-block",
  "config": {
    "id": "intro-block",
    "content": {
      "$ref": "https://lattice.dev/schemas/items/markdown/1.0.0",
      "id": "intro-prose",
      "config": { "source": "Welcome to the dashboard." }
    }
  }
}
```

## Opaque strings: `markdown.source` and `image.src`

The resolver **stores and validates** content text but never **parses, renders,
or fetches** it. Two fields are deliberately opaque:

- **`markdown.source`** carries Markdown prose verbatim. The schema imposes no
  Markdown grammar, flavor, or sanitization — it is a pass-through string a
  downstream renderer interprets.
- **`image.src`** carries an image reference verbatim. The schema imposes no
  `format: uri`, URL grammar, reachability check, or fetch behavior — an absolute
  URL, a relative path, or a data URI are all equally valid, and a downstream
  renderer (never the resolver) loads it.

This is the same no-render boundary the rest of the spec keeps: the resolver
validates shape and attaches concerns; a downstream builder owns rendering and
loading. See [Out of Scope](../reference/out-of-scope.md).

## Variable interpolation in text fields

Every text field on a content leaf supports
[variable interpolation](variables.md#interpolation-var-and-). Interpolation runs
over an instance's `config` **before** validation, so a `${var}` template (or a
`{ "$var": "name" }` typed binding) inside `markdown.source`, `heading.text`,
`image.src`, `image.alt`, or `image.caption` is substituted from the in-scope
variable environment for free:

```json
{ "text": "Introducing ${productName} ${releaseVersion}", "level": 1 }
```

A reference to a name not visible at the node fails fast with
[`VAR_UNDEFINED`](../reference/error-codes.md). Variable scoping and the two
reference forms are covered in full on the [Variables](variables.md) page.

> **Known limitation — a literal `${name}` cannot be escaped.** There is
> currently no escape syntax for the `${…}` template marker. Any `${name}` in a
> text field is always treated as a variable reference: if you want the four
> literal characters `${x}` to survive into the output, you cannot — the
> interpolator will try to resolve `x` as a variable and fail with `VAR_UNDEFINED`
> when it is undefined. A workaround is to declare a variable whose value is the
> literal text you need.

## `markdown` (`.../items/markdown/1.0.0`)

A prose leaf carrying a single block of Markdown.

| Field | Type | Notes |
| --- | --- | --- |
| `source` | string | **Required**, non-empty. The opaque Markdown body, stored verbatim (see [Opaque strings](#opaque-strings-markdownsource-and-imagesrc)). An absent or empty `source` fails with `RESOLVE_CONFIG_INVALID`. |

```json
{
  "$ref": "https://lattice.dev/schemas/items/markdown/1.0.0",
  "id": "intro-prose",
  "config": {
    "source": "Welcome to **${productName}**. This release brings page-like content."
  }
}
```

The single `source` field is runtime-tunable through the
[configurable surface](configurable.md), rendered as a multi-line `textarea`.

## `heading` (`.../items/heading/1.0.0`)

A section-heading leaf.

| Field | Type | Notes |
| --- | --- | --- |
| `text` | string | **Required**, non-empty. The heading label. An absent or empty `text` fails with `RESOLVE_CONFIG_INVALID`. |
| `level` | integer | **Required**, inclusive range **1–6** (1 = top-level section, 6 = deepest). There is **no default** — every heading must state its depth. A value below 1, above 6, or non-integer (e.g. `2.5`) fails with `RESOLVE_CONFIG_INVALID`. |

```json
{
  "$ref": "https://lattice.dev/schemas/items/heading/1.0.0",
  "id": "page-heading",
  "config": { "text": "Introducing ${productName}", "level": 1 }
}
```

Both fields are runtime-tunable through the
[configurable surface](configurable.md) — `text` as a single-line `text-input`,
`level` as a `number-field`.

## `image` (`.../items/image/1.0.0`)

An image-reference leaf.

| Field | Type | Notes |
| --- | --- | --- |
| `src` | string | **Required**, non-empty. The opaque image reference, stored verbatim (see [Opaque strings](#opaque-strings-markdownsource-and-imagesrc)). An absent or empty `src` fails with `RESOLVE_CONFIG_INVALID`. |
| `alt` | string | **Optional**. Alternative text for accessibility and fallback. The leaf resolves with `src` alone. |
| `caption` | string | **Optional**. Caption text rendered alongside the image. The leaf resolves with `src` alone. |

```json
{
  "$ref": "https://lattice.dev/schemas/items/image/1.0.0",
  "id": "architecture-figure",
  "config": {
    "src": "https://assets.example.com/page-content-blocks.png",
    "alt": "Diagram of a page composed from content leaves",
    "caption": "Figure 1. A page assembled from block-wrapped content leaves."
  }
}
```

All three fields are runtime-tunable through the
[configurable surface](configurable.md), each rendered as a single-line
`text-input`.

## Error codes

| Code | When |
| --- | --- |
| `GRAMMAR_REGION_CHILD_INVALID` | A bare (unwrapped) content leaf sits directly under a container or `root` — content must be block-wrapped. |
| `RESOLVE_CONFIG_INVALID` | A required field is missing or empty, or `heading.level` is outside 1–6 / not an integer. |
| `VAR_UNDEFINED` | A `${var}` / `$var` reference in a text field names a variable not visible at the node. |

## Worked example

[`examples/page-dashboard.json`](../reference/examples.md) is a page-like
document: a single body region of block-wrapped content leaves — a `heading`, two
`markdown` paragraphs, and a captioned `image` — that reads like an article. Two
document-scope variables (`productName`, `releaseVersion`) are interpolated into
the heading and prose via `${var}` to show that content text is carried verbatim
with variables substituted.
