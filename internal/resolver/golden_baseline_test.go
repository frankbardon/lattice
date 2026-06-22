package resolver

// golden_baseline_test.go — E1-S3 regression net for the custom-item-types effort.
//
// PURPOSE
// -------
// Before the dogfood migration (epics E2/E3/E4) reworks how built-in item types
// declare their behavior (the `latticeBehavior` keyword replacing hardcoded
// name-string dispatch), this file pins the CURRENT resolved-tree JSON for
// documents that exercise every built-in item type. The migration's success
// criterion is byte-identical resolved output for existing documents; these
// goldens are the proof.
//
// The fixtures and their *.golden.json siblings live under testdata/valid/ and
// are ALSO picked up by TestResolveGolden's directory walk (in resolver_test.go).
// This file does not duplicate that mechanism — it layers an explicit, named,
// importable CASE TABLE on top of the same fixtures and the same compare logic,
// so the coverage is documented in code and the later stories have one symbol to
// re-run.
//
// HOW LATER STORIES REUSE THIS (FR-22)
// ------------------------------------
// E2-S2, E3-S4, and E4-S2 each need to re-prove these same goldens still match
// after their migration step. Because every resolver test is `package resolver`,
// those stories' tests can, WITHOUT copying anything:
//
//   1. Range over BuiltinGoldenBaselineCases (the exported table below) and call
//      AssertBuiltinGolden(t, c) per case — this resolves the fixture with the
//      real repo schema catalog and asserts byte-identity against the committed
//      golden. Do NOT pass -update from a migration story: the whole point is
//      that the bytes must not move, so a mismatch is the regression signal.
//   2. OR simply rely on TestBuiltinGoldenBaseline below already running in the
//      package — a migration that changes built-in resolved output turns it red.
//
// Adding a NEW built-in-type fixture: drop <name>.json into testdata/valid/, add
// a row here, run `go test ./internal/resolver -run TestBuiltinGoldenBaseline
// -update` once to mint <name>.golden.json, and commit both. (The -update flag is
// shared with TestResolveGolden — see resolver_test.go.)

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// BuiltinGoldenCase names one baseline fixture: a human-readable Name, the
// resolver-input document Doc, the committed Golden it must match, and Covers —
// the built-in item types / families the case is the regression net for. Covers
// is documentation only (it lets a reader see the coverage map at a glance and
// lets a later story assert the table still spans every family).
type BuiltinGoldenCase struct {
	Name   string
	Doc    string
	Golden string
	Covers []string
}

// testdataValid is the fixture directory, relative to this package.
const testdataValid = "testdata/valid"

// BuiltinGoldenBaselineCases enumerates the built-in-type baseline. Each Doc is a
// document under testdata/valid/; Golden is its sibling *.golden.json. Together
// the rows satisfy E1-S3's coverage: a grid container with placements; a
// variable-box holding a widget from every family; a block wrapping a leaf AND a
// block wrapping a form; a form with mixed widgets (flow + grid modes); and at
// least one widget per family including range (slider/stepper/number-field) and
// option (select/radio-group/segmented/multiselect/checkbox-group) cases.
//
// Several rows are pre-existing fixtures (minimal-dashboard, nested-subgrid,
// dropdown-input, markdown-leaf): they were already golden-tested by
// TestResolveGolden, and are listed here so the baseline's coverage map is
// complete and self-documenting in one place.
var BuiltinGoldenBaselineCases = []BuiltinGoldenCase{
	{
		Name:   "grid-container-with-placements",
		Doc:    filepath.Join(testdataValid, "minimal-dashboard.json"),
		Golden: filepath.Join(testdataValid, "minimal-dashboard.golden.json"),
		Covers: []string{"container", "block", "table", "grid-placement"},
	},
	{
		Name:   "nested-grid-containers",
		Doc:    filepath.Join(testdataValid, "nested-subgrid.json"),
		Golden: filepath.Join(testdataValid, "nested-subgrid.golden.json"),
		Covers: []string{"container", "block", "table", "nested-grid"},
	},
	{
		Name:   "block-wrapping-a-leaf",
		Doc:    filepath.Join(testdataValid, "markdown-leaf.json"),
		Golden: filepath.Join(testdataValid, "markdown-leaf.golden.json"),
		Covers: []string{"container", "block", "markdown"},
	},
	{
		Name:   "variable-box-with-one-widget",
		Doc:    filepath.Join(testdataValid, "dropdown-input.json"),
		Golden: filepath.Join(testdataValid, "dropdown-input.golden.json"),
		Covers: []string{"container", "variable-box", "select", "block", "table"},
	},
	{
		Name:   "variable-box-every-widget-family",
		Doc:    filepath.Join(testdataValid, "all-widget-families.json"),
		Golden: filepath.Join(testdataValid, "all-widget-families.golden.json"),
		Covers: []string{
			"container", "variable-box",
			"text-input", "textarea", // string family
			"number-field", "slider", "stepper", // number family (range cases)
			"toggle", "checkbox", // boolean family
			"select", "radio-group", "segmented", // enum family (option cases)
			"multiselect", "checkbox-group", "tag-input", // array family (option + freeform)
		},
	},
	{
		Name:   "block-wrapping-a-form-flow-mixed-widgets",
		Doc:    filepath.Join(testdataValid, "form-mixed-widgets.json"),
		Golden: filepath.Join(testdataValid, "form-mixed-widgets.golden.json"),
		Covers: []string{
			"container", "block", "form", "form-flow",
			"text-input", "number-field", "toggle", "select", "tag-input",
		},
	},
	{
		Name:   "block-wrapping-a-form-grid-layout",
		Doc:    filepath.Join(testdataValid, "form-grid-layout.json"),
		Golden: filepath.Join(testdataValid, "form-grid-layout.golden.json"),
		Covers: []string{"container", "block", "form", "form-grid", "text-input", "stepper"},
	},
	{
		// E1-S3: regression-locks element metadata carried verbatim onto the
		// eligible nodes — the document root (merging the top-level document
		// `metadata` with the root instance's own), a regions-or-wrappers
		// container, and a block wrapper — with every scalar value intact and
		// NOT lifted onto the block's wrapped content.
		Name:   "element-metadata-on-eligible-nodes",
		Doc:    filepath.Join(testdataValid, "element-metadata.json"),
		Golden: filepath.Join(testdataValid, "element-metadata.golden.json"),
		Covers: []string{"container", "block", "table", "metadata"},
	},
}

