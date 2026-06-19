# Lattice Dashboard Spec

Lattice is a **declarative JSON format for describing dashboards** and a
companion tool, `dashspec`, that loads, validates, and **resolves** those
documents into a flat, renderer-agnostic tree.

A dashboard document is a single JSON file: a `manifest`, a recursive `root`
item tree, optional document-scoped `variables`, and optional document-scoped
`connections` (data sources). Item types and connection types are described by
versioned JSON Schemas in a local catalog; each node in the document is an
**instance** of one of those types, referenced by `$ref`.

`dashspec` does two things with such a document:

- **`resolve`** — runs a two-pass validation and prints the **resolved tree**:
  a fully validated, interpolated, secret-redacted JSON structure that a
  renderer (or any downstream consumer) can walk without re-reading the source
  document or the schemas.
- **`serve`** — starts a minimal HTTP server that re-resolves the document on
  every request and exposes both an HTML structural sketch and a JSON
  resolved-tree endpoint, with AlpineJS-driven runtime inputs.

This book is the human-facing reference for the format and the schema-catalog
conventions, written so a fresh author — or a future renderer implementer —
can use the spec without reading the Go source.

## Where to start

- New to Lattice? Read the [Overview](overview.md).
- Want to run the tool? See [Building & Running](getting-started/building-and-running.md).
- Authoring a document? Start with [Document Structure](format/document-structure.md).
- Looking for working files? See [Examples](reference/examples.md).

## Scope of this effort

This effort delivers the **format, the schema catalog, and the resolver**. It
deliberately does **not** dial connections, render real charts, or apply styling.
The precise boundary is enumerated in [Out of Scope](reference/out-of-scope.md);
read it before assuming a behavior exists.
