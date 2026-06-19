package resolver

import (
	"bytes"
	"encoding/json"
	stderrors "errors"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
)

// update regenerates the *.golden.json files instead of comparing against them.
// Run: go test ./internal/resolver -run TestResolveGolden -update
//
// This is the golden-test pattern E5-S1 extends: drop a *.json document into
// testdata/valid/, run with -update to mint its golden, and the table below
// picks it up automatically (see TestResolveGolden's directory walk).
var update = flag.Bool("update", false, "update golden files")

// repoSchemasDir is the real schema catalog, relative to this package. The
// resolver reads the dashboard schema and item-type catalog from here, so golden
// tests exercise the exact schemas shipped in the repo.
const repoSchemasDir = "../../schemas"

// newRepoResolver builds a Resolver over the real on-disk schema catalog.
func newRepoResolver(t *testing.T) *Resolver {
	t.Helper()
	fs := afero.NewOsFs()

	data, err := os.ReadFile(filepath.Join(repoSchemasDir, "dashboard.schema.json"))
	if err != nil {
		t.Fatalf("read dashboard schema: %v", err)
	}
	var dashSch jsonschema.Schema
	if err := dashSch.UnmarshalJSON(data); err != nil {
		t.Fatalf("parse dashboard schema: %v", err)
	}

	res, err := New(fs, &dashSch, []string{repoSchemasDir})
	if err != nil {
		t.Fatalf("New resolver: %v", err)
	}
	return res
}

// TestResolveGolden resolves every document under testdata/valid/*.json and
// compares the emitted resolved tree against a sibling *.golden.json. With
// -update the goldens are (re)written. This is the reusable harness; adding a
// case is just adding a document file.
func TestResolveGolden(t *testing.T) {
	docs, err := filepath.Glob(filepath.Join("testdata", "valid", "*.json"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	// Exclude the golden files themselves from the document set.
	var inputs []string
	for _, d := range docs {
		if !isGolden(d) {
			inputs = append(inputs, d)
		}
	}
	if len(inputs) == 0 {
		t.Fatal("no valid testdata documents found")
	}

	for _, doc := range inputs {
		doc := doc
		t.Run(filepath.Base(doc), func(t *testing.T) {
			res := newRepoResolver(t)
			tree, err := res.Resolve(doc)
			if err != nil {
				t.Fatalf("Resolve(%s): %v", doc, err)
			}

			got, err := json.MarshalIndent(tree, "", "  ")
			if err != nil {
				t.Fatalf("marshal tree: %v", err)
			}
			got = append(got, '\n')

			golden := goldenPath(doc)
			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}

			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden %s (run -update to create): %v", golden, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("resolved tree mismatch for %s\n--- got ---\n%s\n--- want ---\n%s",
					filepath.Base(doc), got, want)
			}
		})
	}
}

// TestResolveErrors resolves intentionally-broken documents and asserts the
// first CodedError carries the expected code (fail-fast, no aggregation). For
// instance-scoped errors it also asserts the reported path, since naming the
// offending instance is part of the contract.
func TestResolveErrors(t *testing.T) {
	tests := []struct {
		name     string
		doc      string
		wantCode errors.Code
		wantPath string // expected Details["path"]; "" to skip
	}{
		{
			name:     "missing required manifest field",
			doc:      "testdata/invalid/missing-manifest-title.json",
			wantCode: errors.RESOLVE_DOCUMENT_INVALID,
		},
		{
			name:     "instance config violates item-type schema",
			doc:      "testdata/invalid/bad-table-config.json",
			wantCode: errors.RESOLVE_CONFIG_INVALID,
			wantPath: "root.children[0]",
		},
		{
			name:     "children on a non-container type",
			doc:      "testdata/invalid/children-on-table.json",
			wantCode: errors.RESOLVE_CHILDREN_NOT_ALLOWED,
			wantPath: "root",
		},
		{
			name:     "unknown item-type version",
			doc:      "testdata/invalid/unknown-version.json",
			wantCode: errors.SCHEMA_VERSION_MISMATCH,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := newRepoResolver(t)
			_, err := res.Resolve(tc.doc)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.HasCode(err, tc.wantCode) {
				t.Fatalf("error = %v, want code %s", err, tc.wantCode)
			}
			if tc.wantPath != "" {
				var ce *errors.CodedError
				if !asCoded(err, &ce) {
					t.Fatalf("error is not a CodedError: %v", err)
				}
				if got, _ := ce.Details["path"].(string); got != tc.wantPath {
					t.Errorf("error path = %q, want %q", got, tc.wantPath)
				}
			}
		})
	}
}

func isGolden(p string) bool {
	return filepath.Ext(p) == ".json" &&
		len(p) > len(".golden.json") &&
		p[len(p)-len(".golden.json"):] == ".golden.json"
}

func goldenPath(doc string) string {
	return doc[:len(doc)-len(".json")] + ".golden.json"
}

// asCoded unwraps the chain to the first *CodedError.
func asCoded(err error, target **errors.CodedError) bool {
	return stderrors.As(err, target)
}
