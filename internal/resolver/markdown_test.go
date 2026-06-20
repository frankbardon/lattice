package resolver

import (
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// This file exercises the E1-S1 `markdown` content leaf end to end through the
// real on-disk schema catalog (resolveDoc / resolveDocErr from block_test.go).
// markdown is a pure-schema content leaf: no resolver Go was added for it. The
// tests prove the four behaviors the catalog/grammar/interpolation passes grant a
// new leaf for free:
//
//   - auto-discovery + resolution: a block-wrapped markdown leaf resolves clean
//     and its type identity + opaque source survive into the resolved tree,
//   - config validation: a missing or empty `source` fails RESOLVE_CONFIG_INVALID,
//   - grammar: a bare (unwrapped) markdown leaf under a container fails
//     GRAMMAR_REGION_CHILD_INVALID, while the same leaf wrapped in a block passes,
//   - interpolation: a ${var} in `source` substitutes the in-scope value, and an
//     undefined reference fails fast VAR_UNDEFINED.

// markdownWrapDoc is a grammar-conformant document: root container -> body region
// -> block wrapping a single markdown content leaf. It is the canonical legal
// shape for a content leaf and must resolve clean.
const markdownWrapDoc = `{
  "manifest": { "formatVersion": "1.0.0", "id": "md", "title": "MD" },
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
              "id": "md-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/markdown/1.0.0",
                "id": "md-leaf",
                "config": { "source": "# Title\n\nSome **prose**." }
              }
            }
          }
        ]
      }
    ]
  }
}`

// TestMarkdownLeafAutoDiscoversAndResolves asserts the catalog auto-registers the
// markdown type (no Go registry edit) and a block-wrapped markdown leaf resolves
// into a distinct content-leaf node carrying its opaque source verbatim.
func TestMarkdownLeafAutoDiscoversAndResolves(t *testing.T) {
	tree := resolveDoc(t, markdownWrapDoc)

	// root container -> body region -> block wrapper -> markdown content leaf.
	block := tree.Root.Children[0].Children[0]
	if block.Type.Name != "block" {
		t.Fatalf("region child type = %q, want block", block.Type.Name)
	}
	leaf := block.Children[0]
	if leaf.Type.Name != "markdown" {
		t.Fatalf("block content type = %q, want markdown", leaf.Type.Name)
	}
	if got := leaf.Config["source"]; got != "# Title\n\nSome **prose**." {
		t.Errorf("source = %v, want opaque verbatim prose", got)
	}
}

// TestMarkdownSourceValidation asserts the required, non-empty `source` constraint
// is enforced by item-type config validation: a missing or empty source fails fast
// with RESOLVE_CONFIG_INVALID (the SCHEMA_VALIDATION cause is wrapped under it),
// naming the offending leaf's path.
func TestMarkdownSourceValidation(t *testing.T) {
	cases := []struct {
		name   string
		config string
	}{
		{name: "missing source", config: `{}`},
		{name: "empty source", config: `{ "source": "" }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "md", "title": "MD" },
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
              "id": "md-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/markdown/1.0.0",
                "id": "md-leaf",
                "config": ` + tc.config + `
              }
            }
          }
        ]
      }
    ]
  }
}`
			err := resolveDocErr(t, doc)
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
				t.Errorf("error path = %q, want the markdown leaf's path", got)
			}
		})
	}
}

// TestMarkdownBareLeafFailsGrammar asserts a bare (unwrapped) markdown leaf placed
// directly under a container region fails GRAMMAR_REGION_CHILD_INVALID, naming the
// path: a content leaf must be block-wrapped. The block-wrapped form (covered by
// TestMarkdownLeafAutoDiscoversAndResolves) is the passing counterpart.
func TestMarkdownBareLeafFailsGrammar(t *testing.T) {
	doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "md", "title": "MD" },
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
            "$ref": "https://lattice.dev/schemas/items/markdown/1.0.0",
            "id": "bare",
            "config": { "source": "# bare" }
          }
        ]
      }
    ]
  }
}`
	assertGrammarErr(t, doc, errors.GRAMMAR_REGION_CHILD_INVALID, "root.children[0].children[0]")
}

// TestMarkdownInterpolatesSource asserts a ${var} template inside the opaque
// `source` is substituted from the in-scope variable environment before
// validation — markdown gains interpolation for free, with no parallel path.
func TestMarkdownInterpolatesSource(t *testing.T) {
	doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "md", "title": "MD" },
  "variables": [ { "name": "name", "type": "string", "default": "world" } ],
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
              "id": "md-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/markdown/1.0.0",
                "id": "md-leaf",
                "config": { "source": "Hello, ${name}!" }
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
	if got := leaf.Config["source"]; got != "Hello, world!" {
		t.Errorf("source = %v, want %q (interpolated)", got, "Hello, world!")
	}
}

// TestMarkdownUndefinedVarFailsFast asserts a ${var} in `source` referencing an
// undeclared variable surfaces as a fail-fast VAR_UNDEFINED CodedError.
func TestMarkdownUndefinedVarFailsFast(t *testing.T) {
	doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "md", "title": "MD" },
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
              "id": "md-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/markdown/1.0.0",
                "id": "md-leaf",
                "config": { "source": "Hello, ${ghost}!" }
              }
            }
          }
        ]
      }
    ]
  }
}`
	err := resolveDocErr(t, doc)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.HasCode(err, errors.VAR_UNDEFINED) {
		t.Fatalf("error = %v, want code %s", err, errors.VAR_UNDEFINED)
	}
}
