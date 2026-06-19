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
)

// SCHEMA domain - Type-schema catalog loading and JSON Schema validation.
const (
	// SCHEMA_NOT_FOUND indicates a referenced schema could not be located.
	SCHEMA_NOT_FOUND Code = "SCHEMA_NOT_FOUND"

	// SCHEMA_INVALID indicates a schema failed to parse or is malformed.
	SCHEMA_INVALID Code = "SCHEMA_INVALID"

	// SCHEMA_REF indicates a $ref could not be resolved.
	SCHEMA_REF Code = "SCHEMA_REF"

	// SCHEMA_VALIDATION indicates a document failed JSON Schema validation.
	SCHEMA_VALIDATION Code = "SCHEMA_VALIDATION"
)

// VAR domain - Variable declaration, scoping, and interpolation.
const (
	// VAR_UNDEFINED indicates a referenced variable was not declared.
	VAR_UNDEFINED Code = "VAR_UNDEFINED"

	// VAR_TYPE indicates a variable binding had the wrong type.
	VAR_TYPE Code = "VAR_TYPE"

	// VAR_EXPR indicates a computed-variable expression failed to evaluate.
	VAR_EXPR Code = "VAR_EXPR"
)

// CONNECTION domain - Connection (data source) declaration and binding.
const (
	// CONNECTION_NOT_FOUND indicates a referenced connection was not declared.
	CONNECTION_NOT_FOUND Code = "CONNECTION_NOT_FOUND"

	// CONNECTION_INVALID indicates a connection declaration is malformed.
	CONNECTION_INVALID Code = "CONNECTION_INVALID"
)
