# Typed Schemas & Instances

Lattice separates **types** from **instances**.

- A **type** is a JSON Schema (draft 2020-12) describing the configuration of
  one kind of item or connection. Types live in the catalog under
  `schemas/items/` and `schemas/connections/` and each has a versioned `$id`.
- An **instance** is a node in a dashboard document that *uses* a type by
  pointing at it with `$ref` and supplying a `config` that conforms to that
  type's schema.

This is the central pattern of the format: the document carries the *structure*
and the *configuration values*; the schema catalog carries the *rules* for what
a valid configuration looks like.

```json
{
  "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
  "id": "fruit-table",
  "config": { "title": "Fruit Inventory", "connectionId": "inline-fruits" }
}
```

Here the instance's `$ref` names the `table` type at version `1.0.0`; the
resolver validates `config` against `schemas/items/table.schema.json`.

## `$ref` resolution

The resolver links every instance `$ref` to a type before validating its config.
A `$ref` may resolve in several ways:

- A **catalog `$id`** — an absolute `https://lattice.dev/schemas/...` URI that
  matches a schema's `$id` in the loaded catalog (the normal case).
- A **relative file** — a path looked up under configured relative roots.
- An **inline `$defs` fragment** — a `#/$defs/...` pointer carrying no versioned
  `$id`.

A `$ref` that resolves to nothing fails fast with `SCHEMA_REF_UNRESOLVED`, and a
reference to a known name at a missing or mismatched version fails with
`SCHEMA_VERSION_MISMATCH`.

The resolved tree records the resolved identity in each node's `type` block: the
raw `ref`, the canonical `id`, and the parsed `name` and `version` (the latter
two empty for inline fragments that carry no versioned `$id`).

## Versioned `$id` convention

Catalog `$id`s use the stable base URL `https://lattice.dev/schemas/`. That base
is an **identifier namespace, not a live fetch target** — the resolver keys
schemas by `$id` from the local catalog and never makes a network request to
resolve a `$ref`.

Item-type and connection-type schemas embed a **semver in the `$id` path**:

```
https://lattice.dev/schemas/<name>/<major>.<minor>.<patch>
```

For example `https://lattice.dev/schemas/items/table/1.0.0`. Embedding the
version in the identifier lets multiple versions of the same type coexist in the
catalog, and lets a document pin exactly the type version it was authored
against. The dashboard document schema itself follows the same convention
(`https://lattice.dev/schemas/dashboard/1.0.0`).

## Config validation is additive per type

Each item type's schema defines its own `config` shape. The resolver validates
the **interpolated** config (variable references already substituted) against
the type schema; a mismatch is `RESOLVE_CONFIG_INVALID`, naming the offending
instance path. An absent `config` validates as an empty object, so a type's
required-field constraints still apply.

Some keywords in a type schema are read by the **resolver** rather than by
config validation. The most important is `expectedResult` on the `table` type:
it is a schema-level keyword (not instance config) that declares the
[result-shape contract](connections.md#result-shape-contract) and is ignored by
JSON Schema config validation.
