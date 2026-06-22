package service_test

// custom_region_test.go — E3-S4: prove the REGION role end to end for CUSTOM
// region types, and re-prove the built-in region goldens are byte-identical after
// the form-collapse migration.
//
// A region type is now defined ENTIRELY by its schema: any item type whose schema
// declares `latticeBehavior: {role:"region", childPolicy:..., layout:...}` gets the
// matching structural treatment — children-allowed, grammar child-admission, and
// the grid/flow/none layout pass — with ZERO Go change and NO built-in schema. This
// file proves it by supplying CUSTOM region schemas that exist ONLY in a test
// fs.FS (never shipped in schemas/), composing them with the real dashboard + item
// catalog via the SAME overlay fs.FS pattern E2-S2 introduced (overlaySchemaFS, in
// custom_widget_test.go — reused verbatim here), and driving them through the
// PUBLIC service facade.
//
// The headline proof is the custom FLOW region (kpi-form): a `childPolicy:widgets,
// layout:flow` type NOT named `form`. If it flow-packs its widgets and rejects a
// non-widget child exactly like the built-in form, then collapsing `form` into a
// keyword-driven region truly generalized the behavior — the form name is not
// load-bearing.
//
// Import boundary: everything goes through service.* (and root errors) — no
// internal/* path is named, so the list_schemas / get_schema surface checks are a
// faithful first-class-surface test.

import (
	"io/fs"
	"testing"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/service"
)

// --- custom region schemas (exist ONLY here, never in schemas/) ---------------

// kpiGridSchema is a CUSTOM grid region modeled on `container`:
// childPolicy:regions-or-wrappers, layout:grid. It declares its own relative-weight
// grid surface (the resolver normalizes it via the same weighted-grid path), so a
// child carries an explicit placement and content must be block-wrapped.
const kpiGridSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://lattice.dev/schemas/items/kpi-grid/1.0.0",
  "title": "KPI Grid (custom region)",
  "description": "A custom regions-or-wrappers grid region defined only in a test fs.FS.",
  "type": "object",
  "additionalProperties": false,
  "latticeBehavior": {
    "role": "region",
    "childPolicy": "regions-or-wrappers",
    "layout": "grid"
  },
  "properties": {
    "grid": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "columns": {"type": "array", "minItems": 1, "items": {"type": "number", "exclusiveMinimum": 0}},
        "rows": {"type": "array", "minItems": 1, "items": {"type": "number", "exclusiveMinimum": 0}},
        "gap": {"type": "number", "minimum": 0, "default": 0}
      }
    }
  }
}`

// kpiFormSchema is a CUSTOM flow region modeled on `form` but NOT named form:
// childPolicy:widgets, layout:flow. THIS IS THE KEY PROOF — it must flow-pack its
// widget children and reject non-widget children purely on the strength of its
// latticeBehavior keyword, with no `form` name anywhere.
const kpiFormSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://lattice.dev/schemas/items/kpi-form/1.0.0",
  "title": "KPI Form (custom flow region)",
  "description": "A custom widgets/flow region defined only in a test fs.FS — a form without the form name.",
  "type": "object",
  "additionalProperties": false,
  "latticeBehavior": {
    "role": "region",
    "childPolicy": "widgets",
    "layout": "flow"
  },
  "properties": {
    "layout": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "mode": {"type": "string", "enum": ["flow", "grid"], "default": "flow"},
        "columns": {"type": "integer", "minimum": 1, "maximum": 12, "default": 1}
      }
    }
  }
}`

