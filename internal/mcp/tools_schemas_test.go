package mcp_test

// End-to-end proof of the two grammar tools (E2-S3). It reuses the shared
// in-memory test session (real schema catalog under ../../schemas) and drives
// list_schemas and get_schema through a real MCP client session, asserting the
// catalog enumerates known item types plus the dashboard envelope, that
// get_schema returns valid JSON for a known type, and that an unknown type
// surfaces the facade's SCHEMA_NOT_FOUND coded error as an MCP tool error.

import (
	"context"
	"encoding/json"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestListSchemas asserts list_schemas returns the grammar catalog: known item
// types (block, table, and the markdown/image/heading content types) plus the
// dashboard envelope token.
func TestListSchemas(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "list_schemas"})
	if err != nil {
		t.Fatalf("CallTool list_schemas: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_schemas returned tool error: %v", res.Content)
	}

	var out struct {
		Types []string `json:"types"`
	}
	decodeStructured(t, res, &out)

	have := make(map[string]bool, len(out.Types))
	for _, ty := range out.Types {
		have[ty] = true
	}
	for _, want := range []string{"block", "table", "markdown", "image", "heading", "dashboard"} {
		if !have[want] {
			t.Errorf("catalog missing %q; got %v", want, out.Types)
		}
	}
}

// TestGetSchemaKnownType asserts get_schema returns a valid JSON Schema for a
// known item type, with the type echoed and the schema carrying its $id.
func TestGetSchemaKnownType(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "get_schema",
		Arguments: map[string]any{"type": "block"},
	})
	if err != nil {
		t.Fatalf("CallTool get_schema: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_schema returned tool error: %v", res.Content)
	}

	var out struct {
		Type   string          `json:"type"`
		Schema json.RawMessage `json:"schema"`
	}
	decodeStructured(t, res, &out)

	if out.Type != "block" {
		t.Errorf("type = %q, want %q", out.Type, "block")
	}
	var schema struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(out.Schema, &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	if schema.ID == "" {
		t.Errorf("schema has no $id; got %s", out.Schema)
	}
}

// TestGetSchemaDashboardEnvelope asserts the reserved "dashboard" token returns
// the envelope schema.
func TestGetSchemaDashboardEnvelope(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "get_schema",
		Arguments: map[string]any{"type": "dashboard"},
	})
	if err != nil {
		t.Fatalf("CallTool get_schema: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_schema(dashboard) returned tool error: %v", res.Content)
	}

	var out struct {
		Schema json.RawMessage `json:"schema"`
	}
	decodeStructured(t, res, &out)
	if !json.Valid(out.Schema) {
		t.Fatalf("dashboard schema is not valid JSON: %s", out.Schema)
	}
}

// TestGetSchemaUnknownTypeIsToolError asserts an unknown type surfaces the
// facade's SCHEMA_NOT_FOUND coded error as an MCP tool error (IsError), not a
// protocol error, with the code text reaching the host content.
func TestGetSchemaUnknownTypeIsToolError(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "get_schema",
		Arguments: map[string]any{"type": "no-such-type"},
	})
	if err != nil {
		t.Fatalf("CallTool unexpectedly returned a protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError for unknown type, got success")
	}

	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			text += tc.Text
		}
	}
	if !contains(text, "SCHEMA_NOT_FOUND") {
		t.Errorf("tool error content = %q, want it to carry the SCHEMA_NOT_FOUND code", text)
	}
}
