package changeset

import (
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/resolver"
)

// variablesOf decodes the canonical output and returns the document's
// `variables` array (the physical landing of the `$variables` scope), failing
// the test if it is absent or not an array.
func variablesOf(t *testing.T, doc []byte) []any {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(doc, &root); err != nil {
		t.Fatalf("decode result doc: %v", err)
	}
	v, ok := root["variables"].([]any)
	if !ok {
		t.Fatalf("result doc has no variables array: %v", root["variables"])
	}
	return v
}

// reResolveOK writes the mutated doc and asserts it re-resolves cleanly — the
// positive counterpart to reResolveRejects, proving a $variables edit produces a
// document the resolver's variables pass accepts.
func reResolveOK(t *testing.T, mutated []byte) {
	t.Helper()
	fs := afero.NewOsFs()
	schemaBytes, err := afero.ReadFile(fs, schemasDir+"/dashboard.schema.json")
	if err != nil {
		t.Fatalf("read dashboard schema: %v", err)
	}
	var dashSch jsonschema.Schema
	if err := dashSch.UnmarshalJSON(schemaBytes); err != nil {
		t.Fatalf("parse dashboard schema: %v", err)
	}
	docPath := "../../examples/variables-reresolve-" + t.Name() + ".json"
	if err := afero.WriteFile(fs, docPath, mutated, 0o600); err != nil {
		t.Fatalf("write mutated doc: %v", err)
	}
	t.Cleanup(func() { fs.Remove(docPath) })
	res, err := resolver.New(fs, &dashSch, []string{schemasDir})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	if _, err := res.Resolve(docPath); err != nil {
		t.Fatalf("mutated doc should re-resolve cleanly: %v", err)
	}
}

// TestApplyToBytes_VariablesScopeAdds proves a `$variables` whole-scope add is
// routed PAST the configurable-surface guardrail (no scope surface exists for
// $variables) and persists the variable-declaration array, and the mutated
// document re-resolves cleanly. This is the dashboard variables-manager save path.
func TestApplyToBytes_VariablesScopeAdds(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	cs := parse(t, `[{"op":"add","path":"/$variables","value":[{"name":"region","type":"enum","default":"us","options":["us","eu","apac"]}]}]`)
	out, err := applyToBytes(docBytes, cs, tree)
	if err != nil {
		t.Fatalf("$variables add should apply past the surface guardrail: %v", err)
	}

	vars := variablesOf(t, out)
	if len(vars) != 1 {
		t.Fatalf("want 1 variable persisted, got %d", len(vars))
	}
	if v := vars[0].(map[string]any); v["name"] != "region" || v["default"] != "us" {
		t.Fatalf("persisted variable wrong: %v", v)
	}
	reResolveOK(t, out)
}

// TestApplyToBytes_VariableAppendAndFieldEdit proves appending a declaration
// (`/$variables/-`) and editing a field within one (`/$variables/0/default`) both
// bypass the surface and persist, when the array already exists.
func TestApplyToBytes_VariableAppendAndFieldEdit(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	// Seed the array, then append + edit in a second changeset against the seeded doc.
	seed := parse(t, `[{"op":"add","path":"/$variables","value":[{"name":"region","type":"enum","default":"us","options":["us","eu"]}]}]`)
	seeded, err := applyToBytes(docBytes, seed, tree)
	if err != nil {
		t.Fatalf("seed variables: %v", err)
	}
	seededTree := reResolved(t, seeded)

	cs := parse(t, `[
		{"op":"add","path":"/$variables/-","value":{"name":"window","type":"number","default":7}},
		{"op":"replace","path":"/$variables/0/default","value":"eu"}
	]`)
	out, err := applyToBytes(seeded, cs, seededTree)
	if err != nil {
		t.Fatalf("append + field edit should apply: %v", err)
	}

	vars := variablesOf(t, out)
	if len(vars) != 2 {
		t.Fatalf("want 2 variables, got %d", len(vars))
	}
	if vars[0].(map[string]any)["default"] != "eu" {
		t.Fatalf("region default not updated: %v", vars[0])
	}
	if vars[1].(map[string]any)["name"] != "window" {
		t.Fatalf("appended variable wrong: %v", vars[1])
	}
	reResolveOK(t, out)
}

// TestApplyToBytes_InvalidVariableRejectedByReResolve proves the apply stage lets
// a variables edit through ungated (like placement/metadata) but RE-RESOLVE is the
// gate: a duplicate variable name is rejected with VAR_DECLARATION_INVALID, so an
// invalid declaration never persists silently.
func TestApplyToBytes_InvalidVariableRejectedByReResolve(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	cs := parse(t, `[{"op":"add","path":"/$variables","value":[{"name":"dup","type":"enum","default":"a","options":["a","b"]},{"name":"dup","type":"enum","default":"a","options":["a","b"]}]}]`)
	out, err := applyToBytes(docBytes, cs, tree)
	if err != nil {
		t.Fatalf("apply stage should NOT gate variable validity: %v", err)
	}
	reReResolveRejectsVar(t, out)
}

// reResolved re-resolves mutated bytes and returns the tree (for a follow-up apply
// whose surface index must reflect the new document).
func reResolved(t *testing.T, mutated []byte) *resolver.ResolvedTree {
	t.Helper()
	fs := afero.NewOsFs()
	schemaBytes, err := afero.ReadFile(fs, schemasDir+"/dashboard.schema.json")
	if err != nil {
		t.Fatalf("read dashboard schema: %v", err)
	}
	var dashSch jsonschema.Schema
	if err := dashSch.UnmarshalJSON(schemaBytes); err != nil {
		t.Fatalf("parse dashboard schema: %v", err)
	}
	docPath := "../../examples/variables-seeded-" + t.Name() + ".json"
	if err := afero.WriteFile(fs, docPath, mutated, 0o600); err != nil {
		t.Fatalf("write seeded doc: %v", err)
	}
	t.Cleanup(func() { fs.Remove(docPath) })
	res, err := resolver.New(fs, &dashSch, []string{schemasDir})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	tree, err := res.Resolve(docPath)
	if err != nil {
		t.Fatalf("re-resolve seeded doc: %v", err)
	}
	return tree
}

// reReResolveRejectsVar asserts the mutated doc fails re-resolve with the
// variable-declaration code.
func reReResolveRejectsVar(t *testing.T, mutated []byte) {
	t.Helper()
	reResolveRejects(t, mutated, errors.VAR_DECLARATION_INVALID)
}
