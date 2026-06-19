package resolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/variables"
)

// This file exercises the E5-S1 configurator target-resolution pass two ways:
//
//   - resolveConfigurators directly, over hand-built resolved trees, to cover the
//     MECHANISM (id index, valid target, NOT_FOUND, MISSING_ID) without the schema
//     catalog; and
//   - the full resolver over the real on-disk schema catalog, to confirm the
//     `configurator` item-type auto-registers, its `target` is required, and a
//     dangling target surfaces CONFIGURATOR_TARGET_NOT_FOUND end to end.

// configuratorNode builds a resolved configurator node with the given target.
func configuratorNode(id, target string) *ResolvedInstance {
	return &ResolvedInstance{
		ID:     id,
		Type:   ResolvedTypeRef{ID: "https://lattice.dev/schemas/items/configurator/1.0.0", Name: "configurator"},
		Config: map[string]any{"target": target},
	}
}

// tableNode builds a resolved leaf node carrying a stable id, a valid target.
func tableNode(id string) *ResolvedInstance {
	return &ResolvedInstance{
		ID:   id,
		Type: ResolvedTypeRef{ID: "https://lattice.dev/schemas/items/table/1.0.0", Name: "table"},
	}
}

// containerNode builds a resolved container holding the given children.
func containerNode(children ...*ResolvedInstance) *ResolvedInstance {
	return &ResolvedInstance{
		Type:      ResolvedTypeRef{ID: "https://lattice.dev/schemas/items/container/1.0.0", Name: "container"},
		Container: true,
		Children:  children,
	}
}

// TestResolveConfiguratorValid asserts a configurator whose target names an
// id-carrying sibling resolves cleanly (no error). The id index is built from the
// whole tree, so a target declared anywhere in the document is reachable.
func TestResolveConfiguratorValid(t *testing.T) {
	root := containerNode(
		tableNode("revenue"),
		configuratorNode("cfg", "revenue"),
	)
	if err := resolveConfigurators(root); err != nil {
		t.Fatalf("resolveConfigurators: %v", err)
	}
}

// TestResolveConfiguratorNotFound asserts a configurator whose target names an id
// no item declares fails fast with CONFIGURATOR_TARGET_NOT_FOUND, reporting the
// configurator path and the unresolved target.
func TestResolveConfiguratorNotFound(t *testing.T) {
	root := containerNode(
		tableNode("revenue"),
		configuratorNode("cfg", "ghost"),
	)
	err := resolveConfigurators(root)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.HasCode(err, errors.CONFIGURATOR_TARGET_NOT_FOUND) {
		t.Fatalf("error = %v, want code %s", err, errors.CONFIGURATOR_TARGET_NOT_FOUND)
	}
	var ce *errors.CodedError
	if !asCoded(err, &ce) {
		t.Fatalf("error is not a CodedError: %v", err)
	}
	if got, _ := ce.Details["path"].(string); got != "root.children[1]" {
		t.Errorf("error path = %q, want %q", got, "root.children[1]")
	}
	if got, _ := ce.Details["target"].(string); got != "ghost" {
		t.Errorf("error target = %q, want %q", got, "ghost")
	}
}

// TestResolveConfiguratorMissingID asserts a configurator whose target is empty or
// whitespace-only fails fast with CONFIGURATOR_TARGET_MISSING_ID — the reference
// carries no stable id to look up. (The item-type schema's minLength rejects "" at
// the structural pass; this guard also catches a whitespace-only value.)
func TestResolveConfiguratorMissingID(t *testing.T) {
	for _, target := range []string{"", "   "} {
		root := containerNode(
			tableNode("revenue"),
			configuratorNode("cfg", target),
		)
		err := resolveConfigurators(root)
		if err == nil {
			t.Fatalf("target %q: expected error, got nil", target)
		}
		if !errors.HasCode(err, errors.CONFIGURATOR_TARGET_MISSING_ID) {
			t.Fatalf("target %q: error = %v, want code %s", target, err, errors.CONFIGURATOR_TARGET_MISSING_ID)
		}
		var ce *errors.CodedError
		if !asCoded(err, &ce) {
			t.Fatalf("target %q: error is not a CodedError: %v", target, err)
		}
		if got, _ := ce.Details["path"].(string); got != "root.children[1]" {
			t.Errorf("target %q: error path = %q, want %q", target, got, "root.children[1]")
		}
	}
}

// TestResolveConfiguratorTargetsContainer asserts a configurator may target a
// container (any id-carrying node, not only leaves), since the id index spans the
// whole tree.
func TestResolveConfiguratorTargetsContainer(t *testing.T) {
	target := containerNode(tableNode("inner"))
	target.ID = "panel"
	root := containerNode(
		target,
		configuratorNode("cfg", "panel"),
	)
	if err := resolveConfigurators(root); err != nil {
		t.Fatalf("resolveConfigurators: %v", err)
	}
}

