// Package variables defines the dashboard variable model and computes a
// tree-scoped variable environment for the resolved tree.
//
// A variable DECLARATION is { name, type, default?|expr?, options? } and may
// appear at document scope or on any container instance. Resolution walks the
// item/grid tree and, for every node, computes the set of variables VISIBLE at
// that node by layering declarations from the document root down to the node
// itself: an inner declaration SHADOWS an outer one of the same name
// (item-local -> ancestor containers -> doc).
//
// Each visible variable records WHERE it was declared (the owning node's path),
// so consumers and a future dependency tracker can see var->node visibility and
// perform partial re-resolution (deferred; this story only exposes the mapping).
//
// This package owns the model, scoping, and interpolation. Interpolation
// ($var typed bindings / ${...} string templates) is E3-S2, exposed as the
// reusable Interpolate function (see interpolate.go) which the resolver applies
// to instance configs and E4-S2 reuses for query params.
//
// A declaration may instead carry an `expr` (E3-S3): a string expression
// evaluated with github.com/expr-lang/expr against the in-scope variables. The
// computed value is coerced/validated to the declared type and then flows into
// the SAME value slot a default would, so interpolation and dependency tracking
// treat computed and literal variables identically. Cross-variable references
// are resolved in dependency order with cycle detection (see computed.go).
package variables

import (
	"fmt"
	"math"

	"github.com/frankbardon/lattice/errors"
)

// VarType is the declared type of a variable. The primitive set plus enum and
// array, per the variable declaration schema.
type VarType string

const (
	// VarTypeString is a JSON string value.
	VarTypeString VarType = "string"
	// VarTypeNumber is any JSON number (integer or fractional).
	VarTypeNumber VarType = "number"
	// VarTypeInteger is a JSON number with no fractional part.
	VarTypeInteger VarType = "integer"
	// VarTypeBoolean is a JSON boolean.
	VarTypeBoolean VarType = "boolean"
	// VarTypeEnum is a string constrained to a fixed set of options.
	VarTypeEnum VarType = "enum"
	// VarTypeArray is a JSON array of arbitrary element values.
	VarTypeArray VarType = "array"
	// VarTypeObject is a JSON object of arbitrary keyed values. It backs the
	// STRUCTURED configurable surfaces (a panel `spec`/`request`/`display`, a
	// container `grid`) whose value is an object the create/edit modal drives by
	// its sub-fields; the authoritative structure check is the item-type config
	// schema re-validation, so the type check here is only "it is an object."
	VarTypeObject VarType = "object"
)

// validTypes is the set of accepted declaration types.
var validTypes = map[VarType]bool{
	VarTypeString:  true,
	VarTypeNumber:  true,
	VarTypeInteger: true,
	VarTypeBoolean: true,
	VarTypeEnum:    true,
	VarTypeArray:   true,
	VarTypeObject:  true,
}

// IsValidType reports whether t is one of the accepted variable types. It is the
// exported gate over the same set declaration validation uses, so consumers
// outside this package (e.g. the resolver's configurable-surface pass) can
// type-check a declared value type without duplicating the set.
func IsValidType(t VarType) bool { return validTypes[t] }

// Declaration is a single variable declaration as authored on the document or a
// container instance. It mirrors the JSON shape
// { name, type, default?|expr?, options? }.
type Declaration struct {
	// Name is the variable's identifier, unique within its declaring scope.
	Name string `json:"name"`

	// Type is the declared type (one of VarType).
	Type VarType `json:"type"`

	// Default is the optional default value. When present it is validated
	// against Type. Nil means no default was declared. Mutually exclusive with
	// Expr.
	Default any `json:"default,omitempty"`

	// Expr is an optional string expression (github.com/expr-lang/expr) whose
	// evaluation against the in-scope variables produces the variable's value.
	// Mutually exclusive with Default; the computed result is coerced/validated
	// to Type just like a default would be.
	Expr string `json:"expr,omitempty"`

	// Options is the permitted value set for enum-typed variables. Required for
	// enum, forbidden for every other type.
	Options []any `json:"options,omitempty"`
}

// isComputed reports whether the declaration's value comes from an expression
// rather than a literal default.
func (d Declaration) isComputed() bool { return d.Expr != "" }

