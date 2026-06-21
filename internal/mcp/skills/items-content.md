---
name: items-content
description: The content-leaf item family — `markdown` (prose), `heading` (a section heading), and `image` (an image reference). The page-like leaves that read like an article. They are non-positional, always block-wrapped, carry opaque (never rendered/fetched) text, and support `${var}` / `$var` interpolation in their text fields. Pairs with blocks (the mandatory wrapper), variables (interpolation), and get_schema (the per-type field grammar).
type: reference
kind: items
applies_to: [get_schema, get_node, get_outline, validate_patch]
covers: [markdown, heading, image]
---

# Items: the content-leaf family

This is a per-item **reference** for the three **content leaves** —
**`markdown`**, **`heading`**, and **`image`**. They carry page-like content
(prose, a section heading, an image) rather than data or a control, and together
let a dashboard read like an article. This skill covers what each is *for* and the
two rules that bind all three; it does **not** list their fields. For the field
grammar of any type call **`get_schema`** (`markdown`, `heading`, `image`) — the
schema is authoritative and drifts per server (see **session-bootstrap** → source
layering).

## Two rules that bind every content leaf

**1. Always block-wrapped.** A content leaf is **non-positional** — it is neither
a region nor a widget, so the tree grammar forbids it from sitting directly under
a container or under `root`. A bare (unwrapped) content leaf fails
`GRAMMAR_REGION_CHILD_INVALID`, naming the offending path. Wrap it in a `block`
(its inner `config.content`) to place it; the block then supplies the leaf's
`id`, `title`, `visibility`, and any `theme` override. The wrapper↔content split
and how to address each side in a patch live in **blocks**; the layout family it
sits inside is **items-layout**.

**2. `${var}` everywhere a string lives.** Every text field on a content leaf
supports **variable interpolation**, run over the instance's `config` **before**
config validation. Both reference forms work: the `${name}` string template (the
common case for prose) and the `{ "$var": "name" }` typed binding. A reference to
a name not visible at the node fails fast with `VAR_UNDEFINED`. Scope, shadowing,
and the two forms are covered in **variables**.

> Opaque, never rendered. `markdown.source` and `image.src` are stored and
> validated as strings but **never parsed, rendered, or fetched** by the resolver
> — Markdown flavor, URL grammar, and reachability are a downstream renderer's
> job. The resolver validates shape and substitutes variables; nothing more.

> Known limitation: a literal `${name}` cannot be escaped — any `${…}` in a text
> field is always treated as a variable reference. To emit the literal characters,
> declare a variable holding that text.

## Pick this when…

| Use | Type | Carries |
|---|---|---|
| A block of prose / paragraph(s) | `markdown` | A single opaque Markdown body |
| A section title above content | `heading` | A label + a depth level (1–6) |
| A figure / diagram / picture | `image` | An opaque image reference (+ alt, caption) |

- **`markdown`** — reach for it for any free-form prose: an intro paragraph,
  body copy, a note. Its one body field is interpolation-friendly, so
  `"Welcome to **${productName}**"` resolves with the variable spliced in.
- **`heading`** — reach for it for a section title. It carries a depth `level`
  (1 = top-level section … 6 = deepest); there is **no default**, so every
  heading must state its level — pick the depth to match the document outline.
  Use a `heading` to title a region of `markdown`, not a block `title` (the block
  title is wrapper chrome rendered with the leaf; a `heading` is page content).
- **`image`** — reach for it to reference a figure. The `src` is opaque, so an
  absolute URL, a relative path, or a data URI are all equally valid. Add `alt`
  for accessibility and `caption` for a rendered label when the figure needs one.

For the exact required/optional fields and their constraints (e.g. `heading.level`
range, which fields are required), call `get_schema` for the type — do not guess
them from this table.

## Inline example reference

`examples/page-dashboard.json` is the grounding fixture: a page-like document
whose single body region is a stack of **block-wrapped** content leaves — a
`heading`, two `markdown` paragraphs, and a captioned `image` — that reads like an
article. Two document-scope variables (`productName`, `releaseVersion`) are
interpolated into the heading and prose via `${var}`, demonstrating both binding
rules at once: each leaf is block-wrapped, and its text carries variable
references substituted before validation.

## Cross-links

- **blocks** — the mandatory wrapper a content leaf must sit inside; how `get_node`
  surfaces the leaf's editable fields against the wrapping block node.
- **items-layout** — the `container` + `block` family that hosts these leaves.
- **variables** — `${name}` vs `{ "$var": "name" }`, scope and shadowing, and how
  values flow into a leaf's resolved config.
- **session-bootstrap** — source layering: why the per-type field grammar stays in
  `get_schema`.