// configuratorValidDoc is a minimal dashboard whose container holds a table (with
// a stable id) and a configurator targeting it — the happy path resolved through
// the real schema catalog.
const configuratorValidDoc = `{
  "manifest": { "formatVersion": "1.0.0", "id": "configurator-example", "title": "Configurator Example" },
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": { "grid": { "columns": [1] } },
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
        "id": "fruit",
        "config": { "title": "Fruit", "columns": [{ "header": "Name" }] }
      },
      {
        "$ref": "https://lattice.dev/schemas/items/configurator/1.0.0",
        "id": "cfg",
        "config": { "target": "fruit", "title": "Edit fruit" }
      }
    ]
  }
}`

// TestConfiguratorResolvesThroughCatalog resolves configuratorValidDoc against the
// real on-disk schema catalog, confirming the configurator item-type auto-registers
// (drop-in file), validates its config, declares its own configurable surface
// (the `title` field), and that a valid target resolves end to end.
func TestConfiguratorResolvesThroughCatalog(t *testing.T) {
	res := newRepoResolver(t)
	path := filepath.Join(t.TempDir(), "configurator-dashboard.json")
	if err := os.WriteFile(path, []byte(configuratorValidDoc), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}

	tree, err := res.Resolve(path)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	cfg := tree.Root.Children[1]
	if cfg.Type.Name != "configurator" {
		t.Fatalf("child[1].Type.Name = %q, want configurator", cfg.Type.Name)
	}
	// The configurator declares its OWN configurable surface (the title field),
	// completing the "every item-type fully specified" requirement.
	if len(cfg.Surface) != 1 || cfg.Surface[0].Field != "title" {
		t.Fatalf("configurator surface = %+v, want one field \"title\"", cfg.Surface)
	}
}

// TestConfiguratorTargetNotFoundThroughCatalog resolves a document whose
// configurator targets an id no item declares, asserting the end-to-end resolve
// fails fast with CONFIGURATOR_TARGET_NOT_FOUND.
func TestConfiguratorTargetNotFoundThroughCatalog(t *testing.T) {
	doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "configurator-bad", "title": "Configurator Bad" },
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": { "grid": { "columns": [1] } },
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/configurator/1.0.0",
        "id": "cfg",
        "config": { "target": "ghost" }
      }
    ]
  }
}`
	res := newRepoResolver(t)
	path := filepath.Join(t.TempDir(), "configurator-bad.json")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}

	_, err := res.Resolve(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.HasCode(err, errors.CONFIGURATOR_TARGET_NOT_FOUND) {
		t.Fatalf("error = %v, want code %s", err, errors.CONFIGURATOR_TARGET_NOT_FOUND)
	}
}

// TestConfiguratorMissingTargetThroughCatalog asserts the item-type schema makes
// `target` required: a configurator with no target fails the structural config
// pass (RESOLVE_CONFIG_INVALID) before target resolution runs.
func TestConfiguratorMissingTargetThroughCatalog(t *testing.T) {
	doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "configurator-no-target", "title": "Configurator No Target" },
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": { "grid": { "columns": [1] } },
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/configurator/1.0.0",
        "id": "cfg",
        "config": { "title": "no target here" }
      }
    ]
  }
}`
	res := newRepoResolver(t)
	path := filepath.Join(t.TempDir(), "configurator-no-target.json")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}

	_, err := res.Resolve(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.HasCode(err, errors.RESOLVE_CONFIG_INVALID) {
		t.Fatalf("error = %v, want code %s", err, errors.RESOLVE_CONFIG_INVALID)
	}
}

// surfacedTableNode builds a resolved table node carrying a stable id and a
// representative configurable surface: a string field with a preferred rendering,
// an enum field with options, and an array field with no rendering hint. It lets
// the generation tests assert preferred-rendering, canonical-fallback, binding,
// and constraint pass-through without the schema catalog.
func surfacedTableNode(id string) *ResolvedInstance {
	n := tableNode(id)
	n.Surface = []ConfigurableField{
		{Field: "title", Type: variables.VarTypeString, Label: "Title", Rendering: "text-input"},
		{Field: "sort", Type: variables.VarTypeEnum, Label: "Sort",
			Constraints: map[string]any{"enum": []any{"asc", "desc"}}},
		{Field: "columns", Type: variables.VarTypeArray, Label: "Columns",
			Constraints: map[string]any{"items": map[string]any{}}},
	}
	return n
}

