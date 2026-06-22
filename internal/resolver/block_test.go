package resolver

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// This file exercises the E1-S2 block-wrapper resolution pass end to end through
// the real on-disk schema catalog (the block item-type auto-registers as a
// drop-in file). It asserts:
//
//   - a wrapper and its single inner content emit as SEPARATE resolved nodes,
//   - the inner content resolves IDENTICALLY to how it resolves unwrapped,
//   - a wrapper carries its own concerns (id/title/visibility) + configurable
//     surface, and does NOT duplicate the inner item inside its own config,
//   - a wrapper missing `id` fails fast (WRAPPER_ID_MISSING, path named),
//   - a wrapper not wrapping exactly one content item fails fast
//     (WRAPPER_CHILD_COUNT_INVALID, path named), and
//   - a per-wrapper theme override (E2-S3) is validated against the shared theme
//     vocabulary and emitted SIDE-BY-SIDE with the document default theme: a legal
//     partial override is attached verbatim, an out-of-vocabulary token/value is
//     rejected fail-fast (RESOLVE_CONFIG_INVALID, path named), and the resolver
//     performs NO cascade/merge (no effective/computed theme is produced).

// blockWrapDoc wraps a single table in a block. The block carries its own
// concerns (id/title/visibility) and a theme override; the table is its content.
const blockWrapDoc = `{
  "manifest": { "formatVersion": "1.0.0", "id": "block-wrap", "title": "Block Wrap" },
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
              "id": "revenue-block",
              "title": "Revenue",
              "visibility": true,
              "theme": { "tone": "accent" },
              "content": {
                "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
                "id": "revenue",
                "config": { "title": "Revenue", "columns": [{ "header": "Name" }] }
              }
            }
          }
        ]
      }
    ]
  }
}`

// unwrappedDoc places the SAME table in the same position, but inside a MINIMAL
// block (carrying only the required id, none of blockWrapDoc's title/visibility/
// theme concerns), so a test can assert the wrapped inner node resolves
// identically regardless of which per-block concerns the surrounding wrapper
// carries. Under the E3-S2 grammar a content leaf cannot sit bare under a
// container, so "unwrapped" here means "wrapped by a concern-free block".
const unwrappedDoc = `{
  "manifest": { "formatVersion": "1.0.0", "id": "unwrapped", "title": "Unwrapped" },
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
              "id": "revenue-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
                "id": "revenue",
                "config": { "title": "Revenue", "columns": [{ "header": "Name" }] }
              }
            }
          }
        ]
      }
    ]
  }
}`

// resolveDoc writes doc to a temp file and resolves it against the real catalog.
func resolveDoc(t *testing.T, doc string) *ResolvedTree {
	t.Helper()
	res := newRepoResolver(t)
	path := filepath.Join(t.TempDir(), "doc.json")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	tree, err := res.Resolve(path)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return tree
}

// resolveDocErr resolves an intentionally-broken document and returns the error.
func resolveDocErr(t *testing.T, doc string) error {
	t.Helper()
	res := newRepoResolver(t)
	path := filepath.Join(t.TempDir(), "doc.json")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	_, err := res.Resolve(path)
	return err
}

