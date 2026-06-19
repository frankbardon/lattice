# Changesets (JSON Patch)

Every edit to a document — at **any** scope — is expressed as a **JSON Patch
([RFC 6902](https://www.rfc-editor.org/rfc/rfc6902))** document. This is the
single, universal changeset mechanism for the format: there is no second edit
language, no scope-specific mutation API, and no bespoke diff shape. Whether a
change retunes one item's config, rewrites the manifest, adds a variable, swaps a
connection, flips a theme token, or restructures the root region, it is the same
artifact — an ordered array of JSON Patch operations against the document.

> **Contract only.** This page defines the *changeset contract* — the shape an
> edit takes and the rules that decide whether it is legal. **No patch
> application, enforcement, validation-execution, or persistence code ships in
> this effort.** How a patch is applied, where it is stored, and where it is
> enforced are deferred (see [What is deferred](#what-is-deferred)). Today the
> format only *commits to* JSON Patch as the universal changeset; the machinery
> that consumes one is future work, exposed in ways chosen later.

## Why one mechanism

A document is a single JSON tree: a manifest, a variable set, connections, a
default theme, and a root region of nested items. JSON Patch already addresses
*any* location in such a tree by [JSON Pointer
(RFC 6901)](https://www.rfc-editor.org/rfc/rfc6901) and already defines the six
operations needed to mutate one — `add`, `remove`, `replace`, `move`, `copy`,
`test`. Adopting it wholesale means:

- **Uniformity.** The same operation vocabulary edits an item's config field, a
  document scope, or the whole root. A consumer learns one changeset format, not
  one per scope.
- **Addressability.** A JSON Pointer names exactly the node and field a change
  touches, which is precisely what a [guardrail](#the-guardrail) needs in order
  to decide legality.
- **Ordering and atomicity.** A patch is an ordered list; `test` operations let a
  changeset assert preconditions. The format inherits this for free.

## Uniform across every scope

The changeset format does not vary by what it edits. The same JSON Patch shape
applies to all of these scopes:

| Scope         | Example pointer target                  |
| ------------- | --------------------------------------- |
| **item**      | a node's `config` field                 |
| **manifest**  | the document `title` / `description`    |
| **variables** | the document variable set               |
| **connections** | the document connections              |
| **theme**     | a [theme token](theme.md)               |
| **root**      | the resolved root region                |

These line up one-for-one with the addresses the rest of the format already
speaks: an item is addressed by its stable `id`, and the five document scopes are
the reserved `$`-keywords (`$manifest`, `$variables`, `$connections`, `$theme`,
`$root`) introduced for
[reserved document-scope targets](configurator.md#reserved-document-scope-targets).
A changeset is just the *write* counterpart to those *read/target* addresses,
spelled in JSON Pointer.

A minimal config edit and a minimal theme edit are the same kind of document:

```json
[
  { "op": "replace", "path": "/<item>/config/title", "value": "Pinned" }
]
```

```json
[
  { "op": "replace", "path": "/$theme/density", "value": "compact" }
]
```

> The exact JSON Pointer dialect that maps an item `id` or a `$`-scope keyword to
> a concrete pointer into the on-disk document is **not pinned down here** — that
> is part of the deferred application layer. What this contract fixes is that the
> changeset *is* RFC 6902, uniformly, for every scope above.

## The guardrail

A changeset is not free to touch anything. The
[configurable surface](configurable.md) is the **guardrail**: it enumerates the
paths a patch may legally touch, and a path outside the surface is illegal.

- For an **item**, the surface is the item type's `configurable` declaration —
  the honest list of runtime-tunable fields. A patch operation whose target
  resolves to a field *not* on that surface is out of bounds, exactly as a
  [config override](overrides.md#config-overrides) addressing an unsurfaced field
  fails `CONFIG_OVERRIDE_FIELD_UNKNOWN`.
- For a **document scope**, the surface is the scope's entry in the document
  schema's `documentScopes` keyword — see
  [document-scope surfaces](configurator.md#document-scope-surfaces). It "doubles
  as the guardrail": it enumerates the legal target paths within the scope
  (`$manifest` → `title` / `description`, `$theme` → the six theme tokens, and so
  on). A patch reaching a field the scope does not surface is out of bounds.

So the configurable surface plays the same role for a *persisted* changeset that
it already plays for an *ephemeral* [runtime override](overrides.md): it is the
single source of truth for **which paths are editable**. The override system is
the live, in-memory expression of that boundary; a JSON Patch changeset is the
durable expression of an edit *within the same boundary*. They are two faces of
one rule — the surface declares what is editable, and nothing edits outside it.

This is what makes the surface load-bearing: because it is derived from the
schema and validated on every resolve, the set of legal patch paths can never
drift out of sync with what an item type or scope actually accepts.

## Relationship to runtime overrides

It is worth being precise about the boundary between this contract and the
[runtime override](overrides.md) system, because they share the configurable
surface but differ in lifetime:

| | [Runtime override](overrides.md) | JSON Patch changeset |
| --- | --- | --- |
| **Lifetime** | ephemeral — one resolution | durable — a persisted edit (future) |
| **Shape** | flat `address → value` map | ordered RFC 6902 operation list |
| **Scope** | variables and config fields | every scope, uniformly |
| **Guardrail** | configurable surface | configurable surface |
| **Status today** | implemented | **contract only** |

An override answers "what should this one resolution see?" A changeset answers
"what edit should be recorded against the document?" Both are gated by the same
surface; only the changeset's *application* is deferred.

## What is deferred

To be unambiguous about the boundary of the current effort, the following are
**explicitly out of scope** and not implemented here:

- **Patch application** — taking a JSON Patch and producing a mutated document.
- **Enforcement** — rejecting a patch that touches a path outside the
  configurable-surface guardrail. The guardrail is *defined* here; the code that
  *executes* the check ships later.
- **Validation execution** — re-resolving and re-validating a document after a
  patch is applied.
- **Persistence** — where and how an applied patch (or the resulting document) is
  stored.

These will be exposed through interfaces chosen in a later effort. Nothing in
this page should be read as a claim that a patch can be applied today; it cannot.
What is fixed now is the *contract*: JSON Patch (RFC 6902) is the one universal
changeset mechanism, uniform across every scope, with the configurable surface as
its guardrail.
