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
| `LAYOUT_FORM_COLUMNS_INVALID` | A `form`'s flow-layout column count is out of range. |
| `LAYOUT_FORM_CHILD_INVALID` | A `form` holds a child that is not a variable widget. |

## `CONFIGURABLE_*`, `CONFIG_OVERRIDE_*` — surfaces & overrides

See [Configurable Surfaces](../format/configurable.md) and
[Runtime Overrides](../format/overrides.md).

| Code | Meaning |
| --- | --- |
| `CONFIGURABLE_SURFACE_INVALID` | An item type's `configurable` declaration is malformed: it names a non-existent config field, gives a field an unknown value type, or sets a `rendering` hint naming a widget the catalog does not know. |
| `CONFIG_OVERRIDE_FIELD_UNKNOWN` | A `<node-id>.<field>` config override addressed a field not on the target's configurable surface (or a dotted sub-path). |
| `CONFIG_OVERRIDE_VALUE_INVALID` | A config-override value violates the target surface field's declared type or the item type's config-schema constraints. |

## `WRAPPER_*` — the block wrapper

See [Blocks & the Tree Grammar](../format/blocks-and-grammar.md#the-block-wrapper).

| Code | Meaning |
| --- | --- |
| `WRAPPER_ID_MISSING` | A block wrapper is missing its required stable `id` (absent or whitespace-only). |
| `WRAPPER_CHILD_COUNT_INVALID` | A block wrapper does not wrap exactly one inner content item (`content` absent, null, or not a single instance object). |

## `GRAMMAR_*` — the dashboard tree grammar

See [Blocks & the Tree Grammar](../format/blocks-and-grammar.md#the-grammar-rules).

| Code | Meaning |
| --- | --- |
| `GRAMMAR_ROOT_CHILD_INVALID` | A node directly under `root` is not a positional region (the only legal root children are `positional`-marked types, e.g. `container`, `variable-box`). |
| `GRAMMAR_REGION_CHILD_INVALID` | A `container` region holds an illegal child — a bare (unwrapped) content leaf, which must be block-wrapped. |
| `GRAMMAR_VARIABLE_BOX_CHILD_INVALID` | A `variable-box` holds a child that is not a variable widget held directly. |
| `GRAMMAR_WRAPPER_NESTED` | A block wrapper's single inner content is itself a block wrapper — wrappers do not recurse. |
| `GRAMMAR_REGION_THEME_FORBIDDEN` | A positional region carries a `theme` — regions are layout-only; only block wrappers carry chrome. |

## `CONFIGURATOR_*` — configurators

See [Configurators](../format/configurator.md).

| Code | Meaning |
| --- | --- |
| `CONFIGURATOR_TARGET_NOT_FOUND` | A configurator's `target` named an item id that no node in the tree declares. |
| `CONFIGURATOR_TARGET_MISSING_ID` | A configurator's `target` is empty/whitespace-only, so it names no resolvable id. |
| `CONFIGURATOR_TARGET_SCOPE_UNKNOWN` | A configurator's `target` is a `$`-prefixed keyword naming no known document scope (the recognized scopes are `$manifest`, `$variables`, `$connections`, `$theme`, `$root`). |

## `SERVE_*` — the HTTP layer

| Code | Meaning |
| --- | --- |
| `SERVE_INVALID` | Invalid `serve` invocation (missing document, out-of-range port). |
| `SERVE_RESOLVE` | The served document failed to resolve (wraps the underlying resolver error; rendered on the HTML error page). |
| `SERVE_INTERNAL` | Unexpected error in the web layer. |

## `STORAGE_*` — whole-document persistence

See [Storage Backends](storage.md).

| Code | Meaning |
| --- | --- |
| `STORAGE_ID_INVALID` | A document's `manifest.id` is not usable as a filename-safe addressing key: absent, empty/whitespace-only, containing a path separator, or a relative path element (`.`, `..`). |
| `STORAGE_NOT_FOUND` | A `Load`/`Delete`/`History`/`LoadAt` addressed an id (or revision) that no stored document/commit matches. |
| `STORAGE_IO` | An I/O failure reading or writing a document (open, write, rename, stat, remove), or a git operation failure (including the empty/no-op commit rejected when re-saving byte-identical content). |
| `STORAGE_INVALID` | A document could not be parsed far enough during `Save` to extract its `manifest.id` (malformed JSON or a missing manifest object). |
| `STORAGE_INTERNAL` | An unexpected error in a storage backend. |
| `STORAGE_BACKEND_UNKNOWN` | The `--store` value names no known backend (the recognized kinds are `fs` and `git`). |
