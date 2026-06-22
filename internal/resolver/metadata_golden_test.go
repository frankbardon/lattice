package resolver

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// metadata_golden_test.go — E1-S3 regression net for element metadata.
//
// E1-S2 proved metadata threading with inline documents (metadata_test.go).
// This file LOCKS that behavior with a committed golden fixture and rounds out
// the ineligibility surface so every node kind the resolver refuses metadata on
// is exercised:
//
//   - a committed fixture (testdata/valid/element-metadata.json) resolves and
//     carries metadata verbatim on the document root (merging the top-level
//     document `metadata` with the root instance's own), a regions-or-wrappers
//     container, and a block wrapper — locked byte-for-byte by the baseline
//     table (TestBuiltinGoldenBaseline) AND re-asserted structurally here;
//   - metadata on a WIDGET (a form input) and on a content LEAF (markdown) each
//     fail fast with METADATA_NOT_ELIGIBLE, path + type named (the form case is
//     in metadata_test.go; these complete the leaf/widget surface);
//   - a non-scalar metadata value is rejected by the dashboard schema's
//     structural scalarMetadata constraint, which runs BEFORE the resolver's
//     own scalar guard, so it surfaces as RESOLVE_DOCUMENT_INVALID end to end
//     (the resolver's METADATA_VALUE_NOT_SCALAR guard is unit-proven as
//     defense-in-depth in metadata_test.go's TestCheckScalarMetadataUnit).

// elementMetadataFixture is the committed golden fixture this story locks. It is
// also a row in BuiltinGoldenBaselineCases (golden_baseline_test.go) and is
// walked by TestResolveGolden, so its resolved bytes are regression-locked from
// three angles; this constant lets the structural assertion below name the same
// file the goldens pin.
var elementMetadataFixture = filepath.Join(testdataValid, "element-metadata.json")

// TestMetadataGoldenFixtureCarriesVerbatim resolves the committed golden fixture
// and asserts metadata lands verbatim on the eligible nodes — the document root
// (document-scope metadata merged with the root instance's own), the nested
// container, and the block wrapper — with every scalar value intact and NOT
// lifted onto the block's wrapped content. This is the structural twin of the
// byte-identical golden lock in TestBuiltinGoldenBaseline.
func TestMetadataGoldenFixtureCarriesVerbatim(t *testing.T) {
	res := newRepoResolver(t)
	tree, err := res.Resolve(elementMetadataFixture)
	if err != nil {
		t.Fatalf("Resolve(%s): %v", elementMetadataFixture, err)
	}

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

	// The nested container carries its own metadata verbatim, including null.
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

	// The wrapped content (a table leaf) does NOT inherit the wrapper's metadata.
	inner := wrapper.Children[0]
	if inner.Metadata != nil {
		t.Errorf("inner content metadata = %v, want nil (wrapper metadata must NOT be lifted onto content)", inner.Metadata)
	}
}

// TestMetadataOnWidgetRejected asserts metadata on a widget (a form text-input,
// role==widget) fails fast with METADATA_NOT_ELIGIBLE, naming the offending path
// and type.
func TestMetadataOnWidgetRejected(t *testing.T) {
	doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "meta-widget", "title": "Meta Widget" },
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
                "children": [
                  {
                    "$ref": "https://lattice.dev/schemas/items/text-input/1.0.0",
                    "metadata": { "owner": "nope" },
                    "config": { "variable": "region" }
                  }
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
	if got, _ := ce.Details["type"].(string); got != "text-input" {
		t.Errorf("error type = %q, want text-input", got)
	}
	if got, _ := ce.Details["path"].(string); got == "" {
		t.Error("error path is empty; want the offending node path named")
	}
}

// TestMetadataOnContentLeafRejected asserts metadata on a content leaf (a
// block-wrapped markdown leaf, role==none) fails fast with
// METADATA_NOT_ELIGIBLE, naming the offending path and type. Eligibility is the
// wrapper's concern, not the leaf's.
func TestMetadataOnContentLeafRejected(t *testing.T) {
	doc := `{
  "manifest": { "formatVersion": "1.0.0", "id": "meta-leaf", "title": "Meta Leaf" },
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
                "id": "md",
                "metadata": { "owner": "nope" },
                "config": { "source": "hi" }
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
	if got, _ := ce.Details["type"].(string); got != "markdown" {
		t.Errorf("error type = %q, want markdown", got)
	}
	if got, _ := ce.Details["path"].(string); got == "" {
		t.Error("error path is empty; want the offending node path named")
	}
}

// TestMetadataNonScalarRejectedAtDocument asserts a non-scalar metadata value is
// rejected end to end. The dashboard schema's scalarMetadata $def constrains the
// slot structurally, and the structural pass runs BEFORE the resolver's own
// scalar guard, so the failure surfaces as RESOLVE_DOCUMENT_INVALID — the
// document-validation code, NOT the resolver's METADATA_VALUE_NOT_SCALAR (that
// guard is defense-in-depth, unit-proven in metadata_test.go).
func TestMetadataNonScalarRejectedAtDocument(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"object value", `{ "nested": { "k": "v" } }`},
		{"array value", `{ "list": [1, 2, 3] }`},
	}
	for _, tc := range cases {
		tc := tc
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
			if !errors.HasCode(err, errors.RESOLVE_DOCUMENT_INVALID) {
				t.Fatalf("error = %v, want %s (schema structural pass fires before the resolver guard)",
					err, errors.RESOLVE_DOCUMENT_INVALID)
			}
		})
	}
}