// TestBlockEmitsWrapperAndInnerAsSeparateNodes asserts a block wrapper and its
// single inner content come back as two distinct resolved nodes: the wrapper node
// (type "block") carrying its own concerns + surface, and the inner content as the
// wrapper's single child (type "table"). The inner item is NOT duplicated inside
// the wrapper's own config.
func TestBlockEmitsWrapperAndInnerAsSeparateNodes(t *testing.T) {
	tree := resolveDoc(t, blockWrapDoc)

	// Under the E3-S2 grammar the wrapper sits inside a body region:
	// root -> body region -> block.
	wrapper := tree.Root.Children[0].Children[0]
	if wrapper.Type.Name != "block" {
		t.Fatalf("wrapper type = %q, want block", wrapper.Type.Name)
	}

	// The wrapper exposes its own concerns in its resolved config.
	if got, _ := wrapper.Config["id"].(string); got != "revenue-block" {
		t.Errorf("wrapper config id = %q, want revenue-block", got)
	}
	if got, _ := wrapper.Config["title"].(string); got != "Revenue" {
		t.Errorf("wrapper config title = %q, want Revenue", got)
	}
	if got, ok := wrapper.Config["visibility"].(bool); !ok || got != true {
		t.Errorf("wrapper config visibility = %v, want true", wrapper.Config["visibility"])
	}
	if _, ok := wrapper.Config["theme"].(map[string]any); !ok {
		t.Errorf("wrapper config theme missing/!object: %v", wrapper.Config["theme"])
	}

	// The content is lifted OUT of the wrapper's config — it is a separate node, not
	// duplicated inside the wrapper.
	if _, dup := wrapper.Config["content"]; dup {
		t.Error("wrapper config still carries `content`; the inner item must be a separate node, not duplicated")
	}

	// The wrapper attaches its own configurable surface (title + visibility), like
	// any item.
	if len(wrapper.Surface) != 2 {
		t.Fatalf("wrapper surface has %d fields, want 2 (title, visibility)", len(wrapper.Surface))
	}

	// The inner content is the wrapper's single child node.
	if len(wrapper.Children) != 1 {
		t.Fatalf("wrapper has %d children, want 1 (the inner content)", len(wrapper.Children))
	}
	inner := wrapper.Children[0]
	if inner.Type.Name != "table" {
		t.Errorf("inner type = %q, want table", inner.Type.Name)
	}
	if inner.ID != "revenue" {
		t.Errorf("inner id = %q, want revenue", inner.ID)
	}
}

// TestBlockInnerResolvesIdenticallyToUnwrapped asserts wrapper presence does not
// alter the inner node: the table resolved as a block's content is byte-for-byte
// the same resolved node as the same table resolved unwrapped in the same position.
func TestBlockInnerResolvesIdenticallyToUnwrapped(t *testing.T) {
	wrapped := resolveDoc(t, blockWrapDoc)
	unwrapped := resolveDoc(t, unwrappedDoc)

	// Both inner tables sit at: root -> body region -> block -> table. The wrapper
	// in blockWrapDoc carries title/visibility/theme; the one in unwrappedDoc is
	// concern-free — the inner table node must be identical regardless.
	inner := wrapped.Root.Children[0].Children[0].Children[0]
	bare := unwrapped.Root.Children[0].Children[0].Children[0]

	if !reflect.DeepEqual(inner, bare) {
		t.Errorf("wrapped inner node differs from unwrapped node\n wrapped: %+v\n unwrapped: %+v", inner, bare)
	}
}

// TestBlockMissingIDFailsFast asserts a wrapper whose `id` is absent or
// whitespace-only fails fast with WRAPPER_ID_MISSING, naming the wrapper path. The
// schema requires id (minLength 1), so the empty case is caught structurally as
// RESOLVE_DOCUMENT_INVALID; a whitespace-only id reaches the resolver guard.
func TestBlockMissingIDFailsFast(t *testing.T) {
	// Whitespace-only id: passes the schema's minLength, hit by the resolver guard.
	doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "block-no-id", "title": "Block No Id" },
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": { "grid": { "columns": [1] } },
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
        "config": {
          "id": "   ",
          "content": {
            "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
            "config": { "columns": [{ "header": "Name" }] }
          }
        }
      }
    ]
  }
}`
	err := resolveDocErr(t, doc)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.HasCode(err, errors.WRAPPER_ID_MISSING) {
		t.Fatalf("error = %v, want code %s", err, errors.WRAPPER_ID_MISSING)
	}
	var ce *errors.CodedError
	if !asCoded(err, &ce) {
		t.Fatalf("error is not a CodedError: %v", err)
	}
	if got, _ := ce.Details["path"].(string); got != "root.children[0]" {
		t.Errorf("error path = %q, want root.children[0]", got)
	}
}

// TestBlockMissingContentFailsFast asserts a wrapper declaring no content fails
// fast with WRAPPER_CHILD_COUNT_INVALID, naming the wrapper path: a block must wrap
// exactly one content item. The resolver's exactly-one guard runs on the authored
// config before the block's schema-level config validation.
func TestBlockMissingContentFailsFast(t *testing.T) {
	doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "block-no-content", "title": "Block No Content" },
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": { "grid": { "columns": [1] } },
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
        "config": { "id": "empty-block" }
      }
    ]
  }
}`
	err := resolveDocErr(t, doc)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.HasCode(err, errors.WRAPPER_CHILD_COUNT_INVALID) {
		t.Fatalf("error = %v, want code %s", err, errors.WRAPPER_CHILD_COUNT_INVALID)
	}
	var ce *errors.CodedError
	if !asCoded(err, &ce) {
		t.Fatalf("error is not a CodedError: %v", err)
	}
	if got, _ := ce.Details["path"].(string); got != "root.children[0]" {
		t.Errorf("error path = %q, want root.children[0]", got)
	}
}

