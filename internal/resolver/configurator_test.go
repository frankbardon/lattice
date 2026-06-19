package resolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/frankbardon/lattice/errors"
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
