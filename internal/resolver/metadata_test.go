package resolver

import (
	"reflect"
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// This file exercises element-metadata threading (E1-S2) end to end through the
// real on-disk schema catalog. It asserts:
//
//   - eligible nodes (document root, a regions-or-wrappers container, a block
//     wrapper) carry their declared metadata verbatim;
//   - the top-level document metadata lands on the tree root;
//   - an INELIGIBLE node (a form — role==region but childPolicy==widgets) with
//     metadata fails fast (METADATA_NOT_ELIGIBLE, path named);
//   - a non-scalar metadata value (object/array) is rejected even on an eligible
//     node (METADATA_VALUE_NOT_SCALAR, path+key named);
//   - a block wrapper's metadata stays on the wrapper envelope and is NOT lifted
//     onto its inner content; and
//   - a metadata-free document carries no `metadata` anywhere (passthrough only).

// TestMetadataOnEligibleNodesAndRoot asserts metadata is carried verbatim onto
// the eligible nodes — the document root (from BOTH the top-level document
// metadata and the root instance), a nested container, and a block wrapper — and
// that every value passes through unchanged.
func TestMetadataOnEligibleNodesAndRoot(t *testing.T) {
	doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "meta-ok", "title": "Meta OK" },
  "metadata": { "owner": "platform", "version": 3 },
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": { "grid": { "columns": [1] } },
    "metadata": { "rootKey": "rootVal", "pinned": true },
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
        "id": "body",
        "config": { "grid": { "columns": [1] } },
        "metadata": { "team": "growth", "count": 7, "flagged": false, "note": null },
        "children": [
          {
            "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
            "config": {
              "id": "revenue-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
                "id": "revenue",
                "config": { "columns": [{ "header": "Name" }] }
              }
            },
            "metadata": { "source": "warehouse", "rev": 2 }
          }
        ]
      }
    ]
  }
}`
	tree := resolveDoc(t, doc)

	// Document-level metadata lands on the root, merged with the root instance's
	// own metadata.
	wantRoot := map[string]any{
		"owner":   "platform",
		"version": float64(3),
		"rootKey": "rootVal",
		"pinned":  true,
	}
	if !reflect.DeepEqual(tree.Root.Metadata, wantRoot) {
		t.Errorf("root metadata = %v, want %v", tree.Root.Metadata, wantRoot)
	}

	// A nested container carries its own metadata verbatim, including null.
	body := tree.Root.Children[0]
	wantBody := map[string]any{
		"team":    "growth",
		"count":   float64(7),
		"flagged": false,
		"note":    nil,
	}
	if !reflect.DeepEqual(body.Metadata, wantBody) {
		t.Errorf("container metadata = %v, want %v", body.Metadata, wantBody)
	}

	// The block wrapper carries its metadata on the wrapper envelope.
	wrapper := body.Children[0]
	if wrapper.Type.Name != "block" {
		t.Fatalf("wrapper type = %q, want block", wrapper.Type.Name)
	}
	wantWrapper := map[string]any{"source": "warehouse", "rev": float64(2)}
	if !reflect.DeepEqual(wrapper.Metadata, wantWrapper) {
		t.Errorf("wrapper metadata = %v, want %v", wrapper.Metadata, wantWrapper)
	}

	// Crucially, the inner content does NOT inherit the wrapper's metadata.
	inner := wrapper.Children[0]
	if inner.Metadata != nil {
		t.Errorf("inner content metadata = %v, want nil (wrapper metadata must NOT be lifted onto content)", inner.Metadata)
	}
}

// TestMetadataOnIneligibleNodeRejected asserts metadata on an ineligible node — a
// form (role==region, childPolicy==widgets) — fails fast with
// METADATA_NOT_ELIGIBLE, naming the offending node path.
func TestMetadataOnIneligibleNodeRejected(t *testing.T) {
	doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "meta-form", "title": "Meta Form" },
  "variables": [{ "name": "region", "type": "string", "default": "us" }],
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
              "id": "form-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/form/1.0.0",
                "id": "form",
                "metadata": { "owner": "nope" },
                "children": [
                  { "$ref": "https://lattice.dev/schemas/items/text-input/1.0.0", "config": { "variable": "region" } }
                ]
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
	if !errors.HasCode(err, errors.METADATA_NOT_ELIGIBLE) {
		t.Fatalf("error = %v, want code %s", err, errors.METADATA_NOT_ELIGIBLE)
	}
	var ce *errors.CodedError
	if !asCoded(err, &ce) {
		t.Fatalf("error is not a CodedError: %v", err)
	}
	if got, _ := ce.Details["path"].(string); got != formOwnPath {
		t.Errorf("error path = %q, want %q", got, formOwnPath)
	}
	if got, _ := ce.Details["type"].(string); got != "form" {
		t.Errorf("error type = %q, want form", got)
	}
}

// TestMetadataNonScalarRejected asserts a non-scalar metadata value (object or
// array) is rejected even on an eligible node, with the offending path+key named.
func TestMetadataNonScalarRejected(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"object value", `{ "nested": { "k": "v" } }`},
		{"array value", `{ "list": [1, 2, 3] }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "meta-nonscalar", "title": "Meta Nonscalar" },
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": { "grid": { "columns": [1] } },
    "metadata": ` + tc.value + `,
    "children": []
  }
}`
			err := resolveDocErr(t, doc)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			// The dashboard schema's scalarMetadata also rejects non-scalars
			// structurally; whichever fires, the resolver's scalar guard is the
			// path-bearing one we assert here when it reaches the resolver. The
			// structural pass runs first, so a non-scalar surfaces as a document
			// validation failure OR the resolver's scalar code — accept either, but
			// prefer asserting the resolver code is reachable via a direct unit test
			// below.
			if !errors.HasCode(err, errors.METADATA_VALUE_NOT_SCALAR) &&
				!errors.HasCode(err, errors.RESOLVE_DOCUMENT_INVALID) {
				t.Fatalf("error = %v, want %s or %s", err,
					errors.METADATA_VALUE_NOT_SCALAR, errors.RESOLVE_DOCUMENT_INVALID)
			}
		})
	}
}

// TestCheckScalarMetadataUnit drives the resolver's scalar guard directly to
// prove it rejects non-scalars with the path+key named — the resolver-level
// defense-in-depth over the schema's structural scalarMetadata constraint.
func TestCheckScalarMetadataUnit(t *testing.T) {
	// Scalars pass.
	ok := map[string]any{"s": "x", "n": float64(1), "b": true, "z": nil}
	if err := checkScalarMetadata(ok, "root"); err != nil {
		t.Fatalf("scalar metadata rejected: %v", err)
	}

	// An object value fails fast, naming path + key.
	bad := map[string]any{"nested": map[string]any{"k": "v"}}
	err := checkScalarMetadata(bad, "root.children[2]")
	if err == nil {
		t.Fatal("expected error for object value, got nil")
	}
	if !errors.HasCode(err, errors.METADATA_VALUE_NOT_SCALAR) {
		t.Fatalf("error = %v, want %s", err, errors.METADATA_VALUE_NOT_SCALAR)
	}
	var ce *errors.CodedError
	if !asCoded(err, &ce) {
		t.Fatalf("error is not a CodedError: %v", err)
	}
	if got, _ := ce.Details["path"].(string); got != "root.children[2]" {
		t.Errorf("error path = %q, want root.children[2]", got)
	}
	if got, _ := ce.Details["key"].(string); got != "nested" {
		t.Errorf("error key = %q, want nested", got)
	}

	// An array value also fails.
	if err := checkScalarMetadata(map[string]any{"list": []any{1, 2}}, "root"); !errors.HasCode(err, errors.METADATA_VALUE_NOT_SCALAR) {
		t.Errorf("array value: error = %v, want %s", err, errors.METADATA_VALUE_NOT_SCALAR)
	}
}

// TestMetadataEligibilityUnit drives the eligibility predicate's INTENT directly
// through resolveMetadata: a wrapper and a regions-or-wrappers container accept
// metadata; an ineligible type (nil resolved type, standing in for any
// leaf/widget/form path) is rejected; and the root override accepts regardless.
func TestMetadataEligibilityUnit(t *testing.T) {
	meta := map[string]any{"k": "v"}

	// A nil type is ineligible (a guarded inconsistency) — but the document root
	// override accepts it regardless of type.
	if _, err := resolveMetadata(nil, meta, "root.children[0]", false); !errors.HasCode(err, errors.METADATA_NOT_ELIGIBLE) {
		t.Errorf("ineligible (nil type): err = %v, want %s", err, errors.METADATA_NOT_ELIGIBLE)
	}
	if got, err := resolveMetadata(nil, meta, "root", true); err != nil {
		t.Errorf("root override rejected: %v", err)
	} else if !reflect.DeepEqual(got, meta) {
		t.Errorf("root metadata = %v, want %v", got, meta)
	}

	// Empty metadata is always a no-op returning nil (byte-identical passthrough).
	if got, err := resolveMetadata(nil, nil, "root.children[0]", false); err != nil || got != nil {
		t.Errorf("empty metadata: got %v, err %v; want nil, nil", got, err)
	}
}

// TestMetadataFreeDocUnchanged asserts a document declaring no metadata anywhere
// carries no `metadata` on any resolved node — the passthrough is absent, not an
// empty map, so the resolved shape is byte-identical to before E1-S2.
func TestMetadataFreeDocUnchanged(t *testing.T) {
	tree := resolveDoc(t, blockWrapDoc)
	var walk func(n *ResolvedInstance)
	walk = func(n *ResolvedInstance) {
		if n.Metadata != nil {
			t.Errorf("node %q carries metadata %v; a metadata-free doc must emit none", n.ID, n.Metadata)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(tree.Root)
}