// TestConfiguratorGeneratesForm asserts a resolved configurator produces a form
// whose widgets correspond 1:1 to the target's configurable surface, each using
// the field's preferred rendering (else the canonical widget for its type),
// carrying the <target>.<field> override binding and the field's constraints, and
// laid out via the form flow layout.
func TestConfiguratorGeneratesForm(t *testing.T) {
	cfg := configuratorNode("cfg", "revenue")
	root := containerNode(
		surfacedTableNode("revenue"),
		cfg,
	)
	if err := resolveConfigurators(root); err != nil {
		t.Fatalf("resolveConfigurators: %v", err)
	}

	gen := cfg.Generated
	if gen == nil {
		t.Fatal("configurator.Generated is nil, want a generated form")
	}
	if gen.Target != "revenue" {
		t.Errorf("Generated.Target = %q, want %q", gen.Target, "revenue")
	}

	// 1:1 with the surface, in surface order.
	if len(gen.Widgets) != 3 {
		t.Fatalf("generated %d widgets, want 3 (one per surface field)", len(gen.Widgets))
	}

	cases := []struct {
		field      string
		widget     string // expected widget item-type
		typ        variables.VarType
		label      string
		wantConstr bool
	}{
		// title: declares a preferred rendering -> use it.
		{"title", "text-input", variables.VarTypeString, "Title", false},
		// sort: no rendering -> canonical enum widget is select.
		{"sort", "select", variables.VarTypeEnum, "Sort", true},
		// columns: no rendering -> canonical array widget is multiselect.
		{"columns", "multiselect", variables.VarTypeArray, "Columns", true},
	}
	for i, c := range cases {
		w := gen.Widgets[i]
		if w.Field != c.field {
			t.Errorf("widget[%d].Field = %q, want %q", i, w.Field, c.field)
		}
		if w.Widget != c.widget {
			t.Errorf("widget[%d].Widget = %q, want %q", i, w.Widget, c.widget)
		}
		if w.Type != c.typ {
			t.Errorf("widget[%d].Type = %q, want %q", i, w.Type, c.typ)
		}
		if w.Label != c.label {
			t.Errorf("widget[%d].Label = %q, want %q", i, w.Label, c.label)
		}
		// Every widget binds the <target>.<field> override address.
		if w.Target != "revenue" {
			t.Errorf("widget[%d].Target = %q, want %q", i, w.Target, "revenue")
		}
		if (w.Constraints != nil) != c.wantConstr {
			t.Errorf("widget[%d].Constraints present = %v, want %v", i, w.Constraints != nil, c.wantConstr)
		}
		// The override address a renderer posts is "<target>.<field>".
		addr := variables.OverrideTarget{Kind: variables.OverrideKindNodeField, Name: w.Target, Field: w.Field}.String()
		if want := "revenue." + c.field; addr != want {
			t.Errorf("widget[%d] override address = %q, want %q", i, addr, want)
		}
	}

	// The form is laid out via the flow layout: one cell per widget, default
	// single column.
	if gen.Flow == nil {
		t.Fatal("Generated.Flow is nil, want the form flow layout")
	}
	if gen.Flow.Mode != "flow" {
		t.Errorf("Flow.Mode = %q, want flow", gen.Flow.Mode)
	}
	if len(gen.Flow.Cells) != len(gen.Widgets) {
		t.Errorf("Flow has %d cells, want %d (one per widget)", len(gen.Flow.Cells), len(gen.Widgets))
	}
}

// TestConfiguratorTargetsSurfacelessItem asserts a configurator may target an item
// that declares no configurable surface: the generated form is present but empty,
// so a renderer can still distinguish a resolved configurator from an unresolved
// one.
func TestConfiguratorTargetsSurfacelessItem(t *testing.T) {
	cfg := configuratorNode("cfg", "plain")
	root := containerNode(
		tableNode("plain"), // no Surface set
		cfg,
	)
	if err := resolveConfigurators(root); err != nil {
		t.Fatalf("resolveConfigurators: %v", err)
	}
	if cfg.Generated == nil {
		t.Fatal("Generated is nil, want a present (empty) form")
	}
	if len(cfg.Generated.Widgets) != 0 {
		t.Errorf("Generated.Widgets = %d, want 0 for a surface-less target", len(cfg.Generated.Widgets))
	}
}

