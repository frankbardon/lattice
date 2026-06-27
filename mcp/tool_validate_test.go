package mcp

// Typed-handler + descriptor tests for validate_patch (E2-S2), ported from the
// legacy internal/mcp/tools_validate_test.go but driven WITHOUT a server. They
// confirm: a valid id-rooted field edit returns ok:true with a resolved preview
// reflecting the edit and the document's baseRevision (the fs backend is a
// RevisionedStore) AND leaves the stored document byte-for-byte unchanged (the
// tool never persists); an off-surface field edit surfaces the field-edit
// guardrail's coded error verbatim (ok:false) and likewise leaves the store
// untouched. Reflecting the descriptor's `any` preview output schema without a
// panic is proven by findDescriptor building the catalog.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// TestValidatePatchRegistered asserts validate_patch is present in the Tools()
// catalog with a reflection-generated input+output schema — i.e. NewTool reflected
// the `any` preview without panicking — and the legacy description.
func TestValidatePatchRegistered(t *testing.T) {
	d := findDescriptor(t, "lattice_validate_patch")
	if d.Description != validatePatchDescription {
		t.Errorf("description mismatch:\n got %q\nwant %q", d.Description, validatePatchDescription)
	}
	if len(d.InputSchema) == 0 {
		t.Errorf("validate_patch has empty InputSchema (expected reflection-generated)")
	}
	if len(d.OutputSchema) == 0 {
		t.Errorf("validate_patch has empty OutputSchema (expected reflection-generated)")
	}
}

// TestValidatePatchValid asserts a valid id-rooted field edit validates: ok:true,
// a resolved preview reflecting the edit, and a non-empty baseRevision — and that
// the STORED document is byte-for-byte unchanged afterward (the tool never saves).
// It drives the descriptor's erased Invoke so the wire shape is exercised end to
// end.
func TestValidatePatchValid(t *testing.T) {
	svc := newTestService(t)
	d := findDescriptor(t, "lattice_validate_patch")

	before, err := svc.Load(fixtureID)
	if err != nil {
		t.Fatalf("Load before: %v", err)
	}

	raw, err := d.Invoke(context.Background(), svc, json.RawMessage(
		`{"id":"`+fixtureID+`","ops":[{"op":"replace","path":"/fruits/config/title","value":"Citrus"}]}`))
	if err != nil {
		t.Fatalf("Invoke validate_patch: %v", err)
	}

	var out struct {
		OK           bool            `json:"ok"`
		Preview      json.RawMessage `json:"preview"`
		BaseRevision string          `json:"baseRevision"`
	}
	remarshal(t, raw, &out)

	if !out.OK {
		t.Errorf("ok = false, want true for a valid patch")
	}
	if len(out.Preview) == 0 {
		t.Fatalf("preview is empty, want the resolved tree the patch would produce")
	}
	// The fs backend is a RevisionedStore, so a base revision must be present.
	if out.BaseRevision == "" {
		t.Errorf("baseRevision is empty, want the document's current revision token")
	}
	// The preview must reflect the edited title — proof the dry-run applied the op.
	if !strings.Contains(string(out.Preview), "Citrus") {
		t.Errorf("preview does not reflect the patched title %q: %s", "Citrus", out.Preview)
	}

	// The store must be untouched: validate_patch never persists.
	after, err := svc.Load(fixtureID)
	if err != nil {
		t.Fatalf("Load after: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("stored document changed after validate_patch; the tool must never persist\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestValidatePatchInvalidIsCodedError asserts an off-surface field edit (patching
// the table's non-configurable `rows`) surfaces the field-edit guardrail's coded
// error verbatim (CONFIG_OVERRIDE_FIELD_UNKNOWN) and leaves the store untouched.
func TestValidatePatchInvalidIsCodedError(t *testing.T) {
	svc := newTestService(t)

	before, err := svc.Load(fixtureID)
	if err != nil {
		t.Fatalf("Load before: %v", err)
	}

	// `rows` is NOT in the table's configurable surface (only title/columns/query
	// are), so this field edit is rejected by the field-edit guardrail.
	_, err = validatePatch(context.Background(), svc, validatePatchInput{
		ID: fixtureID,
		Ops: []map[string]any{
			{"op": "replace", "path": "/fruits/config/rows", "value": [][]string{{"Cherry", "Red"}}},
		},
	})
	if err == nil {
		t.Fatalf("expected an error for an off-surface field edit, got success")
	}
	if !errors.HasCode(err, errors.CONFIG_OVERRIDE_FIELD_UNKNOWN) {
		t.Errorf("error = %v, want it to carry the field-edit guardrail's CONFIG_OVERRIDE_FIELD_UNKNOWN", err)
	}

	// The store must be untouched even on a rejected validation.
	after, err := svc.Load(fixtureID)
	if err != nil {
		t.Fatalf("Load after: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("stored document changed after a rejected validate_patch; the tool must never persist\nbefore: %s\nafter:  %s", before, after)
	}
}
