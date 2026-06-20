package resolver

import (
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/schema"
	"github.com/frankbardon/lattice/internal/variables"
)

// nestedSurfaceTypeID is the synthetic item type the nested-surface tests build
// graphs around. It declares a nested `grid` config object so a `configurable`
// declaration can name explicit sub-paths (e.g. "grid.gap") into it (E2-S1).
const nestedSurfaceTypeID = "https://lattice.dev/schemas/items/panel/1.0.0"

// nestedSurfaceProps is the config-property set of the synthetic nested item
// type: a top-level scalar plus a `grid` OBJECT whose own properties are the
// legal nested sub-paths a surface may declare ("grid.gap", "grid.columns").
var nestedSurfaceProps = map[string]*jsonschema.Schema{
	"label": {Type: "string"},
	"grid": {
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"gap":     {Type: "number"},
			"columns": {Type: "array"},
		},
	},
}

// TestResolveSurfaceNestedValid asserts a `configurable` declaration that names a
// real NESTED sub-path (E2-S1) is accepted and its resolved entry carries the
// dotted Field plus the parsed Path segments a guardrail (E2-S2) looks it up by.
func TestResolveSurfaceNestedValid(t *testing.T) {
	decl := map[string]any{
		"grid.gap": map[string]any{
			"type":      "number",
			"label":     "Track gap",
			"rendering": "number-field",
		},
		"label": map[string]any{
			"type":  "string",
			"label": "Label",
		},
	}
	g := newNestedSurfaceGraph(t, decl)
	inst := &ResolvedInstance{Type: ResolvedTypeRef{ID: nestedSurfaceTypeID, Name: "panel"}}

	got, err := resolveSurface(g, inst, "root")
	if err != nil {
		t.Fatalf("resolveSurface: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(surface) = %d, want 2", len(got))
	}

	// Sorted order: "grid.gap" before "label".
	nested := got[0]
	if nested.Field != "grid.gap" {
		t.Fatalf("nested.Field = %q, want %q", nested.Field, "grid.gap")
	}
	if want := []string{"grid", "gap"}; !equalStrings(nested.Path, want) {
		t.Errorf("nested.Path = %v, want %v", nested.Path, want)
	}
	if nested.Type != variables.VarTypeNumber {
		t.Errorf("nested.Type = %q, want number", nested.Type)
	}
	if nested.Rendering != "number-field" {
		t.Errorf("nested.Rendering = %q, want number-field", nested.Rendering)
	}

	// A top-level field still leaves Path nil (its whole address is Field).
	top := got[1]
	if top.Field != "label" {
		t.Fatalf("top.Field = %q, want %q", top.Field, "label")
	}
	if top.Path != nil {
		t.Errorf("top.Path = %v, want nil for a top-level entry", top.Path)
	}
}

// TestResolveSurfaceNestedInvalid covers the two nested-path rejections: a path
// whose intermediate/leaf segment is not a real schema property, and a path that
// exists but whose declared leaf `type` is not a known variable type. Both must
// fail fast with CONFIGURABLE_SURFACE_INVALID naming the offending dotted field.
func TestResolveSurfaceNestedInvalid(t *testing.T) {
	tests := []struct {
		name      string
		decl      map[string]any
		wantField string
	}{
		{
			name: "nonexistent nested path",
			decl: map[string]any{
				// "grid.missing" is not a property of the nested grid object.
				"grid.missing": map[string]any{"type": "number", "label": "Missing"},
			},
			wantField: "grid.missing",
		},
		{
			name: "nonexistent intermediate segment",
			decl: map[string]any{
				// "layout" is not a top-level property at all.
				"layout.gap": map[string]any{"type": "number", "label": "Gap"},
			},
			wantField: "layout.gap",
		},
		{
			name: "wrong leaf type",
			decl: map[string]any{
				// The path exists but "decimal" is not a known variable type.
				"grid.gap": map[string]any{"type": "decimal", "label": "Gap"},
			},
			wantField: "grid.gap",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := newNestedSurfaceGraph(t, tc.decl)
			inst := &ResolvedInstance{Type: ResolvedTypeRef{ID: nestedSurfaceTypeID, Name: "panel"}}

			_, err := resolveSurface(g, inst, "root.children[0]")
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
			if got, _ := ce.Details["field"].(string); got != tc.wantField {
				t.Errorf("error field = %q, want %q", got, tc.wantField)
			}
		})
	}
}

// newNestedSurfaceGraph builds a minimal ResolvedGraph whose single item type
// declares the nested config shape in nestedSurfaceProps and carries the given
// `configurable` declaration in its schema Extra.
func newNestedSurfaceGraph(t *testing.T, decl map[string]any) *schema.ResolvedGraph {
	t.Helper()
	sch := &jsonschema.Schema{ID: nestedSurfaceTypeID, Type: "object", Properties: nestedSurfaceProps}
	if decl != nil {
		sch.Extra = map[string]any{configurableKey: decl}
	}
	return &schema.ResolvedGraph{
		Types: map[string]*schema.ResolvedType{
			nestedSurfaceTypeID: {ID: nestedSurfaceTypeID, Name: "panel", Schema: sch},
		},
	}
}
