package service_test

// custom_widget_test.go — E2-S2: prove the headline goal for the WIDGET role.
//
// A widget type is now defined ENTIRELY by its schema: any item type whose
// schema declares `latticeBehavior: {role:"widget", binds:[...], ...}` gets the
// full binding pass (type-check, range check, options check) with ZERO Go change
// and NO built-in schema. This file proves it by supplying CUSTOM widget schemas
// that exist ONLY in a test fs.FS (never shipped in schemas/), composing them
// with the real dashboard + item catalog via an overlay fs.FS, and driving them
// through the PUBLIC service facade — the same boundary the MCP tools see.
//
// The overlay (overlaySchemaFS) layers the in-memory custom item schemas over
// os.DirFS("../schemas") so the custom widget resolves against the real dashboard
// grammar (root container -> variable-box -> widget) exactly as a built-in would.
//
// Import boundary: everything here goes through service.* (and root errors) — no
// internal/* path is named, so the MCP-surface enumeration check (ListSchemas /
// Schema) is a faithful first-class-surface test.

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/service"
)

// --- custom widget schemas (exist ONLY here, never in schemas/) --------------

// kpiInputSchema is a CUSTOM number-family widget modeled on number-field: it
// binds a `number` variable and opts into the cross-field range check
// (rangeCheck). Its $id follows the catalog convention so a document references
// it as items/kpi-input/1.0.0; the catalog derives the name "kpi-input" from the
// $id path and ListSchemas derives it from the file stem.
const kpiInputSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://lattice.dev/schemas/items/kpi-input/1.0.0",
  "title": "KPI Input (custom)",
  "description": "A custom number-family widget defined only in a test fs.FS.",
  "type": "object",
  "required": ["variable"],
  "additionalProperties": false,
  "latticeBehavior": {
    "role": "widget",
    "binds": ["number"],
    "rangeCheck": true
  },
  "properties": {
    "variable": {"type": "string", "minLength": 1},
    "label": {"type": "string"},
    "min": {"type": "number"},
    "max": {"type": "number"},
    "step": {"type": "number", "exclusiveMinimum": 0}
  }
}`

// kpiSelectSchema is a CUSTOM array-family widget modeled on multiselect: it
// binds an `array` variable and opts into the non-empty option-set check
// (requireOptions).
const kpiSelectSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://lattice.dev/schemas/items/kpi-select/1.0.0",
  "title": "KPI Select (custom)",
  "description": "A custom array-family widget defined only in a test fs.FS.",
  "type": "object",
  "required": ["variable"],
  "additionalProperties": false,
  "latticeBehavior": {
    "role": "widget",
    "binds": ["array"],
    "requireOptions": true
  },
  "properties": {
    "variable": {"type": "string", "minLength": 1},
    "label": {"type": "string"},
    "options": {
      "type": "array",
      "minItems": 1,
      "items": {
        "type": "object",
        "required": ["value"],
        "additionalProperties": false,
        "properties": {
          "value": {"type": "string"},
          "label": {"type": "string"}
        }
      }
    }
  }
}`

// --- overlay schema fs.FS -----------------------------------------------------

// newOverlaySchemaFS overlays a set of extra item-type schema files (keyed by
// their path within the catalog, e.g. "items/kpi-input.schema.json") over the
// embedded core catalog via the PUBLIC service.OverlaySchemas / service.CoreSchemas
// API — the exact composition a downstream consumer uses to add or override item
// types without copying the core schemas. The extra files live in an in-memory
// fstest.MapFS (never shipped in schemas/); MapFS synthesizes the parent "items"
// directory entry, so the merged listing the catalog walk + ListSchemas see
// includes the custom types. It is the shared overlay helper for the custom-type
// service tests (widgets, regions, wrappers).
func newOverlaySchemaFS(t *testing.T, extra map[string][]byte) fs.FS {
	t.Helper()
	m := make(fstest.MapFS, len(extra))
	for p, b := range extra {
		m[p] = &fstest.MapFile{Data: b}
	}
	return service.OverlaySchemas(service.CoreSchemas(), m)
}

