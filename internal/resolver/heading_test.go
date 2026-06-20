package resolver

import (
	"fmt"
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// This file exercises the E1-S2 `heading` content leaf end to end through the
// real on-disk schema catalog (resolveDoc / resolveDocErr from block_test.go).
// heading is a pure-schema content leaf: no resolver Go was added for it. It
// mirrors the E1-S1 markdown leaf, adding numeric-range validation on `level`.
// The tests prove the behaviors the catalog/grammar/interpolation passes grant a
// new leaf for free, plus the integer 1-6 range expressed entirely in JSON Schema:
//
//   - auto-discovery + resolution: a block-wrapped heading leaf resolves clean
//     and its type identity + text/level survive into the resolved tree,
//   - config validation: missing/empty `text`, missing `level`, and an out-of-range
//     or non-integer `level` each fail RESOLVE_CONFIG_INVALID, while levels 1-6 pass,
//   - interpolation: a ${var} in `text` substitutes the in-scope value.

// headingWrapDoc builds a grammar-conformant document: root container -> body
// region -> block wrapping a single heading content leaf with the given config.
// It is the canonical legal shape for a content leaf.
func headingWrapDoc(config string) string {
	return fmt.Sprintf(`{
  "manifest": { "formatVersion": "1.0.0", "id": "hd", "title": "HD" },
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
              "id": "hd-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/heading/1.0.0",
                "id": "hd-leaf",
                "config": %s
              }
            }
          }
        ]
      }
    ]
  }
}`, config)
}

// TestHeadingLeafAutoDiscoversAndResolves asserts the catalog auto-registers the
// heading type (no Go registry edit) and a block-wrapped heading leaf resolves
// into a distinct content-leaf node carrying its text and level.
func TestHeadingLeafAutoDiscoversAndResolves(t *testing.T) {
	tree := resolveDoc(t, headingWrapDoc(`{ "text": "Overview", "level": 2 }`))

	// root container -> body region -> block wrapper -> heading content leaf.
	block := tree.Root.Children[0].Children[0]
	if block.Type.Name != "block" {
		t.Fatalf("region child type = %q, want block", block.Type.Name)
	}
	leaf := block.Children[0]
	if leaf.Type.Name != "heading" {
		t.Fatalf("block content type = %q, want heading", leaf.Type.Name)
	}
	if got := leaf.Config["text"]; got != "Overview" {
		t.Errorf("text = %v, want %q", got, "Overview")
	}
	// JSON numbers decode to float64; level 2 round-trips as 2.0.
	if got, ok := leaf.Config["level"].(float64); !ok || got != 2 {
		t.Errorf("level = %v, want 2", leaf.Config["level"])
	}
}

// TestHeadingLevelsInRangePass asserts every legal level 1-6 resolves clean —
// the inclusive range boundaries (1 and 6) and the interior values all pass.
func TestHeadingLevelsInRangePass(t *testing.T) {
	for level := 1; level <= 6; level++ {
		t.Run(fmt.Sprintf("level %d", level), func(t *testing.T) {
			doc := headingWrapDoc(fmt.Sprintf(`{ "text": "H", "level": %d }`, level))
			tree := resolveDoc(t, doc)
			leaf := tree.Root.Children[0].Children[0].Children[0]
			if got, ok := leaf.Config["level"].(float64); !ok || int(got) != level {
				t.Errorf("level = %v, want %d", leaf.Config["level"], level)
			}
		})
	}
}

// TestHeadingConfigValidation asserts the required/non-empty `text` and the
// required integer `level` constrained to 1-6 are enforced by item-type config
// validation: each malformed config fails fast with RESOLVE_CONFIG_INVALID
// (wrapping the underlying JSON Schema validation failure). The reported path is
// the block wrapper's `.content` (the block validates its inner content under its
// own path), matching the inherited E1-S1 contract.
func TestHeadingConfigValidation(t *testing.T) {
	cases := []struct {
		name   string
		config string
	}{
		{name: "missing text", config: `{ "level": 1 }`},
		{name: "empty text", config: `{ "text": "", "level": 1 }`},
		{name: "missing level", config: `{ "text": "H" }`},
		{name: "level 0 below minimum", config: `{ "text": "H", "level": 0 }`},
		{name: "level 7 above maximum", config: `{ "text": "H", "level": 7 }`},
		{name: "non-integer level", config: `{ "text": "H", "level": 2.5 }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := resolveDocErr(t, headingWrapDoc(tc.config))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.HasCode(err, errors.RESOLVE_CONFIG_INVALID) {
				t.Fatalf("error = %v, want code %s", err, errors.RESOLVE_CONFIG_INVALID)
			}
			var ce *errors.CodedError
			if !asCoded(err, &ce) {
				t.Fatalf("error is not a CodedError: %v", err)
			}
			if got, _ := ce.Details["path"].(string); got != "root.children[0].children[0].content" {
				t.Errorf("error path = %q, want the heading leaf's wrapper path", got)
			}
		})
	}
}

// TestHeadingBareLeafFailsGrammar asserts a bare (unwrapped) heading leaf placed
// directly under a container region fails GRAMMAR_REGION_CHILD_INVALID: a content
// leaf must be block-wrapped. The block-wrapped form is the passing counterpart.
func TestHeadingBareLeafFailsGrammar(t *testing.T) {
	doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "hd", "title": "HD" },
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
            "$ref": "https://lattice.dev/schemas/items/heading/1.0.0",
            "id": "bare",
            "config": { "text": "bare", "level": 1 }
          }
        ]
      }
    ]
  }
}`
	assertGrammarErr(t, doc, errors.GRAMMAR_REGION_CHILD_INVALID, "root.children[0].children[0]")
}

// TestHeadingInterpolatesText asserts a ${var} template inside `text` is
// substituted from the in-scope variable environment before validation — heading
// gains interpolation for free, with no parallel path.
func TestHeadingInterpolatesText(t *testing.T) {
	doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "hd", "title": "HD" },
  "variables": [ { "name": "section", "type": "string", "default": "Reports" } ],
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
              "id": "hd-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/heading/1.0.0",
                "id": "hd-leaf",
                "config": { "text": "${section} Overview", "level": 2 }
              }
            }
          }
        ]
      }
    ]
  }
}`
	tree := resolveDoc(t, doc)
	leaf := tree.Root.Children[0].Children[0].Children[0]
	if got := leaf.Config["text"]; got != "Reports Overview" {
		t.Errorf("text = %v, want %q (interpolated)", got, "Reports Overview")
	}
}
