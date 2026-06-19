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