// kpiPanelSchema is a CUSTOM layout:none region modeled on `variable-box`:
// childPolicy:widgets, layout:none. It holds its widget children directly with NO
// grid/flow layout pass — the resolved node carries neither Layout nor Flow.
const kpiPanelSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://lattice.dev/schemas/items/kpi-panel/1.0.0",
  "title": "KPI Panel (custom layout:none region)",
  "description": "A custom widgets/none region defined only in a test fs.FS.",
  "type": "object",
  "additionalProperties": false,
  "latticeBehavior": {
    "role": "region",
    "childPolicy": "widgets",
    "layout": "none"
  },
  "properties": {
    "arrangement": {"type": "string", "enum": ["stacked", "inline"], "default": "stacked"}
  }
}`

// customRegionSchemaFS layers all three custom region schemas (plus the kpi-input
// widget reused from custom_widget_test.go) over the real schemas/ directory via
// the shared overlay pattern.
func customRegionSchemaFS(t *testing.T) fs.FS {
	t.Helper()
	return newOverlaySchemaFS(t, map[string][]byte{
		"items/kpi-grid.schema.json":  []byte(kpiGridSchema),
		"items/kpi-form.schema.json":  []byte(kpiFormSchema),
		"items/kpi-panel.schema.json": []byte(kpiPanelSchema),
		"items/kpi-input.schema.json": []byte(kpiInputSchema),
	})
}

// newCustomRegionService wires a Service over the region overlay FS plus a
// throwaway store. Documents resolve in-memory via ResolveBytes (no store I/O).
func newCustomRegionService(t *testing.T) *service.Service {
	t.Helper()
	svc, err := service.Open(service.Options{
		Backend: service.BackendFS,
		Root:    t.TempDir(),
		Schemas: customRegionSchemaFS(t),
	})
	if err != nil {
		t.Fatalf("Open over custom region schema FS: %v", err)
	}
	return svc
}

// kpiInputWidget is a kpi-input widget instance bound to the named number variable.
func kpiInputWidget(id, variable string) string {
	return `{
		"$ref": "https://lattice.dev/schemas/items/kpi-input/1.0.0",
		"id": "` + id + `",
		"config": {"variable": "` + variable + `"}
	}`
}

// numberVar declares one document-scope number variable.
func numberVar(name string) string {
	return `{"name": "` + name + `", "type": "number", "default": 0}`
}

// regionDoc nests a single custom region under a built-in `container` document
// root and returns the document JSON. The container root satisfies the grammar's
// root-child rule (root holds only positional regions); the custom region is the
// container's single child, so the region's OWN latticeBehavior.childPolicy is the
// rule that grammar-checks the region's children — exactly what these cases probe.
// vars is the document variables array body (without brackets); children is the
// custom region's children array body. The resolved custom region is therefore at
// tree.Root.Children[0].
func regionDoc(regionType, regionID, regionConfig, vars, children string) string {
	cfg := ""
	if regionConfig != "" {
		cfg = `"config": ` + regionConfig + `,`
	}
	return `{
  "manifest": {"formatVersion": "1.0.0", "id": "kpidoc", "title": "KPI Region Doc"},
  "variables": [` + vars + `],
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "rootc",
    "config": {"grid": {"columns": [1]}},
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/` + regionType + `/1.0.0",
        "id": "` + regionID + `",
        ` + cfg + `
        "children": [` + children + `]
      }
    ]
  }
}`
}

// customRegionNode returns the custom region node nested under the container root.
func customRegionNode(t *testing.T, tree *service.ResolvedTree) *service.ResolvedInstance {
	t.Helper()
	if tree.Root == nil || len(tree.Root.Children) != 1 {
		t.Fatalf("expected container root with one custom-region child; got %+v", tree.Root)
	}
	return tree.Root.Children[0]
}

// blockWrappedMarkdown is a block wrapping a markdown content leaf — a legal child
// of a regions-or-wrappers region.
const blockWrappedMarkdown = `{
	"$ref": "https://lattice.dev/schemas/items/block/1.0.0",
	"id": "wrap",
	"config": {
		"id": "wrap",
		"content": {
			"$ref": "https://lattice.dev/schemas/items/markdown/1.0.0",
			"id": "md",
			"config": {"source": "# hi"}
		}
	}
}`

// --- E3-S4: custom grid region (regions-or-wrappers, grid) --------------------

// TestCustomGridRegionResolvesAndGrammarChecks proves a CUSTOM grid region
// (kpi-grid, schema only in the test fs.FS) gets the regions-or-wrappers grammar:
// it holds a nested region OR a block wrapper (positive), and rejects a bare
// content leaf or a bare widget directly inside (GRAMMAR_REGION_CHILD_INVALID).
func TestCustomGridRegionResolvesAndGrammarChecks(t *testing.T) {
	tests := []struct {
		name     string
		children string
		wantCode errors.Code // "" = resolves
	}{
		{
			name:     "holds a block wrapper -> resolves",
			children: blockWrappedMarkdown,
		},
		{
			name: "holds a nested region -> resolves",
			children: `{
				"$ref": "https://lattice.dev/schemas/items/kpi-panel/1.0.0",
				"id": "nested",
				"children": [` + kpiInputWidget("w", "v") + `]
			}`,
		},
		{
			name: "holds a bare content leaf -> GRAMMAR_REGION_CHILD_INVALID",
			children: `{
				"$ref": "https://lattice.dev/schemas/items/markdown/1.0.0",
				"id": "bare",
				"config": {"source": "# nope"}
			}`,
			wantCode: errors.GRAMMAR_REGION_CHILD_INVALID,
		},
		{
			name:     "holds a bare widget -> GRAMMAR_REGION_CHILD_INVALID",
			children: kpiInputWidget("bareWidget", "v"),
			wantCode: errors.GRAMMAR_REGION_CHILD_INVALID,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := newCustomRegionService(t)
			doc := regionDoc("kpi-grid", "grid", `{"grid": {"columns": [1, 1]}}`, numberVar("v"), tc.children)
			tree, err := svc.ResolveBytes([]byte(doc), tc.name, nil)

			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("ResolveBytes: unexpected error: %v", err)
				}
				region := customRegionNode(t, tree)
				if region.Type.Name != "kpi-grid" {
					t.Errorf("region type = %q, want kpi-grid", region.Type.Name)
				}
				if !region.Container {
					t.Errorf("grid region should carry Container=true (derived from layout:grid)")
				}
				if region.Layout == nil {
					t.Errorf("grid region should carry a normalized Layout block")
				}
				if len(region.Children) != 1 {
					t.Errorf("expected 1 resolved child, got %d", len(region.Children))
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error %s, got nil", tc.wantCode)
			}
			if !errors.HasCode(err, tc.wantCode) {
				t.Fatalf("expected code %s, got: %v", tc.wantCode, err)
			}
		})
	}
}

// --- E3-S4: custom flow region (widgets, flow) — THE KEY PROOF ----------------

// TestCustomFlowRegionPacksLikeForm proves a CUSTOM flow region (kpi-form, schema
// only in the test fs.FS, NOT named `form`) behaves exactly like the built-in
// form: it flow-packs its widget children into a layout.Flow and rejects a
// non-widget child fail-fast. This is the proof that collapsing `form` into a
// keyword-driven region generalized it — the `form` name is not load-bearing.
func TestCustomFlowRegionPacksLikeForm(t *testing.T) {
	t.Run("packs widget children into a flow layout", func(t *testing.T) {
		svc := newCustomRegionService(t)
		children := kpiInputWidget("a", "v1") + "," + kpiInputWidget("b", "v2") + "," + kpiInputWidget("c", "v1")
		doc := regionDoc("kpi-form", "form", `{"layout": {"mode": "flow", "columns": 2}}`,
			numberVar("v1")+","+numberVar("v2"), children)

		tree, err := svc.ResolveBytes([]byte(doc), "flow pack", nil)
		if err != nil {
			t.Fatalf("ResolveBytes: unexpected error: %v", err)
		}
		region := customRegionNode(t, tree)
		if region.Type.Name != "kpi-form" {
			t.Fatalf("region type = %q, want kpi-form", region.Type.Name)
		}
		// The KEY assertion: a custom flow region gets the SAME flow-pack a form
		// gets — Flow is populated, Layout (grid) is not, Container is false.
		if region.Flow == nil {
			t.Fatalf("custom flow region did not flow-pack: Flow is nil (collapsing form did NOT generalize)")
		}
		if region.Flow.Columns != 2 {
			t.Errorf("Flow.Columns = %d, want 2", region.Flow.Columns)
		}
		if len(region.Flow.Cells) != 3 {
			t.Errorf("Flow.Cells = %d, want 3 (one per widget child)", len(region.Flow.Cells))
		}
		if region.Layout != nil {
			t.Errorf("flow region must not carry a grid Layout block; got %+v", region.Layout)
		}
		if region.Container {
			t.Errorf("flow region must not be flagged Container")
		}
	})

	t.Run("rejects a non-widget child", func(t *testing.T) {
		svc := newCustomRegionService(t)
		// A block wrapper is a legal child of a regions-or-wrappers region but NOT a
		// widgets/flow region — it must be rejected just as the built-in form rejects
		// non-widget children.
		doc := regionDoc("kpi-form", "form", "", numberVar("v"), blockWrappedMarkdown)

		_, err := svc.ResolveBytes([]byte(doc), "flow reject", nil)
		if err == nil {
			t.Fatalf("expected a non-widget child to be rejected, got nil")
		}
		// The form pass runs during instance resolution (before the grammar pass),
		// so a non-widget child in a flow region surfaces LAYOUT_FORM_CHILD_INVALID —
		// the same fail-fast code the built-in form emits, reached without the name.
		if !errors.HasCode(err, errors.LAYOUT_FORM_CHILD_INVALID) {
			t.Fatalf("expected LAYOUT_FORM_CHILD_INVALID, got: %v", err)
		}
	})
}

// --- E3-S4: custom layout:none region (widgets, none) -------------------------

// TestCustomNoneRegionResolvesWithoutLayout proves a CUSTOM layout:none region
// (kpi-panel, schema only in the test fs.FS) resolves its widget children with NO
// grid and NO flow layout pass — the resolved node carries neither Layout nor
// Flow, and is not flagged Container.
func TestCustomNoneRegionResolvesWithoutLayout(t *testing.T) {
	svc := newCustomRegionService(t)
	children := kpiInputWidget("a", "v") + "," + kpiInputWidget("b", "v")
	doc := regionDoc("kpi-panel", "panel", `{"arrangement": "inline"}`, numberVar("v"), children)

	tree, err := svc.ResolveBytes([]byte(doc), "none region", nil)
	if err != nil {
		t.Fatalf("ResolveBytes: unexpected error: %v", err)
	}
	region := customRegionNode(t, tree)
	if region.Type.Name != "kpi-panel" {
		t.Fatalf("region type = %q, want kpi-panel", region.Type.Name)
	}
	if len(region.Children) != 2 {
		t.Errorf("expected 2 resolved widget children, got %d", len(region.Children))
	}
	if region.Layout != nil {
		t.Errorf("layout:none region must not carry a grid Layout block; got %+v", region.Layout)
	}
	if region.Flow != nil {
		t.Errorf("layout:none region must not carry a Flow layout; got %+v", region.Flow)
	}
	if region.Container {
		t.Errorf("layout:none region must not be flagged Container")
	}

	// A non-widget child is still rejected by the widgets-policy grammar pass.
	t.Run("rejects a non-widget child via grammar", func(t *testing.T) {
		bad := regionDoc("kpi-panel", "panel", "", numberVar("v"), blockWrappedMarkdown)
		_, err := svc.ResolveBytes([]byte(bad), "none reject", nil)
		if err == nil {
			t.Fatalf("expected a non-widget child to be rejected, got nil")
		}
		if !errors.HasCode(err, errors.GRAMMAR_VARIABLE_BOX_CHILD_INVALID) {
			t.Fatalf("expected GRAMMAR_VARIABLE_BOX_CHILD_INVALID, got: %v", err)
		}
	})
}

// --- E3-S4: custom regions are first-class on the MCP surface -----------------

// TestCustomRegionsAreFirstClassOnMCPSurface proves the custom region types are
// first-class on the grammar surface the MCP tools expose: list_schemas ->
// ListSchemas() enumerates them and get_schema -> Schema(type) returns their raw
// JSON Schema including the latticeBehavior keyword. It goes through the service
// facade only — the exact boundary the MCP layer sees — respecting the import
// boundary (no internal/* path is named).
func TestCustomRegionsAreFirstClassOnMCPSurface(t *testing.T) {
	svc := newCustomRegionService(t)

	names, err := svc.ListSchemas()
	if err != nil {
		t.Fatalf("ListSchemas: %v", err)
	}
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	for _, want := range []string{"kpi-grid", "kpi-form", "kpi-panel"} {
		if !have[want] {
			t.Errorf("ListSchemas did not enumerate custom region %q; got %v", want, names)
		}
	}

	for _, typ := range []string{"kpi-grid", "kpi-form", "kpi-panel"} {
		raw, err := svc.Schema(typ)
		if err != nil {
			t.Fatalf("Schema(%q): %v", typ, err)
		}
		if len(raw) == 0 {
			t.Fatalf("Schema(%q) returned no bytes", typ)
		}
		// The raw schema must carry the latticeBehavior keyword get_schema surfaces
		// verbatim, with the region role and a childPolicy — the keywords that make
		// the type a first-class region.
		if !containsSub(raw, "latticeBehavior") || !containsSub(raw, `"region"`) || !containsSub(raw, "childPolicy") {
			t.Errorf("Schema(%q) did not surface the region latticeBehavior keyword: %s", typ, raw)
		}
	}
}
