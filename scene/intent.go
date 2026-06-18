package scene

import (
	"encoding/json"
	"strconv"

	jsonpatch "github.com/evanphx/json-patch/v5"

	"github.com/frankbardon/lattice/dashboard"
)

// IntentType discriminates the client→server intent. Intents are the only
// thing clients send; the server is authoritative and converts each into an
// RFC6902 patch applied to the in-memory document.
type IntentType string

const (
	// IntentAddBrick appends a new brick to the board.
	IntentAddBrick IntentType = "add_brick"
	// IntentMoveBrick changes a brick's grid position.
	IntentMoveBrick IntentType = "move_brick"
	// IntentResizeBrick changes a brick's grid span.
	IntentResizeBrick IntentType = "resize_brick"
	// IntentDeleteBrick removes a brick from the board.
	IntentDeleteBrick IntentType = "delete_brick"
	// IntentEditTemplate replaces a brick's parameterized template payload.
	IntentEditTemplate IntentType = "edit_template"
	// IntentSetVariable sets (or adds) a board-level variable.
	IntentSetVariable IntentType = "set_variable"
	// IntentRearrange replaces the whole brick ordering with a new sequence of
	// brick ids (a reorder of the existing set).
	IntentRearrange IntentType = "rearrange"
)

// Intent is the wire form of a client request to mutate the board. The Type
// selects which fields are meaningful; unused fields are ignored. Intents are
// transport-agnostic: the realtime layer decodes one from an inbound RPC and
// hands it to Doc.Apply.
type Intent struct {
	Type IntentType `json:"type"`

	// BrickID targets an existing brick for move/resize/delete/edit. For
	// add_brick it is the id of the new brick.
	BrickID string `json:"brick_id,omitempty"`

	// Brick is the full brick to add (add_brick only).
	Brick *dashboard.Brick `json:"brick,omitempty"`

	// Pos is the new position (move_brick only).
	Pos *dashboard.Position `json:"pos,omitempty"`

	// Size is the new span (resize_brick only).
	Size *dashboard.Size `json:"size,omitempty"`

	// Template is the new template payload (edit_template only).
	Template string `json:"template,omitempty"`

	// Name and Value carry a board variable (set_variable only).
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`

	// Order is the desired brick id sequence (rearrange only). It must be a
	// permutation of the current brick ids.
	Order []string `json:"order,omitempty"`
}

// DecodeIntent parses an intent from its JSON wire form.
func DecodeIntent(raw []byte) (Intent, error) {
	var in Intent
	if err := json.Unmarshal(raw, &in); err != nil {
		return Intent{}, wrapError(InvalidIntent, "decode intent", err)
	}
	return in, nil
}

// patchFor resolves an intent against the current document into the RFC6902
// patch that realizes it. It validates intent-level invariants (target brick
// exists, ids are well-formed) and returns InvalidIntent on a miss; the patch
// itself is validated at apply time. doc is read-only here.
func patchFor(doc *dashboard.Dashboard, in Intent) (jsonpatch.Patch, error) {
	switch in.Type {
	case IntentAddBrick:
		if in.Brick == nil {
			return nil, newError(InvalidIntent, "add_brick requires a brick")
		}
		if in.Brick.ID == "" {
			return nil, newError(InvalidIntent, "add_brick requires a brick id")
		}
		if indexOfBrick(doc, in.Brick.ID) >= 0 {
			return nil, newError(InvalidIntent, "add_brick: brick id already exists")
		}
		return buildPatch(op{Op: "add", Path: "/bricks/-", Value: in.Brick})

	case IntentMoveBrick:
		i, err := requireBrick(doc, in.BrickID)
		if err != nil {
			return nil, err
		}
		if in.Pos == nil {
			return nil, newError(InvalidIntent, "move_brick requires pos")
		}
		return buildPatch(op{Op: "replace", Path: brickPath(i) + "/layout/pos", Value: in.Pos})

	case IntentResizeBrick:
		i, err := requireBrick(doc, in.BrickID)
		if err != nil {
			return nil, err
		}
		if in.Size == nil {
			return nil, newError(InvalidIntent, "resize_brick requires size")
		}
		return buildPatch(op{Op: "replace", Path: brickPath(i) + "/layout/size", Value: in.Size})

	case IntentDeleteBrick:
		i, err := requireBrick(doc, in.BrickID)
		if err != nil {
			return nil, err
		}
		return buildPatch(op{Op: "remove", Path: brickPath(i)})

	case IntentEditTemplate:
		i, err := requireBrick(doc, in.BrickID)
		if err != nil {
			return nil, err
		}
		return buildPatch(op{Op: "replace", Path: brickPath(i) + "/template", Value: in.Template})

	case IntentSetVariable:
		if in.Name == "" {
			return nil, newError(InvalidIntent, "set_variable requires a name")
		}
		v := dashboard.Variable{Name: in.Name, Value: in.Value}
		if i := indexOfVariable(doc, in.Name); i >= 0 {
			return buildPatch(op{Op: "replace", Path: "/variables/" + strconv.Itoa(i), Value: v})
		}
		return buildPatch(op{Op: "add", Path: "/variables/-", Value: v})

	case IntentRearrange:
		ordered, err := reorderBricks(doc, in.Order)
		if err != nil {
			return nil, err
		}
		return buildPatch(op{Op: "replace", Path: "/bricks", Value: ordered})

	default:
		return nil, newError(InvalidIntent, "unknown intent type "+string(in.Type))
	}
}

// op is an internal RFC6902 operation used to build a patch before encoding it
// for the json-patch engine.
type op struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

// buildPatch encodes operations into a decoded jsonpatch.Patch.
func buildPatch(ops ...op) (jsonpatch.Patch, error) {
	raw, err := json.Marshal(ops)
	if err != nil {
		return nil, wrapError(Internal, "encode patch", err)
	}
	p, err := jsonpatch.DecodePatch(raw)
	if err != nil {
		return nil, wrapError(Internal, "decode patch", err)
	}
	return p, nil
}

// brickPath returns the JSON pointer to the brick at index i.
func brickPath(i int) string { return "/bricks/" + strconv.Itoa(i) }

// requireBrick returns the index of the brick with id, or InvalidIntent.
func requireBrick(doc *dashboard.Dashboard, id string) (int, error) {
	if id == "" {
		return 0, newError(InvalidIntent, "brick id required")
	}
	i := indexOfBrick(doc, id)
	if i < 0 {
		return 0, newError(InvalidIntent, "brick not found: "+id)
	}
	return i, nil
}

func indexOfBrick(doc *dashboard.Dashboard, id string) int {
	for i := range doc.Bricks {
		if doc.Bricks[i].ID == id {
			return i
		}
	}
	return -1
}

func indexOfVariable(doc *dashboard.Dashboard, name string) int {
	for i := range doc.Variables {
		if doc.Variables[i].Name == name {
			return i
		}
	}
	return -1
}

// reorderBricks returns the bricks reordered to match order, which must be a
// permutation of the current brick ids (same set, no dupes, no extras).
func reorderBricks(doc *dashboard.Dashboard, order []string) ([]dashboard.Brick, error) {
	if len(order) != len(doc.Bricks) {
		return nil, newError(InvalidIntent, "rearrange order must list every brick exactly once")
	}
	byID := make(map[string]dashboard.Brick, len(doc.Bricks))
	for _, b := range doc.Bricks {
		byID[b.ID] = b
	}
	out := make([]dashboard.Brick, 0, len(order))
	seen := make(map[string]bool, len(order))
	for _, id := range order {
		if seen[id] {
			return nil, newError(InvalidIntent, "rearrange order has a duplicate id: "+id)
		}
		b, ok := byID[id]
		if !ok {
			return nil, newError(InvalidIntent, "rearrange order references unknown brick: "+id)
		}
		seen[id] = true
		out = append(out, b)
	}
	return out, nil
}
