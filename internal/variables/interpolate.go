package variables

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/frankbardon/lattice/errors"
)

// varBindingKey is the single reserved key that marks a typed-binding node. An
// object of the exact shape { "$var": "name" } is replaced by the named
// variable's value with its JSON type preserved (an integer stays an integer).
const varBindingKey = "$var"

// templatePattern matches a ${name} string-template reference. The captured
// group is the variable name. Templates are interpolated INTO strings, so the
// result is always a string regardless of the referenced variable's type.
var templatePattern = regexp.MustCompile(`\$\{([^}]*)\}`)

// Interpolate substitutes variable references inside an arbitrary decoded-JSON
// value against env, returning a NEW value (the input is never mutated). It is
// the reusable core of E3-S2: the resolver applies it to each instance's config
// before that config is validated, and E4-S2 reuses it for query params.
//
// Two reference forms are recognized:
//
//   - Typed binding: an object { "$var": "name" } is replaced WHOLESALE by the
//     named variable's value, preserving its JSON type. The object must have
//     exactly that one key and a string value; any other shape is left intact
//     (it is treated as ordinary data, not a binding).
//   - String template: every ${name} occurrence inside a string value is
//     replaced by the named variable's value rendered as text. The surrounding
//     string is preserved, so the result is always a string.
//
// Maps and slices are walked recursively. A reference to a name not visible in
// env is a fail-fast VAR_UNDEFINED CodedError; path identifies the owning
// instance (e.g. "root.children[2]") so an author can locate the reference.
//
// The substituted value of a variable is its EFFECTIVE value: a runtime override
// (E3-S4) when one was supplied for a settable variable, otherwise its declared
// default (or, for a computed variable, its evaluated result). The selection
// happens upstream when the environment is built (see Environment.Extend /
// ExtendWithOverrides), so interpolation uniformly reads ResolvedVar.Default —
// the single value slot every value source flows into.
func Interpolate(value any, env Environment, path string) (any, error) {
	switch v := value.(type) {
	case map[string]any:
		if name, ok := asVarBinding(v); ok {
			return resolveValue(name, env, path)
		}
		out := make(map[string]any, len(v))
		for k, elem := range v {
			res, err := Interpolate(elem, env, path)
			if err != nil {
				return nil, err
			}
			out[k] = res
		}
		return out, nil

	case []any:
		out := make([]any, len(v))
		for i, elem := range v {
			res, err := Interpolate(elem, env, path)
			if err != nil {
				return nil, err
			}
			out[i] = res
		}
		return out, nil

	case string:
		return interpolateString(v, env, path)

	default:
		// Numbers, booleans, and nil carry no references; pass them through.
		return value, nil
	}
}

// asVarBinding reports whether m is exactly a typed-binding node { "$var": name }
// and, if so, returns the referenced name. A binding must have exactly one key
// ("$var") whose value is a string; any other shape is ordinary data.
func asVarBinding(m map[string]any) (string, bool) {
	if len(m) != 1 {
		return "", false
	}
	raw, ok := m[varBindingKey]
	if !ok {
		return "", false
	}
	name, ok := raw.(string)
	return name, ok
}

// resolveValue returns the type-preserved value of the named variable, or a
// VAR_UNDEFINED error naming the offending instance path.
func resolveValue(name string, env Environment, path string) (any, error) {
	rv, ok := env.Lookup(name)
	if !ok {
		return nil, undefined(name, path, varBindingKey)
	}
	return rv.Default, nil
}

// interpolateString replaces every ${name} occurrence in s. The first reference
// to an undefined name fails fast. When s contains no template, it is returned
// unchanged.
func interpolateString(s string, env Environment, path string) (string, error) {
	if !strings.Contains(s, "${") {
		return s, nil
	}

	var firstErr error
	out := templatePattern.ReplaceAllStringFunc(s, func(match string) string {
		if firstErr != nil {
			return match
		}
		name := templatePattern.FindStringSubmatch(match)[1]
		rv, ok := env.Lookup(name)
		if !ok {
			firstErr = undefined(name, path, "${}")
			return match
		}
		return renderScalar(rv.Default)
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

// renderScalar renders a variable value as text for string-template embedding.
// Decoded-JSON numbers are float64; integral values render without a trailing
// ".0" so an integer-typed variable templates cleanly (e.g. 10, not 10.000000).
func renderScalar(v any) string {
	switch n := v.(type) {
	case nil:
		return ""
	case string:
		return n
	case float64:
		if n == float64(int64(n)) {
			return fmt.Sprintf("%d", int64(n))
		}
		return fmt.Sprintf("%g", n)
	default:
		return fmt.Sprintf("%v", n)
	}
}

// undefined builds the fail-fast VAR_UNDEFINED error for a missing reference,
// naming the offending instance path and the reference form.
func undefined(name, path, form string) error {
	return errors.NewCodedErrorWithDetails(errors.VAR_UNDEFINED,
		"reference to an undeclared or unset variable",
		map[string]any{"name": name, "path": path, "form": form})
}
