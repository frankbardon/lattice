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
	if err := resolveConfigurators(root, nil); err != nil {
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
	err := resolveConfigurators(root, nil)
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
		err := resolveConfigurators(root, nil)
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
	if err := resolveConfigurators(root, nil); err != nil {
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
        "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
        "id": "body",
        "config": { "grid": { "columns": [1] } },
        "children": [
          {
            "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
            "config": {
              "id": "fruit-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
                "id": "fruit",
                "config": { "title": "Fruit", "columns": [{ "header": "Name" }] }
              }
            }
          },
          {
            "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
            "config": {
              "id": "cfg-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/configurator/1.0.0",
                "id": "cfg",
                "config": { "target": "fruit", "title": "Edit fruit" }
              }
            }
          }
        ]
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

	// Under the E3-S2 grammar the configurator is block-wrapped inside a body
	// region: root -> body region -> block[1] -> configurator.
	cfg := tree.Root.Children[0].Children[1].Children[0]
	if cfg.Type.Name != "configurator" {
		t.Fatalf("configurator type = %q, want configurator", cfg.Type.Name)
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
        "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
        "id": "body",
        "config": { "grid": { "columns": [1] } },
        "children": [
          {
            "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
            "config": {
              "id": "cfg-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/configurator/1.0.0",
                "id": "cfg",
                "config": { "target": "ghost" }
              }
            }
          }
        ]
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
	if err := resolveConfigurators(root, nil); err != nil {
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
	if err := resolveConfigurators(root, nil); err != nil {
		t.Fatalf("resolveConfigurators: %v", err)
	}
	if cfg.Generated == nil {
		t.Fatal("Generated is nil, want a present (empty) form")
	}
	if len(cfg.Generated.Widgets) != 0 {
		t.Errorf("Generated.Widgets = %d, want 0 for a surface-less target", len(cfg.Generated.Widgets))
	}
}

// TestResolveConfiguratorReservedTargets asserts every RESERVED document-scope
// keyword (E4-S1) routes to its scope and (E4-S2) generates a document-level form
// from that scope's SURFACE: a `$`-prefixed target resolves cleanly without any
// matching item id, the configurator carries a present GeneratedForm keyed by the
// reserved keyword, and the form's widgets are derived from the scope surface
// supplied to the pass. A scope absent from the surface map yields a present-but-
// empty form (mirroring a surface-less item). The id index here holds only
// ordinary items, so a clean resolve proves the reserved targets bypass the item
// lookup entirely.
func TestResolveConfiguratorReservedTargets(t *testing.T) {
	// A representative scope-surface map: $theme carries two enum tokens, the other
	// scopes are surface-less (absent) -> present-but-empty forms.
	scopeSurfaces := map[string][]ConfigurableField{
		"$theme": {
			{Field: "emphasis", Type: variables.VarTypeEnum, Label: "Emphasis", Rendering: "select",
				Constraints: map[string]any{"enum": []any{"none", "low", "high"}}},
			{Field: "spacing", Type: variables.VarTypeEnum, Label: "Spacing", Rendering: "select",
				Constraints: map[string]any{"enum": []any{"compact", "cosy", "roomy"}}},
		},
	}
	for _, target := range []string{"$manifest", "$variables", "$connections", "$theme", "$root"} {
		cfg := configuratorNode("cfg", target)
		root := containerNode(
			tableNode("revenue"),
			cfg,
		)
		if err := resolveConfigurators(root, scopeSurfaces); err != nil {
			t.Fatalf("target %q: resolveConfigurators: %v", target, err)
		}
		if cfg.Generated == nil {
			t.Fatalf("target %q: Generated is nil, want a present form routed to the scope", target)
		}
		if cfg.Generated.Target != target {
			t.Errorf("target %q: Generated.Target = %q, want the reserved keyword", target, cfg.Generated.Target)
		}
		wantWidgets := len(scopeSurfaces[target])
		if len(cfg.Generated.Widgets) != wantWidgets {
			t.Errorf("target %q: Generated.Widgets = %d, want %d (one per scope surface field)", target, len(cfg.Generated.Widgets), wantWidgets)
		}
	}
}

