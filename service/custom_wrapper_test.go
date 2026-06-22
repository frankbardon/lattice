package service_test

// custom_wrapper_test.go — E4-S1b: prove the SERVICE-layer wrapper surface
// delegation is keyword-driven, not gated on the literal type name "block".
//
// A wrapper type is now defined ENTIRELY by its schema: any item type whose
// schema declares `latticeBehavior: {role:"wrapper", contentField:"..."}` is a
// first-class wrapper. The service NodeView delegates a wrapper's editable
// surface to its single inner content item — it must do so for a CUSTOM wrapper
// (any role:wrapper type not named "block") exactly as it does for the built-in
// block. This file proves it by supplying a CUSTOM wrapper schema that exists
// ONLY in a test fs.FS (never shipped in schemas/), composing it with the real
// dashboard + item catalog via the overlay fs.FS (defined in custom_widget_test.go),
// and driving it through the PUBLIC service facade — the same boundary the get_node
// MCP tool sees.
//
// The custom wrapper declares its OWN configurable surface ("caption") that is
// DISTINCT from the content it wraps, so the test can prove the surface returned
// by NodeView is the CONTENT's surface (heading's text/level), not the wrapper's.

import (
	"testing"

	"github.com/frankbardon/lattice/service"
)

// panelWrapperSchema is a CUSTOM wrapper modeled on the built-in block: it
// declares role:"wrapper" with contentField:"content", a required stable id, and
// its OWN configurable surface ("caption") that differs from any content it wraps.
// Its $id follows the catalog convention so a document references it as
// items/panel/1.0.0.
const panelWrapperSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://lattice.dev/schemas/items/panel/1.0.0",
  "title": "Panel (custom wrapper)",
  "description": "A custom wrapper defined only in a test fs.FS.",
  "type": "object",
  "required": ["id", "content"],
  "additionalProperties": false,
  "latticeBehavior": {
    "role": "wrapper",
    "contentField": "content"
  },
  "configurable": {
    "caption": {
      "type": "string",
      "label": "Caption",
      "rendering": "text-input",
      "constraints": {"description": "The panel's caption."}
    }
  },
  "properties": {
    "id": {"type": "string", "minLength": 1},
    "caption": {"type": "string"},
    "content": {"$ref": "https://lattice.dev/schemas/dashboard/1.0.0#/$defs/instance"}
  }
}`

// customWrapperDoc builds a stored document: root container -> custom panel
// wrapper (id "panel1") wrapping a heading content leaf (id "h"). The heading
// declares a configurable surface (text, level) distinct from the panel's own
// (caption), so surface delegation is observable.
func customWrapperDoc() string {
	return `{
  "manifest": {"formatVersion": "1.0.0", "id": "paneldoc", "title": "Panel Doc"},
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
            "$ref": "https://lattice.dev/schemas/items/panel/1.0.0",
            "id": "panel1",
            "config": {
              "id": "panel1",
              "caption": "Overview",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/heading/1.0.0",
                "id": "h",
                "config": {"text": "Hello", "level": 2}
              }
            }
          }
        ]
      }
    ]
  }
}`
}

// newPanelService wires a Service over the overlay schema FS carrying the custom
// panel wrapper plus a throwaway store, so the facade's resolve + NodeView run
// against the custom catalog.
func newPanelService(t *testing.T) *service.Service {
	t.Helper()
	svc, err := service.Open(service.Options{
		Backend: service.BackendFS,
		Root:    t.TempDir(),
		Schemas: newOverlaySchemaFS(t, map[string][]byte{
			"items/panel.schema.json": []byte(panelWrapperSchema),
		}),
	})
	if err != nil {
		t.Fatalf("Open over custom wrapper schema FS: %v", err)
	}
	return svc
}

// TestCustomWrapperSurfaceDelegatedThroughFacade proves NodeView delegates a
// CUSTOM wrapper's editable surface to its inner content — keyword-driven, no
// "block" name check. NodeView("panel1") must return the heading content's
// surface (text, level), NOT the panel's own surface (caption).
func TestCustomWrapperSurfaceDelegatedThroughFacade(t *testing.T) {
	svc := newPanelService(t)
	if err := svc.Save([]byte(customWrapperDoc())); err != nil {
		t.Fatalf("Save: %v", err)
	}

	view, err := svc.NodeView("paneldoc", "panel1")
	if err != nil {
		t.Fatalf("NodeView(panel1): %v", err)
	}

	got := surfaceFields(view.Surface)
	// The delegated surface is the HEADING content's (text, level), proving the
	// wrapper delegated by behavior, not name.
	for _, want := range []string{"text", "level"} {
		if !got[want] {
			t.Errorf("delegated surface missing content field %q; got fields %v", want, got)
		}
	}
	// The panel's OWN surface field must NOT appear — delegation, not own surface.
	if got["caption"] {
		t.Errorf("delegated surface leaked the wrapper's own field %q; got fields %v", "caption", got)
	}
}

// TestCustomWrapperContentNodeSurfaceMatchesDelegation confirms addressing the
// inner content node directly yields the same surface the wrapper delegates to,
// closing the loop that the wrapper surfaces exactly what its content does.
func TestCustomWrapperContentNodeSurfaceMatchesDelegation(t *testing.T) {
	svc := newPanelService(t)
	if err := svc.Save([]byte(customWrapperDoc())); err != nil {
		t.Fatalf("Save: %v", err)
	}

	wrapperView, err := svc.NodeView("paneldoc", "panel1")
	if err != nil {
		t.Fatalf("NodeView(panel1): %v", err)
	}
	contentView, err := svc.NodeView("paneldoc", "h")
	if err != nil {
		t.Fatalf("NodeView(h): %v", err)
	}

	if w, c := surfaceFields(wrapperView.Surface), surfaceFields(contentView.Surface); !sameStringSet(w, c) {
		t.Errorf("wrapper-delegated surface %v != content's own surface %v", w, c)
	}
}

// surfaceFields collects the Field names of a configurable surface into a set.
func surfaceFields(surface []service.ConfigurableField) map[string]bool {
	out := make(map[string]bool, len(surface))
	for _, f := range surface {
		out[f.Field] = true
	}
	return out
}

// sameStringSet reports whether two string sets are equal.
func sameStringSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
