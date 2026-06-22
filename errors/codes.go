package errors

// Code is a typed string representing categorical error codes.
// Each code identifies a specific error category within a domain.
type Code string

// RESOLVE domain - Dashboard document resolution and validation.
const (
	// RESOLVE_INVALID indicates an invalid or malformed dashboard document.
	RESOLVE_INVALID Code = "RESOLVE_INVALID"

	// RESOLVE_IO indicates I/O failures while loading a dashboard document.
	RESOLVE_IO Code = "RESOLVE_IO"

	// RESOLVE_INTERNAL indicates an unexpected error in the resolver.
	RESOLVE_INTERNAL Code = "RESOLVE_INTERNAL"

	// RESOLVE_DOCUMENT_INVALID indicates the dashboard document failed the
	// first-pass structural validation against the dashboard JSON Schema.
	RESOLVE_DOCUMENT_INVALID Code = "RESOLVE_DOCUMENT_INVALID"

	// RESOLVE_CONFIG_INVALID indicates an instance's config failed the
	// second-pass validation against its resolved item-type schema. The
	// offending instance path is reported in Details["path"].
	RESOLVE_CONFIG_INVALID Code = "RESOLVE_CONFIG_INVALID"

	// RESOLVE_CHILDREN_NOT_ALLOWED indicates an instance declared children on a
	// non-container item type. The offending instance path is reported in
	// Details["path"].
	RESOLVE_CHILDREN_NOT_ALLOWED Code = "RESOLVE_CHILDREN_NOT_ALLOWED"
)

// SCHEMA domain - Type-schema catalog loading and JSON Schema validation.
const (
	// SCHEMA_NOT_FOUND indicates a referenced schema could not be located.
	SCHEMA_NOT_FOUND Code = "SCHEMA_NOT_FOUND"

	// SCHEMA_IO indicates an I/O failure while reading schemas or the dashboard
	// document from the filesystem.
	SCHEMA_IO Code = "SCHEMA_IO"

	// SCHEMA_INVALID indicates a schema failed to parse or is malformed.
	SCHEMA_INVALID Code = "SCHEMA_INVALID"

	// SCHEMA_REF indicates a $ref could not be resolved.
	SCHEMA_REF Code = "SCHEMA_REF"

	// SCHEMA_VALIDATION indicates a document failed JSON Schema validation.
	SCHEMA_VALIDATION Code = "SCHEMA_VALIDATION"

	// SCHEMA_REF_UNRESOLVED indicates an instance $ref could not be resolved to
	// any catalog schema, relative file, or inline $defs fragment.
	SCHEMA_REF_UNRESOLVED Code = "SCHEMA_REF_UNRESOLVED"

	// SCHEMA_VERSION_MISMATCH indicates an instance referenced a schema by name
	// but the pinned semver version is missing from or mismatched against the
	// catalog.
	SCHEMA_VERSION_MISMATCH Code = "SCHEMA_VERSION_MISMATCH"

	// SCHEMA_BEHAVIOR_INVALID indicates an item-type schema's `latticeBehavior`
	// keyword is incoherent and is rejected at catalog-index time (when the
	// ResolvedType is built), so a custom-type author finds the mistake
	// immediately rather than at resolve time. The block fails validation when:
	// `role` is absent or names an unknown role; a role is missing a required
	// sub-key (a `wrapper` without `contentField`, a `widget` with empty/absent
	// `binds`); `contentField` names a config property the schema does not
	// declare; `childPolicy` or `layout` is present on a non-region role; or any
	// of `role`/`childPolicy`/`layout`/`binds` members carries a value outside its
	// permitted enum. A schema that declares NO `latticeBehavior` block is a plain
	// leaf and is never rejected. The offending schema's $id (or source) is named
	// in Details["schema"], with the specific failing sub-key/value in
	// Details["field"]/["value"] when known.
	SCHEMA_BEHAVIOR_INVALID Code = "SCHEMA_BEHAVIOR_INVALID"
)

