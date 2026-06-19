package resolver

import (
	"fmt"
	"testing"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/layout"
)

// widgetChildPlaced is a widget child instance JSON of the given type bound to
// the given variable, carrying an explicit placement object (spliced verbatim,
// e.g. `{"colStart": 2, "colSpan": 1}`). Used to exercise grid-mode placement.
func widgetChildPlaced(widgetType, variable, placementJSON string) string {
	return fmt.Sprintf(`{"$ref": "https://lattice.dev/schemas/items/%s/1.0.0", "config": {"variable": %q}, "placement": %s}`, widgetType, variable, placementJSON)
}

// formDoc builds a minimal dashboard whose single content leaf is a `form` (with
// the supplied layout config object, e.g. `{"columns": 2}` or empty `{}`) holding
// the given child instances. Under the E3-S2 grammar a form is a content leaf, so
// it is block-wrapped inside a body region: root -> body region -> block -> form.
// The form's own path is therefore "root.children[0].children[0].content" and its
// widget children are form-internals (validated by the form pass, not the grammar
// walk). Variables it needs are declared at document scope so the widget children
// bind cleanly. childrenJSON is spliced verbatim as the form's children array.
func formDoc(layoutJSON, varsJSON, childrenJSON string) string {
	cfg := ""
	if layoutJSON != "" {
		cfg = fmt.Sprintf(`, "config": {"layout": %s}`, layoutJSON)
	}
	return fmt.Sprintf(`{
  "manifest": {"formatVersion": "1.0.0", "id": "fdoc", "title": "Form Doc"},
  "variables": [%s],
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": {"grid": {"columns": [1]}},
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
        "id": "body",
        "config": {"grid": {"columns": [1]}},
        "children": [
          {
            "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
            "config": {
              "id": "form-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/form/1.0.0",
                "id": "form"%s,
                "children": [%s]
              }
            }
          }
        ]
      }
    ]
  }
}`, varsJSON, cfg, childrenJSON)
}

// formNode returns the form node from a tree built by formDoc, navigating the
// E3-S2 grammar wrapping: root -> body region -> block -> form.
func formNode(tree *ResolvedTree) *ResolvedInstance {
	return tree.Root.Children[0].Children[0].Children[0]
}

// formChildPath is the resolver path of the form's i-th widget child under the
// E3-S2 grammar wrapping. The form's own path is the block content path
// "root.children[0].children[0].content".
func formChildPath(i int) string {
	return fmt.Sprintf("root.children[0].children[0].content.children[%d]", i)
}