// ResolvedVar is one variable as VISIBLE at a node: its declaration plus the
// path of the node that declared it. DeclaredAt powers var->node visibility:
// a dependency tracker can see which scope a name resolves to from any node.
type ResolvedVar struct {
	// Name is the variable name (duplicated from the declaration for convenient
	// indexing in serialized form).
	Name string `json:"name"`

	// Type is the declared type.
	Type VarType `json:"type"`

	// Default is the variable's value: the declared literal default, or, for a
	// computed variable, the evaluated-and-coerced expression result. Consumers
	// (interpolation, dependency tracking) read this slot uniformly regardless of
	// how the value was produced.
	Default any `json:"default,omitempty"`

	// Expr is the source expression for a computed variable, retained for
	// provenance/diagnostics. Empty for literal variables.
	Expr string `json:"expr,omitempty"`

	// Options is the enum option set, if any.
	Options []any `json:"options,omitempty"`

	// DeclaredAt is the resolved-tree path of the node that declared this
	// variable ("root", "root.children[1]", ...). It is the shadowing winner:
	// the nearest declaration of Name on the path from the root to the node.
	DeclaredAt string `json:"declaredAt"`
}

// validateDeclaration checks a single declaration's type and value invariants.
// path identifies the declaring node; index is the declaration's position in
// that node's variables array. Returns the first violation as a CodedError.
func validateDeclaration(d Declaration, path string, index int) error {
	loc := fmt.Sprintf("%s.variables[%d]", path, index)

	if d.Name == "" {
		return errors.NewCodedErrorWithDetails(errors.VAR_DECLARATION_INVALID,
			"variable declaration is missing a name",
			map[string]any{"path": loc})
	}
	if !validTypes[d.Type] {
		return errors.NewCodedErrorWithDetails(errors.VAR_DECLARATION_INVALID,
			"variable declaration has an unknown type",
			map[string]any{"path": loc, "name": d.Name, "type": string(d.Type)})
	}

	// A declaration's value is either a literal default OR a computed expr, never
	// both. (Either may be absent: a bare {name,type} is a valid, value-less
	// declaration.)
	if d.Default != nil && d.isComputed() {
		return errors.NewCodedErrorWithDetails(errors.VAR_DECLARATION_INVALID,
			"variable declaration may not set both default and expr",
			map[string]any{"path": loc, "name": d.Name})
	}

	// Enum requires options; every other type forbids them.
	if d.Type == VarTypeEnum {
		if len(d.Options) == 0 {
			return errors.NewCodedErrorWithDetails(errors.VAR_OPTIONS_INVALID,
				"enum variable declaration requires a non-empty options set",
				map[string]any{"path": loc, "name": d.Name})
		}
		for i, opt := range d.Options {
			if _, ok := opt.(string); !ok {
				return errors.NewCodedErrorWithDetails(errors.VAR_OPTIONS_INVALID,
					"enum option must be a string",
					map[string]any{"path": loc, "name": d.Name, "optionIndex": i})
			}
		}
	} else if len(d.Options) > 0 {
		return errors.NewCodedErrorWithDetails(errors.VAR_OPTIONS_INVALID,
			"options may only be declared on enum variables",
			map[string]any{"path": loc, "name": d.Name, "type": string(d.Type)})
	}

	// A declared default must satisfy the declared type (and enum membership).
	if d.Default != nil {
		if err := validateValue(d.Default, d, loc); err != nil {
			return err
		}
	}
	return nil
}

// validateValue checks that v conforms to declaration d. loc is the declaring
// location, reported in errors. Values arrive as decoded JSON, so numbers are
// float64 and integers are float64 with no fractional part.
func validateValue(v any, d Declaration, loc string) error {
	typeErr := func() error {
		return errors.NewCodedErrorWithDetails(errors.VAR_TYPE,
			"variable value does not match its declared type",
			map[string]any{"path": loc, "name": d.Name, "type": string(d.Type)})
	}

	switch d.Type {
	case VarTypeString:
		if _, ok := v.(string); !ok {
			return typeErr()
		}
	case VarTypeBoolean:
		if _, ok := v.(bool); !ok {
			return typeErr()
		}
	case VarTypeNumber:
		if !isNumber(v) {
			return typeErr()
		}
	case VarTypeInteger:
		n, ok := v.(float64)
		if !ok || n != math.Trunc(n) {
			return typeErr()
		}
	case VarTypeArray:
		if _, ok := v.([]any); !ok {
			return typeErr()
		}
	case VarTypeObject:
		if _, ok := v.(map[string]any); !ok {
			return typeErr()
		}
	case VarTypeEnum:
		s, ok := v.(string)
		if !ok {
			return typeErr()
		}
		found := false
		for _, opt := range d.Options {
			if os, ok := opt.(string); ok && os == s {
				found = true
				break
			}
		}
		if !found {
			return errors.NewCodedErrorWithDetails(errors.VAR_OPTIONS_INVALID,
				"variable value is not one of the declared enum options",
				map[string]any{"path": loc, "name": d.Name, "value": s})
		}
	}
	return nil
}

// isNumber reports whether v is a JSON number. Decoded JSON numbers are float64;
// json.Number is accepted defensively in case a decoder is configured for it.
func isNumber(v any) bool {
	switch v.(type) {
	case float64, float32, int, int64:
		return true
	default:
		return false
	}
}