// VAR domain - Variable declaration, scoping, and interpolation.
const (
	// VAR_UNDEFINED indicates a referenced variable was not declared.
	VAR_UNDEFINED Code = "VAR_UNDEFINED"

	// VAR_TYPE indicates a variable binding had the wrong type.
	VAR_TYPE Code = "VAR_TYPE"

	// VAR_EXPR indicates a computed-variable expression failed to evaluate.
	VAR_EXPR Code = "VAR_EXPR"

	// VAR_DECLARATION_INVALID indicates a variable declaration is malformed: a
	// missing name, an unknown type, or a duplicate name within one scope. The
	// offending declaration path is reported in Details["path"].
	VAR_DECLARATION_INVALID Code = "VAR_DECLARATION_INVALID"

	// VAR_OPTIONS_INVALID indicates an enum variable's options are missing or
	// malformed, options were declared on a non-enum variable, or an enum default
	// is not one of the declared options. The offending path is in
	// Details["path"].
	VAR_OPTIONS_INVALID Code = "VAR_OPTIONS_INVALID"

	// VAR_CYCLE indicates a computed variable's expression participates in a
	// dependency cycle (an expression that, directly or transitively, depends on
	// its own value), so no evaluation order exists. The offending path and the
	// participating names are reported in Details.
	VAR_CYCLE Code = "VAR_CYCLE"

	// VAR_OVERRIDE_INVALID indicates a runtime override ADDRESS is malformed: an
	// empty address, or a node+field address ("<node-id>.<field>") missing its
	// node id or field path. The offending address is reported in
	// Details["address"]. (Whether the addressed variable/node exists is decided
	// later by application, not by address parsing.)
	VAR_OVERRIDE_INVALID Code = "VAR_OVERRIDE_INVALID"
)

// CONNECTION domain - Connection (data source) declaration and binding.
const (
	// CONNECTION_NOT_FOUND indicates a referenced connection was not declared.
	CONNECTION_NOT_FOUND Code = "CONNECTION_NOT_FOUND"

	// CONNECTION_INVALID indicates a connection declaration is malformed.
	CONNECTION_INVALID Code = "CONNECTION_INVALID"

	// CONNECTION_DUPLICATE_ID indicates two document-scoped connections share the
	// same id. The offending connection path is reported in Details["path"].
	CONNECTION_DUPLICATE_ID Code = "CONNECTION_DUPLICATE_ID"

	// CONNECTION_TYPE_UNRESOLVED indicates a connection's $ref could not be
	// resolved to any known connection-type schema in the catalog.
	CONNECTION_TYPE_UNRESOLVED Code = "CONNECTION_TYPE_UNRESOLVED"

	// CONNECTION_CONFIG_INVALID indicates a connection's config failed validation
	// against its resolved connection-type schema. The offending connection path
	// is reported in Details["path"].
	CONNECTION_CONFIG_INVALID Code = "CONNECTION_CONFIG_INVALID"

	// SECRET_INVALID indicates a malformed { "$secret": "name" } reference in a
	// connection's config: the node has the reserved key but an empty or
	// non-string name. The offending connection path is reported in
	// Details["path"].
	SECRET_INVALID Code = "SECRET_INVALID"

	// SECRET_MISSING indicates a { "$secret": "name" } reference named a secret
	// that is not present in the process environment at resolution time. The
	// secret name and connection path are reported in Details["name"]/["path"].
	SECRET_MISSING Code = "SECRET_MISSING"

	// BINDING_INVALID indicates an item's data binding is malformed: a query was
	// declared without a connectionId. The offending instance path is reported in
	// Details["path"].
	BINDING_INVALID Code = "BINDING_INVALID"

	// BINDING_CONNECTION_NOT_FOUND indicates an item's connectionId did not match
	// any document-scoped connection declared in the dashboard. The offending
	// instance path and connectionId are reported in
	// Details["path"]/["connectionId"].
	BINDING_CONNECTION_NOT_FOUND Code = "BINDING_CONNECTION_NOT_FOUND"

	// CONTRACT_MISSING indicates a bound item (one declaring a connectionId)
	// belongs to an item type that declares no expectedResult result-shape
	// contract, so the item↔connection wiring has no shape to validate against.
	// The offending instance path and item type are reported in
	// Details["path"]/["type"].
	CONTRACT_MISSING Code = "CONTRACT_MISSING"

	// CONTRACT_INVALID indicates a bound item's item-type expectedResult is not a
	// well-formed JSON Schema fragment (it fails to parse or compile). The
	// offending instance path and item type are reported in
	// Details["path"]/["type"].
	CONTRACT_INVALID Code = "CONTRACT_INVALID"

	// RESULT_SHAPE_INVALID indicates a static connection's inline data does not
	// conform to the consuming item's expectedResult contract — the one place a
	// real shape check is possible without a live fetch. The offending instance
	// path and connectionId are reported in Details["path"]/["connectionId"].
	RESULT_SHAPE_INVALID Code = "RESULT_SHAPE_INVALID"
)