// formOwnPath is the resolver path of the form node itself (the block's content).
const formOwnPath = "root.children[0].children[0].content"

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

			form := formNode(tree)
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
			wantPath: formChildPath(1),
		},
		{
			name:     "container child rejected",
			vars:     `{"name": "region", "type": "string", "default": "us"}`,
			children: `{"$ref": "https://lattice.dev/schemas/items/container/1.0.0"}`,
			wantCode: errors.LAYOUT_FORM_CHILD_INVALID,
			wantPath: formChildPath(0),
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
			wantPath: formOwnPath,
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
				if formNode(tree).Flow == nil {
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

// TestResolveFormGrid drives a grid-mode `form` through the real pipeline: with
// `layout.mode == "grid"` the form normalizes its weighted grid into the SAME
// layout.Block path container uses (fractional tracks + validated child
// placements) and carries NO flow. It exercises track normalization, gap
// passthrough, explicit child placement, and placement defaulting.
func TestResolveFormGrid(t *testing.T) {
	const vars = `{"name": "region", "type": "string", "default": "us"},
		{"name": "active", "type": "boolean", "default": true}`

	tests := []struct {
		name        string
		layout      string
		children    string
		wantColumns []float64
		wantRows    []float64
		wantGap     float64
		wantPlaces  []layout.Placement
	}{
		{
			name:   "two-column grid normalizes tracks and honors placements",
			layout: `{"mode": "grid", "columns": [1, 3], "gap": 0.5}`,
			children: widgetChildPlaced("text-input", "region", `{"colStart": 1}`) + "," +
				widgetChildPlaced("toggle", "active", `{"colStart": 2}`),
			wantColumns: []float64{0.25, 0.75},
			wantRows:    []float64{1.0},
			wantGap:     0.5,
			wantPlaces: []layout.Placement{
				{ColStart: 1, ColSpan: 1, RowStart: 1, RowSpan: 1},
				{ColStart: 2, ColSpan: 1, RowStart: 1, RowSpan: 1},
			},
		},
		{
			name:   "rows and spans across a 2x2 grid",
			layout: `{"mode": "grid", "columns": [1, 1], "rows": [2, 1]}`,
			children: widgetChildPlaced("text-input", "region", `{"colStart": 1, "colSpan": 2, "rowStart": 1}`) + "," +
				widgetChildPlaced("toggle", "active", `{"colStart": 1, "rowStart": 2, "rowSpan": 1}`),
			wantColumns: []float64{0.5, 0.5},
			wantRows:    []float64{2.0 / 3.0, 1.0 / 3.0},
			wantGap:     0,
			wantPlaces: []layout.Placement{
				{ColStart: 1, ColSpan: 2, RowStart: 1, RowSpan: 1},
				{ColStart: 1, ColSpan: 1, RowStart: 2, RowSpan: 1},
			},
		},
		{
			name:     "missing grid and placement default to a single full cell",
			layout:   `{"mode": "grid"}`,
			children: widgetChild("text-input", "region"),
			wantColumns: []float64{1.0},
			wantRows:    []float64{1.0},
			wantGap:     0,
			wantPlaces: []layout.Placement{
				{ColStart: 1, ColSpan: 1, RowStart: 1, RowSpan: 1},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := newRepoResolver(t)
			doc := formDoc(tc.layout, vars, tc.children)
			tree, err := res.resolveBytes([]byte(doc), tc.name, nil)
			if err != nil {
				t.Fatalf("resolveBytes: unexpected error: %v", err)
			}

			form := formNode(tree)
			if form.Layout == nil {
				t.Fatalf("grid-mode form has no layout block")
			}
			if form.Flow != nil {
				t.Errorf("grid-mode form unexpectedly carries a flow: %+v", form.Flow)
			}
			if form.Container {
				t.Errorf("form should not report Container=true")
			}
			if !floatsNear(form.Layout.Columns, tc.wantColumns) {
				t.Errorf("columns = %v, want %v", form.Layout.Columns, tc.wantColumns)
			}
			if !floatsNear(form.Layout.Rows, tc.wantRows) {
				t.Errorf("rows = %v, want %v", form.Layout.Rows, tc.wantRows)
			}
			if form.Layout.Gap != tc.wantGap {
				t.Errorf("gap = %v, want %v", form.Layout.Gap, tc.wantGap)
			}
			if len(form.Layout.Placements) != len(tc.wantPlaces) {
				t.Fatalf("placements = %+v, want %d", form.Layout.Placements, len(tc.wantPlaces))
			}
			for i := range tc.wantPlaces {
				if form.Layout.Placements[i] != tc.wantPlaces[i] {
					t.Errorf("placement[%d] = %+v, want %+v", i, form.Layout.Placements[i], tc.wantPlaces[i])
				}
			}
		})
	}
}

// TestResolveFormGridPlacementValidation covers grid-mode placement bounds and
// positivity, asserting the form reuses the SAME LAYOUT_PLACEMENT_* errors as
// container (no forked validation) and names the offending child path.
func TestResolveFormGridPlacementValidation(t *testing.T) {
	const vars = `{"name": "region", "type": "string", "default": "us"}`

	tests := []struct {
		name     string
		layout   string
		children string
		wantCode errors.Code // "" => resolves successfully
		wantPath string
	}{
		{
			name:     "in-bounds placement resolves",
			layout:   `{"mode": "grid", "columns": [1, 1]}`,
			children: widgetChildPlaced("text-input", "region", `{"colStart": 2}`),
		},
		{
			name:     "column out of bounds",
			layout:   `{"mode": "grid", "columns": [1, 1]}`,
			children: widgetChildPlaced("text-input", "region", `{"colStart": 3}`),
			wantCode: errors.LAYOUT_PLACEMENT_OUT_OF_BOUNDS,
			wantPath: formChildPath(0),
		},
		{
			name:     "span exceeds column bounds",
			layout:   `{"mode": "grid", "columns": [1, 1]}`,
			children: widgetChildPlaced("text-input", "region", `{"colStart": 2, "colSpan": 2}`),
			wantCode: errors.LAYOUT_PLACEMENT_OUT_OF_BOUNDS,
			wantPath: formChildPath(0),
		},
		{
			name:     "row out of bounds",
			layout:   `{"mode": "grid", "columns": [1], "rows": [1, 1]}`,
			children: widgetChildPlaced("text-input", "region", `{"rowStart": 3}`),
			wantCode: errors.LAYOUT_PLACEMENT_OUT_OF_BOUNDS,
			wantPath: formChildPath(0),
		},
		{
			name:     "non-positive span rejected",
			layout:   `{"mode": "grid", "columns": [1, 1]}`,
			children: widgetChildPlaced("text-input", "region", `{"colStart": 1, "colSpan": 0}`),
			wantCode: errors.LAYOUT_PLACEMENT_INVALID,
			wantPath: formChildPath(0),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := newRepoResolver(t)
			doc := formDoc(tc.layout, vars, tc.children)
			tree, err := res.resolveBytes([]byte(doc), tc.name, nil)

			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("resolveBytes: unexpected error: %v", err)
				}
				if formNode(tree).Layout == nil {
					t.Errorf("resolved grid-mode form has no layout block")
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

// TestResolveFormModeSwitching asserts the `layout.mode` discriminator picks the
// representation per instance: flow mode (default or explicit) yields a
// layout.Flow and no Block; grid mode yields a layout.Block and no Flow. The two
// are MODES of the one `form` type.
func TestResolveFormModeSwitching(t *testing.T) {
	const vars = `{"name": "region", "type": "string", "default": "us"}`
	child := widgetChild("text-input", "region")

	tests := []struct {
		name     string
		layout   string
		wantFlow bool
		wantGrid bool
	}{
		{name: "default (no layout) is flow", layout: "", wantFlow: true},
		{name: "explicit flow mode", layout: `{"mode": "flow", "columns": 2}`, wantFlow: true},
		{name: "explicit grid mode", layout: `{"mode": "grid", "columns": [1, 1]}`, wantGrid: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := newRepoResolver(t)
			doc := formDoc(tc.layout, vars, child)
			tree, err := res.resolveBytes([]byte(doc), tc.name, nil)
			if err != nil {
				t.Fatalf("resolveBytes: unexpected error: %v", err)
			}
			form := formNode(tree)
			if tc.wantFlow {
				if form.Flow == nil {
					t.Errorf("expected flow layout, got none")
				}
				if form.Layout != nil {
					t.Errorf("flow-mode form unexpectedly carries a Block: %+v", form.Layout)
				}
			}
			if tc.wantGrid {
				if form.Layout == nil {
					t.Errorf("expected grid layout block, got none")
				}
				if form.Flow != nil {
					t.Errorf("grid-mode form unexpectedly carries a Flow: %+v", form.Flow)
				}
			}
		})
	}
}

// TestResolveStandaloneWidgetPlacement proves a variable widget placed DIRECTLY
// in a `variable-box` region resolves (not only inside a `form`): the widget binds
// its variable through the same widget pass. Under the E3-S2 grammar a bare/input
// widget that is a standalone dashboard control lives in a variable-box — the
// dedicated, leaf-only home for the widget family — held directly (not block-
// wrapped, not in a plain grid container). The widget is an ordinary leaf item
// instance, so it carries no nested layout of its own.
func TestResolveStandaloneWidgetPlacement(t *testing.T) {
	doc := `{
  "manifest": {"formatVersion": "1.0.0", "id": "standalone", "title": "Standalone Widget"},
  "variables": [{"name": "region", "type": "string", "default": "us"}],
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": {"grid": {"columns": [1]}},
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/variable-box/1.0.0",
        "id": "controls",
        "placement": {"colStart": 1, "rowStart": 1},
        "children": [
          {
            "$ref": "https://lattice.dev/schemas/items/text-input/1.0.0",
            "id": "region-input",
            "config": {"label": "Region", "variable": "region"}
          }
        ]
      }
    ]
  }
}`

	res := newRepoResolver(t)
	tree, err := res.resolveBytes([]byte(doc), "standalone", nil)
	if err != nil {
		t.Fatalf("resolveBytes: unexpected error: %v", err)
	}

	vbox := tree.Root.Children[0]
	if vbox.Type.Name != "variable-box" {
		t.Fatalf("root child type = %q, want variable-box", vbox.Type.Name)
	}

	widget := vbox.Children[0]
	if widget.Type.Name != "text-input" {
		t.Errorf("child type = %q, want text-input", widget.Type.Name)
	}
	if got := widget.Config["variable"]; got != "region" {
		t.Errorf("widget variable = %v, want region", got)
	}
	// A standalone widget is a leaf; it carries no nested layout of its own.
	if widget.Flow != nil {
		t.Errorf("standalone widget unexpectedly carries a flow: %+v", widget.Flow)
	}
	if widget.Layout != nil {
		t.Errorf("standalone widget unexpectedly carries a layout block: %+v", widget.Layout)
	}
}

// floatsNear reports whether two float slices match within a small epsilon,
// tolerating normalization rounding.
func floatsNear(got, want []float64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		d := got[i] - want[i]
		if d < -1e-9 || d > 1e-9 {
			return false
		}
	}
	return true
}
