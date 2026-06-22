package resolver

import (
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/schema"
	"github.com/frankbardon/lattice/internal/variables"
)

// surfaceTypeID is the synthetic item type the surface tests build graphs around.
const surfaceTypeID = "https://lattice.dev/schemas/items/gauge/1.0.0"

// surfaceProps is the config-property set the synthetic item type declares. The
// "unknown field" check validates configurable-surface field names against this.
var surfaceProps = map[string]*jsonschema.Schema{
	"min":   {Type: "number"},
	"max":   {Type: "number"},
	"label": {Type: "string"},
}

// TestResolveSurfaceValid exercises a well-formed configurable surface: every
// declared field is a real config property, each carries a known value type, and
// the rendering hints name catalogued widgets. It asserts resolveSurface returns
// the fields (in declared/sorted order) with their type, label, constraints, and
// rendering preserved — the surface a configurator (E5) reads.
func TestResolveSurfaceValid(t *testing.T) {
	decl := map[string]any{
		"min": map[string]any{
			"type":        "number",
			"label":       "Minimum",
			"constraints": map[string]any{"minimum": float64(0)},
			"rendering":   "slider",
		},
		"label": map[string]any{
			"type":  "string",
			"label": "Caption",
			// no rendering hint — optional
		},
	}
	g := newSurfaceGraph(t, decl)
	inst := &ResolvedInstance{Type: ResolvedTypeRef{ID: surfaceTypeID, Name: "gauge"}}

	got, err := resolveSurface(g, inst, surfaceTestWidgets, "root")
	if err != nil {
		t.Fatalf("resolveSurface: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(surface) = %d, want 2", len(got))
	}

	// Fields come back in sorted order: "label" before "min".
	if got[0].Field != "label" || got[1].Field != "min" {
		t.Fatalf("field order = [%q %q], want [label min]", got[0].Field, got[1].Field)
	}

	label := got[0]
	if label.Type != variables.VarTypeString {
		t.Errorf("label.Type = %q, want string", label.Type)
	}
	if label.Label != "Caption" {
		t.Errorf("label.Label = %q, want Caption", label.Label)
	}
	if label.Rendering != "" {
		t.Errorf("label.Rendering = %q, want empty", label.Rendering)
	}

	min := got[1]
	if min.Type != variables.VarTypeNumber {
		t.Errorf("min.Type = %q, want number", min.Type)
	}
	if min.Label != "Minimum" {
		t.Errorf("min.Label = %q, want Minimum", min.Label)
	}
	if min.Rendering != "slider" {
		t.Errorf("min.Rendering = %q, want slider", min.Rendering)
	}
	if min.Constraints == nil {
		t.Errorf("min.Constraints is nil, want the declared constraints")
	} else if _, ok := min.Constraints["minimum"]; !ok {
		t.Errorf("min.Constraints missing key %q", "minimum")
	}
}

// TestResolveSurfaceAttached asserts the pass EXPOSES the validated surface on
// the resolved instance (and recurses into children) — the contract E4/E5 read.
func TestResolveSurfaceAttached(t *testing.T) {
	decl := map[string]any{
		"min": map[string]any{"type": "number", "label": "Minimum"},
	}
	g := newSurfaceGraph(t, decl)
	root := &ResolvedInstance{
		Type: ResolvedTypeRef{ID: surfaceTypeID, Name: "gauge"},
		Children: []*ResolvedInstance{
			{Type: ResolvedTypeRef{ID: surfaceTypeID, Name: "gauge"}},
		},
	}

	if err := resolveSurfaces(g, root, surfaceTestWidgets); err != nil {
		t.Fatalf("resolveSurfaces: %v", err)
	}
	if len(root.Surface) != 1 || root.Surface[0].Field != "min" {
		t.Fatalf("root.Surface = %+v, want one field \"min\"", root.Surface)
	}
	if len(root.Children[0].Surface) != 1 {
		t.Fatalf("child.Surface = %+v, want the surface attached on recursion", root.Children[0].Surface)
	}
}