// SERVE domain - The HTTP reference-renderer web layer (serve subcommand).
const (
	// SERVE_INVALID indicates invalid serve invocation or arguments (e.g. a
	// missing document path or out-of-range port).
	SERVE_INVALID Code = "SERVE_INVALID"

	// SERVE_RESOLVE indicates the served document failed to resolve; the
	// underlying resolver CodedError is wrapped as the cause and rendered on the
	// HTML error page.
	SERVE_RESOLVE Code = "SERVE_RESOLVE"

	// SERVE_INTERNAL indicates an unexpected error in the web layer (template
	// parsing, asset mounting, response encoding, or the listener).
	SERVE_INTERNAL Code = "SERVE_INTERNAL"
)

// WIDGET domain - Variable widget binding (E1) — the controls that set a
// document/container variable's runtime value.
const (
	// WIDGET_TYPE_MISMATCH indicates a variable widget bound a variable whose
	// declared type is not permitted by the widget's family (e.g. a string-family
	// text-input bound to a number variable). The offending instance path, the
	// bound variable name, the widget type, and the variable's declared type are
	// reported in Details["path"]/["variable"]/["widget"]/["varType"].
	WIDGET_TYPE_MISMATCH Code = "WIDGET_TYPE_MISMATCH"

	// CONFIGURABLE_SURFACE_INVALID indicates an item type's configurable-surface
	// declaration (E3-S1) is malformed: it names a config field the item type does
	// not declare, gives a field an unknown value type, or sets a rendering hint
	// naming a widget item-type the catalog does not know. The offending instance
	// path, the item type, and the offending surface field are reported in
	// Details["path"]/["type"]/["field"] (rendering violations also report
	// Details["rendering"]).
	CONFIGURABLE_SURFACE_INVALID Code = "CONFIGURABLE_SURFACE_INVALID"

	// CONFIG_OVERRIDE_FIELD_UNKNOWN indicates a node+field config override
	// ("<node-id>.<field>", E4-S2) addressed a field that is NOT exposed by the
	// target item type's configurable surface (E3) — either the field is not a
	// declared surface field, or the address is a dotted sub-path into a nested
	// object (surfaces cover top-level config fields only). The offending instance
	// path, the item type, the target node id, and the offending field are reported
	// in Details["path"]/["type"]/["node"]/["field"].
	CONFIG_OVERRIDE_FIELD_UNKNOWN Code = "CONFIG_OVERRIDE_FIELD_UNKNOWN"

	// CONFIG_OVERRIDE_VALUE_INVALID indicates a node+field config override value
	// (E4-S2) violates the target surface field's declared type or the item type's
	// config-schema constraints for that field. The offending instance path, the
	// item type, the target node id, and the offending field are reported in
	// Details["path"]/["type"]/["node"]/["field"].
	CONFIG_OVERRIDE_VALUE_INVALID Code = "CONFIG_OVERRIDE_VALUE_INVALID"
)

