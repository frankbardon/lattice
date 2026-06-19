package resolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/variables"
)

// This file exercises the E4-S2 config-override pass end to end against the real
// on-disk schema catalog: a node+field override ("<node-id>.<field>") carried by
// the unified OverrideSet (E4-S1) mutates a resolved instance's config, validated
// against that item type's configurable surface (E3), wins over an interpolated
// value (precedence), and never touches the document on disk (ephemerality).

// overrideDoc is a minimal dashboard whose root container holds one table with an
// id ("tbl"). The table's `title` field is exposed by the table surface (string,
// rendered text-input) and is interpolated from the doc-scope "region" variable —
// so the same field exercises both interpolation and override precedence.
const overrideDoc = `{
  "manifest": {"formatVersion": "1.0.0", "id": "ov", "title": "Override"},
  "variables": [{"name": "region", "type": "string", "default": "us"}],
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
              "id": "tbl-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
                "id": "tbl",
                "config": {"title": "Report for ${region}", "columns": [{"header": "Name"}]}
              }
            }
          }
        ]
      }
    ]
  }
}`

// resolveWithOverrides resolves overrideDoc against the real catalog with the
// given unified override set and returns the resolved table child.
func resolveWithOverrides(t *testing.T, overrides variables.OverrideSet) (*ResolvedInstance, error) {
	t.Helper()
	res := newRepoResolver(t)
	tree, err := res.resolveBytes([]byte(overrideDoc), "inline", overrides)
	if err != nil {
		return nil, err
	}
	if len(tree.Root.Children) != 1 {
		t.Fatalf("root children = %d, want 1", len(tree.Root.Children))
	}
	// Under the E3-S2 grammar the table is block-wrapped inside a body region:
	// root -> body region -> block -> table.
	return tree.Root.Children[0].Children[0].Children[0], nil
}

// TestConfigOverrideValidMutation asserts a valid node+field override mutates the
// target instance's resolved config field.
func TestConfigOverrideValidMutation(t *testing.T) {
	table, err := resolveWithOverrides(t, variables.OverrideSet{"tbl.title": "Overridden"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := table.Config["title"]; got != "Overridden" {
		t.Errorf("title = %v, want %q", got, "Overridden")
	}
}

// TestConfigOverridePrecedence asserts a config override WINS over the
// interpolated value for the same field. Without the override the title
// interpolates to "Report for us"; the override replaces it post-interpolation.
func TestConfigOverridePrecedence(t *testing.T) {
	// Baseline: no override -> interpolation fills the title.
	base, err := resolveWithOverrides(t, nil)
	if err != nil {
		t.Fatalf("resolve baseline: %v", err)
	}
	if got := base.Config["title"]; got != "Report for us" {
		t.Fatalf("baseline title = %v, want interpolated %q", got, "Report for us")
	}

	// With the override -> the override value replaces the interpolated one.
	table, err := resolveWithOverrides(t, variables.OverrideSet{"tbl.title": "Pinned"})
	if err != nil {
		t.Fatalf("resolve with override: %v", err)
	}
	if got := table.Config["title"]; got != "Pinned" {
		t.Errorf("title = %v, want override %q to win over interpolation", got, "Pinned")
	}
}

// TestConfigOverrideFieldUnknown asserts an override targeting a field NOT in the
// target item type's surface fails fast with CONFIG_OVERRIDE_FIELD_UNKNOWN. The
// table surface exposes only columns/query/title, so "rows" (a real config field
// but not a surface field) and a dotted sub-path are both unknown.
func TestConfigOverrideFieldUnknown(t *testing.T) {
	tests := []struct {
		name  string
		addr  string
		field string
	}{
		{name: "field not in surface", addr: "tbl.rows", field: "rows"},
		{name: "dotted sub-path into nested object", addr: "tbl.title.text", field: "title.text"},
		{name: "unknown node id", addr: "nope.title", field: "title"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveWithOverrides(t, variables.OverrideSet{tc.addr: "x"})
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.HasCode(err, errors.CONFIG_OVERRIDE_FIELD_UNKNOWN) {
				t.Fatalf("error = %v, want code %s", err, errors.CONFIG_OVERRIDE_FIELD_UNKNOWN)
			}
			var ce *errors.CodedError
			if !asCoded(err, &ce) {
				t.Fatalf("error is not a CodedError: %v", err)
			}
			if got, _ := ce.Details["field"].(string); got != tc.field {
				t.Errorf("error field = %q, want %q", got, tc.field)
			}
		})
	}
}

// TestConfigOverrideValueInvalid asserts an override whose value violates the
// surface field's declared type OR the item type's config constraints fails fast
// with CONFIG_OVERRIDE_VALUE_INVALID.
func TestConfigOverrideValueInvalid(t *testing.T) {
	tests := []struct {
		name     string
		override variables.OverrideSet
	}{
		{
			// title is a string surface field; a number violates its declared type.
			name:     "wrong type for string field",
			override: variables.OverrideSet{"tbl.title": float64(42)},
		},
		{
			// columns is an array surface field, but each item must carry a non-empty
			// "header" per the table config schema — an item missing it passes the
			// array type check yet violates the schema constraint.
			name:     "value violates config-schema constraint",
			override: variables.OverrideSet{"tbl.columns": []any{map[string]any{"label": "oops"}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveWithOverrides(t, tc.override)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.HasCode(err, errors.CONFIG_OVERRIDE_VALUE_INVALID) {
				t.Fatalf("error = %v, want code %s", err, errors.CONFIG_OVERRIDE_VALUE_INVALID)
			}
			var ce *errors.CodedError
			if !asCoded(err, &ce) {
				t.Fatalf("error is not a CodedError: %v", err)
			}
			if got, _ := ce.Details["field"].(string); got == "" {
				t.Errorf("error missing field detail")
			}
		})
	}
}

// TestConfigOverrideEphemeral asserts the document on disk is UNCHANGED after a
// config override is applied — the mutation lives only in the resolved tree.
func TestConfigOverrideEphemeral(t *testing.T) {
	res := newRepoResolver(t)

	path := filepath.Join(t.TempDir(), "override-dashboard.json")
	if err := os.WriteFile(path, []byte(overrideDoc), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	tree, err := res.ResolveWithValues(path, variables.OverrideSet{"tbl.title": "Overridden"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// The override applied to the in-memory tree (root -> body region -> block -> table)...
	if got := tree.Root.Children[0].Children[0].Children[0].Config["title"]; got != "Overridden" {
		t.Fatalf("in-memory title = %v, want override applied", got)
	}

	// ...but the file on disk is byte-for-byte unchanged.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("document on disk changed after override application")
	}
}
