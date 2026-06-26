package mcp

// Shared fixtures + helpers for the typed-handler / descriptor tests. The tools
// migrated into this package are exercised WITHOUT a server: tests call the typed
// handler (e.g. getOutline) directly, or drive the ToolDescriptor.Invoke produced
// by Tools(cfg), over a *service.Service built on an in-memory store. This keeps
// the SDK out of the core's test set, preserving the import firewall.

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/service"
)

// fixtureID is the manifest.id of the seeded minimal example document.
const fixtureID = "example-minimal"

// newTestService builds the service over an in-memory store seeded with the
// minimal example document and the repo's real schema catalog.
func newTestService(t *testing.T) *service.Service {
	t.Helper()

	store, err := service.NewStore(service.BackendFS, afero.NewMemMapFs(), "docs")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	doc, err := os.ReadFile("../examples/minimal-dashboard.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := store.Save(doc); err != nil {
		t.Fatalf("store.Save: %v", err)
	}
	res, err := service.NewResolver(os.DirFS("../schemas"))
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	return service.New(store, res)
}

// metadataFixtureID is the manifest.id of the metadata-carrying fixture seeded by
// newMetadataService.
const metadataFixtureID = "example-metadata"

// metadataFixtureDoc is a legal dashboard that attaches freeform element metadata
// (element-metadata) to two of the eligible node kinds — the document root
// container and a block wrapper — while leaving the body container and the bare
// table leaves metadata-free. It exercises the outline's per-node metadata
// exposure (present where carried, omitted where absent).
const metadataFixtureDoc = `{
  "manifest": {
    "formatVersion": "1.0.0",
    "id": "example-metadata",
    "title": "Metadata Example Dashboard",
    "author": "Lattice"
  },
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": { "grid": { "columns": [1] } },
    "metadata": { "owner": "platform-team", "revision": 7 },
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
        "id": "body",
        "config": { "grid": { "columns": [1, 1], "rows": [1], "gap": 1 } },
        "children": [
          {
            "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
            "id": "fruits-block",
            "placement": { "colStart": 1, "colSpan": 1, "rowStart": 1, "rowSpan": 1 },
            "metadata": { "source": "produce-api" },
            "config": {
              "id": "fruits-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
                "id": "fruits",
                "config": {
                  "title": "Fruits",
                  "columns": [ { "header": "Name" } ],
                  "rows": [ ["Apple"] ]
                }
              }
            }
          },
          {
            "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
            "id": "metrics-block",
            "placement": { "colStart": 2, "colSpan": 1, "rowStart": 1, "rowSpan": 1 },
            "config": {
              "id": "metrics-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
                "id": "metrics",
                "config": {
                  "title": "Metrics",
                  "columns": [ { "header": "Metric" } ],
                  "rows": [ ["Requests"] ]
                }
              }
            }
          }
        ]
      }
    ]
  }
}`

// newMetadataService builds the service over an in-memory store seeded with
// metadataFixtureDoc (rather than the minimal example).
func newMetadataService(t *testing.T) *service.Service {
	t.Helper()

	store, err := service.NewStore(service.BackendFS, afero.NewMemMapFs(), "docs")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Save([]byte(metadataFixtureDoc)); err != nil {
		t.Fatalf("store.Save: %v", err)
	}
	res, err := service.NewResolver(os.DirFS("../schemas"))
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	return service.New(store, res)
}

// findDescriptor returns the ToolDescriptor with the given name from the catalog
// Tools builds, failing the test if it is absent. It lets a test exercise the
// type-erased Invoke path (the shape a transport adapter dispatches) in addition
// to the typed handler.
func findDescriptor(t *testing.T, name string) ToolDescriptor {
	t.Helper()
	for _, d := range Tools(Config{Version: "test"}) {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("tool %q not present in Tools() catalog", name)
	return ToolDescriptor{}
}

// remarshal round-trips v through JSON into target, so a test can assert against
// the wire shape an Invoke result (returned as `any`) serializes to.
func remarshal(t *testing.T, v any, target any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := json.Unmarshal(b, target); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}