// LAYOUT domain - Container grid interpretation and child placement (E2-S1).
const (
	// LAYOUT_PLACEMENT_INVALID indicates a child placement carried a
	// non-positive span or start. The offending instance path is reported in
	// Details["path"] (with "field" and "value").
	LAYOUT_PLACEMENT_INVALID Code = "LAYOUT_PLACEMENT_INVALID"

	// LAYOUT_PLACEMENT_OUT_OF_BOUNDS indicates a child placement extends beyond
	// its parent container's grid bounds. The offending instance path is
	// reported in Details["path"] (with "axis", "start", "span", "tracks").
	LAYOUT_PLACEMENT_OUT_OF_BOUNDS Code = "LAYOUT_PLACEMENT_OUT_OF_BOUNDS"

	// LAYOUT_FORM_COLUMNS_INVALID indicates a `form` container's flow-layout
	// column count is out of range (non-positive or above the maximum). The
	// offending form path is reported in Details["path"] (with "field" and
	// "value").
	LAYOUT_FORM_COLUMNS_INVALID Code = "LAYOUT_FORM_COLUMNS_INVALID"

	// LAYOUT_FORM_CHILD_INVALID indicates a `form` container holds a child that
	// is not a variable widget. A form arranges widget controls in flow mode and
	// rejects non-widget children fail-fast. The offending child's instance path
	// and resolved item-type name are reported in Details["path"]/["type"].
	LAYOUT_FORM_CHILD_INVALID Code = "LAYOUT_FORM_CHILD_INVALID"
)

// WRAPPER domain - The block wrapper item type (E1): an item that wraps exactly
// one inner content item and carries the cross-cutting per-block concerns
// (stable id, theme override, title/label, visibility) applied to whatever it
// wraps. The resolver emits the wrapper and its single inner content as separate
// nodes; these codes guard the wrapper's own invariants fail-fast.
const (
	// WRAPPER_ID_MISSING indicates a block wrapper is missing its required stable
	// `id` config field, or carries one that is empty/whitespace-only — so the
	// block has no stable anchor for patches/configurators to address it by. The
	// item-type schema requires `id` (minLength 1) at the structural pass; this is
	// the defense-in-depth resolver guard (it also catches a whitespace-only id the
	// schema's minLength accepts). The offending wrapper's instance path is reported
	// in Details["path"].
	WRAPPER_ID_MISSING Code = "WRAPPER_ID_MISSING"

	// WRAPPER_CHILD_COUNT_INVALID indicates a block wrapper does not wrap EXACTLY
	// ONE inner content item: its `content` is absent, null, or not a single
	// instance object. A block holds exactly one content leaf and applies its
	// per-block concerns to it. The offending wrapper's instance path and the
	// observed content count are reported in Details["path"]/["count"].
	WRAPPER_CHILD_COUNT_INVALID Code = "WRAPPER_CHILD_COUNT_INVALID"
)

// GRAMMAR domain - The dashboard tree grammar (E3-S2): the structural shape the
// resolved tree must obey beyond per-instance schema validation. The grammar pass
// runs once over the assembled tree (index-once, fail-fast) and enforces where
// each kind of node may appear: root holds only positional regions; a region
// holds nested regions or block wrappers (never a bare content leaf); a
// variable-box holds variable widgets directly; a wrapper holds exactly one
// content leaf and is never re-wrapped; and a positional region carries no theme.
const (
	// GRAMMAR_ROOT_CHILD_INVALID indicates a node placed directly under `root` is
	// not a positional region (the only legal root children are positional region
	// types, marker-driven via the schema-level `positional` keyword — initially
	// container and variable-box). A content leaf or a block wrapper placed at root
	// fails. The offending child's instance path and resolved item-type name are
	// reported in Details["path"]/["type"].
	GRAMMAR_ROOT_CHILD_INVALID Code = "GRAMMAR_ROOT_CHILD_INVALID"

	// GRAMMAR_REGION_CHILD_INVALID indicates a `container` region holds an illegal
	// child: a container may nest other positional regions OR hold block wrappers,
	// but a bare (unwrapped) content leaf under a container fails — content must be
	// wrapped in a block. The offending child's instance path and resolved
	// item-type name are reported in Details["path"]/["type"].
	GRAMMAR_REGION_CHILD_INVALID Code = "GRAMMAR_REGION_CHILD_INVALID"

	// GRAMMAR_VARIABLE_BOX_CHILD_INVALID indicates a `variable-box` region holds a
	// child that is not a variable widget. A variable-box is the dedicated home for
	// the variable-widget family and holds them DIRECTLY (not block-wrapped); any
	// non-widget child — a nested region, a block wrapper, or a plain content leaf —
	// fails. The offending child's instance path and resolved item-type name are
	// reported in Details["path"]/["type"].
	GRAMMAR_VARIABLE_BOX_CHILD_INVALID Code = "GRAMMAR_VARIABLE_BOX_CHILD_INVALID"

	// GRAMMAR_WRAPPER_NESTED indicates a block wrapper's single inner content is
	// itself a block wrapper — a wrapper inside a wrapper. Wrappers do not recurse:
	// a block holds exactly one CONTENT leaf, never another wrapper. The offending
	// inner wrapper's instance path is reported in Details["path"].
	GRAMMAR_WRAPPER_NESTED Code = "GRAMMAR_WRAPPER_NESTED"

	// GRAMMAR_REGION_THEME_FORBIDDEN indicates a positional region node (container,
	// variable-box, …) carries a `theme` element. Positional regions are layout-only
	// and carry no chrome/theme — only block wrappers carry theme. The positional
	// schemas forbid theme structurally (additionalProperties:false), but the
	// grammar pass also rejects a theme appearing on a region node so the violation
	// surfaces with a clear, grammar-specific code. The offending region's instance
	// path and resolved item-type name are reported in Details["path"]/["type"].
	GRAMMAR_REGION_THEME_FORBIDDEN Code = "GRAMMAR_REGION_THEME_FORBIDDEN"
)

