package mcp_test

// End-to-end proof of get_outline (E2-S1). It reuses newTestSession (in
// tools_read_test.go) to stand up the MCP server over an in-memory store seeded
// with the minimal example document and drives get_outline through a real MCP
// client session. The assertions confirm: the outline carries the expected node
// ids in the expected tree shape, it includes a revision (the fs backend is a
// RevisionedStore), and — crucially — it carries NO config bodies anywhere (a
// node's table title is allowed; the table columns/rows config is not). Running
// the test also proves the SDK does not panic when generating the output schema
// for the recursive skeleton at tool-registration time.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// marshalStructured returns the tool result's StructuredContent as a JSON string,
// so a test can assert on the raw serialized outline (e.g. that no config body
// leaked through).
func marshalStructured(t *testing.T, res *sdkmcp.CallToolResult) string {
	t.Helper()
	if res.StructuredContent == nil {
		t.Fatalf("result has no StructuredContent")
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	return string(b)
}

// TestGetOutlineListed asserts get_outline appears in the host's tool list with a
// reflection-generated input schema — i.e. registration (and output-schema
// generation for the recursive skeleton) did not panic.
func TestGetOutlineListed(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var found bool
	for _, tool := range res.Tools {
		if tool.Name == "get_outline" {
			found = true
			if tool.InputSchema == nil {
				t.Errorf("get_outline has nil InputSchema (expected reflection-generated)")
			}
		}
	}
	if !found {
		t.Fatalf("get_outline not listed by host")
	}
}

// outlineNode mirrors the tool's config-free skeleton for decoding. Children is
// decoded recursively; the tool emits children as a nested array.
type outlineNode struct {
	ID        string        `json:"id"`
	Type      string        `json:"type"`
	Title     string        `json:"title"`
	Container bool          `json:"container"`
	Placement string        `json:"placement"`
	Children  []outlineNode `json:"children"`
}

// TestGetOutline asserts the outline mirrors the fixture tree (ids + shape),
// carries a title only where the config declares one, includes a revision, and
// carries NO config bodies.
func TestGetOutline(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "get_outline",
		Arguments: map[string]any{"id": fixtureID},
	})
	if err != nil {
		t.Fatalf("CallTool get_outline: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_outline returned tool error: %v", res.Content)
	}

	var out struct {
		ID       string `json:"id"`
		Revision string `json:"revision"`
		Document struct {
			Variables   []string `json:"variables"`
			Connections []string `json:"connections"`
			Theme       bool     `json:"theme"`
		} `json:"document"`
		Root *outlineNode `json:"root"`
	}
	decodeStructured(t, res, &out)

	if out.ID != fixtureID {
		t.Errorf("id = %q, want %q", out.ID, fixtureID)
	}
	// The fs backend is a RevisionedStore, so a revision must be present.
	if out.Revision == "" {
		t.Errorf("revision is empty, want the store's current revision token")
	}

	if out.Root == nil {
		t.Fatalf("root is nil")
	}

	// Collect every node id and title in the skeleton.
	ids := map[string]bool{}
	titles := map[string]string{}
	var walk func(n *outlineNode)
	walk = func(n *outlineNode) {
		if n.ID != "" {
			ids[n.ID] = true
			titles[n.ID] = n.Title
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(out.Root)

	// The fixture's full id set across the nested tree.
	for _, want := range []string{"root", "body", "fruits-block", "fruits", "metrics-block", "metrics"} {
		if !ids[want] {
			t.Errorf("outline missing expected node id %q", want)
		}
	}

	// Tree shape: root -> body -> two blocks, each block -> one table.
	if got := out.Root.ID; got != "root" {
		t.Errorf("root id = %q, want %q", got, "root")
	}
	if len(out.Root.Children) != 1 || out.Root.Children[0].ID != "body" {
		t.Fatalf("root children = %+v, want single 'body'", out.Root.Children)
	}
	body := out.Root.Children[0]
	if len(body.Children) != 2 {
		t.Fatalf("body children = %d, want 2", len(body.Children))
	}

	// Title present only where the config declares one: the tables carry titles,
	// the structural containers/blocks do not.
	if titles["fruits"] != "Fruits" {
		t.Errorf("fruits title = %q, want %q", titles["fruits"], "Fruits")
	}
	if titles["metrics"] != "Metrics" {
		t.Errorf("metrics title = %q, want %q", titles["metrics"], "Metrics")
	}
	if titles["root"] != "" {
		t.Errorf("root title = %q, want empty (container declares no title)", titles["root"])
	}

	// Container flag carried through.
	if !out.Root.Container {
		t.Errorf("root.container = false, want true")
	}

	// A placed block carries a compact placement summary, not the raw object.
	var firstBlock *outlineNode
	for i := range body.Children {
		if body.Children[i].ID == "fruits-block" {
			firstBlock = &body.Children[i]
		}
	}
	if firstBlock == nil {
		t.Fatalf("fruits-block not found under body")
	}
	if firstBlock.Placement == "" {
		t.Errorf("fruits-block placement summary is empty, want a compact summary")
	}

	// CONFIG-FREE assertion: re-marshal the whole structured result and confirm no
	// config field values from the fixture leak through. The table config carries
	// distinctive values ("Apple", "Banana", column header "Color", "rows") that
	// must NOT appear anywhere in the outline.
	raw := marshalStructured(t, res)
	for _, leak := range []string{"Apple", "Banana", "Color", "\"rows\"", "\"columns\"", "\"grid\"", "\"config\""} {
		if strings.Contains(raw, leak) {
			t.Errorf("outline leaked config body %q:\n%s", leak, raw)
		}
	}
}

// TestGetOutlineUnknownIDIsToolError asserts an unknown id surfaces the store's
// STORAGE_NOT_FOUND coded error as an MCP tool error (IsError), not a protocol
// error.
func TestGetOutlineUnknownIDIsToolError(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "get_outline",
		Arguments: map[string]any{"id": "does-not-exist"},
	})
	if err != nil {
		t.Fatalf("CallTool unexpectedly returned a protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError for unknown id, got success")
	}

	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			text += tc.Text
		}
	}
	if !contains(text, "STORAGE_NOT_FOUND") {
		t.Errorf("tool error content = %q, want it to carry the STORAGE_NOT_FOUND code", text)
	}
}