// TestResolveConfiguratorThemeScopeForm asserts the `$theme` scope generates an
// editor whose widgets are derived 1:1 from the theme scope surface (E4-S2): one
// widget per token, each using the surface's preferred rendering, carrying the
// `$theme.<token>` override binding and the token's constraint (option set), laid
// out via the shared form flow layout — the same generation path an item-targeting
// configurator uses.
func TestResolveConfiguratorThemeScopeForm(t *testing.T) {
	scopeSurfaces := map[string][]ConfigurableField{
		"$theme": {
			{Field: "emphasis", Type: variables.VarTypeEnum, Label: "Emphasis", Rendering: "select",
				Constraints: map[string]any{"enum": []any{"none", "low", "high"}}},
			{Field: "tone", Type: variables.VarTypeEnum, Label: "Tone", Rendering: "select",
				Constraints: map[string]any{"enum": []any{"neutral", "accent"}}},
		},
	}
	cfg := configuratorNode("cfg", "$theme")
	root := containerNode(tableNode("revenue"), cfg)
	if err := resolveConfigurators(root, scopeSurfaces); err != nil {
		t.Fatalf("resolveConfigurators: %v", err)
	}
	gen := cfg.Generated
	if gen == nil {
		t.Fatal("Generated is nil, want the $theme scope form")
	}
	if gen.Target != "$theme" {
		t.Errorf("Generated.Target = %q, want $theme", gen.Target)
	}
	if len(gen.Widgets) != 2 {
		t.Fatalf("generated %d widgets, want 2 (one per theme token)", len(gen.Widgets))
	}
	for i, want := range []string{"emphasis", "tone"} {
		w := gen.Widgets[i]
		if w.Field != want {
			t.Errorf("widget[%d].Field = %q, want %q", i, w.Field, want)
		}
		if w.Widget != "select" {
			t.Errorf("widget[%d].Widget = %q, want select (token enum rendering)", i, w.Widget)
		}
		if w.Target != "$theme" {
			t.Errorf("widget[%d].Target = %q, want $theme", i, w.Target)
		}
		if w.Constraints == nil {
			t.Errorf("widget[%d].Constraints is nil, want the token's option set", i)
		}
		// The override address a renderer posts is "$theme.<token>".
		addr := variables.OverrideTarget{Kind: variables.OverrideKindNodeField, Name: w.Target, Field: w.Field}.String()
		if want := "$theme." + w.Field; addr != want {
			t.Errorf("widget[%d] override address = %q, want %q", i, addr, want)
		}
	}
	if gen.Flow == nil {
		t.Fatal("Generated.Flow is nil, want the form flow layout")
	}
	if len(gen.Flow.Cells) != len(gen.Widgets) {
		t.Errorf("Flow has %d cells, want %d (one per widget)", len(gen.Flow.Cells), len(gen.Widgets))
	}
}

// TestResolveConfiguratorEmptyScopeForm asserts a scope with NO surface (absent
// from the scope-surface map) yields a present-but-EMPTY form (E4-S2) — a renderer
// can still distinguish a resolved configurator from an unresolved one, exactly as
// for a surface-less item target.
func TestResolveConfiguratorEmptyScopeForm(t *testing.T) {
	cfg := configuratorNode("cfg", "$root")
	root := containerNode(tableNode("revenue"), cfg)
	// $root is absent from the map -> surface-less scope.
	if err := resolveConfigurators(root, map[string][]ConfigurableField{}); err != nil {
		t.Fatalf("resolveConfigurators: %v", err)
	}
	if cfg.Generated == nil {
		t.Fatal("Generated is nil, want a present (empty) scope form")
	}
	if cfg.Generated.Target != "$root" {
		t.Errorf("Generated.Target = %q, want $root", cfg.Generated.Target)
	}
	if len(cfg.Generated.Widgets) != 0 {
		t.Errorf("Generated.Widgets = %d, want 0 for a surface-less scope", len(cfg.Generated.Widgets))
	}
}