// STORAGE domain - Whole-document persistence backends (storage-backends E1):
// the dumb blob store that loads and saves dashboard documents by manifest.id.
// Backends (filesystem, git) share these codes; the store is upstream of the
// resolver and carries no JSON Patch awareness.
const (
	// STORAGE_ID_INVALID indicates a document's manifest.id is not usable as an
	// addressing key: it is absent, empty/whitespace-only, contains a path
	// separator, or is a relative path element (".", ".."). The id is the
	// filename stem (<id>.json), so it must be filename-safe. The offending id,
	// when present, is reported in Details["id"].
	STORAGE_ID_INVALID Code = "STORAGE_ID_INVALID"

	// STORAGE_NOT_FOUND indicates a Load addressed an id that no stored document
	// matches. The requested id is reported in Details["id"].
	STORAGE_NOT_FOUND Code = "STORAGE_NOT_FOUND"

	// STORAGE_IO indicates an I/O failure while reading or writing a document
	// (open, write, rename, stat, remove). The offending id and/or path are
	// reported in Details when known.
	STORAGE_IO Code = "STORAGE_IO"

	// STORAGE_INVALID indicates a document could not be parsed far enough to
	// extract its manifest.id during Save (malformed JSON or a missing manifest
	// object).
	STORAGE_INVALID Code = "STORAGE_INVALID"

	// STORAGE_INTERNAL indicates an unexpected error in a storage backend, or a
	// backend operation not yet implemented in the current slice.
	STORAGE_INTERNAL Code = "STORAGE_INTERNAL"

	// STORAGE_BACKEND_UNKNOWN indicates the requested backend kind (the --store
	// flag value) names no known backend. The recognized kinds are "fs" and
	// "git". The offending value is reported in Details["store"].
	STORAGE_BACKEND_UNKNOWN Code = "STORAGE_BACKEND_UNKNOWN"

	// STORAGE_CAPABILITY_UNSUPPORTED indicates a caller invoked a capability-gated
	// store operation — version history (History/LoadAt, the VersionedStore
	// capability) or current-revision lookup (Revision, the RevisionedStore
	// capability) — but the configured backend does NOT implement that optional
	// capability. The store capabilities are detected by type assertion and are not
	// uniform across backends: the filesystem backend implements RevisionedStore
	// but not VersionedStore, and a custom/injected store may implement neither.
	// Rather than silently degrade, the operation is rejected so the caller learns
	// the backend cannot answer. The manifest id is reported in Details["id"].
	// (This is the STORE-capability analogue of CHANGESET_REVISION_UNSUPPORTED,
	// which guards a precondition the apply pipeline cannot enforce.)
	STORAGE_CAPABILITY_UNSUPPORTED Code = "STORAGE_CAPABILITY_UNSUPPORTED"
)

