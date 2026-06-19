package resolver

import (
	"fmt"
	"testing"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/layout"
)

// formDoc builds a minimal dashboard whose root is a `form` (with the supplied
// layout config object, e.g. `{"columns": 2}` or empty `{}`) holding the given
// child instances. Variables it needs are declared at document scope so the
// widget children bind cleanly. childrenJSON is spliced verbatim as the form's
// children array.
func formDoc(layoutJSON, varsJSON, childrenJSON string) string {
	cfg := ""
	if layoutJSON != "" {
		cfg = fmt.Sprintf(`, "config": {"layout": %s}`, layoutJSON)
	}
	return fmt.Sprintf(`{
  "manifest": {"formatVersion": "1.0.0", "id": "fdoc", "title": "Form Doc"},
  "variables": [%s],
  "root": {
    "$ref": "https://lattice.dev/schemas/items/form/1.0.0"%s,
    "children": [%s]
  }
}`, varsJSON, cfg, childrenJSON)
}

// widgetChild is a small helper building a widget child instance JSON of the
// given type bound to the given variable.
func widgetChild(widgetType, variable string) string {
	return fmt.Sprintf(`{"$ref": "https://lattice.dev/schemas/items/%s/1.0.0", "config": {"variable": %q}}`, widgetType, variable)
}

// TestResolveFormFlow drives a `form` through the real pipeline: several widget
// children resolve, the form carries a normalized flow layout (no weighted-grid
// Block), and the widgets carry NO placement (they do not consume a main-grid
// cell). Column splitting is exercised end to end.
func TestResolveFormFlow(t *testing.T) {
	tests := []struct {
		name        string
		layout      string
		wantColumns int
		wantCells   []layout.FlowCell
	}{
		{
			name:        "default single column stacks widgets",
			layout:      "",
			wantColumns: 1,
			wantCells: []layout.FlowCell{
				{Column: 1, Row: 1},
				{Column: 1, Row: 2},
				{Column: 1, Row: 3},
			},
		},
		{
			name:        "two-column flow wraps row-major",
			layout:      `{"mode": "flow", "columns": 2}`,
			wantColumns: 2,
			wantCells: []layout.FlowCell{
				{Column: 1, Row: 1},
				{Column: 2, Row: 1},
				{Column: 1, Row: 2}, // third widget wraps
			},
		},
	}

	const vars = `{"name": "region", "type": "string", "default": "us"},
		{"name": "active", "type": "boolean", "default": true},
		{"name": "notes", "type": "string", "default": ""}`
	children := widgetChild("text-input", "region") + "," +
		widgetChild("toggle", "active") + "," +
		widgetChild("textarea", "notes")

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := newRepoResolver(t)
			doc := formDoc(tc.layout, vars, children)
			tree, err := res.resolveBytes([]byte(doc), tc.name, nil)
			if err != nil {
				t.Fatalf("resolveBytes: unexpected error: %v", err)
			}

			form := tree.Root
			if form.Flow == nil {
				t.Fatalf("form has no flow layout")
			}
			if form.Layout != nil {
				t.Errorf("form unexpectedly carries a weighted-grid Block: %+v", form.Layout)
			}
			if form.Container {
				t.Errorf("form should not report Container=true")
			}
			if form.Flow.Mode != layout.FlowMode {
				t.Errorf("flow mode = %q, want %q", form.Flow.Mode, layout.FlowMode)
			}
			if form.Flow.Columns != tc.wantColumns {
				t.Errorf("flow columns = %d, want %d", form.Flow.Columns, tc.wantColumns)
			}
			if len(form.Flow.Cells) != len(tc.wantCells) {
				t.Fatalf("flow cells = %+v, want %d cells", form.Flow.Cells, len(tc.wantCells))
			}
			for i := range tc.wantCells {
				if form.Flow.Cells[i] != tc.wantCells[i] {
					t.Errorf("cell[%d] = %+v, want %+v", i, form.Flow.Cells[i], tc.wantCells[i])
				}
			}

			// Widgets in a form must NOT consume a main-grid placement.
			for i, child := range form.Children {
				if child.Placement != nil {
					t.Errorf("child[%d] unexpectedly carries a placement: %+v", i, child.Placement)
				}
				if child.Layout != nil {
					t.Errorf("child[%d] unexpectedly carries a layout block", i)
				}
			}
		})
	}
}

// TestResolveFormChildValidation covers the form child-validation rule: a
// non-widget child (here a `table`, a valid leaf item type that is not a widget)
// is rejected with LAYOUT_FORM_CHILD_INVALID naming the offending child path,
// while an all-widget form resolves. An out-of-range column count fails with
// LAYOUT_FORM_COLUMNS_INVALID.
func TestResolveFormChildValidation(t *testing.T) {
	tests := []struct {
		name     string
		layout   string
		vars     string
		children string
		wantCode errors.Code // "" => resolves successfully
		wantPath string      // expected Details["path"] when wantCode set
	}{
		{
			name:     "all-widget form resolves",
			vars:     `{"name": "region", "type": "string", "default": "us"}`,
			children: widgetChild("text-input", "region"),
		},
		{
			name:     "non-widget child rejected",
			vars:     `{"name": "region", "type": "string", "default": "us"}`,
			children: widgetChild("text-input", "region") + `,{"$ref": "https://lattice.dev/schemas/items/table/1.0.0"}`,
			wantCode: errors.LAYOUT_FORM_CHILD_INVALID,
			wantPath: "root.children[1]",
		},
		{
			name:     "container child rejected",
			vars:     `{"name": "region", "type": "string", "default": "us"}`,
			children: `{"$ref": "https://lattice.dev/schemas/items/container/1.0.0"}`,
			wantCode: errors.LAYOUT_FORM_CHILD_INVALID,
			wantPath: "root.children[0]",
		},
		{
			// The form schema bounds columns to [1, 12], so an out-of-range count is
			// caught fail-fast at Pass 2 config validation (the resolver's
			// LAYOUT_FORM_COLUMNS_INVALID is a defensive backstop, exercised directly
			// in the layout package's flow tests).
			name:     "out-of-range column count rejected by schema",
			layout:   `{"columns": 99}`,
			vars:     `{"name": "region", "type": "string", "default": "us"}`,
			children: widgetChild("text-input", "region"),
			wantCode: errors.RESOLVE_CONFIG_INVALID,
			wantPath: "root",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := newRepoResolver(t)
			doc := formDoc(tc.layout, tc.vars, tc.children)
			tree, err := res.resolveBytes([]byte(doc), tc.name, nil)

			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("resolveBytes: unexpected error: %v", err)
				}
				if tree.Root.Flow == nil {
					t.Errorf("resolved form has no flow layout")
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error %s, got nil", tc.wantCode)
			}
			if !errors.HasCode(err, tc.wantCode) {
				t.Fatalf("error = %v, want code %s", err, tc.wantCode)
			}
			var ce *errors.CodedError
			if !asCoded(err, &ce) {
				t.Fatalf("error is not a CodedError: %v", err)
			}
			if got, _ := ce.Details["path"].(string); got != tc.wantPath {
				t.Errorf("error path = %q, want %q", got, tc.wantPath)
			}
		})
	}
}
