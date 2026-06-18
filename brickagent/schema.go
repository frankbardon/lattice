package brickagent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	jsc "github.com/santhosh-tekuri/jsonschema/v6"
)

// TemplateSchema is the JSON Schema the brick agent's structured output must
// satisfy: the pulse_prism brick template envelope {pulse_request, prism_spec}.
// It is the SAME contract the render.PulsePrismTemplate decodes — both fields
// are required objects — but it is deliberately permissive about their inner
// shape: the agent authors a PARAMETERIZED template whose values may be ${var}
// placeholder strings (e.g. "limit": "${rows}") that are not yet valid Pulse /
// Prism JSON until the DataResolver substitutes them at render time. Pinning the
// full Pulse/Prism inner schemas here would reject those placeholders, so this
// schema validates the envelope and leaves field-level validation to the
// renderer (which runs after resolution).
//
// This schema doubles as the agent's ResponseFormat contract: it is injected
// into the brick-agent engine config (the nexus.gate.json_schema gate) so the
// LLM is forced to emit a conforming object, and it is re-validated server-side
// here before the template is applied — defence in depth, and the unit-testable
// half of the loop (no LLM required).
const TemplateSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://lattice.local/schemas/brickagent/pulse_prism_template.json",
  "title": "pulse_prism brick template",
  "type": "object",
  "additionalProperties": false,
  "required": ["pulse_request", "prism_spec"],
  "properties": {
    "pulse_request": {
      "type": "object",
      "description": "A declarative Pulse query (pulse.types.Request). Values may be ${var} placeholders resolved server-side before the query runs."
    },
    "prism_spec": {
      "type": "object",
      "description": "A Prism visualization spec (Vega-Lite-style). Its data binding is overwritten with the Pulse response at render time, so any data block is advisory. Values may be ${var} placeholders.",
      "required": ["mark"]
    }
  }
}`

// compiledTemplateSchema is the parsed TemplateSchema, compiled once.
var compiledTemplateSchema = mustCompileTemplateSchema()

func mustCompileTemplateSchema() *jsc.Schema {
	s, err := compileSchema(TemplateSchema)
	if err != nil {
		panic(fmt.Sprintf("brickagent: TemplateSchema does not compile: %v", err))
	}
	return s
}

// compileSchema compiles a JSON Schema document into a validator.
func compileSchema(doc string) (*jsc.Schema, error) {
	res, err := jsc.UnmarshalJSON(strings.NewReader(doc))
	if err != nil {
		return nil, fmt.Errorf("unmarshal schema: %w", err)
	}
	c := jsc.NewCompiler()
	const url = "schema.json"
	if err := c.AddResource(url, res); err != nil {
		return nil, fmt.Errorf("add schema resource: %w", err)
	}
	return c.Compile(url)
}

// validateTemplate checks that raw is a JSON object satisfying TemplateSchema.
// It returns a coded InvalidOutput error (with the validation detail as cause)
// when raw is not valid JSON or does not conform; nil when it conforms.
func validateTemplate(raw []byte) error {
	inst, err := jsc.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return wrapError(InvalidOutput, "agent output is not valid JSON", err)
	}
	if err := compiledTemplateSchema.Validate(inst); err != nil {
		return wrapError(InvalidOutput, "agent output does not match the pulse_prism template schema", err)
	}
	return nil
}

// extractTemplate pulls the {pulse_request, prism_spec} JSON object out of an
// agent's raw assistant reply. The json_schema gate constrains the LLM to emit
// the object alone, but a model may still wrap it in prose or a ```json fence;
// extractTemplate finds the outermost balanced JSON object so the loop is robust
// to that. It returns the object bytes, or an InvalidOutput error when no JSON
// object can be found.
func extractTemplate(reply string) ([]byte, error) {
	reply = strings.TrimSpace(reply)
	// Strip a leading/trailing markdown code fence if present.
	if strings.HasPrefix(reply, "```") {
		if i := strings.IndexByte(reply, '\n'); i >= 0 {
			reply = reply[i+1:]
		}
		reply = strings.TrimSuffix(strings.TrimSpace(reply), "```")
		reply = strings.TrimSpace(reply)
	}
	start := strings.IndexByte(reply, '{')
	if start < 0 {
		return nil, newError(InvalidOutput, "agent output contains no JSON object")
	}
	// Walk to the matching closing brace, respecting strings/escapes so braces
	// inside string literals do not unbalance the scan.
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(reply); i++ {
		c := reply[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return []byte(reply[start : i+1]), nil
			}
		}
	}
	return nil, newError(InvalidOutput, "agent output has an unterminated JSON object")
}

// isParameterized reports whether the template references at least one ${var}
// placeholder. A parameterized template is the whole point of the build loop:
// it lets a later variable change re-render the brick WITHOUT re-invoking the
// agent (the E3-S2 path). The check is advisory — the loop logs a warning rather
// than rejecting a template with no placeholders, since some bricks legitimately
// have nothing to parameterize — but it is exported so the build loop and tests
// can assert the contract.
func isParameterized(template []byte) bool {
	return bytes.Contains(template, []byte("${"))
}

// canonicalize re-marshals a validated template object so the stored template
// string is compact, stable JSON regardless of the agent's whitespace/key order
// quirks. It preserves ${var} placeholders verbatim (they live inside string
// values or as standalone tokens, which json round-trips unchanged when they sit
// in strings; standalone ${var} tokens are kept by the agent inside strings per
// the system prompt, so a round-trip is safe).
func canonicalize(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, wrapError(InvalidOutput, "canonicalize template", err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, wrapError(Internal, "marshal canonical template", err)
	}
	return out, nil
}