// CONFIGURATOR domain - The configurator item type (E5): an item that renders an
// editor for another item in the same document, referenced by its stable id.
const (
	// CONFIGURATOR_TARGET_NOT_FOUND indicates a configurator's `target` named an
	// instance id that NO item in the resolved tree declares — the tree-wide id
	// index has no entry for it, so there is nothing to configure. The offending
	// configurator's instance path and the unresolved target id are reported in
	// Details["path"]/["target"].
	CONFIGURATOR_TARGET_NOT_FOUND Code = "CONFIGURATOR_TARGET_NOT_FOUND"

	// CONFIGURATOR_TARGET_MISSING_ID indicates a configurator's `target` reference
	// is itself non-stable: present but empty/whitespace-only, so it names no
	// resolvable id. Targeting requires a stable, declared id; an empty target has
	// no id to look up. (A normal NOT_FOUND covers the case where the id is
	// well-formed but unmatched; MISSING_ID is the defense-in-depth guard for a
	// target that carries no stable id at all.) The offending configurator's
	// instance path is reported in Details["path"].
	CONFIGURATOR_TARGET_MISSING_ID Code = "CONFIGURATOR_TARGET_MISSING_ID"

	// CONFIGURATOR_TARGET_SCOPE_UNKNOWN indicates a configurator's `target` is a
	// reserved, `$`-prefixed keyword that names no known document-level scope
	// (E4-S1). A `$`-prefixed target is ALWAYS routed to a document scope, never an
	// item id, so an unrecognized scope keyword fails fast rather than falling
	// through to an item lookup. The recognized scopes are `$manifest`,
	// `$variables`, `$connections`, `$theme`, and `$root`. The offending
	// configurator's instance path and the unknown scope keyword are reported in
	// Details["path"]/["target"].
	CONFIGURATOR_TARGET_SCOPE_UNKNOWN Code = "CONFIGURATOR_TARGET_SCOPE_UNKNOWN"
)

