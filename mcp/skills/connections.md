---
name: connections
description: Document-scoped data sources — the http connection (a query-style endpoint) vs the static connection (inline rows), how connections are declared in the top-level `connections` array (`{id, $ref, config?, secretRefs?}`) and bound by an item's `config.connectionId` + `query`, plus secret handling ($secret env refs are never inlined or persisted). Connections are declared + validated only, never dialed. Grammar stays in lattice_get_schema {type: "dashboard"}.
type: guide
kind: workflow
covers: [http, static]
applies_to: [lattice_get_outline, lattice_get_node, lattice_get_schema, lattice_validate_patch]
---

# Connections

A **connection** is a **document-scoped data source**. Items bind to one by id
and carry their own query. In this effort connections are **declared and
validated only — never dialed**: no live fetch, no network request, no real
data. The point is to validate the *model* — that the wiring is well-formed and
the shapes line up. This skill covers the two connection types and how
connections are declared and referenced; the exact field grammar stays in
`lattice_get_schema` (see **Reading the grammar**).

## The two connection types

| Type | `$ref` stem | What it is | Use it for |
|---|---|---|---|
| **`http`** | `connections/http/1.0.0` | A query-style HTTP(S) endpoint: `config` carries `url`, `method`, static `headers`/`query`. Never actually fetched in this effort. | Modelling a real remote data source's wiring. |
| **`static`** | `connections/static/1.0.0` | An inline source: `config.rows` embeds the result rows directly (objects of scalar cells), with optional `columns` ordering. | A fixture/no-backend source — and the one place inline data is checked against an item's result-shape contract. |

Both are **model-only**: `http` is never dialed; `static`'s `rows` are the only
data a result-shape contract can actually validate without a live fetch.

## Declaring connections

Connections live in the document's top-level **`connections` array**. Each
instance has the shape `{id, $ref, config?, secretRefs?}`:

```json
{
  "id": "metrics-api",
  "$ref": "https://lattice.dev/schemas/connections/http/1.0.0",
  "config": { "url": "https://api.example.com/metrics", "method": "GET" },
  "secretRefs": { "token": "vault://lattice/metrics-api#token" }
}
```

| Field | Required | Meaning |
|---|---|---|
| `id` | yes | Document-unique id; items bind by this id. Duplicate → `CONNECTION_DUPLICATE_ID`. |
| `$ref` | yes | URI of the connection-type schema (http or static), resolved against the catalog. Unresolvable → `CONNECTION_TYPE_UNRESOLVED`. |
| `config` | no | Per-connection config; shape is defined by the connection type. Invalid → `CONNECTION_CONFIG_INVALID`. |
| `secretRefs` | no | Indirection map from a logical secret name to an **opaque** reference token (e.g. a vault URI). Secret *values* are never inlined. |

For editing, the `connections` array is the **`$connections` scope** (id-rooted
patch paths lead with `/$connections/...`).

## Referencing a connection (the binding model)

An item draws data by naming a connection in its **`config`**:

```json
{
  "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
  "config": {
    "connectionId": "metrics-api",
    "query": {
      "region": { "$var": "region" },
      "hours":  { "$var": "window" },
      "label":  "last ${window}h"
    }
  }
}
```

- **`connectionId`** names a document-scoped connection. No match →
  `BINDING_CONNECTION_NOT_FOUND`.
- **`query`** is an arbitrary object passed to the connection. Its parameters
  **may reference [variables](variables.md)** via the same `$var` / `${}` forms
  as any config — interpolation runs over the whole item config (query included)
  *before* binding, so the resolved binding carries **concrete, typed values**,
  not references.
- A `query` **without** a `connectionId` is malformed (`BINDING_INVALID`).

In the resolved tree a bound item gains a `binding` block:
`{connectionId, query: <concrete values>, contract: {…}}`.

### Result-shape contract

A bound item type must declare an `expectedResult` (else `CONTRACT_MISSING`);
that fragment must be a well-formed schema (else `CONTRACT_INVALID`). For a
**`static`** connection, the inline `rows` are validated against the contract —
non-conforming data fails `RESULT_SHAPE_INVALID`. (The contract is stricter than
static's own config schema, which permits null cells.) Only the *declared* shape
(and inline static data) is checked — never live data.

## Secrets — never inlined, never persisted

Credentials are never stored in a document or its resolved tree. A connection's
`config` may carry a secret reference of the exact shape `{ "$secret": "NAME" }`.
At resolution the resolver reads `NAME` from the **process environment**
(`os.LookupEnv`), substitutes it **only to validate** the config, then
**discards** the value. The serialized output keeps:

- the **`{ "$secret": "NAME" }` reference object**, unchanged, and
- a sorted **`secrets`** list of the consumed names — **names only, never
  values**.

So the resolved tree is **secret-value-free by construction**. A malformed
reference (empty/non-string name) → `SECRET_INVALID`; a `NAME` absent from the
environment → `SECRET_MISSING` (so a document using `$secret` needs the env var
set even to resolve/validate). The separate `secretRefs` map is a complementary
indirection (logical name → opaque token), passed through verbatim and likewise
value-free.

## Reading the grammar (source layering)

Per **session-bootstrap**, a skill never re-emits schema grammar. The connection
**instance** shape and both connection-type schemas are referenced by the
dashboard envelope's `connections` member — the type schemas are not standalone
`lattice_get_schema` type tokens, so fetch the envelope:

- **`lattice_get_schema {type: "dashboard"}`** — the authoritative grammar for the
  `connections` array, the `{id, $ref, config?, secretRefs?}` instance shape, and
  (via `$ref`) the http/static config fields.
- **`lattice_get_outline {id}`** — its document-scope summary lists the declared
  connection **ids** (names only — no config bodies), so you can see what an item
  may bind to before drilling in.
- **`lattice_get_node {id, nodeId}`** — a bound item's `subtree` shows its
  `config.connectionId` / `config.query`; the `surface` lists which of those
  fields are settable.

Author or edit a connection (or a binding), then prove it with
**`lattice_validate_patch`** before a human commits — it surfaces the coded errors above
(`CONNECTION_*`, `BINDING_*`, `CONTRACT_*`, `RESULT_SHAPE_INVALID`,
`SECRET_*`) verbatim so you can correct and re-validate.

## Cross-links

- **variables** — the `$var` / `${}` interpolation a `query` may use.
- **patch-authoring** — the `$connections` scope and id-rooted pointer dialect.
- **session-bootstrap** — why the connection-type grammar stays in `lattice_get_schema`.
