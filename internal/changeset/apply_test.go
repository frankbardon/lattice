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

func TestApplyToBytes_SurfacedFieldAccepted(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	// `fruits.title` (item surface) and `$manifest.title` (scope surface) are both
	// surfaced; replacing them is accepted.
	cs := parse(t, `[
		{"op":"replace","path":"/fruits/config/title","value":"Citrus"},
		{"op":"replace","path":"/$manifest/title","value":"Renamed"}
	]`)

	out, err := applyToBytes(docBytes, cs, tree)
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

func TestApplyToBytes_NestedSurfacedFieldAccepted(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	// `grid.gap` is a declared NESTED surface entry of the body container (E2-S1):
	// an id-rooted patch targeting the nested path `/body/config/grid/gap` is on the
	// allow-list, so the number replace applies. The seed gap is 1.
	cs := parse(t, `[{"op":"replace","path":"/body/config/grid/gap","value":4}]`)

	out, err := applyToBytes(docBytes, cs, tree)
	if err != nil {
		t.Fatalf("applyToBytes: %v", err)
	}

	// The nested gap is mutated in place; the sibling tracks (columns/rows) are
	// untouched. The body container is root.children[0].
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("decode result doc: %v", err)
	}
	children := doc["root"].(map[string]any)["children"].([]any)
	body := children[0].(map[string]any)
	grid := body["config"].(map[string]any)["grid"].(map[string]any)
	if grid["gap"] != float64(4) {
		t.Fatalf("body grid.gap = %v, want 4", grid["gap"])
	}
	if _, ok := grid["columns"]; !ok {
		t.Fatalf("nested edit dropped sibling grid.columns: %v", grid)
	}
}

func TestApplyToBytes_OffAllowListNestedPathRejected(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	// `grid.foo` is NOT an enumerated nested surface entry (only grid, grid.gap,
	// grid.columns, grid.rows are). A nested path off the allow-list is rejected —
	// the surface stays the single source of truth for nested editability.
	cs := parse(t, `[{"op":"replace","path":"/body/config/grid/foo","value":1}]`)
	_, err := applyToBytes(docBytes, cs, tree)
	hasCode(t, err, errors.CONFIG_OVERRIDE_FIELD_UNKNOWN)
}

func TestApplyToBytes_NestedBadValueRejected(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	// `grid.gap` is a number-typed nested leaf; a string value is the wrong type, so
	// value validation rejects it exactly as it does for a top-level field.
	cs := parse(t, `[{"op":"replace","path":"/body/config/grid/gap","value":"wide"}]`)
	_, err := applyToBytes(docBytes, cs, tree)
	hasCode(t, err, errors.CONFIG_OVERRIDE_VALUE_INVALID)
}

func TestApplyToBytes_OffSurfaceFieldRejected(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	cases := map[string]string{
		// `rows` is a real table config field but NOT on the table's surface.
		"unsurfaced item field": `[{"op":"replace","path":"/fruits/config/rows","value":[]}]`,
		// A nested path into a surfaced array field — the dotted key
		// `columns.0.header` is not an enumerated nested surface entry of the table.
		"unsurfaced nested path": `[{"op":"replace","path":"/fruits/config/columns/0/header","value":"X"}]`,
		// The item node addressed as a whole (no config/<field>).
		"node as a whole": `[{"op":"replace","path":"/fruits/config","value":{}}]`,
		// A field not under the item's `config`.
		"non-config path": `[{"op":"replace","path":"/fruits/id","value":"x"}]`,
		// A scope field not on the $manifest surface (formatVersion is not settable).
		"unsurfaced scope field": `[{"op":"replace","path":"/$manifest/formatVersion","value":"2.0.0"}]`,
	}
	for name, patch := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := applyToBytes(docBytes, parse(t, patch), tree)
			hasCode(t, err, errors.CONFIG_OVERRIDE_FIELD_UNKNOWN)
		})
	}
}

func TestApplyToBytes_BadValueRejected(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	// `$manifest.title` is a string surface field; a number value is the wrong type.
	cs := parse(t, `[{"op":"replace","path":"/$manifest/title","value":42}]`)
	_, err := applyToBytes(docBytes, cs, tree)
	hasCode(t, err, errors.CONFIG_OVERRIDE_VALUE_INVALID)
}

func TestApplyToBytes_BadEnumValueRejected(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	// `$theme.emphasis` is an enum (none/low/high); an out-of-set value is rejected
	// by the enum-membership check even though the type (string) matches.
	cs := parse(t, `[{"op":"replace","path":"/$theme/emphasis","value":"loud"}]`)
	_, err := applyToBytes(docBytes, cs, tree)
	hasCode(t, err, errors.CONFIG_OVERRIDE_VALUE_INVALID)
}

