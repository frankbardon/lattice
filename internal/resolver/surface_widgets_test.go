package resolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/frankbardon/lattice/internal/variables"
)

// This file asserts the CONCRETE configurable surfaces declared by the variable
// widgets and the `form` container (E3-S3). Unlike surface_test.go — which
// exercises the surface MECHANISM against a synthetic item type — these tests
// resolve a real document through the repo's on-disk schema catalog (the exact
// schemas the binary ships) and assert the surface the resolver exposes on the
// resolved instance, so a drift between a schema's `configurable` block and the
// contract downstream epics (E4 overrides, E5 configurator) read is caught.
//
// The widget catalog is large; rather than assert every one of the 13 widgets
// individually, these tests pin one representative of each surface SHAPE — a
// string widget (placeholder), a number widget (min/max/step), an enum widget
// (sort), a boolean widget (label/disabled only) — plus the form container.
// surface.go validates EVERY node's declaration fail-fast on resolve, so a
// malformed surface on any of the other widgets would fail resolution here too.

// widgetSurfaceDoc is a dashboard whose root form holds one widget of each
// surface shape, each binding a typed variable so the document resolves cleanly.
const widgetSurfaceDoc = `{
  "manifest": { "formatVersion": "1.0.0", "id": "widget-surface", "title": "Widget Surface Example" },
  "variables": [
    { "name": "name", "type": "string", "default": "" },
    { "name": "count", "type": "number", "default": 1 },
    { "name": "region", "type": "enum", "default": "us", "options": ["us", "eu"] },
    { "name": "live", "type": "boolean", "default": true }
  ],
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": { "grid": { "columns": [1] } },
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
        "id": "body",
        "config": { "grid": { "columns": [1] } },
        "children": [
          {
            "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
            "config": {
              "id": "form-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/form/1.0.0",
                "id": "form",
                "config": { "layout": { "mode": "flow", "columns": 1 } },
                "children": [
                  {
                    "$ref": "https://lattice.dev/schemas/items/text-input/1.0.0",
                    "id": "name-input",
                    "config": { "variable": "name", "label": "Name", "placeholder": "type a name" }
                  },
                  {
                    "$ref": "https://lattice.dev/schemas/items/number-field/1.0.0",
                    "id": "count-field",
                    "config": { "variable": "count", "label": "Count", "min": 0, "max": 10, "step": 1 }
                  },
                  {
                    "$ref": "https://lattice.dev/schemas/items/select/1.0.0",
                    "id": "region-select",
                    "config": {
                      "variable": "region", "label": "Region",
                      "options": [{ "value": "us", "label": "US" }, { "value": "eu", "label": "EU" }]
                    }
                  },
                  {
                    "$ref": "https://lattice.dev/schemas/items/toggle/1.0.0",
                    "id": "live-toggle",
                    "config": { "variable": "live", "label": "Live" }
                  }
                ]
              }
            }
          }
        ]
      }
    ]
  }
}`

