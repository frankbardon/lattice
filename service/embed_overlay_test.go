package service_test

// embed_overlay_test.go — proves the library-inheritance surface added so a
// downstream module need not copy lattice's core schemas:
//
//   - service.CoreSchemas() exposes the embedded core catalog, and Open/NewResolver
//     fall back to it when Options.Schemas is nil (zero-config inheritance).
//   - service.OverlaySchemas composes a consumer's extra/overriding item types over
//     that core, with the consumer winning on a path collision.
//
// It also locks the two core-schema mods absorbed from the downstream app: the
// container grid configurable surface is an OBJECT (exercising VarTypeObject end
// to end) and the block carries a `tone` enum surface.
//
// Everything goes through service.* (+ root errors) — the MCP import boundary.

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/service"
)

// minimalEmbeddedDoc is the smallest grammar-valid document: a root container
// whose configurable grid is the OBJECT form. Resolving it through a zero-config
// service exercises both the embedded core catalog and the object grid surface.
const minimalEmbeddedDoc = `{
  "manifest": {"formatVersion": "1.0.0", "id": "embed-min", "title": "Embedded Minimal"},
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": {"grid": {"columns": [1, 2], "rows": [1], "gap": 1}}
  }
}`

// blockToneDoc wraps a markdown leaf in a block that sets the absorbed `tone`
// chrome field; the tone value is injected so the same doc drives the valid and
// rejected cases.
func blockToneDoc(tone string) string {
	return `{
  "manifest": {"formatVersion": "1.0.0", "id": "tonedoc", "title": "Tone"},
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
            "id": "b1",
            "config": {
              "id": "b1",
              "tone": "` + tone + `",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/markdown/1.0.0",
                "config": {"source": "# hi"}
              }
            }
          }
        ]
      }
    ]
  }
}`
}

// TestEmbeddedCoreZeroConfig proves Open with no Options.Schemas inherits the
// embedded core catalog: the core item types enumerate and a minimal document
// resolves — no on-disk schemas directory, no copy.
func TestEmbeddedCoreZeroConfig(t *testing.T) {
	svc, err := service.Open(service.Options{Backend: service.BackendFS, Root: t.TempDir()})
	if err != nil {
		t.Fatalf("Open with nil Schemas (embedded core): %v", err)
	}

	names, err := svc.ListSchemas()
	if err != nil {
		t.Fatalf("ListSchemas: %v", err)
	}
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	for _, want := range []string{"container", "block", "markdown", "dashboard"} {
		if !have[want] {
			t.Errorf("embedded ListSchemas missing %q; got %v", want, names)
		}
	}

	if _, err := svc.ResolveBytes([]byte(minimalEmbeddedDoc), "embedded", nil); err != nil {
		t.Fatalf("ResolveBytes over embedded core (object grid surface): %v", err)
	}
}

// TestCoreSchemasMatchEmbedded proves CoreSchemas() is the same catalog the
// nil-default uses: it carries the dashboard schema and the item types directly.
func TestCoreSchemasMatchEmbedded(t *testing.T) {
	core := service.CoreSchemas()
	for _, path := range []string{"dashboard.schema.json", "items/block.schema.json", "items/container.schema.json"} {
		if _, err := fs.Stat(core, path); err != nil {
			t.Errorf("CoreSchemas missing %q: %v", path, err)
		}
	}
}

// TestOverlayConsumerOverridesCore proves OverlaySchemas lets a consumer (1) add
// a brand-new type and (2) override a core type of the same path — the override
// wins on Schema() and the new type is first-class on ListSchemas.
func TestOverlayConsumerOverridesCore(t *testing.T) {
	const overridingBlock = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://lattice.dev/schemas/items/block/1.0.0",
  "title": "Block Item (OVERRIDDEN BY CONSUMER)",
  "type": "object",
  "required": ["id", "content"],
  "additionalProperties": false,
  "latticeBehavior": {"role": "wrapper", "contentField": "content"},
  "properties": {
    "id": {"type": "string", "minLength": 1},
    "content": {"$ref": "https://lattice.dev/schemas/dashboard/1.0.0#/$defs/instance"}
  }
}`
	extra := fstest.MapFS{
		"items/kpi-input.schema.json": {Data: []byte(kpiInputSchema)},
		"items/block.schema.json":     {Data: []byte(overridingBlock)},
	}
	svc, err := service.Open(service.Options{
		Backend: service.BackendFS,
		Root:    t.TempDir(),
		Schemas: service.OverlaySchemas(service.CoreSchemas(), extra),
	})
	if err != nil {
		t.Fatalf("Open over overlay: %v", err)
	}

	// (1) the brand-new consumer type enumerates.
	names, err := svc.ListSchemas()
	if err != nil {
		t.Fatalf("ListSchemas: %v", err)
	}
	if !contains(names, "kpi-input") {
		t.Errorf("overlay did not enumerate added type kpi-input; got %v", names)
	}
	// the core types still enumerate (overlay is additive over core).
	if !contains(names, "container") {
		t.Errorf("overlay dropped core type container; got %v", names)
	}

	// (2) the consumer's block overrides the core block (consumer wins by path).
	raw, err := svc.Schema("block")
	if err != nil {
		t.Fatalf("Schema(block): %v", err)
	}
	if !containsSub(raw, "OVERRIDDEN BY CONSUMER") {
		t.Errorf("Schema(block) returned core bytes, want the consumer override: %s", raw)
	}
}

// TestOverlayFromDirFS proves the documented downstream form — overlaying an
// on-disk custom-schemas directory over the embedded core — works through Open.
func TestOverlayFromDirFS(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "items"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "items", "kpi-input.schema.json"), []byte(kpiInputSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	svc, err := service.Open(service.Options{
		Backend: service.BackendFS,
		Root:    t.TempDir(),
		Schemas: service.OverlaySchemas(service.CoreSchemas(), os.DirFS(dir)),
	})
	if err != nil {
		t.Fatalf("Open over os.DirFS overlay: %v", err)
	}
	names, err := svc.ListSchemas()
	if err != nil {
		t.Fatalf("ListSchemas: %v", err)
	}
	if !contains(names, "kpi-input") || !contains(names, "container") {
		t.Errorf("DirFS overlay did not union custom + core; got %v", names)
	}
}

// TestBlockToneSurface proves the absorbed block `tone` enum: a vocabulary tone
// resolves, an out-of-vocabulary tone is rejected by config validation.
func TestBlockToneSurface(t *testing.T) {
	svc, err := service.Open(service.Options{Backend: service.BackendFS, Root: t.TempDir()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := svc.ResolveBytes([]byte(blockToneDoc("accent")), "tone-ok", nil); err != nil {
		t.Fatalf("ResolveBytes tone=accent: unexpected error: %v", err)
	}

	_, err = svc.ResolveBytes([]byte(blockToneDoc("bogus")), "tone-bad", nil)
	if err == nil {
		t.Fatalf("tone=bogus resolved; want config-invalid (enum rejected)")
	}
	if !errors.HasCode(err, errors.RESOLVE_CONFIG_INVALID) {
		t.Fatalf("tone=bogus: want RESOLVE_CONFIG_INVALID, got: %v", err)
	}
}

// contains reports whether s is in xs.
func contains(xs []string, s string) bool { return slices.Contains(xs, s) }
