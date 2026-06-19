# Lattice schema catalog

JSON Schema (draft 2020-12) definitions for Lattice dashboard documents and item
types.

## Base URL

All schema `$id`s use the stable base URL:

```
https://lattice.dev/schemas/
```

The URL is an identifier namespace, not a live fetch target — the resolver loads
schemas from this local catalog and keys them by `$id`. Item-type schemas embed a
semver in the path so multiple versions can coexist:

```
https://lattice.dev/schemas/<name>/<major>.<minor>.<patch>
```

## Layout

- `dashboard.schema.json` — top-level document schema (`https://lattice.dev/schemas/dashboard/1.0.0`).
  Defines the `manifest` and the recursive `root` item instance. An instance node
  is `{ "$ref": <item-type uri>, "config", "placement", "children"? }`.
  `children` is permitted structurally on any node; the rule that only containers
  may have children is enforced by the resolver (E1-S4), not by this schema.
- `items/` — item-type schemas referenced by instance `$ref`s.
  - `container.schema.json` (`.../items/container/1.0.0`) — the only
    structurally-special type; groups children on a (stubbed) grid. E2-S1
    formalizes relative-weight tracks + placement.
  - `table.schema.json` (`.../items/table/1.0.0`) — a static leaf type with
    opaque config; no data binding.
  - `dropdown.schema.json` (`.../items/dropdown/1.0.0`) — a runtime-input leaf
    type (E3-S4) binding a variable name to a fixed option set. The reference
    renderer draws it as an Alpine `<select>` and re-resolves the document on
    change; the chosen value becomes the variable's runtime override
    (override > default), so dependent `${var}`/`$var` consumers update live.
- `connections/` — connection (data source) type schemas, referenced by
  document-scoped connection instances (`{ id, $ref, config, secretRefs? }`).
  Loaded into the same catalog as item types and validated the same way by the
  resolver (E4-S1). Connections are declared and validated only — never dialed.
  - `http.schema.json` (`.../connections/http/1.0.0`) — a query-style backend
    (endpoint + request shape; credentials via `secretRefs`, never inlined).
  - `static.schema.json` (`.../connections/static/1.0.0`) — an inline data
    source whose rows live in `config`; lets the result-shape contract (E4-S3)
    be exercised without a real backend.

## Examples

Hand-written conforming documents live in `../examples/` and serve as downstream
fixtures.