// customSchemaFS builds the overlay carrying both custom widgets under items/.
func customSchemaFS(t *testing.T) fs.FS {
	t.Helper()
	return newOverlaySchemaFS(t, map[string][]byte{
		"items/kpi-input.schema.json":  []byte(kpiInputSchema),
		"items/kpi-select.schema.json": []byte(kpiSelectSchema),
	})
}

// newCustomService wires a Service over the overlay schema FS plus a throwaway
// store, so the facade's resolve + schema-surface methods run against the custom
// catalog. Documents are resolved in-memory via ResolveBytes (no store I/O).
func newCustomService(t *testing.T) *service.Service {
	t.Helper()
	svc, err := service.Open(service.Options{
		Backend: service.BackendFS,
		Root:    t.TempDir(),
		Schemas: customSchemaFS(t),
	})
	if err != nil {
		t.Fatalf("Open over custom schema FS: %v", err)
	}
	return svc
}

// --- document builders --------------------------------------------------------

// customWidgetDoc builds the canonical grammar home for a standalone widget:
// root container -> variable-box -> widget, with one document-scope variable.
// The widget's resolver path is therefore "root.children[0].children[0]". The
// widget config map is injected verbatim (it must carry at least "variable").
func customWidgetDoc(itemType, varName, varType, varDefault, widgetConfig string) string {
	return `{
  "manifest": {"formatVersion": "1.0.0", "id": "kpidoc", "title": "KPI Doc"},
  "variables": [{"name": "` + varName + `", "type": "` + varType + `", "default": ` + varDefault + `}],
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": {"grid": {"columns": [1]}},
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/variable-box/1.0.0",
        "id": "controls",
        "children": [
          {
            "$ref": "https://lattice.dev/schemas/items/` + itemType + `/1.0.0",
            "id": "w",
            "config": ` + widgetConfig + `
          }
        ]
      }
    ]
  }
}`
}

// widgetNode returns the widget instance from a customWidgetDoc tree:
// root -> variable-box -> widget.
func widgetNode(t *testing.T, tree *service.ResolvedTree) *service.ResolvedInstance {
	t.Helper()
	if tree.Root == nil || len(tree.Root.Children) == 0 || len(tree.Root.Children[0].Children) == 0 {
		t.Fatalf("resolved tree is missing the expected root->variable-box->widget shape")
	}
	return tree.Root.Children[0].Children[0]
}

// --- E2-S2 acceptance: number-family custom widget (binds + rangeCheck) -------