// resolveWidgetSurfaceDoc resolves widgetSurfaceDoc against the real schema
// catalog and returns the resolved root form plus its four widget children keyed
// by item-type name.
func resolveWidgetSurfaceDoc(t *testing.T) (form *ResolvedInstance, byType map[string]*ResolvedInstance) {
	t.Helper()
	res := newRepoResolver(t)

	path := filepath.Join(t.TempDir(), "widget-surface-dashboard.json")
	if err := os.WriteFile(path, []byte(widgetSurfaceDoc), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	tree, err := res.Resolve(path)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// Under the E3-S2 grammar the form is block-wrapped inside a body region:
	// root -> body region -> block -> form.
	form = tree.Root.Children[0].Children[0].Children[0]
	if form.Type.Name != "form" {
		t.Fatalf("inner content type = %q, want form", form.Type.Name)
	}
	byType = map[string]*ResolvedInstance{}
	for _, child := range form.Children {
		byType[child.Type.Name] = child
	}
	return form, byType
}

// TestFormSurface asserts the form container exposes its layout as a
// runtime-configurable field with the expected type, label, and nested-field
// constraints (mirroring the container `grid` surface convention).
func TestFormSurface(t *testing.T) {
	form, _ := resolveWidgetSurfaceDoc(t)

	if len(form.Surface) != 1 {
		t.Fatalf("form.Surface has %d fields, want 1 (layout)", len(form.Surface))
	}
	layout := fieldByName(t, form.Surface, "layout")
	if layout.Type != variables.VarTypeArray {
		t.Errorf("layout.Type = %q, want %q", layout.Type, variables.VarTypeArray)
	}
	if layout.Label == "" {
		t.Errorf("layout.Label is empty, want a human label")
	}
	if layout.Constraints == nil {
		t.Errorf("layout.Constraints is nil, want the declared layout-field constraints")
	} else if _, ok := layout.Constraints["fields"]; !ok {
		t.Errorf("layout.Constraints missing %q describing the mode/columns/rows/gap sub-fields", "fields")
	}
}

// TestWidgetSurfacesShared asserts every widget surfaces the two fields every
// family shares — `label` (string, text-input hint) and `disabled` (boolean,
// toggle hint).
func TestWidgetSurfacesShared(t *testing.T) {
	_, byType := resolveWidgetSurfaceDoc(t)

	for _, name := range []string{"text-input", "number-field", "select", "toggle"} {
		w, ok := byType[name]
		if !ok {
			t.Fatalf("resolved tree missing %q widget", name)
		}

		label := fieldByName(t, w.Surface, "label")
		if label.Type != variables.VarTypeString {
			t.Errorf("%s label.Type = %q, want %q", name, label.Type, variables.VarTypeString)
		}
		if label.Rendering != "text-input" {
			t.Errorf("%s label.Rendering = %q, want text-input", name, label.Rendering)
		}

		disabled := fieldByName(t, w.Surface, "disabled")
		if disabled.Type != variables.VarTypeBoolean {
			t.Errorf("%s disabled.Type = %q, want %q", name, disabled.Type, variables.VarTypeBoolean)
		}
		if disabled.Rendering != "toggle" {
			t.Errorf("%s disabled.Rendering = %q, want toggle", name, disabled.Rendering)
		}
	}
}

// TestStringWidgetSurface asserts the string family surfaces its placeholder.
func TestStringWidgetSurface(t *testing.T) {
	_, byType := resolveWidgetSurfaceDoc(t)

	placeholder := fieldByName(t, byType["text-input"].Surface, "placeholder")
	if placeholder.Type != variables.VarTypeString {
		t.Errorf("placeholder.Type = %q, want %q", placeholder.Type, variables.VarTypeString)
	}
	if placeholder.Rendering != "text-input" {
		t.Errorf("placeholder.Rendering = %q, want text-input", placeholder.Rendering)
	}
}

// TestNumberWidgetSurface asserts the number family surfaces its min/max/step
// bounds, each a number field.
func TestNumberWidgetSurface(t *testing.T) {
	_, byType := resolveWidgetSurfaceDoc(t)

	surface := byType["number-field"].Surface
	for _, field := range []string{"min", "max", "step"} {
		f := fieldByName(t, surface, field)
		if f.Type != variables.VarTypeNumber {
			t.Errorf("%s.Type = %q, want %q", field, f.Type, variables.VarTypeNumber)
		}
		if f.Rendering != "number-field" {
			t.Errorf("%s.Rendering = %q, want number-field", field, f.Rendering)
		}
	}
}

// TestEnumWidgetSurface asserts the enum family surfaces its option-sort control
// as an enum-typed field carrying the declared sort options.
func TestEnumWidgetSurface(t *testing.T) {
	_, byType := resolveWidgetSurfaceDoc(t)

	sort := fieldByName(t, byType["select"].Surface, "sort")
	if sort.Type != variables.VarTypeEnum {
		t.Errorf("sort.Type = %q, want %q", sort.Type, variables.VarTypeEnum)
	}
	if sort.Constraints == nil {
		t.Errorf("sort.Constraints is nil, want the declared sort enum")
	} else if _, ok := sort.Constraints["enum"]; !ok {
		t.Errorf("sort.Constraints missing %q listing the sort options", "enum")
	}
}

// TestBooleanWidgetSurface asserts a boolean widget surfaces ONLY the shared
// label/disabled fields — no spurious extra field.
func TestBooleanWidgetSurface(t *testing.T) {
	_, byType := resolveWidgetSurfaceDoc(t)

	toggle := byType["toggle"].Surface
	if len(toggle) != 2 {
		t.Fatalf("toggle.Surface has %d fields, want 2 (disabled, label)", len(toggle))
	}
	// Surface comes back in sorted field order.
	wantOrder := []string{"disabled", "label"}
	for i, want := range wantOrder {
		if got := toggle[i].Field; got != want {
			t.Errorf("toggle.Surface[%d].Field = %q, want %q", i, got, want)
		}
	}
}
