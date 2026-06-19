# Connections

A **connection** is a document-scoped data source. Items bind to a connection by
id and carry their own query. In this effort connections are **declared and
validated only — never dialed**: there is no live fetch, no network request, no
real data. The point is to validate the *model* — that the wiring is well-formed
and the shapes line up.

## Declaring connections

Connections live in the top-level `connections` array. Each instance has the
shape `{ id, $ref, config?, secretRefs? }`:

```json
{
  "id": "metrics-api",
  "$ref": "https://lattice.dev/schemas/connections/http/1.0.0",
  "config": { "url": "https://api.example.com/metrics", "method": "GET" },
  "secretRefs": { "token": "vault://lattice/metrics-api#token" }
}
```

| Field | Required | Notes |
| --- | --- | --- |
| `id` | yes | Document-unique identifier; items bind by this id. |
| `$ref` | yes | URI of the connection-type schema (validated against the catalog). |
| `config` | no | Per-connection config; shape defined by the connection type. |
| `secretRefs` | no | Indirection map from a logical secret name to an opaque reference token. Secret *values* are never inlined. |

The resolver resolves each `$ref` to a connection-type schema using the same
machinery as item `$ref`s, validates the config against that schema, and rejects
duplicate ids (`CONNECTION_DUPLICATE_ID`). An unresolvable `$ref` is
`CONNECTION_TYPE_UNRESOLVED`; an invalid config is `CONNECTION_CONFIG_INVALID`.

The two connection types are [`http`](catalog.md#http-connectionshttp100) (a
query-style endpoint) and [`static`](catalog.md#static-connectionsstatic100)
(inline rows embedded in `config`).

## The direct binding model

An item draws data by naming a connection in its `config`:

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

- `connectionId` names a document-scoped connection. If it matches no declared
  connection, resolution fails with `BINDING_CONNECTION_NOT_FOUND`.
- `query` is an arbitrary object passed to the connection. Its **parameters may
  reference variables** using the same `$var` / `${}` forms as any config — the
  interpolation pass runs over the whole item config (including the query)
  before binding, so by the time the binding is lifted onto the resolved node the
  query carries **concrete, typed values**, not references.
- A `query` declared **without** a `connectionId` is malformed
  (`BINDING_INVALID`).

In the resolved tree, a bound item gains a `binding` block:

```json
"binding": {
  "connectionId": "metrics-api",
  "query": { "region": "us-east", "hours": 24, "label": "last 24h" },
  "contract": { ... }
}
```

## Secret handling

Credentials are never stored in a dashboard document or its resolved tree. A
connection's `config` may carry a secret reference of the exact shape
`{ "$secret": "NAME" }`. At resolution time the resolver:

1. Reads `NAME` from the **process environment** (`os.LookupEnv`).
2. Substitutes the value **only to validate** the connection config (a
   connection-type schema expects the concrete value, e.g. a header string).
3. **Discards the resolved value** immediately afterward.

What is kept in the resolved tree is *not* the value:

- The connection's `config` retains the **`{ "$secret": "NAME" }` reference
  object**, unchanged.
- A sorted `secrets` list records **which** secret names the connection
  consumed — names only, never values.

So the serialized resolved tree is **secret-value-free by construction**. A
malformed reference (empty or non-string name) fails fast with `SECRET_INVALID`;
a reference whose `NAME` is absent from the environment fails fast with
`SECRET_MISSING`.

> Because a `$secret` must resolve from the environment, documents that use one
> require the variable to be set even just to `resolve` by hand. For example,
> `examples/kitchen-sink-dashboard.json` needs `METRICS_API_TOKEN`:
>
> ```sh
> METRICS_API_TOKEN=xyz lattice resolve examples/kitchen-sink-dashboard.json
> ```
>
> The token value never appears in the output.

The `secretRefs` map on a connection is a separate, complementary indirection:
it maps a logical secret name to an **opaque reference token** (e.g. a vault
URI) and is passed through verbatim. It, too, never carries a secret value.

## Result-shape contract

A bound item type may declare what its connection's results should look like via
the `expectedResult` schema-level keyword. For the `table` type, that contract
is: an array of non-empty row objects whose cells are non-null scalars (string,
number, or boolean).

When an item declares a `connectionId`, the resolver enforces the contract:

- The item's type **must** declare an `expectedResult`, or resolution fails with
  `CONTRACT_MISSING` — a binding with no shape to validate against is an error.
- The `expectedResult` fragment **must be a well-formed** draft 2020-12 schema,
  or resolution fails with `CONTRACT_INVALID`.
- For a **`static` connection**, the inline `rows` are the one place a real data
  check is possible without a live fetch, so the resolver validates them against
  the contract; non-conforming data fails with `RESULT_SHAPE_INVALID`. (Note the
  contract is stricter than the static connection's own config schema, which
  also permits null cells.)

The validated contract is recorded on the binding as `contract`
(`{ itemType, connectionId, expectedResult }`). It is **model-only**: the
resolver validates the declared shape (and inline static data), never live
fetched data.
