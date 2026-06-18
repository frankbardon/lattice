// Package resolve is lattice's server-side variable resolver. Neither Pulse nor
// Prism substitutes variables, so the server owns interpolation: a brick's
// parameterized template (markdown source, or a Pulse request + Prism spec with
// ${var} placeholders) is turned into a concrete template by substituting the
// dashboard's current variable values here, BEFORE a Renderer parses it.
//
// # The substitution seam (E2-S2)
//
// The pulse_prism renderer documented the seam as "substitute ${var} in the raw
// template JSON before Render parses it". Substitute implements exactly that: it
// operates on the raw template string (which for pulse_prism is JSON), so the
// same code resolves placeholders in both the Pulse request and the Prism spec
// in one pass, and markdown templates resolve the same way. The substituted
// string is then handed to the registry/Renderer unchanged.
//
// # JSON-safety
//
// pulse_prism templates are JSON, so a value spliced in must keep the document
// valid. The substitution is context-aware:
//
//   - A placeholder that is already inside a JSON string literal ("...${var}...")
//     is replaced with the raw value, JSON-string-escaped (quotes, backslashes,
//     control chars), so the surrounding string stays well-formed. This covers
//     ${var} used as part of a path, label, or filter string.
//   - A placeholder that stands alone as a JSON value (e.g. "limit": ${n}) is
//     replaced with a JSON token: a number variable inlines bare (10), and a
//     string/enum value inlines as a quoted, escaped JSON string ("us").
//
// Markdown templates are not JSON; there a placeholder is replaced with the raw
// value verbatim (no quoting), which is what a human-authored document wants.
//
// # Reference tracking
//
// References reports which variables a template mentions (its ${var} tokens), so
// the scene layer can re-render exactly the bricks that reference a changed
// variable and leave the rest untouched. Detection is a pure scan of the
// template string; no document or agent state is consulted.
package resolve

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/frankbardon/lattice/dashboard"
)

// placeholder matches a ${name} token. Names are the variable-name grammar used
// across lattice variables: letters, digits and underscores. A token whose name
// has no matching variable is left intact (see Substitute).
var placeholder = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Values is the resolved current value of each variable, keyed by name. A
// variable resolves to its Value, falling back to its Default when Value is
// empty (an unset variable still renders its default rather than an empty
// placeholder). It also remembers each variable's type so Substitute can decide
// whether a standalone JSON placeholder inlines as a number or a quoted string.
type Values struct {
	vals  map[string]string
	types map[string]string
}

// FromVariables builds the resolved Values from a dashboard's variable
// definitions. Value wins; an empty Value falls back to Default.
func FromVariables(vars []dashboard.Variable) Values {
	v := Values{
		vals:  make(map[string]string, len(vars)),
		types: make(map[string]string, len(vars)),
	}
	for _, def := range vars {
		val := def.Value
		if val == "" {
			val = def.Default
		}
		v.vals[def.Name] = val
		v.types[def.Name] = def.Type
	}
	return v
}

// has reports whether a variable with name is defined.
func (v Values) has(name string) bool {
	_, ok := v.vals[name]
	return ok
}

// References returns the distinct variable names a template mentions via ${var}
// tokens, in first-seen order. It is a pure scan of the template string — the
// basis for the brick→variable index that scopes re-renders. data.ref is not a
// separate placeholder form in v1 (Prism's data.ref binds a Pulse dataset, not a
// dashboard variable); variable references are exclusively ${var} tokens, so
// data.ref support is deferred until a concrete need exists.
func References(template string) []string {
	matches := placeholder.FindAllStringSubmatch(template, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// Substitute replaces every ${var} token in template with its resolved value.
//
// json controls escaping. When json is true (a pulse_prism template, whose raw
// form is JSON) the substitution is context-aware so the result stays valid
// JSON: a token inside a string literal is escaped into that string, and a token
// that stands alone as a JSON value inlines as a JSON token (bare for number
// variables, quoted for everything else). When json is false (markdown) the raw
// value is spliced in verbatim.
//
// A token whose variable is not defined is left intact: leaving "${ghost}" in
// place surfaces the authoring mistake instead of silently corrupting the
// template (and for a not-yet-defined variable, the brick re-renders once the
// variable is added).
func Substitute(template string, vals Values, json bool) string {
	if !strings.Contains(template, "${") {
		return template
	}
	if !json {
		return placeholder.ReplaceAllStringFunc(template, func(tok string) string {
			name := placeholder.FindStringSubmatch(tok)[1]
			if !vals.has(name) {
				return tok
			}
			return vals.vals[name]
		})
	}
	return substituteJSON(template, vals)
}

// substituteJSON performs context-aware substitution over a JSON template. It
// walks the string tracking whether the cursor is inside a JSON string literal
// (respecting backslash escapes) so each placeholder is replaced in the right
// mode: in-string tokens are escaped into the surrounding string; standalone
// tokens are emitted as a JSON value token.
func substituteJSON(template string, vals Values) string {
	var b strings.Builder
	b.Grow(len(template))

	inString := false
	escaped := false
	for i := 0; i < len(template); {
		c := template[i]
		if inString {
			if escaped {
				escaped = false
				b.WriteByte(c)
				i++
				continue
			}
			if c == '\\' {
				escaped = true
				b.WriteByte(c)
				i++
				continue
			}
			if c == '"' {
				inString = false
				b.WriteByte(c)
				i++
				continue
			}
		} else if c == '"' {
			inString = true
			b.WriteByte(c)
			i++
			continue
		}

		if c == '$' && i+1 < len(template) && template[i+1] == '{' {
			if loc := placeholder.FindStringSubmatchIndex(template[i:]); loc != nil && loc[0] == 0 {
				name := template[i+loc[2] : i+loc[3]]
				tok := template[i : i+loc[1]]
				if vals.has(name) {
					b.WriteString(renderToken(vals, name, inString))
				} else {
					b.WriteString(tok)
				}
				i += loc[1]
				continue
			}
		}

		b.WriteByte(c)
		i++
	}
	return b.String()
}

// renderToken renders a resolved variable for JSON output. Inside a string
// literal it emits the value escaped for a JSON string (without surrounding
// quotes, since the quotes are already in the template). Standing alone as a
// value it emits a JSON token: a number variable inlines bare so it parses as a
// JSON number; anything else inlines as a quoted, escaped JSON string.
func renderToken(vals Values, name string, inString bool) string {
	val := vals.vals[name]
	if inString {
		return jsonInnerString(val)
	}
	if vals.types[name] == dashboard.VarNumber && val != "" {
		// A number variable's value parsed at define/set time; inline it bare so
		// it lands as a JSON number (e.g. "limit": 10).
		return val
	}
	return jsonQuotedString(val)
}

// jsonQuotedString returns val as a complete, quoted JSON string token.
func jsonQuotedString(val string) string {
	b, err := json.Marshal(val)
	if err != nil {
		// json.Marshal of a string never fails; fall back to an empty JSON string.
		return `""`
	}
	return string(b)
}

// jsonInnerString returns val escaped for use INSIDE an existing JSON string
// literal: it marshals to a quoted token, then strips the surrounding quotes.
func jsonInnerString(val string) string {
	q := jsonQuotedString(val)
	return q[1 : len(q)-1]
}
