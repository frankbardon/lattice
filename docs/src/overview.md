# Overview

## What Lattice is

Lattice is a **specification**, not a running product. The deliverable is:

1. A **JSON document format** for dashboards (`schemas/dashboard.schema.json`).
2. A **catalog of typed schemas** for item types and connection types
   (`schemas/items/`, `schemas/connections/`).
3. A resolver, shipped as the **`lattice`** binary, that validates a document
   against the catalog and emits a resolved tree.

A dashboard is a tree of **items**. The only structurally special item is the
**container**, which arranges its children on a relative-weight grid. Every
other item type (today: `table` and `dropdown`) is a leaf. Items can declare
**variables**, reference document-scoped **connections** for their data, and
interpolate variable values into their configuration.

## The resolution pipeline

`lattice resolve` runs a **fail-fast, two-pass** pipeline. The first error
stops the run and is reported as a coded error (see [Error Codes](reference/error-codes.md));
errors are never aggregated.

1. **Pass 1 — structural.** The whole document is validated against the
   dashboard JSON Schema. A document that is not well-formed JSON, or that
   violates the top-level shape, fails here (`RESOLVE_DOCUMENT_INVALID`).
2. **Reference linking.** Every instance `$ref` is resolved to an item-type
   schema in the catalog (`SCHEMA_*` on failure).
3. **Variable model.** The tree-scoped variable environment is built: document
   and per-container declarations are layered with inner-shadows-outer
   semantics, computed `expr` variables are evaluated, and any runtime overrides
   are applied.
4. **Pass 2 — per-instance.** For each node, in one walk: layer its variable
   scope, **interpolate** variable references into its config, validate the
   *interpolated* config against its item-type schema, and enforce the
   container-only-children rule.
5. **Connections.** Each document-scoped connection's `$ref` is resolved and
   its config validated against the connection-type schema; `$secret`
   references are resolved from the environment for validation and then
   discarded. Duplicate connection ids fail fast.
6. **Bindings & contracts.** Items that declare a `connectionId` are wired to
   their connection, their query (already interpolated) is lifted onto the node,
   and the item↔connection **result-shape contract** is validated.

The output is the **resolved tree**: the manifest passed through verbatim, the
recursively resolved root, and the resolved connections. Its shape is a stable,
documented contract — see [Document Structure](format/document-structure.md#the-resolved-tree).

## The two commands

| Command | Purpose |
| --- | --- |
| `lattice resolve <document>` | Validate a document and print the resolved-tree JSON to stdout. |
| `lattice serve <document>` | Serve the document over HTTP: an HTML structural sketch plus a JSON resolved-tree endpoint, re-resolved per request with runtime inputs. |

Both are covered in [Building & Running](getting-started/building-and-running.md).
