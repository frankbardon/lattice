package layoutagent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	jsc "github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/frankbardon/lattice/dashboard"
)

// Action types the layout coordinator may emit in its plan. These are the
// board-level structural operations — distinct from a brick's own template
// edit, which is delegated to the brick agent.
const (
	// ActionCreateBrick adds a new brick at a position/size and carries a
	// per-brick Prompt the server hands to that brick's own agent to fill in.
	ActionCreateBrick = "create_brick"
	// ActionMoveBrick repositions an existing brick on the grid.
	ActionMoveBrick = "move_brick"
	// ActionResizeBrick changes an existing brick's span.
	ActionResizeBrick = "resize_brick"
	// ActionDeleteBrick removes an existing brick.
	ActionDeleteBrick = "delete_brick"
)

// PlanSchema is the JSON Schema the layout coordinator's structured output must
// satisfy: a {actions:[...]} envelope where each action is one layout
// operation. It is deliberately distinct from brickagent.TemplateSchema — the
// coordinator emits STRUCTURE (what bricks exist + where), never a Pulse/Prism
// template. create_brick carries a "prompt" describing the brick's content,
// which the server delegates to that brick's agent; the coordinator does NOT
// author chart specs.
//
// This schema doubles as the agent's ResponseFormat contract (injected into the
// layout engine config via the nexus.gate.json_schema gate so the LLM is forced
// to emit a conforming object) and is re-validated server-side here before any
// action is applied — defence in depth, and the unit-testable half of the loop
// (no LLM required).
const PlanSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://lattice.local/schemas/layoutagent/plan.json",
  "title": "layout coordinator plan",
  "type": "object",
  "additionalProperties": false,
  "required": ["actions"],
  "properties": {
    "actions": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["type"],
        "properties": {
          "type": {
            "type": "string",
            "enum": ["create_brick", "move_brick", "resize_brick", "delete_brick"]
          },
          "brick_id": { "type": "string" },
          "prompt": { "type": "string" },
          "position": {
            "type": "object",
            "additionalProperties": false,
            "required": ["x", "y"],
            "properties": {
              "x": { "type": "integer", "minimum": 0 },
              "y": { "type": "integer", "minimum": 0 }
            }
          },
          "size": {
            "type": "object",
            "additionalProperties": false,
            "required": ["width", "height"],
            "properties": {
              "width": { "type": "integer", "minimum": 1 },
              "height": { "type": "integer", "minimum": 1 }
            }
          }
        }
      }
    }
  }
}`

// Plan is the decoded layout-actions envelope.
type Plan struct {
	Actions []Action `json:"actions"`
}

// Action is one layout operation in a Plan. Which fields are meaningful depends
// on Type (validated by PlanSchema + validateAction).
type Action struct {
	Type     string              `json:"type"`
	BrickID  string              `json:"brick_id,omitempty"`
	Prompt   string              `json:"prompt,omitempty"`
	Position *dashboard.Position `json:"position,omitempty"`
	Size     *dashboard.Size     `json:"size,omitempty"`
}

// compiledPlanSchema is the parsed PlanSchema, compiled once.
var compiledPlanSchema = mustCompilePlanSchema()

func mustCompilePlanSchema() *jsc.Schema {
	s, err := compileSchema(PlanSchema)
	if err != nil {
		panic(fmt.Sprintf("layoutagent: PlanSchema does not compile: %v", err))
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

// parsePlan extracts the {actions:[...]} JSON object from the agent's raw reply,
// validates it against PlanSchema, decodes it into a Plan, and validates each
// action's per-type invariants (e.g. create_brick needs position+size; the
// others need a brick_id). It returns a coded InvalidOutput error on any miss.
func parsePlan(reply string) (Plan, error) {
	raw, err := extractObject(reply)
	if err != nil {
		return Plan{}, err
	}
	inst, err := jsc.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return Plan{}, wrapError(InvalidOutput, "agent output is not valid JSON", err)
	}
	if err := compiledPlanSchema.Validate(inst); err != nil {
		return Plan{}, wrapError(InvalidOutput, "agent output does not match the layout plan schema", err)
	}
	var plan Plan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return Plan{}, wrapError(InvalidOutput, "decode layout plan", err)
	}
	for i := range plan.Actions {
		if err := validateAction(plan.Actions[i]); err != nil {
			return Plan{}, err
		}
	}
	return plan, nil
}

// validateAction enforces the per-type field invariants the JSON Schema cannot
// express cross-field (e.g. create_brick requires position+size; move/resize/
// delete require a brick_id).
func validateAction(a Action) error {
	switch a.Type {
	case ActionCreateBrick:
		if a.Position == nil {
			return newError(InvalidOutput, "create_brick requires a position")
		}
		if a.Size == nil {
			return newError(InvalidOutput, "create_brick requires a size")
		}
		if strings.TrimSpace(a.Prompt) == "" {
			return newError(InvalidOutput, "create_brick requires a prompt for the brick agent")
		}
	case ActionMoveBrick:
		if a.BrickID == "" {
			return newError(InvalidOutput, "move_brick requires a brick_id")
		}
		if a.Position == nil {
			return newError(InvalidOutput, "move_brick requires a position")
		}
	case ActionResizeBrick:
		if a.BrickID == "" {
			return newError(InvalidOutput, "resize_brick requires a brick_id")
		}
		if a.Size == nil {
			return newError(InvalidOutput, "resize_brick requires a size")
		}
	case ActionDeleteBrick:
		if a.BrickID == "" {
			return newError(InvalidOutput, "delete_brick requires a brick_id")
		}
	default:
		return newError(InvalidOutput, "unknown layout action type: "+a.Type)
	}
	return nil
}

// extractObject pulls the outermost balanced JSON object out of an agent reply.
// The json_schema gate constrains the LLM to emit the object alone, but a model
// may still wrap it in prose or a ```json fence; this finds the object so the
// loop is robust to that. It returns the object bytes or an InvalidOutput error.
func extractObject(reply string) ([]byte, error) {
	reply = strings.TrimSpace(reply)
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
