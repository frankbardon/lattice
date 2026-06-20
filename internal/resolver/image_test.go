package resolver

import (
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// This file exercises the E1-S3 `image` content leaf end to end through the real
// on-disk schema catalog (resolveDoc / resolveDocErr from block_test.go). image is
// a pure-schema content leaf: no resolver Go was added for it. It mirrors the
// E1-S1 markdown leaf, adding two OPTIONAL string fields (alt, caption) alongside
// the required opaque `src`. The tests prove the behaviors the
// catalog/grammar/interpolation passes grant a new leaf for free:
//
//   - auto-discovery + resolution: a block-wrapped image leaf resolves clean with
//     src only (alt/caption omitted) AND with all three fields present, and its
//     type identity + opaque src survive into the resolved tree,
//   - opaqueness: a relative path and a data URI are equally valid src values —
//     this schema imposes no `format: uri` or reachability constraint,
//   - config validation: a missing or empty `src` fails RESOLVE_CONFIG_INVALID,
//     an unknown extra property fails (additionalProperties:false), while absence
//     of alt/caption is valid,
//   - interpolation: a ${var} in src/alt/caption substitutes the in-scope value.

// imageWrapDoc builds a grammar-conformant document: root container -> body region
// -> block wrapping a single image content leaf with the given config. It is the
// canonical legal shape for a content leaf.
func imageWrapDoc(config string) string {
	return `{
  "manifest": { "formatVersion": "1.0.0", "id": "img", "title": "IMG" },
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
              "id": "img-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/image/1.0.0",
                "id": "img-leaf",
                "config": ` + config + `
              }
            }
          }
        ]
      }
    ]
  }
}`
}

// TestImageLeafSrcOnlyResolves asserts the catalog auto-registers the image type
// (no Go registry edit) and a block-wrapped image leaf resolves into a distinct
// content-leaf node carrying its opaque src verbatim — with alt and caption
// omitted entirely (the optional fields' absence is valid).
func TestImageLeafSrcOnlyResolves(t *testing.T) {
	tree := resolveDoc(t, imageWrapDoc(`{ "src": "/assets/logo.png" }`))

	// root container -> body region -> block wrapper -> image content leaf.
	block := tree.Root.Children[0].Children[0]
	if block.Type.Name != "block" {
		t.Fatalf("region child type = %q, want block", block.Type.Name)
	}
	leaf := block.Children[0]
	if leaf.Type.Name != "image" {
		t.Fatalf("block content type = %q, want image", leaf.Type.Name)
	}
	if got := leaf.Config["src"]; got != "/assets/logo.png" {
		t.Errorf("src = %v, want opaque verbatim reference", got)
	}
	if _, present := leaf.Config["alt"]; present {
		t.Errorf("alt should be absent, got %v", leaf.Config["alt"])
	}
	if _, present := leaf.Config["caption"]; present {
		t.Errorf("caption should be absent, got %v", leaf.Config["caption"])
	}
}

// TestImageLeafAllFieldsResolve asserts a block-wrapped image leaf with all three
// fields present resolves clean and carries src, alt, and caption verbatim.
func TestImageLeafAllFieldsResolve(t *testing.T) {
	config := `{ "src": "https://cdn.example.com/a.jpg", "alt": "An A", "caption": "Figure 1" }`
	tree := resolveDoc(t, imageWrapDoc(config))
	leaf := tree.Root.Children[0].Children[0].Children[0]
	if got := leaf.Config["src"]; got != "https://cdn.example.com/a.jpg" {
		t.Errorf("src = %v, want the verbatim URL", got)
	}
	if got := leaf.Config["alt"]; got != "An A" {
		t.Errorf("alt = %v, want %q", got, "An A")
	}
	if got := leaf.Config["caption"]; got != "Figure 1" {
		t.Errorf("caption = %v, want %q", got, "Figure 1")
	}
}

// TestImageSrcIsOpaque asserts src is an opaque string with no URL/format
// constraint: a relative path and a data URI both resolve clean. This guards the
// interview's explicit choice to omit `format: uri`.
func TestImageSrcIsOpaque(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "relative path", src: "./img/photo.png"},
		{name: "data uri", src: "data:image/png;base64,iVBORw0KGgo="},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := resolveDoc(t, imageWrapDoc(`{ "src": "`+tc.src+`" }`))
			leaf := tree.Root.Children[0].Children[0].Children[0]
			if got := leaf.Config["src"]; got != tc.src {
				t.Errorf("src = %v, want %q (opaque, unvalidated)", got, tc.src)
			}
		})
	}
}

// TestImageConfigValidation asserts the required, non-empty `src` constraint and
// additionalProperties:false are enforced by item-type config validation: a
// missing/empty src and an unknown extra property each fail fast with
// RESOLVE_CONFIG_INVALID (the SCHEMA_VALIDATION cause is wrapped under it), naming
// the block wrapper's `.content` path per the inherited E1-S1 contract.
func TestImageConfigValidation(t *testing.T) {
	cases := []struct {
		name   string
		config string
	}{
		{name: "missing src", config: `{}`},
		{name: "empty src", config: `{ "src": "" }`},
		{name: "unknown extra property", config: `{ "src": "/a.png", "width": 100 }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := resolveDocErr(t, imageWrapDoc(tc.config))
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
				t.Errorf("error path = %q, want the image leaf's wrapper path", got)
			}
		})
	}
}

// TestImageBareLeafFailsGrammar asserts a bare (unwrapped) image leaf placed
// directly under a container region fails GRAMMAR_REGION_CHILD_INVALID, naming the
// path: a content leaf must be block-wrapped. The block-wrapped form (covered by
// TestImageLeafSrcOnlyResolves) is the passing counterpart.
func TestImageBareLeafFailsGrammar(t *testing.T) {
	doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "img", "title": "IMG" },
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
            "$ref": "https://lattice.dev/schemas/items/image/1.0.0",
            "id": "bare",
            "config": { "src": "/bare.png" }
          }
        ]
      }
    ]
  }
}`
	assertGrammarErr(t, doc, errors.GRAMMAR_REGION_CHILD_INVALID, "root.children[0].children[0]")
}

// TestImageInterpolatesFields asserts a ${var} template inside any of src, alt, or
// caption is substituted from the in-scope variable environment before validation —
// image gains interpolation for free across all its string fields, with no parallel
// path.
func TestImageInterpolatesFields(t *testing.T) {
	doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "img", "title": "IMG" },
  "variables": [
    { "name": "host", "type": "string", "default": "cdn.example.com" },
    { "name": "name", "type": "string", "default": "Logo" },
    { "name": "fig", "type": "string", "default": "Figure 7" }
  ],
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
              "id": "img-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/image/1.0.0",
                "id": "img-leaf",
                "config": { "src": "https://${host}/a.png", "alt": "${name} mark", "caption": "${fig}" }
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
	if got := leaf.Config["src"]; got != "https://cdn.example.com/a.png" {
		t.Errorf("src = %v, want interpolated URL", got)
	}
	if got := leaf.Config["alt"]; got != "Logo mark" {
		t.Errorf("alt = %v, want %q (interpolated)", got, "Logo mark")
	}
	if got := leaf.Config["caption"]; got != "Figure 7" {
		t.Errorf("caption = %v, want %q (interpolated)", got, "Figure 7")
	}
}