// TestResolveSurfaceNoDeclaration asserts an item type that declares no
// `configurable` keyword yields no surface (and no error) — the surface is opt-in.
func TestResolveSurfaceNoDeclaration(t *testing.T) {
	g := newSurfaceGraph(t, nil) // nil decl => no `configurable` keyword
	inst := &ResolvedInstance{Type: ResolvedTypeRef{ID: surfaceTypeID, Name: "gauge"}}

	got, err := resolveSurface(g, inst, surfaceTestWidgets, "root")
	if err != nil {
		t.Fatalf("resolveSurface: %v", err)
	}
	if got != nil {
		t.Fatalf("surface = %+v, want nil for a type declaring no configurable surface", got)
	}
}

// TestResolveSurfaceInvalid covers each malformed declaration: an unknown config
// field, a bad value type, and a rendering hint naming a non-existent widget. All
// must fail fast with CONFIGURABLE_SURFACE_INVALID, naming the offending path,
// type, and field.
func TestResolveSurfaceInvalid(t *testing.T) {
	tests := []struct {
		name      string
		decl      map[string]any
		wantField string
	}{
		{
			name: "unknown config field",
			decl: map[string]any{
				// "ceiling" is not a declared config property of the item type.
				"ceiling": map[string]any{"type": "number", "label": "Ceiling"},
			},
			wantField: "ceiling",
		},
		{
			name: "bad value type",
			decl: map[string]any{
				"min": map[string]any{"type": "decimal", "label": "Minimum"},
			},
			wantField: "min",
		},
		{
			name: "rendering hint references non-existent widget",
			decl: map[string]any{
				"min": map[string]any{"type": "number", "label": "Minimum", "rendering": "dial"},
			},
			wantField: "min",
		},
		{
			name: "surface entry is not an object",
			decl: map[string]any{
				"min": "number",
			},
			wantField: "min",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := newSurfaceGraph(t, tc.decl)
			inst := &ResolvedInstance{Type: ResolvedTypeRef{ID: surfaceTypeID, Name: "gauge"}}

			_, err := resolveSurface(g, inst, surfaceTestWidgets, "root.children[0]")
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.HasCode(err, errors.CONFIGURABLE_SURFACE_INVALID) {
				t.Fatalf("error = %v, want code %s", err, errors.CONFIGURABLE_SURFACE_INVALID)
			}
			var ce *errors.CodedError
			if !asCoded(err, &ce) {
				t.Fatalf("error is not a CodedError: %v", err)
			}
			if got, _ := ce.Details["path"].(string); got != "root.children[0]" {
				t.Errorf("error path = %q, want %q", got, "root.children[0]")
			}
			if got, _ := ce.Details["field"].(string); got != tc.wantField {
				t.Errorf("error field = %q, want %q", got, tc.wantField)
			}
		})
	}
}

// surfaceTestWidgets is the widget-name set the surface tests validate `rendering`
// hints against — the real catalogued widget names a configurable surface may name
// (post-E2-S1, sourced from Catalog.WidgetNames in production). A hint outside this
// set (e.g. "dial") is rejected CONFIGURABLE_SURFACE_INVALID.
var surfaceTestWidgets = map[string]bool{
	"text-input":     true,
	"textarea":       true,
	"number-field":   true,
	"slider":         true,
	"stepper":        true,
	"toggle":         true,
	"checkbox":       true,
	"select":         true,
	"radio-group":    true,
	"segmented":      true,
	"multiselect":    true,
	"checkbox-group": true,
	"tag-input":      true,
}

// newSurfaceGraph builds a minimal ResolvedGraph whose single item type declares
// the config properties in surfaceProps and carries the given `configurable`
// declaration in its schema Extra (or none when decl is nil), mirroring how
// google/jsonschema-go surfaces a schema-level keyword.
func newSurfaceGraph(t *testing.T, decl map[string]any) *schema.ResolvedGraph {
	t.Helper()
	sch := &jsonschema.Schema{ID: surfaceTypeID, Type: "object", Properties: surfaceProps}
	if decl != nil {
		sch.Extra = map[string]any{configurableKey: decl}
	}
	return &schema.ResolvedGraph{
		Types: map[string]*schema.ResolvedType{
			surfaceTypeID: {ID: surfaceTypeID, Name: "gauge", Schema: sch},
		},
	}
}
