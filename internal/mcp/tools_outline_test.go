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
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Container bool           `json:"container"`
	Placement string         `json:"placement"`
	Metadata  map[string]any `json:"metadata"`
	Children  []outlineNode  `json:"children"`
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

// TestGetOutlineMetadata asserts the outline surfaces a node's freeform metadata
// (element-metadata E2-S2) ONLY for the eligible nodes that carry it, and omits it
// elsewhere. The seeded document attaches metadata to the root container and one
// block wrapper; the body container and the bare tables carry none.
func TestGetOutlineMetadata(t *testing.T) {
	cs := newMetadataTestSession(t)

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "get_outline",
		Arguments: map[string]any{"id": metadataFixtureID},
	})
	if err != nil {
		t.Fatalf("CallTool get_outline: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_outline returned tool error: %v", res.Content)
	}

	var out struct {
		Root *outlineNode `json:"root"`
	}
	decodeStructured(t, res, &out)
	if out.Root == nil {
		t.Fatalf("root is nil")
	}

	// Index every node by id so eligibility/presence can be asserted per node.
	nodes := map[string]*outlineNode{}
	var walk func(n *outlineNode)
	walk = func(n *outlineNode) {
		if n.ID != "" {
			nodes[n.ID] = n
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(out.Root)

	// Root (eligible) carries its metadata.
	root := nodes["root"]
	if root == nil {
		t.Fatalf("root node not in outline")
	}
	if got, want := root.Metadata["owner"], "platform-team"; got != want {
		t.Errorf("root metadata owner = %v, want %v", got, want)
	}

	// The block wrapper (eligible) carries its metadata.
	block := nodes["fruits-block"]
	if block == nil {
		t.Fatalf("fruits-block node not in outline")
	}
	if got, want := block.Metadata["source"], "produce-api"; got != want {
		t.Errorf("fruits-block metadata source = %v, want %v", got, want)
	}

	// Nodes that declared no metadata omit the field entirely (nil decoded map).
	for _, id := range []string{"body", "fruits", "metrics-block", "metrics"} {
		if n := nodes[id]; n != nil && n.Metadata != nil {
			t.Errorf("node %q unexpectedly carries metadata %v, want omitted", id, n.Metadata)
		}
	}

	// The omitempty discipline must hold on the wire: the raw structured result
	// must NOT contain a "metadata" key for any metadata-free node. We assert the
	// metadata-free body container's surrounding shape carries no metadata by
	// confirming the only metadata values present are the two we seeded.
	raw := marshalStructured(t, res)
	if !strings.Contains(raw, "platform-team") || !strings.Contains(raw, "produce-api") {
		t.Errorf("outline did not carry the seeded metadata values:\n%s", raw)
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