// AssertBuiltinGolden resolves c.Doc against the real repo schema catalog and
// asserts the emitted resolved tree marshals byte-for-byte to c.Golden. With the
// shared -update flag it (re)writes the golden instead of comparing. This is the
// single reusable assertion E2-S2 / E3-S4 / E4-S2 invoke per case after their
// migration step (they must NOT pass -update — a mismatch is the regression).
func AssertBuiltinGolden(t *testing.T, c BuiltinGoldenCase) {
	t.Helper()

	res := newRepoResolver(t)
	tree, err := res.Resolve(c.Doc)
	if err != nil {
		t.Fatalf("Resolve(%s): %v", c.Doc, err)
	}

	got, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		t.Fatalf("marshal tree for %s: %v", c.Name, err)
	}
	got = append(got, '\n')

	if *update {
		if err := os.WriteFile(c.Golden, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", c.Golden, err)
		}
		return
	}

	want, err := os.ReadFile(c.Golden)
	if err != nil {
		t.Fatalf("read golden %s (run -update to create): %v", c.Golden, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("resolved tree mismatch for baseline case %q (%s)\n--- got ---\n%s\n--- want ---\n%s",
			c.Name, c.Doc, got, want)
	}
}

// TestBuiltinGoldenBaseline runs the whole baseline table. It is the regression
// net the migration epics turn red if built-in resolved output drifts. The
// migration stories re-run exactly this (no new fixtures, no new goldens).
func TestBuiltinGoldenBaseline(t *testing.T) {
	if len(BuiltinGoldenBaselineCases) == 0 {
		t.Fatal("baseline table is empty")
	}
	for _, c := range BuiltinGoldenBaselineCases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			AssertBuiltinGolden(t, c)
		})
	}
}

// TestBuiltinGoldenBaselineCoversEveryWidgetFamily is a meta-guard: it asserts the
// baseline table's Covers union includes every widget item type the repo catalog
// knows. A widget is now defined by its schema (the `latticeBehavior.role ==
// "widget"` keyword, E2-S1) rather than a hardcoded resolver map, so the guard
// derives the full widget surface from the catalog — Catalog.WidgetNames() — which
// reads that keyword. If a future widget type is catalogued but no baseline fixture
// exercises it, this test fails, forcing the baseline to keep spanning the full
// widget surface the migration must preserve. (The source of truth is the schemas,
// not a copy.)
func TestBuiltinGoldenBaselineCoversEveryWidgetFamily(t *testing.T) {
	covered := map[string]bool{}
	for _, c := range BuiltinGoldenBaselineCases {
		for _, item := range c.Covers {
			covered[item] = true
		}
	}
	res := newRepoResolver(t)
	widgets := res.cat.WidgetNames()
	if len(widgets) == 0 {
		t.Fatal("catalog reports no widget-role item types; expected the migrated widget schemas")
	}
	for widgetType := range widgets {
		if !covered[widgetType] {
			t.Errorf("widget type %q declares the widget role but no baseline case covers it; add a fixture", widgetType)
		}
	}
}
