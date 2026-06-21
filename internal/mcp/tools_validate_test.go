package mcp_test

// End-to-end proof of validate_patch (E3-S2). It reuses newTestSession (in
// tools_read_test.go) to stand up the MCP server over an in-memory store seeded
// with the minimal example document and drives validate_patch through a real MCP
// client session. The assertions confirm: a valid id-rooted field edit returns
// ok:true with a resolved preview and the document's baseRevision (the fs backend
// is a RevisionedStore) AND leaves the stored document byte-for-byte unchanged (the
// tool never persists); an off-surface edit returns a coded tool error (ok:false)
// and likewise leaves the store untouched. Running the test also proves the SDK
// does not panic generating the output schema (the preview is typed `any`).

import (
	"context"
	"encoding/json"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestValidatePatchListed asserts validate_patch appears in the host's tool list
// with a reflection-generated input schema — i.e. registration (and output-schema
// generation for the `any` preview) did not panic.
func TestValidatePatchListed(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var found bool
	for _, tool := range res.Tools {
		if tool.Name == "validate_patch" {
			found = true
			if tool.InputSchema == nil {
				t.Errorf("validate_patch has nil InputSchema (expected reflection-generated)")
			}
		}
	}
	if !found {
		t.Fatalf("validate_patch not listed by host")
	}
}

// TestValidatePatchValid asserts a valid id-rooted field edit validates: ok:true,
// a resolved preview reflecting the edit, and a non-empty baseRevision — and that
// the STORED document is byte-for-byte unchanged afterward (the tool never saves).
func TestValidatePatchValid(t *testing.T) {
	cs := newTestSession(t)

	before := getRawDocument(t, cs)

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "validate_patch",
		Arguments: map[string]any{
			"id": fixtureID,
			"ops": []map[string]any{
				{"op": "replace", "path": "/fruits/config/title", "value": "Citrus"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool validate_patch: %v", err)
	}
	if res.IsError {
		t.Fatalf("validate_patch returned tool error: %v", res.Content)
	}

	var out struct {
		OK           bool            `json:"ok"`
		Preview      json.RawMessage `json:"preview"`
		BaseRevision string          `json:"baseRevision"`
	}
	decodeStructured(t, res, &out)

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
	if !contains(string(out.Preview), "Citrus") {
		t.Errorf("preview does not reflect the patched title %q: %s", "Citrus", out.Preview)
	}

	// The store must be untouched: validate_patch never persists.
	after := getRawDocument(t, cs)
	if string(before) != string(after) {
		t.Errorf("stored document changed after validate_patch; the tool must never persist\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestValidatePatchInvalidIsToolError asserts an off-surface field edit (patching
// the table's non-configurable `rows`) surfaces a coded error as a tool error
// (IsError, ok:false) and leaves the store untouched.
func TestValidatePatchInvalidIsToolError(t *testing.T) {
	cs := newTestSession(t)

	before := getRawDocument(t, cs)

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "validate_patch",
		Arguments: map[string]any{
			"id": fixtureID,
			// `rows` is NOT in the table's configurable surface (only title/columns/query
			// are), so this field edit is rejected by the field-edit guardrail.
			"ops": []map[string]any{
				{"op": "replace", "path": "/fruits/config/rows", "value": [][]string{{"Cherry", "Red"}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool unexpectedly returned a protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError for an off-surface field edit, got success")
	}

	// The carried coded-error code must reach the host so the model can self-correct.
	text := toolErrorText(res)
	if !contains(text, "CONFIG_OVERRIDE") && !contains(text, "PATCH_") {
		t.Errorf("off-surface tool error = %q, want it to carry the field-edit guardrail's coded error", text)
	}

	// The store must be untouched even on a rejected validation.
	after := getRawDocument(t, cs)
	if string(before) != string(after) {
		t.Errorf("stored document changed after a rejected validate_patch; the tool must never persist\nbefore: %s\nafter:  %s", before, after)
	}
}

// getRawDocument fetches the fixture's raw stored bytes via get_document (no
// resolved flag), so a test can compare the stored document before and after a
// validate_patch call to prove the tool never persists.
func getRawDocument(t *testing.T, cs *sdkmcp.ClientSession) []byte {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "get_document",
		Arguments: map[string]any{"id": fixtureID},
	})
	if err != nil {
		t.Fatalf("CallTool get_document: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_document returned tool error: %v", res.Content)
	}
	var out struct {
		Document json.RawMessage `json:"document"`
	}
	decodeStructured(t, res, &out)
	return out.Document
}
