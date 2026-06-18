// Package dashboard defines the lattice document model: a Dashboard is a
// masonry board of Bricks plus the Variables that parameterize them. The model
// is serialized verbatim (JSON) by the store layer, so its JSON shape is the
// persisted document format.
package dashboard

// Dashboard is the top-level document: a named board of bricks plus the
// variables that parameterize their templates.
type Dashboard struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Variables []Variable `json:"variables"`
	Bricks    []Brick    `json:"bricks"`
}

// Variable is a named board-level parameter. Brick templates reference it via
// ${name} placeholders, resolved server-side at render time.
type Variable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
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
