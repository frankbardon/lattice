package mcp_test

// End-to-end proof of get_node (E2-S2). It reuses newTestSession (in
// tools_read_test.go) to stand up the MCP server over an in-memory store seeded
// with the minimal example document and drives get_node through a real MCP client
// session. The assertions confirm: get_node on a BLOCK id returns the whole block
// subtree (wrapper plus config/content, including the wrapped table's config), the
// surface lists the CONTENT item's editable fields (the table's title/columns/...),
// a revision is present (the fs backend is a RevisionedStore), and an unknown
// document id vs an unknown node id give DISTINCT coded errors. Running the test
// also proves the SDK does not panic generating the output schema (the stored
// subtree is typed `any`; the surface list is a flat struct).

import (
	"context"
	"encoding/json"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestGetNodeListed asserts get_node appears in the host's tool list with a
// reflection-generated input schema — i.e. registration (and output-schema
// generation for the `any` subtree + flat surface) did not panic.
func TestGetNodeListed(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var found bool
	for _, tool := range res.Tools {
		if tool.Name == "get_node" {
			found = true
			if tool.InputSchema == nil {
				t.Errorf("get_node has nil InputSchema (expected reflection-generated)")
			}
		}
	}
	if !found {
		t.Fatalf("get_node not listed by host")
	}
}

// TestGetNodeBlock asserts get_node on a block id returns the whole block subtree
// (wrapper + config/content with the wrapped table's config), surfaces the CONTENT
// item's editable fields, and includes a revision.
func TestGetNodeBlock(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "get_node",
		Arguments: map[string]any{"id": fixtureID, "nodeId": "fruits-block"},
	})
	if err != nil {
		t.Fatalf("CallTool get_node: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_node returned tool error: %v", res.Content)
	}

	var out struct {
		ID       string          `json:"id"`
		NodeID   string          `json:"nodeId"`
		Revision string          `json:"revision"`
		Subtree  json.RawMessage `json:"subtree"`
		Surface  []struct {
			Key  string `json:"key"`
			Type string `json:"type"`
		} `json:"surface"`
	}
	decodeStructured(t, res, &out)

	if out.ID != fixtureID {
		t.Errorf("id = %q, want %q", out.ID, fixtureID)
	}
	if out.NodeID != "fruits-block" {
		t.Errorf("nodeId = %q, want %q", out.NodeID, "fruits-block")
	}
	// The fs backend is a RevisionedStore, so a revision must be present.
	if out.Revision == "" {
		t.Errorf("revision is empty, want the store's current revision token")
	}

	// The subtree is the WHOLE block: wrapper id + content table config.
	var subtree struct {
		ID     string `json:"id"`
		Config struct {
			ID      string `json:"id"`
			Content struct {
				ID     string `json:"id"`
				Config struct {
					Title   string `json:"title"`
					Columns []any  `json:"columns"`
				} `json:"config"`
			} `json:"content"`
		} `json:"config"`
	}
	if err := json.Unmarshal(out.Subtree, &subtree); err != nil {
		t.Fatalf("unmarshal subtree: %v\n%s", err, out.Subtree)
	}
	if subtree.ID != "fruits-block" {
		t.Errorf("subtree wrapper id = %q, want %q", subtree.ID, "fruits-block")
	}
	if subtree.Config.Content.ID != "fruits" {
		t.Errorf("subtree content id = %q, want %q (block subtree must include config/content)", subtree.Config.Content.ID, "fruits")
	}
	if subtree.Config.Content.Config.Title != "Fruits" {
		t.Errorf("subtree content title = %q, want %q", subtree.Config.Content.Config.Title, "Fruits")
	}

	// The surface is the CONTENT (table) item's editable fields, not the block's.
	keys := map[string]string{}
	for _, f := range out.Surface {
		keys[f.Key] = f.Type
	}
	for _, want := range []string{"title", "columns"} {
		if _, ok := keys[want]; !ok {
			t.Errorf("surface missing content field %q; got %v", want, keys)
		}
	}
	// The block's own surface fields (title/visibility live on the wrapper) must NOT
	// stand in for the content's surface: the content table declares no "visibility"
	// field, so its presence would mean we surfaced the wrapper by mistake.
	if _, ok := keys["visibility"]; ok {
		t.Errorf("surface carries the block wrapper's 'visibility' field; want the content item's surface only: %v", keys)
	}
}

// TestGetNodeUnknownIDDistinctError asserts an unknown document id surfaces
// STORAGE_NOT_FOUND as a tool error.
func TestGetNodeUnknownIDDistinctError(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "get_node",
		Arguments: map[string]any{"id": "does-not-exist", "nodeId": "fruits-block"},
	})
	if err != nil {
		t.Fatalf("CallTool unexpectedly returned a protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError for unknown document id, got success")
	}
	text := toolErrorText(res)
	if !contains(text, "STORAGE_NOT_FOUND") {
		t.Errorf("unknown-id tool error = %q, want it to carry STORAGE_NOT_FOUND", text)
	}
}

// TestGetNodeUnknownNodeDistinctError asserts an unknown node id surfaces
// CHANGESET_TARGET_NOT_FOUND — DISTINCT from the unknown-document-id error.
func TestGetNodeUnknownNodeDistinctError(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "get_node",
		Arguments: map[string]any{"id": fixtureID, "nodeId": "no-such-node"},
	})
	if err != nil {
		t.Fatalf("CallTool unexpectedly returned a protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError for unknown node id, got success")
	}
	text := toolErrorText(res)
	if !contains(text, "CHANGESET_TARGET_NOT_FOUND") {
		t.Errorf("unknown-node tool error = %q, want it to carry CHANGESET_TARGET_NOT_FOUND", text)
	}
	// Distinct from the unknown-document-id code.
	if contains(text, "STORAGE_NOT_FOUND") {
		t.Errorf("unknown-node tool error also carried STORAGE_NOT_FOUND; the two cases must be distinct: %q", text)
	}
}

// toolErrorText concatenates the text content of a tool-error result so a test can
// assert the carried coded-error code reached the host.
func toolErrorText(res *sdkmcp.CallToolResult) string {
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			text += tc.Text
		}
	}
	return text
}