// blockThemeOverrideDoc declares a document DEFAULT theme and a block wrapper with
// a PARTIAL theme override (only some tokens). Used to assert the override is
// accepted, attached to the wrapper node, and emitted SIDE-BY-SIDE with the
// document default — with NO merge.
const blockThemeOverrideDoc = `{
  "manifest": { "formatVersion": "1.0.0", "id": "block-theme", "title": "Block Theme" },
  "theme": { "emphasis": "low", "spacing": "cosy" },
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
              "id": "themed-block",
              "theme": { "emphasis": "high", "tone": "accent" },
              "content": {
                "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
                "id": "t",
                "config": { "columns": [{ "header": "Name" }] }
              }
            }
          }
        ]
      }
    ]
  }
}`

// TestBlockThemeOverrideAcceptedAndAttached asserts a legal per-wrapper theme
// override (a PARTIAL subset of the shared vocabulary, overriding the document
// default's choices) is accepted and attached verbatim to the resolved wrapper
// node (E2-S3).
func TestBlockThemeOverrideAcceptedAndAttached(t *testing.T) {
	tree := resolveDoc(t, blockThemeOverrideDoc)

	wrapper := tree.Root.Children[0].Children[0]
	override, ok := wrapper.Config["theme"].(map[string]any)
	if !ok {
		t.Fatalf("wrapper theme override missing/!object: %v", wrapper.Config["theme"])
	}
	// A partial override (only two of six tokens) validates and is attached verbatim.
	if got, _ := override["emphasis"].(string); got != "high" {
		t.Errorf("override emphasis = %q, want high", got)
	}
	if got, _ := override["tone"].(string); got != "accent" {
		t.Errorf("override tone = %q, want accent", got)
	}
	if len(override) != 2 {
		t.Errorf("override has %d tokens, want 2 (partial override must not be padded): %v", len(override), override)
	}
}

// TestBlockThemeOverrideSideBySideNoMerge is the CRITICAL no-merge assertion: the
// resolved tree exposes the document default theme AND the wrapper override
// SEPARATELY, side-by-side. The resolver performs NO cascade/merge — it produces
// no effective/computed theme. We assert each layer is verbatim and untouched by
// the other (the wrapper override does NOT absorb the default's `spacing`, and the
// default does NOT absorb the override's `tone`).
func TestBlockThemeOverrideSideBySideNoMerge(t *testing.T) {
	tree := resolveDoc(t, blockThemeOverrideDoc)

	// Layer 1: document default theme, verbatim, untouched by the override.
	if tree.DefaultTheme == nil {
		t.Fatal("DefaultTheme is nil; the document default layer must be exposed")
	}
	wantDefault := map[string]any{"emphasis": "low", "spacing": "cosy"}
	if !reflect.DeepEqual(tree.DefaultTheme, wantDefault) {
		t.Errorf("DefaultTheme = %v, want %v (must be verbatim, no merge from override)", tree.DefaultTheme, wantDefault)
	}

	// Layer 2: wrapper override, verbatim, untouched by the default.
	wrapper := tree.Root.Children[0].Children[0]
	override, _ := wrapper.Config["theme"].(map[string]any)
	wantOverride := map[string]any{"emphasis": "high", "tone": "accent"}
	if !reflect.DeepEqual(override, wantOverride) {
		t.Errorf("wrapper override = %v, want %v (must be verbatim, no merge from default)", override, wantOverride)
	}

	// No cascade: the override did NOT inherit the default's `spacing`, and the
	// default did NOT absorb the override's `tone`. The two layers are independent.
	if _, leaked := override["spacing"]; leaked {
		t.Error("wrapper override leaked the default's `spacing`; resolver must NOT merge layers")
	}
	if _, leaked := tree.DefaultTheme["tone"]; leaked {
		t.Error("DefaultTheme leaked the override's `tone`; resolver must NOT merge layers")
	}

	// No effective/computed theme: the resolved contract carries only the default
	// layer and per-node overrides — there is no merged theme field anywhere.
	if got := wrapper.Config["effectiveTheme"]; got != nil {
		t.Errorf("wrapper carries an effectiveTheme (%v); the resolver must produce NO merged theme", got)
	}
}