// configuratorTableDoc is a dashboard whose container holds a connection-backed
// table (with title/columns/query surface) and a configurator targeting it — the
// end-to-end E5-S2 fixture: the configurator's generated form must mirror the
// table's surface, and a config override against a generated field must re-resolve
// the table ephemerally.
const configuratorTableDoc = `{
  "manifest": { "formatVersion": "1.0.0", "id": "configurator-table", "title": "Configurator over Table" },
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": { "grid": { "columns": [1] } },
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
        "id": "tbl",
        "config": { "title": "Fruit", "columns": [{ "header": "Name" }] }
      },
      {
        "$ref": "https://lattice.dev/schemas/items/configurator/1.0.0",
        "id": "cfg",
        "config": { "target": "tbl", "title": "Edit table" }
      }
    ]
  }
}`

// TestConfiguratorOverTableThroughCatalog resolves configuratorTableDoc against the
// real schema catalog and asserts the configurator's generated form matches the
// table's surface 1:1 (columns, query, title — in surface order), each generated
// widget binding the "tbl.<field>" config-override address. This is the
// acceptance criterion "a configurator targeting a table yields widgets matching
// its surface".
func TestConfiguratorOverTableThroughCatalog(t *testing.T) {
	res := newRepoResolver(t)
	path := filepath.Join(t.TempDir(), "configurator-table.json")
	if err := os.WriteFile(path, []byte(configuratorTableDoc), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}

	tree, err := res.Resolve(path)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	cfg := tree.Root.Children[1]
	if cfg.Type.Name != "configurator" {
		t.Fatalf("child[1] type = %q, want configurator", cfg.Type.Name)
	}
	gen := cfg.Generated
	if gen == nil {
		t.Fatal("configurator.Generated is nil, want a generated form")
	}
	if gen.Target != "tbl" {
		t.Errorf("Generated.Target = %q, want tbl", gen.Target)
	}

	// The table's surface comes back sorted: columns, query, title.
	wantFields := []string{"columns", "query", "title"}
	if len(gen.Widgets) != len(wantFields) {
		t.Fatalf("generated %d widgets, want %d (table surface)", len(gen.Widgets), len(wantFields))
	}
	for i, want := range wantFields {
		w := gen.Widgets[i]
		if w.Field != want {
			t.Errorf("widget[%d].Field = %q, want %q", i, w.Field, want)
		}
		if w.Target != "tbl" {
			t.Errorf("widget[%d].Target = %q, want tbl", i, w.Target)
		}
		if _, ok := widgetFamilies[w.Widget]; !ok {
			t.Errorf("widget[%d].Widget = %q is not a registered widget family", i, w.Widget)
		}
	}

	// title declares a text-input rendering hint; the generated widget honors it.
	for _, w := range gen.Widgets {
		if w.Field == "title" && w.Widget != "text-input" {
			t.Errorf("title widget = %q, want text-input (preferred rendering)", w.Widget)
		}
	}
}

// TestConfiguratorGeneratedOverrideReResolves asserts that posting a config
// override against a generated widget's field ("<target-id>.<field>") re-resolves
// the TARGET ephemerally: the override flows through the same E4-S2 path the
// generated widget binds, mutating the target's config in the resolved tree while
// leaving the on-disk document untouched.
func TestConfiguratorGeneratedOverrideReResolves(t *testing.T) {
	res := newRepoResolver(t)
	path := filepath.Join(t.TempDir(), "configurator-table.json")
	if err := os.WriteFile(path, []byte(configuratorTableDoc), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}

	// Baseline: the table renders its authored title.
	base, err := res.Resolve(path)
	if err != nil {
		t.Fatalf("Resolve baseline: %v", err)
	}
	if got, _ := base.Root.Children[0].Config["title"].(string); got != "Fruit" {
		t.Fatalf("baseline table title = %q, want Fruit", got)
	}

	// Re-resolve with the override the generated `title` widget posts:
	// "tbl.title" -> "Citrus". This is exactly the address encoded on the
	// generated widget (Target "tbl", Field "title").
	overrides := variables.OverrideSet{"tbl.title": "Citrus"}
	got, err := res.ResolveWithValues(path, overrides)
	if err != nil {
		t.Fatalf("ResolveWithValues: %v", err)
	}
	if title, _ := got.Root.Children[0].Config["title"].(string); title != "Citrus" {
		t.Fatalf("overridden table title = %q, want Citrus", title)
	}

	// The mutation is ephemeral: the on-disk document is untouched, so a fresh
	// resolve still sees the authored title.
	fresh, err := res.Resolve(path)
	if err != nil {
		t.Fatalf("Resolve after override: %v", err)
	}
	if title, _ := fresh.Root.Children[0].Config["title"].(string); title != "Fruit" {
		t.Errorf("table title after ephemeral override = %q, want Fruit (document untouched)", title)
	}
	on, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read doc: %v", err)
	}
	if string(on) != configuratorTableDoc {
		t.Errorf("on-disk document changed after override; want it untouched")
	}
}