func TestApplyToBytes_CanonicalOutputStable(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	// An empty changeset is a no-op: applying it then canonically marshaling an
	// already-canonical document must round-trip to identical bytes.
	empty := parse(t, `[]`)
	first, err := applyToBytes(docBytes, empty, tree)
	if err != nil {
		t.Fatalf("ApplyChangeset (no-op): %v", err)
	}
	// Re-applying the no-op to the canonical output yields identical bytes.
	second, err := applyToBytes(first, empty, tree)
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

// metadataOf navigates the canonical output to the `metadata` map of the instance
// declaring id, searching the same physical instance-bearing locations the
// translator indexes (root, child slots, and a block wrapper's config/content
// leaf). It fails the test if the node or its metadata is absent.
func metadataOf(t *testing.T, doc []byte, id string) map[string]any {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(doc, &root); err != nil {
		t.Fatalf("decode result doc: %v", err)
	}
	inst := findInstance(root["root"].(map[string]any), id)
	if inst == nil {
		t.Fatalf("instance %q not found in result doc", id)
	}
	md, ok := inst[metadataKey].(map[string]any)
	if !ok {
		t.Fatalf("instance %q has no metadata object: %v", id, inst[metadataKey])
	}
	return md
}

// findInstance walks the decoded document for the instance declaring id, descending
// child slots and a block wrapper's config/content leaf (the physical
// instance-bearing locations). Pre-order; first match wins; nil if absent.
func findInstance(inst map[string]any, id string) map[string]any {
	if got, ok := inst["id"].(string); ok && got == id {
		return inst
	}
	if cfg, ok := inst["config"].(map[string]any); ok {
		if content, ok := cfg["content"].(map[string]any); ok {
			if found := findInstance(content, id); found != nil {
				return found
			}
		}
	}
	if children, ok := inst["children"].([]any); ok {
		for _, child := range children {
			childInst, ok := child.(map[string]any)
			if !ok {
				continue
			}
			if found := findInstance(childInst, id); found != nil {
				return found
			}
		}
	}
	return nil
}

func TestApplyToBytes_MetadataWholeObjectRoundTrip(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	// add a whole metadata object on an eligible container (`body`), then replace it,
	// then remove it. None of these is a configurable-surface field, yet all are
	// accepted (ungated) and re-resolve (here just applyToBytes) keeps them — the
	// eligible node carries scalar metadata.
	add := parse(t, `[{"op":"add","path":"/body/metadata","value":{"owner":"team-a","pinned":true}}]`)
	out, err := applyToBytes(docBytes, add, tree)
	if err != nil {
		t.Fatalf("add whole metadata: %v", err)
	}
	md := metadataOf(t, out, "body")
	if md["owner"] != "team-a" || md["pinned"] != true {
		t.Fatalf("metadata after add = %v, want owner=team-a pinned=true", md)
	}

	replace := parse(t, `[{"op":"replace","path":"/body/metadata","value":{"owner":"team-b"}}]`)
	out2, err := applyToBytes(out, replace, tree)
	if err != nil {
		t.Fatalf("replace whole metadata: %v", err)
	}
	md = metadataOf(t, out2, "body")
	if md["owner"] != "team-b" {
		t.Fatalf("metadata after replace = %v, want owner=team-b", md)
	}
	if _, ok := md["pinned"]; ok {
		t.Fatalf("replace should have dropped pinned: %v", md)
	}

	remove := parse(t, `[{"op":"remove","path":"/body/metadata"}]`)
	out3, err := applyToBytes(out2, remove, tree)
	if err != nil {
		t.Fatalf("remove whole metadata: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(out3, &root); err != nil {
		t.Fatalf("decode after remove: %v", err)
	}
	body := findInstance(root["root"].(map[string]any), "body")
	if _, ok := body[metadataKey]; ok {
		t.Fatalf("remove should have dropped the metadata member: %v", body[metadataKey])
	}
}

func TestApplyToBytes_MetadataSingleKeyRoundTrip(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	// Seed a metadata object on a block wrapper (`fruits-block`, eligible), then
	// add / replace / remove a SINGLE key under it. Each is ungated and survives.
	seed := parse(t, `[{"op":"add","path":"/fruits-block/metadata","value":{"owner":"team-a"}}]`)
	out, err := applyToBytes(docBytes, seed, tree)
	if err != nil {
		t.Fatalf("seed metadata: %v", err)
	}

	addKey := parse(t, `[{"op":"add","path":"/fruits-block/metadata/priority","value":3}]`)
	out, err = applyToBytes(out, addKey, tree)
	if err != nil {
		t.Fatalf("add metadata key: %v", err)
	}
	if md := metadataOf(t, out, "fruits-block"); md["priority"] != float64(3) {
		t.Fatalf("metadata.priority after add = %v, want 3", md["priority"])
	}

	replaceKey := parse(t, `[{"op":"replace","path":"/fruits-block/metadata/priority","value":7}]`)
	out, err = applyToBytes(out, replaceKey, tree)
	if err != nil {
		t.Fatalf("replace metadata key: %v", err)
	}
	if md := metadataOf(t, out, "fruits-block"); md["priority"] != float64(7) {
		t.Fatalf("metadata.priority after replace = %v, want 7", md["priority"])
	}

	removeKey := parse(t, `[{"op":"remove","path":"/fruits-block/metadata/priority"}]`)
	out, err = applyToBytes(out, removeKey, tree)
	if err != nil {
		t.Fatalf("remove metadata key: %v", err)
	}
	md := metadataOf(t, out, "fruits-block")
	if _, ok := md["priority"]; ok {
		t.Fatalf("remove should have dropped metadata.priority: %v", md)
	}
	if md["owner"] != "team-a" {
		t.Fatalf("remove should have kept sibling metadata.owner: %v", md)
	}
}

func TestApplyToBytes_DocumentRootMetadataRoundTrip(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	// The document root instance is addressed via the `$root` scope; its metadata is
	// always eligible. Whole-object and single-key edits behave consistently with an
	// item id.
	add := parse(t, `[{"op":"add","path":"/$root/metadata","value":{"team":"platform"}}]`)
	out, err := applyToBytes(docBytes, add, tree)
	if err != nil {
		t.Fatalf("add root metadata: %v", err)
	}
	if md := metadataOf(t, out, "root"); md["team"] != "platform" {
		t.Fatalf("root metadata.team = %v, want platform", md["team"])
	}

	addKey := parse(t, `[{"op":"add","path":"/$root/metadata/env","value":"prod"}]`)
	out, err = applyToBytes(out, addKey, tree)
	if err != nil {
		t.Fatalf("add root metadata key: %v", err)
	}
	if md := metadataOf(t, out, "root"); md["env"] != "prod" {
		t.Fatalf("root metadata.env = %v, want prod", md["env"])
	}
}

func TestApplyToBytes_MetadataOnIneligibleNodeRejected(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	// `fruits` is a table LEAF (a widget, not a region/wrapper/root): re-resolve
	// rejects metadata on it via the E1-S2 eligibility guard. The op is ungated by
	// the surface (it is not CONFIG_OVERRIDE_FIELD_UNKNOWN) but fails re-resolve.
	cs := parse(t, `[{"op":"add","path":"/fruits/metadata","value":{"owner":"x"}}]`)
	out, err := applyToBytes(docBytes, cs, tree)
	if err != nil {
		t.Fatalf("applyToBytes should not reject metadata at the apply stage: %v", err)
	}
	// applyToBytes is the pure engine; the eligibility guard runs in re-resolve. Prove
	// the mutated bytes carry the (illegal) metadata, then re-resolve to see the guard
	// fire — that the surface gate did NOT swallow the op as off-surface.
	if md := metadataOf(t, out, "fruits"); md["owner"] != "x" {
		t.Fatalf("metadata should have been applied (ungated) before re-resolve: %v", md)
	}
	reResolveRejects(t, out, errors.METADATA_NOT_ELIGIBLE)
}

func TestApplyToBytes_NonScalarMetadataRejected(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	// An object value on an ELIGIBLE node (`body`) is ungated by the surface but
	// rejected by re-resolve. The dashboard schema's scalarMetadata $def catches a
	// non-scalar value structurally FIRST (RESOLVE_DOCUMENT_INVALID), ahead of the
	// resolver's defense-in-depth METADATA_VALUE_NOT_SCALAR guard — so the
	// schema-validation code is what an author actually sees.
	cs := parse(t, `[{"op":"add","path":"/body/metadata","value":{"nested":{"deep":1}}}]`)
	out, err := applyToBytes(docBytes, cs, tree)
	if err != nil {
		t.Fatalf("applyToBytes should not reject non-scalar metadata at the apply stage: %v", err)
	}
	reResolveRejects(t, out, errors.RESOLVE_DOCUMENT_INVALID)
}

// reResolveRejects writes mutated document bytes beside the shipped examples dir
// and resolves them against the real schema catalog, asserting the resolver rejects
// with the expected coded error. It proves the apply-stage bypass defers metadata
// eligibility/scalar enforcement to re-resolve (the pipeline's guardrail), so an
// illegal metadata write never persists. The temp doc shares the examples dir so
// its relative path and the relative catalog root resolve against the same cwd, as
// resolveFixture does.
func reResolveRejects(t *testing.T, mutated []byte, code errors.Code) {
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

	docPath := "../../examples/metadata-reresolve-" + t.Name() + ".json"
	if err := afero.WriteFile(fs, docPath, mutated, 0o600); err != nil {
		t.Fatalf("write mutated doc: %v", err)
	}
	t.Cleanup(func() { fs.Remove(docPath) })

	res, err := resolver.New(fs, &dashSch, []string{schemasDir})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	_, err = res.Resolve(docPath)
	hasCode(t, err, code)
}

func TestApplyToBytes_TestOpMismatchRejected(t *testing.T) {
	docBytes, tree := resolveFixture(t)

	// A `test` op whose expected value does not match the document fails at apply
	// time (the standard applier's precondition), surfacing as PATCH_APPLY_FAILED.
	// `title` is surfaced, so it passes the guardrail and reaches the applier.
	cs := parse(t, `[{"op":"test","path":"/$manifest/title","value":"Wrong"}]`)
	_, err := applyToBytes(docBytes, cs, tree)
	hasCode(t, err, errors.PATCH_APPLY_FAILED)
}
