package resolver

import (
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// This file exercises the E3-S2 dashboard tree GRAMMAR pass end to end through the
// real on-disk schema catalog (resolveDoc / resolveDocErr from block_test.go). It
// covers each violation class plus a clean multi-level pass:
//
//   - GRAMMAR_ROOT_CHILD_INVALID         — a non-region directly under root,
//   - GRAMMAR_REGION_CHILD_INVALID       — a bare content leaf under a container,
//   - GRAMMAR_VARIABLE_BOX_CHILD_INVALID — a non-widget under a variable-box,
//   - GRAMMAR_WRAPPER_NESTED             — a wrapper inside a wrapper,
//   - GRAMMAR_REGION_THEME_FORBIDDEN     — a theme element on a positional region,
//   - a clean pass: root -> container -> nested container -> block -> leaf, and
//     root -> variable-box -> widget.
//
// The grammar is fail-fast and names the offending path, so each negative case
// asserts both the code and Details["path"].

// grammarCleanDoc is a fully grammar-conformant document exercising every legal
// shape at once: root holds two positional regions; a container nests another
// container which holds a block wrapping a table leaf; a variable-box holds a
// variable widget directly. It must resolve clean.
const grammarCleanDoc = `{
  "manifest": { "formatVersion": "1.0.0", "id": "grammar-clean", "title": "Grammar Clean" },
  "variables": [ { "name": "q", "type": "string", "default": "hi" } ],
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": { "grid": { "columns": [1, 1] } },
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
        "id": "outer-region",
        "config": { "grid": { "columns": [1] } },
        "children": [
          {
            "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
            "id": "inner-region",
            "config": { "grid": { "columns": [1] } },
            "children": [
              {
                "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
                "config": {
                  "id": "leaf-block",
                  "content": {
                    "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
                    "id": "leaf",
                    "config": { "columns": [{ "header": "Name" }] }
                  }
                }
              }
            ]
          }
        ]
      },
      {
        "$ref": "https://lattice.dev/schemas/items/variable-box/1.0.0",
        "id": "vbox",
        "children": [
          {
            "$ref": "https://lattice.dev/schemas/items/text-input/1.0.0",
            "id": "q-input",
            "config": { "label": "Query", "variable": "q" }
          }
        ]
      }
    ]
  }
}`

// TestGrammarCleanMultiLevelResolves asserts a fully conformant multi-level tree
// (root -> container -> nested container -> block -> leaf, plus root -> variable-box
// -> widget) resolves without a grammar error.
func TestGrammarCleanMultiLevelResolves(t *testing.T) {
	tree := resolveDoc(t, grammarCleanDoc)

	if len(tree.Root.Children) != 2 {
		t.Fatalf("root has %d children, want 2 (two regions)", len(tree.Root.Children))
	}
	outer := tree.Root.Children[0]
	if outer.Type.Name != "container" {
		t.Errorf("first region type = %q, want container", outer.Type.Name)
	}
	inner := outer.Children[0]
	block := inner.Children[0]
	if block.Type.Name != "block" {
		t.Fatalf("nested region child type = %q, want block", block.Type.Name)
	}
	if leaf := block.Children[0]; leaf.Type.Name != "table" {
		t.Errorf("block content type = %q, want table", leaf.Type.Name)
	}
	vbox := tree.Root.Children[1]
	if vbox.Type.Name != "variable-box" {
		t.Fatalf("second region type = %q, want variable-box", vbox.Type.Name)
	}
	if w := vbox.Children[0]; w.Type.Name != "text-input" {
		t.Errorf("variable-box child type = %q, want text-input", w.Type.Name)
	}
}

// TestGrammarRootChildInvalid asserts a non-region placed directly under root
// fails with GRAMMAR_ROOT_CHILD_INVALID, naming the path. A bare content leaf and
// a block wrapper are both rejected at root (only positional regions are legal).
func TestGrammarRootChildInvalid(t *testing.T) {
	cases := []struct {
		name  string
		child string
	}{
		{
			name: "content leaf at root",
			child: `{
        "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
        "id": "t",
        "config": { "columns": [{ "header": "Name" }] }
      }`,
		},
		{
			name: "wrapper at root",
			child: `{
        "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
        "config": {
          "id": "b",
          "content": {
            "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
            "config": { "columns": [{ "header": "Name" }] }
          }
        }
      }`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "g", "title": "G" },
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": { "grid": { "columns": [1] } },
    "children": [ ` + tc.child + ` ]
  }
}`
			assertGrammarErr(t, doc, errors.GRAMMAR_ROOT_CHILD_INVALID, "root.children[0]")
		})
	}
}

// TestGrammarRegionChildInvalid asserts a bare (unwrapped) content leaf under a
// container region fails with GRAMMAR_REGION_CHILD_INVALID, naming the path.
// Content under a container must be wrapped in a block.
func TestGrammarRegionChildInvalid(t *testing.T) {
	doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "g", "title": "G" },
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": { "grid": { "columns": [1] } },
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
        "id": "region",
        "config": { "grid": { "columns": [1] } },
        "children": [
          {
            "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
            "id": "bare",
            "config": { "columns": [{ "header": "Name" }] }
          }
        ]
      }
    ]
  }
}`
	assertGrammarErr(t, doc, errors.GRAMMAR_REGION_CHILD_INVALID, "root.children[0].children[0]")
}

