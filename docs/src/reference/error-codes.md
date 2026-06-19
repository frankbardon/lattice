# Error Codes

Every failure is a `CodedError` with a typed `Code`, a message, and optional
structured `Details` (often a `path` naming the offending instance or
connection). Resolution is **fail-fast**: the first error stops the run and is
returned. With the global `--json` flag, the error is emitted as a
`{code, message, details}` JSON envelope on stderr.

Codes are grouped by domain.

## `RESOLVE_*` — document resolution

| Code | Meaning |
| --- | --- |
| `RESOLVE_INVALID` | Invalid invocation (e.g. missing document path argument). |
| `RESOLVE_IO` | I/O failure loading the document. |
| `RESOLVE_INTERNAL` | Unexpected internal error. |
| `RESOLVE_DOCUMENT_INVALID` | The document failed Pass 1 (structural validation) or is not valid JSON. |
| `RESOLVE_CONFIG_INVALID` | An instance's interpolated config failed item-type schema validation. |
| `RESOLVE_CHILDREN_NOT_ALLOWED` | Children declared on a non-container item type. |

## `SCHEMA_*` — catalog & reference resolution

| Code | Meaning |
| --- | --- |
| `SCHEMA_NOT_FOUND` | A referenced schema could not be located. |
| `SCHEMA_IO` | I/O failure reading a schema or document. |
| `SCHEMA_INVALID` | A schema failed to parse / is malformed. |
| `SCHEMA_REF` | A `$ref` could not be resolved. |
| `SCHEMA_VALIDATION` | A document failed JSON Schema validation. |
| `SCHEMA_REF_UNRESOLVED` | An instance `$ref` matched no catalog schema, relative file, or inline fragment. |
| `SCHEMA_VERSION_MISMATCH` | A `$ref` named a type whose pinned semver is missing/mismatched in the catalog. |

## `VAR_*` — variables

| Code | Meaning |
| --- | --- |
| `VAR_UNDEFINED` | A `$var` / `${}` reference named an undeclared or unset variable. |
| `VAR_TYPE` | A variable value (default, computed result, or override) did not match its declared type. |
| `VAR_EXPR` | A computed-variable expression failed to compile or evaluate. |
| `VAR_DECLARATION_INVALID` | Malformed declaration: missing name, unknown type, both `default` and `expr`, or a duplicate name in one scope. |
| `VAR_OPTIONS_INVALID` | Enum options missing/malformed, options on a non-enum, or a value outside the enum set. |
| `VAR_CYCLE` | Computed variables form a dependency cycle. |

## `CONNECTION_*`, `SECRET_*`, `BINDING_*`, `CONTRACT_*` — data

| Code | Meaning |
| --- | --- |
| `CONNECTION_NOT_FOUND` | A referenced connection was not declared. |
| `CONNECTION_INVALID` | A connection declaration is malformed (e.g. missing `$ref`). |
| `CONNECTION_DUPLICATE_ID` | Two connections share an id. |
| `CONNECTION_TYPE_UNRESOLVED` | A connection's `$ref` matched no connection-type schema. |
| `CONNECTION_CONFIG_INVALID` | A connection's config failed its connection-type schema. |
| `SECRET_INVALID` | A `{ "$secret": "name" }` reference has an empty or non-string name. |
| `SECRET_MISSING` | A referenced secret is not set in the environment. |
| `BINDING_INVALID` | A query without a `connectionId`, or a malformed `connectionId`/`query`. |
| `BINDING_CONNECTION_NOT_FOUND` | An item's `connectionId` matched no declared connection. |
| `CONTRACT_MISSING` | A bound item's type declares no `expectedResult` contract. |
| `CONTRACT_INVALID` | A bound item type's `expectedResult` is not a well-formed schema fragment. |
| `RESULT_SHAPE_INVALID` | A `static` connection's inline data violates the consuming item's contract. |

## `LAYOUT_*` — container grids

| Code | Meaning |
| --- | --- |
| `LAYOUT_PLACEMENT_INVALID` | A child placement carried a non-positive span or start. |
| `LAYOUT_PLACEMENT_OUT_OF_BOUNDS` | A child placement extends beyond the parent grid bounds. |

## `SERVE_*` — the HTTP layer

| Code | Meaning |
| --- | --- |
| `SERVE_INVALID` | Invalid `serve` invocation (missing document, out-of-range port). |
| `SERVE_RESOLVE` | The served document failed to resolve (wraps the underlying resolver error; rendered on the HTML error page). |
| `SERVE_INTERNAL` | Unexpected error in the web layer. |
