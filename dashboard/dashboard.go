// Package dashboard defines the lattice document model: a Dashboard is a
// masonry board of Bricks plus the Variables that parameterize them. The model
// is serialized verbatim (JSON) by the store layer, so its JSON shape is the
// persisted document format.
package dashboard

import (
	"fmt"
	"strconv"
)

// Dashboard is the top-level document: a named board of bricks plus the
// variables that parameterize their templates.
type Dashboard struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Variables []Variable `json:"variables"`
	Bricks    []Brick    `json:"bricks"`
}

// Variable is a named board-level parameter. Brick templates reference it via
// ${name} placeholders, resolved server-side at render time (E3-S2; this story
// only stores/edits them).
//
// Type breadth (v1, intentionally small): "string" (free text), "number" (must
// parse as a Go float64) and "enum" (Value must be one of Options). Options is
// only meaningful for the enum type; it is the enum's allowed-values list. New
// types (bool, date, color, …) are a FOLLOWUP. Default seeds a freshly-defined
// variable's Value; both are stored as strings and coerced at render time.
type Variable struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Default string   `json:"default"`
	Value   string   `json:"value"`
	Options []string `json:"options,omitempty"`
}

// Variable type tags. The set is deliberately small for v1 (see Variable).
const (
	VarString = "string"
	VarNumber = "number"
	VarEnum   = "enum"
)

// ValidateVariable checks a variable's type-level invariants with minimal
// strictness: the type must be one of the known tags; a number's Value/Default
// (when non-empty) must parse as a float64; an enum needs at least one option
// and its Value/Default (when non-empty) must be one of those options. An empty
// Value/Default is always allowed (an unset variable). It returns a plain error
// describing the first violation, or nil when the variable is well-formed.
func ValidateVariable(v Variable) error {
	switch v.Type {
	case VarString:
		// Any string value is acceptable.
	case VarNumber:
		for _, s := range []string{v.Default, v.Value} {
			if s == "" {
				continue
			}
			if _, err := strconv.ParseFloat(s, 64); err != nil {
				return fmt.Errorf("number variable %q has non-numeric value %q", v.Name, s)
			}
		}
	case VarEnum:
		if len(v.Options) == 0 {
			return fmt.Errorf("enum variable %q requires at least one option", v.Name)
		}
		for _, s := range []string{v.Default, v.Value} {
			if s == "" {
				continue
			}
			if !contains(v.Options, s) {
				return fmt.Errorf("enum variable %q value %q is not one of its options", v.Name, s)
			}
		}
	default:
		return fmt.Errorf("variable %q has unknown type %q", v.Name, v.Type)
	}
	return nil
}

func contains(opts []string, s string) bool {
	for _, o := range opts {
		if o == s {
			return true
		}
	}
	return false
}

// Brick is a single tile on the board. Kind selects the renderer; Template is
// the parameterized payload (e.g. markdown source, or a Prism spec + Pulse
// request with ${var} placeholders) emitted by the brick's agent.
type Brick struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Layout   Layout `json:"layout"`
	Template string `json:"template"`
	AgentID  string `json:"agent_id"`
}

// Layout is a brick's position and size on the masonry grid.
type Layout struct {
	Pos  Position `json:"pos"`
	Size Size     `json:"size"`
}

// Position is the top-left grid coordinate of a brick.
type Position struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Size is the grid span of a brick.
type Size struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}
