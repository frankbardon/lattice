package resolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/frankbardon/lattice/internal/variables"
)

// This file asserts the CONCRETE configurable surfaces declared by the two
// meatiest shipped item types (E3-S2): `container` (its relative-weight grid) and
// `table` (its title, columns, and query parameters). Unlike surface_test.go —
// which exercises the surface MECHANISM against a synthetic item type — these
// tests resolve a real document through the repo's on-disk schema catalog (the
// exact schemas the binary ships) and assert the surface the resolver exposes on
// the resolved instance, so a drift between the schema's `configurable` block and
// the contract downstream epics (E4 overrides, E5 configurator) read is caught.

// surfaceDoc is a minimal dashboard whose root container holds one table. Both
// types declare a configurable surface, so resolving it exposes both surfaces on
// the tree. The table carries a connectionId/query so its query surface refers to
// a genuinely-bindable field, matching the honest-surface acceptance criterion.
const surfaceDoc = `{
  "manifest": { "formatVersion": "1.0.0", "id": "surface-example", "title": "Surface Example" },
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": { "grid": { "columns": [1] } },
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
        "id": "tbl",
        "config": {
          "title": "Fruit",
          "connectionId": "inline",
          "columns": [{ "header": "Name" }]
        }
      }
    ]
  },
  "connections": [
    {
      "id": "inline",
      "$ref": "https://lattice.dev/schemas/connections/static/1.0.0",
      "config": { "columns": ["name"], "rows": [{ "name": "apple" }] }
    }
  ]
}`

// resolveSurfaceDoc resolves surfaceDoc against the real schema catalog and
// returns the resolved root container and its single table child.
func resolveSurfaceDoc(t *testing.T) (container, table *ResolvedInstance) {
	t.Helper()
	res := newRepoResolver(t)

	path := filepath.Join(t.TempDir(), "surface-dashboard.json")
	if err := os.WriteFile(path, []byte(surfaceDoc), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	tree, err := res.Resolve(path)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	container = tree.Root
	if container.Type.Name != "container" {
		t.Fatalf("root type = %q, want container", container.Type.Name)
	}
	if len(container.Children) != 1 {
		t.Fatalf("root children = %d, want 1", len(container.Children))
	}
	table = container.Children[0]
	if table.Type.Name != "table" {
		t.Fatalf("child type = %q, want table", table.Type.Name)
	}
	return container, table
}

// fieldByName returns the configurable-surface entry for the named field, failing
// the test when absent — every assertion below names a real config property.
func fieldByName(t *testing.T, surface []ConfigurableField, name string) ConfigurableField {
	t.Helper()
	for _, f := range surface {
		if f.Field == name {
			return f
		}
	}
	t.Fatalf("surface has no field %q (got %d fields)", name, len(surface))
	return ConfigurableField{}
}

// TestContainerSurface asserts the container item type exposes its grid as a
// runtime-configurable field with the expected type, label, and constraints.
func TestContainerSurface(t *testing.T) {
	container, _ := resolveSurfaceDoc(t)

	if len(container.Surface) != 1 {
		t.Fatalf("container.Surface has %d fields, want 1 (grid)", len(container.Surface))
	}
	grid := fieldByName(t, container.Surface, "grid")
	if grid.Type != variables.VarTypeArray {
		t.Errorf("grid.Type = %q, want %q", grid.Type, variables.VarTypeArray)
	}
	if grid.Label == "" {
		t.Errorf("grid.Label is empty, want a human label")
	}
	if grid.Constraints == nil {
		t.Errorf("grid.Constraints is nil, want the declared grid-field constraints")
	} else if _, ok := grid.Constraints["fields"]; !ok {
		t.Errorf("grid.Constraints missing %q describing the columns/rows/gap sub-fields", "fields")
	}
}

// TestTableSurface asserts the table item type exposes its title, columns, and
// query as runtime-configurable fields with the expected types and rendering
// hints. Each surfaced field is a real top-level config property of the table.
func TestTableSurface(t *testing.T) {
	_, table := resolveSurfaceDoc(t)

	if len(table.Surface) != 3 {
		t.Fatalf("table.Surface has %d fields, want 3 (columns, query, title)", len(table.Surface))
	}

	// Surface comes back in sorted field order: columns, query, title.
	wantOrder := []string{"columns", "query", "title"}
	for i, want := range wantOrder {
		if got := table.Surface[i].Field; got != want {
			t.Fatalf("table.Surface[%d].Field = %q, want %q", i, got, want)
		}
	}

	title := fieldByName(t, table.Surface, "title")
	if title.Type != variables.VarTypeString {
		t.Errorf("title.Type = %q, want %q", title.Type, variables.VarTypeString)
	}
	if title.Rendering != "text-input" {
		t.Errorf("title.Rendering = %q, want a text-input rendering hint", title.Rendering)
	}

	columns := fieldByName(t, table.Surface, "columns")
	if columns.Type != variables.VarTypeArray {
		t.Errorf("columns.Type = %q, want %q", columns.Type, variables.VarTypeArray)
	}
	if columns.Constraints == nil {
		t.Errorf("columns.Constraints is nil, want the declared column-shape constraints")
	}

	query := fieldByName(t, table.Surface, "query")
	if query.Type != variables.VarTypeArray {
		t.Errorf("query.Type = %q, want %q", query.Type, variables.VarTypeArray)
	}
}