// TestResolveConfiguratorReservedTargetUnknown asserts an unknown `$`-prefixed
// target fails fast with CONFIGURATOR_TARGET_SCOPE_UNKNOWN, naming the offending
// configurator path and the unknown scope keyword — it is NEVER reinterpreted as an
// item id (so it does not fall through to NOT_FOUND).
func TestResolveConfiguratorReservedTargetUnknown(t *testing.T) {
	cfg := configuratorNode("cfg", "$bogus")
	root := containerNode(
		tableNode("revenue"),
		cfg,
	)
	err := resolveConfigurators(root, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.HasCode(err, errors.CONFIGURATOR_TARGET_SCOPE_UNKNOWN) {
		t.Fatalf("error = %v, want code %s", err, errors.CONFIGURATOR_TARGET_SCOPE_UNKNOWN)
	}
	var ce *errors.CodedError
	if !asCoded(err, &ce) {
		t.Fatalf("error is not a CodedError: %v", err)
	}
	if got, _ := ce.Details["path"].(string); got != "root.children[1]" {
		t.Errorf("error path = %q, want %q", got, "root.children[1]")
	}
	if got, _ := ce.Details["target"].(string); got != "$bogus" {
		t.Errorf("error target = %q, want %q", got, "$bogus")
	}
	if cfg.Generated != nil {
		t.Errorf("Generated = %+v, want nil for an unknown reserved scope", cfg.Generated)
	}
}

// TestResolveConfiguratorReservedDoesNotMatchItem asserts collision avoidance from
// the SCOPE side: even when an item literally carries the id "$theme", a
// configurator targeting "$theme" routes to the document THEME scope (an empty
// scope form), not the item — the `$` sigil is decisive, so the id index is never
// consulted for a reserved target.
func TestResolveConfiguratorReservedDoesNotMatchItem(t *testing.T) {
	collider := surfacedTableNode("$theme") // an item literally named like the keyword
	cfg := configuratorNode("cfg", "$theme")
	root := containerNode(
		collider,
		cfg,
	)
	if err := resolveConfigurators(root, nil); err != nil {
		t.Fatalf("resolveConfigurators: %v", err)
	}
	if cfg.Generated == nil {
		t.Fatal("Generated is nil, want a present scope form")
	}
	// Routed to the scope, NOT the colliding item: the item has 3 surface fields, so
	// an item-routed form would have 3 widgets. The scope form is empty (E4-S2).
	if len(cfg.Generated.Widgets) != 0 {
		t.Errorf("Generated.Widgets = %d, want 0 (routed to scope, not the colliding item)", len(cfg.Generated.Widgets))
	}
}

// TestResolveConfiguratorItemNamedLikeKeywordNoSigil asserts collision avoidance
// from the ITEM side: an item whose id is the bare word "theme" (no `$` sigil) is
// targeted as an ordinary item id and resolves to that item — the reserved-scope
// routing only ever triggers on the `$`-prefixed form, so ordinary item-id
// targeting is completely unaffected.
func TestResolveConfiguratorItemNamedLikeKeywordNoSigil(t *testing.T) {
	item := surfacedTableNode("theme") // bare "theme", an ordinary id
	cfg := configuratorNode("cfg", "theme")
	root := containerNode(
		item,
		cfg,
	)
	if err := resolveConfigurators(root, nil); err != nil {
		t.Fatalf("resolveConfigurators: %v", err)
	}
	if cfg.Generated == nil {
		t.Fatal("Generated is nil, want a form generated from the targeted item")
	}
	// Routed to the ITEM: the item-routed form mirrors the item's 3-field surface.
	if len(cfg.Generated.Widgets) != 3 {
		t.Errorf("Generated.Widgets = %d, want 3 (item-id targeting unaffected)", len(cfg.Generated.Widgets))
	}
	if cfg.Generated.Target != "theme" {
		t.Errorf("Generated.Target = %q, want %q", cfg.Generated.Target, "theme")
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
        "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
        "id": "body",
        "config": { "grid": { "columns": [1] } },
        "children": [
          {
            "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
            "config": {
              "id": "tbl-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
                "id": "tbl",
                "config": { "title": "Fruit", "columns": [{ "header": "Name" }] }
              }
            }
          },
          {
            "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
            "config": {
              "id": "cfg-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/configurator/1.0.0",
                "id": "cfg",
                "config": { "target": "tbl", "title": "Edit table" }
              }
            }
          }
        ]
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

	// Under the E3-S2 grammar the configurator is block-wrapped inside a body
	// region: root -> body region -> block[1] -> configurator.
	cfg := tree.Root.Children[0].Children[1].Children[0]
	if cfg.Type.Name != "configurator" {
		t.Fatalf("configurator type = %q, want configurator", cfg.Type.Name)
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
		if !res.cat.WidgetNames()[w.Widget] {
			t.Errorf("widget[%d].Widget = %q does not declare the widget role", i, w.Widget)
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
	// The table sits at: root -> body region -> block[0] -> table.
	if got, _ := base.Root.Children[0].Children[0].Children[0].Config["title"].(string); got != "Fruit" {
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
	if title, _ := got.Root.Children[0].Children[0].Children[0].Config["title"].(string); title != "Citrus" {
		t.Fatalf("overridden table title = %q, want Citrus", title)
	}

	// The mutation is ephemeral: the on-disk document is untouched, so a fresh
	// resolve still sees the authored title.
	fresh, err := res.Resolve(path)
	if err != nil {
		t.Fatalf("Resolve after override: %v", err)
	}
	if title, _ := fresh.Root.Children[0].Children[0].Children[0].Config["title"].(string); title != "Fruit" {
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

// configuratorThemeScopeDoc is a dashboard whose body block wraps a configurator
// targeting the reserved `$theme` document scope (E4-S2). The configurator's
// generated form must be derived from the `$theme` scope surface declared on the
// document schema (the six theme tokens), end to end through the real catalog.
const configuratorThemeScopeDoc = `{
  "manifest": { "formatVersion": "1.0.0", "id": "theme-configurator", "title": "Theme Configurator" },
  "theme": { "emphasis": "high", "spacing": "cosy" },
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
              "id": "theme-cfg-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/configurator/1.0.0",
                "id": "theme-cfg",
                "config": { "target": "$theme", "title": "Edit theme" }
              }
            }
          }
        ]
      }
    ]
  }
}`

// TestConfiguratorThemeScopeThroughCatalog resolves configuratorThemeScopeDoc
// against the real schema catalog and asserts the configurator's generated form is
// derived from the `$theme` document-scope surface declared on the dashboard
// schema: one widget per theme token, in surface (sorted) order, each binding the
// "$theme.<token>" override address and rendered with the token's preferred widget.
// This is the E4-S2 acceptance criterion "form generation for $theme — widgets
// derived from the theme surface".
func TestConfiguratorThemeScopeThroughCatalog(t *testing.T) {
	res := newRepoResolver(t)
	path := filepath.Join(t.TempDir(), "theme-configurator.json")
	if err := os.WriteFile(path, []byte(configuratorThemeScopeDoc), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}

	tree, err := res.Resolve(path)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// root -> body region -> block[0] -> configurator.
	cfg := tree.Root.Children[0].Children[0].Children[0]
	if cfg.Type.Name != "configurator" {
		t.Fatalf("configurator type = %q, want configurator", cfg.Type.Name)
	}
	gen := cfg.Generated
	if gen == nil {
		t.Fatal("configurator.Generated is nil, want the $theme scope form")
	}
	if gen.Target != "$theme" {
		t.Errorf("Generated.Target = %q, want $theme", gen.Target)
	}

	// The six base theme tokens come back sorted.
	wantTokens := []string{"border", "density", "emphasis", "radius", "spacing", "tone"}
	if len(gen.Widgets) != len(wantTokens) {
		t.Fatalf("generated %d widgets, want %d (theme tokens)", len(gen.Widgets), len(wantTokens))
	}
	for i, want := range wantTokens {
		w := gen.Widgets[i]
		if w.Field != want {
			t.Errorf("widget[%d].Field = %q, want %q", i, w.Field, want)
		}
		if w.Type != variables.VarTypeEnum {
			t.Errorf("widget[%d].Type = %q, want enum", i, w.Type)
		}
		if w.Target != "$theme" {
			t.Errorf("widget[%d].Target = %q, want $theme", i, w.Target)
		}
		if !res.cat.WidgetNames()[w.Widget] {
			t.Errorf("widget[%d].Widget = %q does not declare the widget role", i, w.Widget)
		}
		// Each token carries its option set in constraints (the guardrail enumerating
		// the legal values for that scope field).
		if w.Constraints == nil {
			t.Errorf("widget[%d].Constraints is nil, want the token option set", i)
		}
	}
}

// TestConfiguratorScopeNoChangeApplied asserts the document-scope configurator is
// GENERATION ONLY: resolving a document with a `$theme`-targeting configurator
// leaves the document's own scopes untouched — the resolved default theme still
// reflects the authored tokens and the on-disk document is unchanged. The
// resolver applies no change from a scope surface or its generated form.
func TestConfiguratorScopeNoChangeApplied(t *testing.T) {
	res := newRepoResolver(t)
	path := filepath.Join(t.TempDir(), "theme-configurator.json")
	if err := os.WriteFile(path, []byte(configuratorThemeScopeDoc), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}

	tree, err := res.Resolve(path)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// The default theme is passed through verbatim — generating the scope editor
	// did NOT mutate the document's authored theme.
	if got, _ := tree.DefaultTheme["emphasis"].(string); got != "high" {
		t.Errorf("DefaultTheme.emphasis = %q, want high (untouched by scope generation)", got)
	}
	if got, _ := tree.DefaultTheme["spacing"].(string); got != "cosy" {
		t.Errorf("DefaultTheme.spacing = %q, want cosy (untouched by scope generation)", got)
	}
	// The manifest is passed through verbatim too.
	if got, _ := tree.Manifest["title"].(string); got != "Theme Configurator" {
		t.Errorf("Manifest.title = %q, want unchanged", got)
	}

	// The on-disk document is untouched.
	on, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read doc: %v", err)
	}
	if string(on) != configuratorThemeScopeDoc {
		t.Errorf("on-disk document changed after scope generation; want it untouched")
	}
}
