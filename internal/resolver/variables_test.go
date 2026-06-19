package resolver

import (
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// docWithVars is a minimal valid dashboard carrying doc-scope and
// container-scoped variable declarations, used to exercise the per-node
// environment attachment (E3-S1). The shapes match the real catalog schemas so
// the document passes the structural + config passes that run before the
// variable pass.
const docWithVars = `{
  "manifest": {"formatVersion": "1.0.0", "id": "vars", "title": "Vars"},
  "variables": [
    {"name": "region", "type": "string", "default": "us"},
    {"name": "limit", "type": "integer", "default": 10}
  ],
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": {"grid": {"columns": [1]}},
    "variables": [
      {"name": "region", "type": "string", "default": "eu"}
    ],
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
        "id": "leaf",
        "config": {"title": "T", "columns": [{"header": "H"}], "rows": [["v"]]}
      }
    ]
  }
}`

// TestVariableEnvironmentAttached resolves a document with nested variable
// declarations and asserts the per-node environment honours shadowing and
// records var->node visibility via DeclaredAt.
func TestVariableEnvironmentAttached(t *testing.T) {
	res := newRepoResolver(t)
	tree, err := res.resolveBytes([]byte(docWithVars), "inline")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	root := tree.Root
	if root == nil {
		t.Fatal("nil root")
	}

	// Root container shadows doc-scope "region" and inherits "limit".
	region, ok := root.VarEnv.Lookup("region")
	if !ok {
		t.Fatal("region not visible at root")
	}
	if region.Default != "eu" {
		t.Errorf("root region default = %v, want eu", region.Default)
	}
	if region.DeclaredAt != "root" {
		t.Errorf("root region declaredAt = %q, want root", region.DeclaredAt)
	}
	limit, ok := root.VarEnv.Lookup("limit")
	if !ok {
		t.Fatal("limit not visible at root")
	}
	if limit.DeclaredAt != "doc" {
		t.Errorf("limit declaredAt = %q, want doc", limit.DeclaredAt)
	}

	// Leaf inherits the root container's shadowed view.
	if len(root.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(root.Children))
	}
	leaf := root.Children[0]
	leafRegion, ok := leaf.VarEnv.Lookup("region")
	if !ok {
		t.Fatal("region not visible at leaf")
	}
	if leafRegion.DeclaredAt != "root" {
		t.Errorf("leaf region declaredAt = %q, want root", leafRegion.DeclaredAt)
	}
}

// TestVariableEnvironmentEmptyOmitted confirms a document without variables
// leaves VarEnv empty (so the resolved-tree golden contract is unchanged).
func TestVariableEnvironmentEmptyOmitted(t *testing.T) {
	res := newRepoResolver(t)
	tree, err := res.Resolve("testdata/valid/minimal-dashboard.json")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(tree.Root.VarEnv) != 0 {
		t.Errorf("expected empty VarEnv, got %v", tree.Root.VarEnv)
	}
}

// TestResolveVariableErrors asserts invalid declarations surface as fail-fast
// VAR_* CodedErrors naming the offending path.
func TestResolveVariableErrors(t *testing.T) {
	tests := []struct {
		name     string
		doc      string
		wantCode errors.Code
	}{
		{
			name: "bad default type at doc scope",
			doc: `{
              "manifest": {"formatVersion": "1.0.0", "id": "x", "title": "X"},
              "variables": [{"name": "n", "type": "integer", "default": "nope"}],
              "root": {"$ref": "https://lattice.dev/schemas/items/container/1.0.0", "id": "r", "config": {"grid": {"columns": [1]}}}
            }`,
			wantCode: errors.VAR_TYPE,
		},
		{
			name: "enum without options on instance",
			doc: `{
              "manifest": {"formatVersion": "1.0.0", "id": "x", "title": "X"},
              "root": {"$ref": "https://lattice.dev/schemas/items/container/1.0.0", "id": "r", "config": {"grid": {"columns": [1]}}, "variables": [{"name": "e", "type": "enum"}]}
            }`,
			wantCode: errors.VAR_OPTIONS_INVALID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := newRepoResolver(t)
			_, err := res.resolveBytes([]byte(tt.doc), "inline")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.HasCode(err, tt.wantCode) {
				t.Fatalf("expected %s, got %v", tt.wantCode, err)
			}
		})
	}
}
