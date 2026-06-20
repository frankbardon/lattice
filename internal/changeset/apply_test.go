package changeset

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/resolver"
)

// schemasDir is the real on-disk schema catalog, relative to this package — the
// exact schemas the binary ships with (mirrors the CLI golden harness). Resolving
// the example fixtures against it gives a real ResolvedTree whose configurable
// surfaces the field-edit guardrail is checked against.
const schemasDir = "../../schemas"

// minimalDocPath is a shipped fixture whose resolved tree carries real surfaces: a
// `fruits` table (surface: title, columns) wrapped in a block, plus the document
// `$manifest` (title, description) and `$theme` (emphasis enum, …) scope surfaces.
const minimalDocPath = "../../examples/minimal-dashboard.json"

// resolveFixture loads minimalDocPath, returning its raw bytes and its resolved
// tree (the surface source the guardrail consults). It builds a resolver over the
// shipped schema catalog, exactly as the CLI does.
func resolveFixture(t *testing.T) ([]byte, *resolver.ResolvedTree) {
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

	res, err := resolver.New(fs, &dashSch, []string{schemasDir})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}

	docBytes, err := afero.ReadFile(fs, minimalDocPath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	tree, err := res.Resolve(minimalDocPath)
	if err != nil {
		t.Fatalf("resolve fixture: %v", err)
	}
	return docBytes, tree
}

// parse builds a changeset from a JSON Patch array literal.
func parse(t *testing.T, patch string) *Changeset {
	t.Helper()
	cs, err := Parse([]byte(patch))
	if err != nil {
		t.Fatalf("parse changeset %q: %v", patch, err)
	}
	return cs
}

// readField navigates the canonical document bytes to a nested field by keys,
// failing the test if any segment is missing.
func readField(t *testing.T, doc []byte, keys ...string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(doc, &v); err != nil {
		t.Fatalf("decode result doc: %v", err)
	}
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("path segment %q: not an object", k)
		}
		v, ok = m[k]
		if !ok {
			t.Fatalf("path segment %q: missing", k)
		}
	}
	return v
}

func TestApplyChangeset_SurfacedFieldAccepted(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	// `fruits.title` (item surface) and `$manifest.title` (scope surface) are both
	// surfaced; replacing them is accepted.
	cs := parse(t, `[
		{"op":"replace","path":"/fruits/config/title","value":"Citrus"},
		{"op":"replace","path":"/$manifest/title","value":"Renamed"}
	]`)

	out, err := ApplyChangeset(docBytes, cs, tree)
	if err != nil {
		t.Fatalf("ApplyChangeset: %v", err)
	}

	// The block-wrapped table's title moved to "Citrus" (asserted by substring on
	// the canonical bytes, since the table is nested behind a block content slot).
	if !bytes.Contains(out, []byte(`"Citrus"`)) {
		t.Fatalf("expected fruits title replaced with Citrus, got:\n%s", out)
	}
	if title := readField(t, out, "manifest", "title"); title != "Renamed" {
		t.Fatalf("manifest title = %v, want Renamed", title)
	}
	if bytes.Contains(out, []byte(`"Fruits"`)) {
		t.Fatalf("old fruits title should be gone, got:\n%s", out)
	}
}

func TestApplyChangeset_OffSurfaceFieldRejected(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	cases := map[string]string{
		// `rows` is a real table config field but NOT on the table's surface.
		"unsurfaced item field": `[{"op":"replace","path":"/fruits/config/rows","value":[]}]`,
		// A nested path into a surfaced array field — surfaces cover top-level only.
		"nested path": `[{"op":"replace","path":"/fruits/config/columns/0/header","value":"X"}]`,
		// The item node addressed as a whole (no config/<field>).
		"node as a whole": `[{"op":"replace","path":"/fruits/config","value":{}}]`,
		// A field not under the item's `config`.
		"non-config path": `[{"op":"replace","path":"/fruits/id","value":"x"}]`,
		// A scope field not on the $manifest surface (formatVersion is not settable).
		"unsurfaced scope field": `[{"op":"replace","path":"/$manifest/formatVersion","value":"2.0.0"}]`,
	}
	for name, patch := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ApplyChangeset(docBytes, parse(t, patch), tree)
			hasCode(t, err, errors.CONFIG_OVERRIDE_FIELD_UNKNOWN)
		})
	}
}

func TestApplyChangeset_BadValueRejected(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	// `$manifest.title` is a string surface field; a number value is the wrong type.
	cs := parse(t, `[{"op":"replace","path":"/$manifest/title","value":42}]`)
	_, err := ApplyChangeset(docBytes, cs, tree)
	hasCode(t, err, errors.CONFIG_OVERRIDE_VALUE_INVALID)
}

func TestApplyChangeset_BadEnumValueRejected(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	// `$theme.emphasis` is an enum (none/low/high); an out-of-set value is rejected
	// by the enum-membership check even though the type (string) matches.
	cs := parse(t, `[{"op":"replace","path":"/$theme/emphasis","value":"loud"}]`)
	_, err := ApplyChangeset(docBytes, cs, tree)
	hasCode(t, err, errors.CONFIG_OVERRIDE_VALUE_INVALID)
}

func TestApplyChangeset_CanonicalOutputStable(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	// An empty changeset is a no-op: applying it then canonically marshaling an
	// already-canonical document must round-trip to identical bytes.
	empty := parse(t, `[]`)
	first, err := ApplyChangeset(docBytes, empty, tree)
	if err != nil {
		t.Fatalf("ApplyChangeset (no-op): %v", err)
	}
	// Re-applying the no-op to the canonical output yields identical bytes.
	second, err := ApplyChangeset(first, empty, tree)
	if err != nil {
		t.Fatalf("ApplyChangeset (re-run): %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("canonical output not stable across re-runs:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}

	// The canonical form has sorted keys: "manifest" sorts before "root".
	mi := bytes.Index(first, []byte(`"manifest"`))
	ri := bytes.Index(first, []byte(`"root"`))
	if mi < 0 || ri < 0 || mi > ri {
		t.Fatalf("expected sorted top-level keys (manifest before root), got:\n%s", first)
	}
	// And a fixed 2-space indent (the manifest object opens on its own indented line).
	if !bytes.Contains(first, []byte("\n  \"manifest\": {")) {
		t.Fatalf("expected 2-space indented manifest, got:\n%s", first)
	}
}

func TestApplyChangeset_TestOpMismatchRejected(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	// A `test` op whose expected value does not match the document fails at apply
	// time (the standard applier's precondition), surfacing as PATCH_APPLY_FAILED.
	// `title` is surfaced, so it passes the guardrail and reaches the applier.
	cs := parse(t, `[{"op":"test","path":"/$manifest/title","value":"Wrong"}]`)
	_, err := ApplyChangeset(docBytes, cs, tree)
	hasCode(t, err, errors.PATCH_APPLY_FAILED)
}
