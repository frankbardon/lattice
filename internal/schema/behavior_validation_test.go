package schema

import (
	stderrors "errors"
	"testing"

	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
)

// loadBehaviorSchema writes a single item-type schema into a fresh catalog and
// returns the resulting index-time error (nil if it indexed cleanly). It drives
// the production catalog path so the SCHEMA_BEHAVIOR_INVALID wiring — not just
// the validator in isolation — is exercised.
func loadBehaviorSchema(t *testing.T, raw string) error {
	t.Helper()
	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "cat/type.schema.json", []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := NewCatalog(fs, "cat")
	return err
}

// codedDetails extracts the Details map from a SCHEMA_BEHAVIOR_INVALID error and
// asserts the offending schema is named.
func codedDetails(t *testing.T, err error) map[string]any {
	t.Helper()
	if !errors.HasCode(err, errors.SCHEMA_BEHAVIOR_INVALID) {
		t.Fatalf("error = %v, want SCHEMA_BEHAVIOR_INVALID", err)
	}
	var ce *errors.CodedError
	if !stderrors.As(err, &ce) {
		t.Fatalf("error %v is not a *CodedError", err)
	}
	if ce.Details["schema"] == nil || ce.Details["schema"] == "" {
		t.Errorf("error details missing schema name: %v", ce.Details)
	}
	return ce.Details
}

// TestValidateBehaviorRejectsMalformed covers every malformed case the story
// enumerates, each asserting SCHEMA_BEHAVIOR_INVALID and that the offending
// schema is named in the details.
func TestValidateBehaviorRejectsMalformed(t *testing.T) {
	const idLine = `"$id": "https://lattice.dev/schemas/items/custom/1.0.0",`
	cases := []struct {
		name string
		raw  string
	}{
		{
			name: "unknown role",
			raw: `{` + idLine + `"type":"object","latticeBehavior":{"role":"gadget"}}`,
		},
		{
			name: "missing role",
			raw:  `{` + idLine + `"type":"object","latticeBehavior":{"layout":"grid"}}`,
		},
		{
			name: "wrapper without contentField",
			raw:  `{` + idLine + `"type":"object","latticeBehavior":{"role":"wrapper"}}`,
		},
		{
			name: "contentField names undeclared property",
			raw: `{` + idLine + `"type":"object",
				"properties":{"title":{"type":"string"}},
				"latticeBehavior":{"role":"wrapper","contentField":"content"}}`,
		},
		{
			name: "widget with absent binds",
			raw:  `{` + idLine + `"type":"object","latticeBehavior":{"role":"widget"}}`,
		},
		{
			name: "widget with empty binds",
			raw:  `{` + idLine + `"type":"object","latticeBehavior":{"role":"widget","binds":[]}}`,
		},
		{
			name: "binds member not a known kind",
			raw:  `{` + idLine + `"type":"object","latticeBehavior":{"role":"widget","binds":["string","color"]}}`,
		},
		{
			name: "childPolicy on a non-region",
			raw:  `{` + idLine + `"type":"object","latticeBehavior":{"role":"widget","binds":["string"],"childPolicy":"widgets"}}`,
		},
		{
			name: "layout on a non-region",
			raw: `{` + idLine + `"type":"object",
				"properties":{"content":{"type":"object"}},
				"latticeBehavior":{"role":"wrapper","contentField":"content","layout":"grid"}}`,
		},
		{
			name: "invalid childPolicy enum",
			raw:  `{` + idLine + `"type":"object","latticeBehavior":{"role":"region","childPolicy":"anything"}}`,
		},
		{
			name: "invalid layout enum",
			raw:  `{` + idLine + `"type":"object","latticeBehavior":{"role":"region","layout":"masonry"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := loadBehaviorSchema(t, tc.raw)
			details := codedDetails(t, err)
			if got := details["schema"]; got != "https://lattice.dev/schemas/items/custom/1.0.0" {
				t.Errorf("details[schema] = %v, want the custom type $id", got)
			}
		})
	}
}

// TestValidateBehaviorAcceptsWellFormed verifies a clean block of each role
// indexes without error.
func TestValidateBehaviorAcceptsWellFormed(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{
			name: "region grid",
			raw: `{"$id":"https://lattice.dev/schemas/items/region/1.0.0","type":"object",
				"latticeBehavior":{"role":"region","childPolicy":"regions-or-wrappers","layout":"grid"}}`,
		},
		{
			name: "region flow widgets",
			raw: `{"$id":"https://lattice.dev/schemas/items/form/1.0.0","type":"object",
				"latticeBehavior":{"role":"region","childPolicy":"widgets","layout":"flow"}}`,
		},
		{
			name: "region layout none",
			raw: `{"$id":"https://lattice.dev/schemas/items/stack/1.0.0","type":"object",
				"latticeBehavior":{"role":"region","layout":"none"}}`,
		},
		{
			name: "wrapper",
			raw: `{"$id":"https://lattice.dev/schemas/items/block/1.0.0","type":"object",
				"properties":{"content":{"type":"object"}},
				"latticeBehavior":{"role":"wrapper","contentField":"content"}}`,
		},
		{
			name: "widget",
			raw: `{"$id":"https://lattice.dev/schemas/items/slider/1.0.0","type":"object",
				"latticeBehavior":{"role":"widget","binds":["number"],"rangeCheck":true}}`,
		},
		{
			name: "widget enum requires options",
			raw: `{"$id":"https://lattice.dev/schemas/items/select/1.0.0","type":"object",
				"latticeBehavior":{"role":"widget","binds":["enum","string"],"requireOptions":true}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := loadBehaviorSchema(t, tc.raw); err != nil {
				t.Errorf("well-formed %s rejected: %v", tc.name, err)
			}
		})
	}
}

// TestValidateBehaviorNoBlockIsClean confirms a schema with NO latticeBehavior
// block (a plain leaf, as every built-in still is) validates untouched — the
// block must never be required.
func TestValidateBehaviorNoBlockIsClean(t *testing.T) {
	raw := `{"$id":"https://lattice.dev/schemas/items/plain/1.0.0","type":"object",
		"properties":{"title":{"type":"string"}}}`
	if err := loadBehaviorSchema(t, raw); err != nil {
		t.Errorf("plain leaf rejected: %v", err)
	}
}