// TestBlockThemeOverrideIllegalTokenRejected asserts an override carrying an
// out-of-vocabulary token (or value) fails fast with a coded, path-named error.
// The block schema $refs the shared theme vocabulary, whose tokens are a closed,
// enum-constrained set with additionalProperties:false; the wrapper's config
// validation pass therefore rejects it via RESOLVE_CONFIG_INVALID (the same
// schema-validation path E2-S2 reuses, not a new bespoke code).
func TestBlockThemeOverrideIllegalTokenRejected(t *testing.T) {
	cases := []struct {
		name  string
		theme string
	}{
		{"unknown token", `{ "accent": "blue" }`},
		{"out-of-vocab value for known token", `{ "emphasis": "loud" }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "block-bad-theme", "title": "Block Bad Theme" },
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": { "grid": { "columns": [1] } },
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
        "config": {
          "id": "bad-block",
          "theme": ` + tc.theme + `,
          "content": {
            "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
            "config": { "columns": [{ "header": "Name" }] }
          }
        }
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
			if got, _ := ce.Details["path"].(string); got != "root.children[0]" {
				t.Errorf("error path = %q, want root.children[0]", got)
			}
		})
	}
}

// TestBlockChildCountGuardDirect drives extractBlockContent directly to cover the
// resolver's exactly-one-content guard for content shapes the item-type schema
// cannot reject structurally (absent, null, an array of items). This is the
// WRAPPER_CHILD_COUNT_INVALID defense-in-depth.
func TestBlockChildCountGuardDirect(t *testing.T) {
	good := map[string]any{
		"$ref":   "https://lattice.dev/schemas/items/table/1.0.0",
		"config": map[string]any{"columns": []any{map[string]any{"header": "Name"}}},
	}

	cases := []struct {
		name    string
		config  map[string]any
		code    errors.Code
		wantErr bool
	}{
		{
			name:    "exactly one content",
			config:  map[string]any{"id": "b", "content": good},
			wantErr: false,
		},
		{
			name:    "absent content (zero)",
			config:  map[string]any{"id": "b"},
			code:    errors.WRAPPER_CHILD_COUNT_INVALID,
			wantErr: true,
		},
		{
			name:    "null content",
			config:  map[string]any{"id": "b", "content": nil},
			code:    errors.WRAPPER_CHILD_COUNT_INVALID,
			wantErr: true,
		},
		{
			name:    "array content (more than one)",
			config:  map[string]any{"id": "b", "content": []any{good, good}},
			code:    errors.WRAPPER_CHILD_COUNT_INVALID,
			wantErr: true,
		},
		{
			name:    "missing id",
			config:  map[string]any{"content": good},
			code:    errors.WRAPPER_ID_MISSING,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ownConfig, err := extractBlockContent(tc.config, "content", "root.children[0]")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !errors.HasCode(err, tc.code) {
					t.Fatalf("error = %v, want code %s", err, tc.code)
				}
				var ce *errors.CodedError
				if !asCoded(err, &ce) {
					t.Fatalf("error is not a CodedError: %v", err)
				}
				if got, _ := ce.Details["path"].(string); got != "root.children[0]" {
					t.Errorf("error path = %q, want root.children[0]", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if _, dup := ownConfig["content"]; dup {
				t.Error("ownConfig still carries `content`; it must be lifted out")
			}
		})
	}
}