// TestCustomNumberWidgetThroughFacade proves a CUSTOM number widget (kpi-input,
// schema only in the test fs.FS) gets the full binding pass: a number bind
// resolves, an inverted min/max fires the range check, and a non-number bind
// fires WIDGET_TYPE_MISMATCH — all with no built-in schema and no Go change.
func TestCustomNumberWidgetThroughFacade(t *testing.T) {
	tests := []struct {
		name       string
		varType    string
		varDefault string
		config     string
		wantCode   errors.Code // "" = resolves successfully
	}{
		{
			name:       "binds a number variable -> resolves",
			varType:    "number",
			varDefault: "42",
			config:     `{"variable": "target", "min": 0, "max": 100}`,
		},
		{
			name:       "inverted min/max -> range check fires",
			varType:    "number",
			varDefault: "42",
			config:     `{"variable": "target", "min": 100, "max": 0}`,
			wantCode:   errors.RESOLVE_CONFIG_INVALID,
		},
		{
			name:       "bound to a string variable -> WIDGET_TYPE_MISMATCH",
			varType:    "string",
			varDefault: `"x"`,
			config:     `{"variable": "target"}`,
			wantCode:   errors.WIDGET_TYPE_MISMATCH,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := newCustomService(t)
			doc := customWidgetDoc("kpi-input", "target", tc.varType, tc.varDefault, tc.config)
			tree, err := svc.ResolveBytes([]byte(doc), tc.name, nil)

			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("ResolveBytes: unexpected error: %v", err)
				}
				w := widgetNode(t, tree)
				if got := w.Config["variable"]; got != "target" {
					t.Errorf("widget variable = %v, want %q", got, "target")
				}
				if w.Type.Name != "kpi-input" {
					t.Errorf("widget type name = %q, want %q", w.Type.Name, "kpi-input")
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

// --- E2-S2 acceptance: array-family custom widget (binds + requireOptions) ----

// TestCustomArrayWidgetOptionsThroughFacade proves a CUSTOM array widget
// (kpi-select, schema only in the test fs.FS) gets the requireOptions check:
// with a non-empty option set it resolves; with the set absent the resolver
// reports RESOLVE_CONFIG_INVALID. A non-array bind also fires
// WIDGET_TYPE_MISMATCH, confirming binds is honored.
func TestCustomArrayWidgetOptionsThroughFacade(t *testing.T) {
	tests := []struct {
		name       string
		varType    string
		varDefault string
		config     string
		wantCode   errors.Code
	}{
		{
			name:       "options present -> resolves",
			varType:    "array",
			varDefault: "[]",
			config:     `{"variable": "picks", "options": [{"value": "a"}, {"value": "b"}]}`,
		},
		{
			name:       "options absent -> RESOLVE_CONFIG_INVALID",
			varType:    "array",
			varDefault: "[]",
			config:     `{"variable": "picks"}`,
			wantCode:   errors.RESOLVE_CONFIG_INVALID,
		},
		{
			name:       "bound to a number variable -> WIDGET_TYPE_MISMATCH",
			varType:    "number",
			varDefault: "1",
			config:     `{"variable": "picks", "options": [{"value": "a"}]}`,
			wantCode:   errors.WIDGET_TYPE_MISMATCH,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := newCustomService(t)
			doc := customWidgetDoc("kpi-select", "picks", tc.varType, tc.varDefault, tc.config)
			tree, err := svc.ResolveBytes([]byte(doc), tc.name, nil)

			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("ResolveBytes: unexpected error: %v", err)
				}
				w := widgetNode(t, tree)
				if w.Type.Name != "kpi-select" {
					t.Errorf("widget type name = %q, want %q", w.Type.Name, "kpi-select")
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

// --- E2-S2 acceptance: MCP-surface first-class enumeration --------------------

// TestCustomWidgetIsFirstClassOnMCPSurface proves the custom widget types are
// first-class on the grammar surface the MCP tools expose: list_schemas ->
// ListSchemas() enumerates them and get_schema -> Schema(type) returns their raw
// JSON Schema (including the latticeBehavior keyword the MCP get_schema surfaces
// verbatim). It goes through the service facade only — the exact boundary the MCP
// layer sees — so it respects the import boundary.
func TestCustomWidgetIsFirstClassOnMCPSurface(t *testing.T) {
	svc := newCustomService(t)

	names, err := svc.ListSchemas()
	if err != nil {
		t.Fatalf("ListSchemas: %v", err)
	}
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	for _, want := range []string{"kpi-input", "kpi-select"} {
		if !have[want] {
			t.Errorf("ListSchemas did not enumerate custom widget %q; got %v", want, names)
		}
	}

	for _, typ := range []string{"kpi-input", "kpi-select"} {
		raw, err := svc.Schema(typ)
		if err != nil {
			t.Fatalf("Schema(%q): %v", typ, err)
		}
		if len(raw) == 0 {
			t.Fatalf("Schema(%q) returned no bytes", typ)
		}
		// The raw schema must carry the latticeBehavior keyword get_schema
		// surfaces verbatim (the keyword is what makes the type a widget).
		if !containsSub(raw, "latticeBehavior") || !containsSub(raw, `"role"`) {
			t.Errorf("Schema(%q) did not surface the latticeBehavior keyword: %s", typ, raw)
		}
	}
}

// containsSub reports whether b contains the substring sub (byte-level, no
// allocation of a string copy of b).
func containsSub(b []byte, sub string) bool {
	s := []byte(sub)
	if len(s) == 0 {
		return true
	}
	for i := 0; i+len(s) <= len(b); i++ {
		match := true
		for j := range s {
			if b[i+j] != s[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