// CHANGESET domain - The JSON Patch apply layer (patch-write-pipeline E1): a
// changeset is an RFC 6902 JSON Patch document whose pointers are ID-ROOTED — the
// leading pointer segment is an item's stable `id` or a reserved `$`-scope
// keyword, and the remainder is literal RFC 6901. These codes guard parsing a
// changeset document and translating its id-rooted pointers into physical RFC
// 6901 pointers against the decoded document tree. Unknown `$`-scopes reuse the
// configurator domain's CONFIGURATOR_TARGET_SCOPE_UNKNOWN code.
const (
	// CHANGESET_INVALID indicates a changeset document is malformed: it is not a
	// JSON array of operation objects, or an operation is missing/has a wrong-typed
	// required member (a non-string/absent `op` or `path`, an unknown `op`, a
	// `value`-requiring op without `value`, or a `from`-requiring op without a
	// `from`). The offending operation index and the reason are reported in Details
	// when known (Details["index"]/["op"]).
	CHANGESET_INVALID Code = "CHANGESET_INVALID"

	// CHANGESET_POINTER_INVALID indicates an id-rooted changeset pointer is not
	// well-formed for translation: it is empty, does not begin with "/", or carries
	// an empty leading id/scope segment, so it names no item id or `$`-scope to
	// resolve. The offending pointer and operation index are reported in
	// Details["pointer"]/["index"].
	CHANGESET_POINTER_INVALID Code = "CHANGESET_POINTER_INVALID"

	// CHANGESET_TARGET_NOT_FOUND indicates an id-rooted changeset pointer's leading
	// segment names an item `id` that NO node in the decoded document tree declares
	// — there is nothing to address, so the pointer cannot be translated to a
	// physical location. (A `$`-scope that is unknown reuses
	// CONFIGURATOR_TARGET_SCOPE_UNKNOWN instead; this code is the item-id miss.) The
	// unresolved id, the offending pointer, and the operation index are reported in
	// Details["id"]/["pointer"]/["index"].
	CHANGESET_TARGET_NOT_FOUND Code = "CHANGESET_TARGET_NOT_FOUND"

	// PATCH_APPLY_FAILED indicates a translated changeset could not be applied to
	// the document by the RFC 6902 applier: the patch did not decode, or an
	// operation failed at apply time (e.g. a `test` precondition mismatch, a
	// remove/replace of a member that does not exist, or an out-of-range array
	// index). The whole changeset is rejected and nothing is persisted (atomic).
	PATCH_APPLY_FAILED Code = "PATCH_APPLY_FAILED"

	// PATCH_INVALID indicates an invalid `lattice patch` invocation: a missing
	// manifest id argument, a missing/unreadable --changeset file, or a stdin read
	// failure. It guards the CLI command's inputs before the apply pipeline runs;
	// pipeline-internal failures surface as their own CHANGESET_*/PATCH_APPLY_FAILED
	// (or storage/resolver) codes.
	PATCH_INVALID Code = "PATCH_INVALID"

	// CHANGESET_REVISION_CONFLICT indicates an OPTIMISTIC-CONCURRENCY precondition
	// failed (E4-S2): the apply carried an expected revision (WithExpectedRevision),
	// but the store's CURRENT revision — re-read immediately before Save — no longer
	// matches it, so the document changed since the caller loaded it. The whole
	// changeset is rejected and nothing is persisted (atomic), leaving the stored
	// document byte-for-byte unchanged. The code is distinct so callers can RETRY:
	// reload, re-derive the changeset against the new bytes, and re-apply. The
	// expected and current revision tokens are reported in
	// Details["expected"]/["current"] (with the manifest id in Details["id"]).
	CHANGESET_REVISION_CONFLICT Code = "CHANGESET_REVISION_CONFLICT"

	// CHANGESET_REVISION_UNSUPPORTED indicates an apply carried an expected revision
	// (WithExpectedRevision) but the configured store does NOT implement the
	// RevisionedStore capability, so the precondition cannot be enforced. Rather than
	// silently ignore a precondition the caller asked for, the apply is rejected and
	// nothing is persisted. The manifest id is reported in Details["id"]. (Both the
	// filesystem and git backends implement RevisionedStore, so this guards a
	// custom/stub store only.)
	CHANGESET_REVISION_UNSUPPORTED Code = "CHANGESET_REVISION_UNSUPPORTED"

	// CHANGESET_STRUCTURAL_ID_INVALID indicates a structural `add` op's value (a
	// full item instance inserted into a `children` array) does not carry a valid,
	// document-unique `id`: the value is not an object, its `id` member is missing
	// or not a non-empty string, or the `id` collides with an id already present in
	// the document being patched. Structural adds are gated by re-resolve for
	// grammar/schema, but id presence + uniqueness is enforced HERE because the
	// resolver's id index is last-wins and would silently accept a duplicate. The
	// offending pointer, the supplied id (when present), and the operation index are
	// reported in Details["pointer"]/["id"]/["index"]. The whole changeset is
	// rejected and nothing is persisted (atomic).
	CHANGESET_STRUCTURAL_ID_INVALID Code = "CHANGESET_STRUCTURAL_ID_INVALID"
)

// MCP domain - The Model Context Protocol server layer (mcp subcommand). The MCP
// server exposes lattice's read/dry-run capabilities to an MCP host over a
// transport (stdio now, HTTP later). It NEVER persists. These codes guard the
// `lattice mcp` command's inputs and transport before any tool runs; tool-level
// failures surface their own RESOLVE_*/CHANGESET_*/etc. codes verbatim.
const (
	// MCP_INVALID indicates an invalid `lattice mcp` invocation: a missing or
	// malformed flag value (e.g. an unparseable --store backend) detected before
	// the server is constructed.
	MCP_INVALID Code = "MCP_INVALID"

	// MCP_INTERNAL indicates an unexpected failure in the MCP server layer: the
	// service facade could not be assembled, or the transport failed while serving.
	MCP_INTERNAL Code = "MCP_INTERNAL"

	// MCP_SKILL_NOT_FOUND indicates the get_skill tool was asked for a skill name
	// that no embedded skill file matches — there is no such skill in the corpus.
	// The skills corpus is a fixed, self-contained embedded data set (no store or
	// resolver is consulted), so no existing STORAGE_*/SCHEMA_* code fits: this is
	// a miss against the skills package itself. The requested name is reported in
	// Details["name"].
	MCP_SKILL_NOT_FOUND Code = "MCP_SKILL_NOT_FOUND"
)