// TestGrammarVariableBoxChildInvalid asserts a non-widget child of a variable-box
// fails with GRAMMAR_VARIABLE_BOX_CHILD_INVALID, naming the path. A variable-box
// holds only variable widgets, directly — a table (content leaf) is illegal.
func TestGrammarVariableBoxChildInvalid(t *testing.T) {
	doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "g", "title": "G" },
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": { "grid": { "columns": [1] } },
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/variable-box/1.0.0",
        "id": "vbox",
        "children": [
          {
            "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
            "id": "not-a-widget",
            "config": { "columns": [{ "header": "Name" }] }
          }
        ]
      }
    ]
  }
}`
	assertGrammarErr(t, doc, errors.GRAMMAR_VARIABLE_BOX_CHILD_INVALID, "root.children[0].children[0]")
}

// TestGrammarWrapperNested asserts a block wrapping another block fails with
// GRAMMAR_WRAPPER_NESTED, naming the inner wrapper's path. Wrappers do not recurse.
func TestGrammarWrapperNested(t *testing.T) {
	doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "g", "title": "G" },
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": { "grid": { "columns": [1] } },
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
        "id": "region",
        "config": { "grid": { "columns": [1] } },
        "children": [
          {
            "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
            "config": {
              "id": "outer-wrapper",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
                "config": {
                  "id": "inner-wrapper",
                  "content": {
                    "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
                    "config": { "columns": [{ "header": "Name" }] }
                  }
                }
              }
            }
          }
        ]
      }
    ]
  }
}`
	// The outer wrapper sits at root.children[0].children[0]; its single inner
	// content (the nested wrapper) is the outer wrapper's child node.
	assertGrammarErr(t, doc, errors.GRAMMAR_WRAPPER_NESTED, "root.children[0].children[0].children[0]")
}

// TestGrammarRegionThemeForbidden asserts a positional region carrying a `theme`
// element fails with GRAMMAR_REGION_THEME_FORBIDDEN, naming the region's path.
// Regions are layout-only; only block wrappers carry theme. The container schema
// forbids `theme` structurally (additionalProperties:false), so the document fails
// the structural pass first — proving the chrome-on-region prohibition holds — and
// the grammar pass is the defense-in-depth guard when a region node reaches it with
// a theme. We assert the grammar guard directly against an assembled region node.
func TestGrammarRegionThemeForbidden(t *testing.T) {
	region := &ResolvedInstance{
		Type:   ResolvedTypeRef{Name: "container"},
		Config: map[string]any{"theme": map[string]any{"tone": "accent"}},
	}
	err := checkRegionNoTheme(region, "root.children[0]")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.HasCode(err, errors.GRAMMAR_REGION_THEME_FORBIDDEN) {
		t.Fatalf("error = %v, want code %s", err, errors.GRAMMAR_REGION_THEME_FORBIDDEN)
	}
	var ce *errors.CodedError
	if !asCoded(err, &ce) {
		t.Fatalf("error is not a CodedError: %v", err)
	}
	if got, _ := ce.Details["path"].(string); got != "root.children[0]" {
		t.Errorf("error path = %q, want root.children[0]", got)
	}
}

// assertGrammarErr resolves an intentionally-broken document and asserts the first
// error carries the expected grammar code and Details["path"].
func assertGrammarErr(t *testing.T, doc string, code errors.Code, wantPath string) {
	t.Helper()
	err := resolveDocErr(t, doc)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.HasCode(err, code) {
		t.Fatalf("error = %v, want code %s", err, code)
	}
	var ce *errors.CodedError
	if !asCoded(err, &ce) {
		t.Fatalf("error is not a CodedError: %v", err)
	}
	if got, _ := ce.Details["path"].(string); got != wantPath {
		t.Errorf("error path = %q, want %q", got, wantPath)
	}
}
