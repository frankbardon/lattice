package variables

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/frankbardon/lattice/errors"
)

// Environment is the set of variables VISIBLE at a single node, keyed by name.
// It is the shadowing winner per name: the value for a name is the nearest
// declaration on the path from the document root to the node. A nil/empty
// Environment means no variables are in scope at that node.
//
// Environment is the per-node artifact attached to the resolved tree. Because
// each ResolvedVar records DeclaredAt, the full var->node visibility mapping is
// recoverable from the tree alone (no separate index needed), which is what
// makes future dependency-tracked partial re-resolution possible.
type Environment map[string]ResolvedVar

// Names returns the visible variable names in sorted order, for deterministic
// iteration and diagnostics.
func (e Environment) Names() []string {
	if len(e) == 0 {
		return nil
	}
	names := make([]string, 0, len(e))
	for n := range e {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Lookup returns the visible variable for name and whether it is in scope.
func (e Environment) Lookup(name string) (ResolvedVar, bool) {
	v, ok := e[name]
	return v, ok
}

// Overrides is a set of EXTERNAL/runtime variable values keyed by name (E3-S4):
// values supplied at resolution time from a runtime input (a dropdown) or URL
// query params. An override replaces the EFFECTIVE value of a settable
// (literal-or-defaultable) variable of that name; computed (expr-bearing)
// variables are never overridable and keep their evaluated value. A nil/empty
// Overrides leaves every variable at its declared default. The override value is
// validated against the variable's declared type, so a bad runtime value fails
// fast with the same VAR_TYPE/VAR_OPTIONS_INVALID codes a bad default would.
type Overrides map[string]any

// Extend layers a node's own declarations onto the parent environment, returning
// a NEW environment (the parent is never mutated, so sibling subtrees stay
// independent). A declaration on this node SHADOWS an inherited one of the same
// name. path is the declaring node's resolved-tree path, recorded as DeclaredAt.
//
// Declarations are validated before they enter the environment; the first
// invalid declaration is returned as a CodedError (fail-fast). A duplicate name
// WITHIN a single node's declarations is rejected as a declaration error.
//
// Computed declarations (those carrying an expr, E3-S3) are layered LAST, after
// every literal in this scope is in place, and resolved in dependency order so
// an expression may reference inherited variables, sibling literals, and other
// computed variables (resolveComputed; a cycle fails fast with VAR_CYCLE).
func (e Environment) Extend(decls []Declaration, path string) (Environment, error) {
	return e.ExtendWithOverrides(decls, path, nil)
}

// ExtendWithOverrides is Extend plus E3-S4 runtime overrides: before computed
// declarations are evaluated, any override matching a settable variable declared
// on THIS node replaces that variable's effective value (override > default).
// Computed variables are never overridden. Because overrides are applied to the
// value slot computed expressions read, a computed variable that references an
// overridden literal recomputes against the runtime value — which is what makes
// a dropdown drive a `${var}` consumer through a computed chain.
func (e Environment) ExtendWithOverrides(decls []Declaration, path string, overrides Overrides) (Environment, error) {
	if len(decls) == 0 {
		// No local declarations: the node sees exactly its parent's scope. Return
		// the parent map directly; callers must treat environments as read-only.
		return e, nil
	}

	next := make(Environment, len(e)+len(decls))
	for k, v := range e {
		next[k] = v
	}

	seen := make(map[string]bool, len(decls))
	for i, d := range decls {
		if err := validateDeclaration(d, path, i); err != nil {
			return nil, err
		}
		if seen[d.Name] {
			return nil, errors.NewCodedErrorWithDetails(errors.VAR_DECLARATION_INVALID,
				"variable name is declared more than once on the same node",
				map[string]any{"path": path, "name": d.Name})
		}
		seen[d.Name] = true

		value := d.Default
		// E3-S4: a runtime override replaces a settable variable's effective value.
		// Computed variables (those carrying an expr) are never overridable.
		if !d.isComputed() {
			if ov, ok := overrides[d.Name]; ok {
				coerced, err := coerceOverride(ov, d, path)
				if err != nil {
					return nil, err
				}
				value = coerced
			}
		}

		// Computed values are filled in by resolveComputed below; placing the
		// declaration with a nil value first keeps the name in scope (so its
		// shadowing of an inherited var is correct) without a premature value.
		next[d.Name] = ResolvedVar{
			Name:       d.Name,
			Type:       d.Type,
			Default:    value,
			Expr:       d.Expr,
			Options:    d.Options,
			DeclaredAt: path,
		}
	}

	// E3-S3: evaluate computed declarations in dependency order, overwriting
	// their placeholder entries with the coerced expression results.
	if err := resolveComputed(decls, next, path); err != nil {
		return nil, err
	}
	return next, nil
}

// CoerceValue coerces v to the variable type t (with enum options, if any) and
// validates it, returning the canonical decoded-JSON value or a CodedError. It is
// the exported gate over the same coerce+validate logic the runtime variable
// override path uses (coerceOverride): a string from an untyped transport (a URL
// param) is parsed to t, then the value is type/enum checked, failing fast with
// the same VAR_TYPE / VAR_OPTIONS_INVALID codes a bad default would.
//
// It lets consumers outside this package (the resolver's E4-S2 config-override
// pass) type-check a supplied value against a declared type/option set without
// duplicating the coercion rules. loc is the node path the value belongs to and
// name labels it, both surfaced in any error's details; callers that need a
// different error CODE (e.g. CONFIG_OVERRIDE_VALUE_INVALID) wrap the returned
// CodedError.
func CoerceValue(v any, t VarType, options []any, loc, name string) (any, error) {
	return coerceOverride(v, Declaration{Name: name, Type: t, Options: options}, loc)
}

// coerceOverride normalizes a runtime override value to declaration d's declared
// type and validates it. Override values arrive either as already-typed decoded
// JSON (from the re-resolve endpoint) or as raw strings (from URL query params),
// so a string override targeting a non-string variable is parsed to the declared
// type before validation. A value that cannot be coerced or that fails the type
// (or enum-membership) check fails fast with a VAR_TYPE / VAR_OPTIONS_INVALID
// CodedError, exactly as a bad literal default would.
func coerceOverride(v any, d Declaration, path string) (any, error) {
	loc := fmt.Sprintf("%s.variables[%s]", path, d.Name)

	// URL query params (and other untyped sources) deliver every value as a
	// string; parse it to the declared type so a numeric/boolean variable can be
	// driven from a string-only transport.
	if s, ok := v.(string); ok && d.Type != VarTypeString && d.Type != VarTypeEnum {
		parsed, err := parseStringValue(s, d.Type)
		if err != nil {
			return nil, errors.WrapCodedErrorWithDetails(err, errors.VAR_TYPE,
				"runtime override value does not match its declared type",
				map[string]any{"path": loc, "name": d.Name, "type": string(d.Type), "value": s})
		}
		v = parsed
	}

	if err := validateValue(v, d, loc); err != nil {
		return nil, err
	}
	return v, nil
}

// parseStringValue parses a string-encoded override into the canonical
// decoded-JSON shape for the declared type (numbers as float64, booleans as
// bool, arrays as []any via JSON). Used for URL-query overrides where every
// value arrives as text.
func parseStringValue(s string, t VarType) (any, error) {
	switch t {
	case VarTypeNumber, VarTypeInteger:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("not a number: %q", s)
		}
		return f, nil
	case VarTypeBoolean:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return nil, fmt.Errorf("not a boolean: %q", s)
		}
		return b, nil
	case VarTypeArray:
		var arr []any
		if err := json.Unmarshal([]byte(s), &arr); err != nil {
			return nil, fmt.Errorf("not a JSON array: %q", s)
		}
		return arr, nil
	case VarTypeObject:
		var obj map[string]any
		if err := json.Unmarshal([]byte(s), &obj); err != nil {
			return nil, fmt.Errorf("not a JSON object: %q", s)
		}
		return obj, nil
	default:
		return s, nil
	}
}
