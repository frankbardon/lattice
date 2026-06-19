package resolver

import (
	stderrors "errors"
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
        "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
        "id": "body",
        "config": {"grid": {"columns": [1]}},
        "children": [
          {
            "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
            "config": {
              "id": "leaf-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
                "id": "leaf",
                "config": {"title": "T", "columns": [{"header": "H"}], "rows": [["v"]]}
              }
            }
          }
        ]
      }
    ]
  }
}`

// TestVariableEnvironmentAttached resolves a document with nested variable
// declarations and asserts the per-node environment honours shadowing and
// records var->node visibility via DeclaredAt.
func TestVariableEnvironmentAttached(t *testing.T) {
	res := newRepoResolver(t)
	tree, err := res.resolveBytes([]byte(docWithVars), "inline", nil)
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

// docInterpolated drives the E3-S2 interpolation pass through resolveBytes: a
// ${} template and a {"$var"} typed binding in instance config, resolved against
// the per-node environment. The root container shadows the doc-scope "region",
// so the leaf interpolates "eu" (the nearest declaration), proving interpolation
// runs against the correctly-scoped environment.
const docInterpolated = `{
  "manifest": {"formatVersion": "1.0.0", "id": "interp", "title": "Interp"},
  "variables": [
    {"name": "region", "type": "string", "default": "us"},
    {"name": "rowCount", "type": "integer", "default": 2}
  ],
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": {"grid": {"columns": [1]}},
    "variables": [{"name": "region", "type": "string", "default": "eu"}],
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
        "id": "body",
        "config": {"grid": {"columns": [1]}},
        "children": [
          {
            "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
            "id": "leaf-block",
            "config": {
              "id": "leaf-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
                "id": "leaf",
                "config": {"title": "Report for ${region}", "columns": [{"header": "H"}], "rows": [["${region}"]]}
              }
            }
          }
        ]
      }
    ]
  }
}`

// TestResolveInterpolatesConfig confirms that interpolation runs during the
// instance walk (before config validation) and substitutes scoped values: the
// leaf's ${region} template resolves to the shadowing winner "eu", not the
// doc-scope "us".
func TestResolveInterpolatesConfig(t *testing.T) {
	res := newRepoResolver(t)
	tree, err := res.resolveBytes([]byte(docInterpolated), "inline", nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// root container -> body region -> block wrapper -> table content leaf
	leaf := tree.Root.Children[0].Children[0].Children[0]
	if got := leaf.Config["title"]; got != "Report for eu" {
		t.Errorf("title = %v, want %q", got, "Report for eu")
	}
	rows, ok := leaf.Config["rows"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("rows = %#v", leaf.Config["rows"])
	}
	row0 := rows[0].([]any)
	if row0[0] != "eu" {
		t.Errorf("rows[0][0] = %v, want eu", row0[0])
	}
}

// TestResolveTypedBindingPreservesType confirms a {"$var"} typed binding keeps
// the variable's JSON type and that a config requiring a concrete typed value
// still passes Pass 2 validation because interpolation runs first.
func TestResolveTypedBindingPreservesType(t *testing.T) {
	const doc = `{
      "manifest": {"formatVersion": "1.0.0", "id": "bind", "title": "Bind"},
      "variables": [{"name": "gap", "type": "integer", "default": 3}],
      "root": {
        "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
        "id": "root",
        "config": {"grid": {"columns": [1], "gap": {"$var": "gap"}}}
      }
    }`
	res := newRepoResolver(t)
	tree, err := res.resolveBytes([]byte(doc), "inline", nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	grid := tree.Root.Config["grid"].(map[string]any)
	gap, ok := grid["gap"].(float64)
	if !ok {
		t.Fatalf("gap is %T, want float64 (type preserved)", grid["gap"])
	}
	if gap != 3 {
		t.Errorf("gap = %v, want 3", gap)
	}
}

// TestResolveMissingVarFailsFast confirms an undeclared reference in instance
// config surfaces as a fail-fast VAR_UNDEFINED CodedError naming the path.
func TestResolveMissingVarFailsFast(t *testing.T) {
	const doc = `{
      "manifest": {"formatVersion": "1.0.0", "id": "miss", "title": "Miss"},
      "root": {
        "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
        "id": "root",
        "config": {"grid": {"columns": [1]}},
        "children": [
          {
            "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
            "id": "leaf",
            "config": {"title": "${ghost}", "columns": [{"header": "H"}], "rows": [["x"]]}
          }
        ]
      }
    }`
	res := newRepoResolver(t)
	_, err := res.resolveBytes([]byte(doc), "inline", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.HasCode(err, errors.VAR_UNDEFINED) {
		t.Fatalf("expected VAR_UNDEFINED, got %v", err)
	}
	var ce *errors.CodedError
	if stderrors.As(err, &ce) && ce.Details["path"] != "root.children[0]" {
		t.Errorf("path = %v, want root.children[0]", ce.Details["path"])
	}
}

// TestResolveExternalOverride confirms a runtime override (E3-S4) replaces a
// settable variable's effective value (override > default) and flows through
// interpolation: the leaf's ${region} template and {"$var":"region"} binding
// pick up the override "apac" rather than the declared default "us".
func TestResolveExternalOverride(t *testing.T) {
	res := newRepoResolver(t)
	tree, err := res.resolveBytes([]byte(docInterpolated), "inline",
		map[string]any{"region": "apac"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// The override applies wherever the name is declared. docInterpolated shadows
	// "region" at root with default "eu"; the override replaces that effective
	// value too, so the leaf interpolates "apac".
	// root container -> body region -> block wrapper -> table content leaf
	leaf := tree.Root.Children[0].Children[0].Children[0]
	if got := leaf.Config["title"]; got != "Report for apac" {
		t.Errorf("title = %v, want %q", got, "Report for apac")
	}
	region, ok := tree.Root.VarEnv.Lookup("region")
	if !ok {
		t.Fatal("region not visible at root")
	}
	if region.Default != "apac" {
		t.Errorf("root region effective value = %v, want apac", region.Default)
	}
}

// TestResolveOverrideCoercesString confirms a string-encoded override (the form
// URL query params deliver) is coerced to the variable's declared type before
// it enters the environment.
func TestResolveOverrideCoercesString(t *testing.T) {
	const doc = `{
      "manifest": {"formatVersion": "1.0.0", "id": "ov", "title": "Ov"},
      "variables": [{"name": "gap", "type": "integer", "default": 1}],
      "root": {
        "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
        "id": "root",
        "config": {"grid": {"columns": [1], "gap": {"$var": "gap"}}}
      }
    }`
	res := newRepoResolver(t)
	tree, err := res.resolveBytes([]byte(doc), "inline", map[string]any{"gap": "4"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	grid := tree.Root.Config["grid"].(map[string]any)
	gap, ok := grid["gap"].(float64)
	if !ok || gap != 4 {
		t.Errorf("gap = %#v, want 4 (string override coerced to integer)", grid["gap"])
	}
}

// TestResolveOverrideDoesNotApplyToComputed confirms a computed (expr) variable
// is never overridable: an override for its name is ignored and it keeps its
// evaluated value.
func TestResolveOverrideDoesNotApplyToComputed(t *testing.T) {
	const doc = `{
      "manifest": {"formatVersion": "1.0.0", "id": "comp", "title": "Comp"},
      "variables": [
        {"name": "base", "type": "integer", "default": 2},
        {"name": "doubled", "type": "integer", "expr": "base * 2"}
      ],
      "root": {
        "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
        "id": "root",
        "config": {"grid": {"columns": [1], "gap": {"$var": "doubled"}}}
      }
    }`
	res := newRepoResolver(t)
	// Override base (settable) -> 5; doubled must recompute to 10 and ignore its
	// own override attempt (99).
	tree, err := res.resolveBytes([]byte(doc), "inline",
		map[string]any{"base": "5", "doubled": "99"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	grid := tree.Root.Config["grid"].(map[string]any)
	gap, _ := grid["gap"].(float64)
	if gap != 10 {
		t.Errorf("computed doubled = %v, want 10 (recomputed from overridden base, not overridden directly)", gap)
	}
}

// TestResolveBadOverrideFailsFast confirms a runtime override that violates its
// variable's declared type/enum surfaces as a fail-fast VAR_* CodedError.
func TestResolveBadOverrideFailsFast(t *testing.T) {
	tests := []struct {
		name     string
		override map[string]any
		wantCode errors.Code
	}{
		{"out of enum", map[string]any{"region": "moon"}, errors.VAR_OPTIONS_INVALID},
	}
	const doc = `{
      "manifest": {"formatVersion": "1.0.0", "id": "bad", "title": "Bad"},
      "variables": [{"name": "region", "type": "enum", "default": "us", "options": ["us", "eu"]}],
      "root": {
        "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
        "id": "root",
        "config": {"grid": {"columns": [1]}}
      }
    }`
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := newRepoResolver(t)
			_, err := res.resolveBytes([]byte(doc), "inline", tt.override)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.HasCode(err, tt.wantCode) {
				t.Fatalf("expected %s, got %v", tt.wantCode, err)
			}
		})
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
			_, err := res.resolveBytes([]byte(tt.doc), "inline", nil)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.HasCode(err, tt.wantCode) {
				t.Fatalf("expected %s, got %v", tt.wantCode, err)
			}
		})
	}
}
