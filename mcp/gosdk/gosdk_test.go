package gosdk_test

// These tests exercise the adapter end-to-end over the SDK's in-memory
// client/server transport: a bare server is Register-ed, a client connects, and
// tools/resources are called exactly as a real host would. This is the highest-
// fidelity check that the low-level handler closure reproduces ToolHandlerFor's
// behavior (structured output on success, IsError-packed coded errors on
// failure) and that the skill resources match the legacy registration. The SDK
// lives only in this _test file and the adapter, preserving the core firewall.

import (
	"context"
	"os"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/mcp/gosdk"
	"github.com/frankbardon/lattice/service"
)

const fixtureID = "example-minimal"

// newTestService builds a service over an in-memory store seeded with the
// minimal example document and the repo's real schema catalog. Paths are
// relative to this package dir (mcp/gosdk).
func newTestService(t *testing.T) *service.Service {
	t.Helper()
	store, err := service.NewStore(service.BackendFS, afero.NewMemMapFs(), "docs")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	doc, err := os.ReadFile("../../examples/minimal-dashboard.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := store.Save(doc); err != nil {
		t.Fatalf("store.Save: %v", err)
	}
	res, err := service.NewResolver(os.DirFS("../../schemas"))
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	return service.New(store, res)
}

// connect builds a bare server, Registers lattice onto it, wires an in-memory
// client/server pair, and returns the connected client session. Sessions are
// closed via t.Cleanup.
func connect(t *testing.T) *sdkmcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "lattice-test", Version: "test"}, nil)
	gosdk.Register(server, newTestService(t), "v9.9.9")

	clientT, serverT := sdkmcp.NewInMemoryTransports()
	serverConn, err := server.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverConn.Close() })

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "test"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// TestRegisterMountsAllTools asserts the full descriptor catalog (10 tools) is
// mounted and listable, matching the mcp core's Tools(cfg).
func TestRegisterMountsAllTools(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	got := make(map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		got[tool.Name] = true
		if tool.InputSchema == nil {
			t.Errorf("tool %q has nil input schema", tool.Name)
		}
	}
	want := []string{
		"lattice_list_dashboards", "lattice_get_document", "lattice_get_outline", "lattice_get_node",
		"lattice_list_schemas", "lattice_get_schema", "lattice_validate_patch",
		"lattice_list_skills", "lattice_get_skill", "lattice_get_manifest",
	}
	if len(res.Tools) != len(want) {
		t.Errorf("tool count = %d, want %d (%v)", len(res.Tools), len(want), toolNames(res.Tools))
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("tool %q not mounted", name)
		}
	}
}

func toolNames(tools []*sdkmcp.Tool) []string {
	out := make([]string, len(tools))
	for i, tool := range tools {
		out[i] = tool.Name
	}
	return out
}

// TestSuccessfulCallReturnsStructuredOutput asserts a successful tool call
// surfaces structured content plus mirrored JSON text, the ToolHandlerFor
// success contract the adapter reproduces.
func TestSuccessfulCallReturnsStructuredOutput(t *testing.T) {
	cs := connect(t)
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "lattice_get_outline",
		Arguments: map[string]any{"id": fixtureID},
	})
	if err != nil {
		t.Fatalf("CallTool get_outline: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_outline reported IsError; content=%v", textOf(res))
	}
	if res.StructuredContent == nil {
		t.Error("get_outline returned nil StructuredContent")
	}
	if txt := textOf(res); txt == "" || !strings.Contains(txt, fixtureID) {
		t.Errorf("get_outline text content missing fixture id; got %q", txt)
	}
}

// TestVersionFlowsToManifest asserts the version passed to Register reaches the
// version-aware manifest tool (retiring the legacy serverVersion global).
func TestVersionFlowsToManifest(t *testing.T) {
	cs := connect(t)
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "lattice_get_manifest"})
	if err != nil {
		t.Fatalf("CallTool get_manifest: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_manifest reported IsError; content=%v", textOf(res))
	}
	if txt := textOf(res); !strings.Contains(txt, "v9.9.9") {
		t.Errorf("manifest output missing version v9.9.9; got %q", txt)
	}
}

// TestErrorCallSurfacesCodedError asserts a tool error is packed into the result
// as IsError + the facade's coded-error text (NOT a protocol error) — the legacy
// ToolHandlerFor error semantics the low-level closure must reproduce.
func TestErrorCallSurfacesCodedError(t *testing.T) {
	cs := connect(t)
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "lattice_get_skill",
		Arguments: map[string]any{"name": "no-such-skill"},
	})
	if err != nil {
		t.Fatalf("CallTool get_skill should not return a protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatal("get_skill on unknown name should set IsError")
	}
	txt := textOf(res)
	if !strings.Contains(txt, "MCP_SKILL_NOT_FOUND") {
		t.Errorf("error content missing coded error MCP_SKILL_NOT_FOUND; got %q", txt)
	}
}

// TestSkillResourceReadReturnsMarkdown asserts the lattice-skill://<name>
// resources are mounted and a read returns the skill's markdown body, matching
// the legacy resources_skills.go registration.
func TestSkillResourceReadReturnsMarkdown(t *testing.T) {
	cs := connect(t)

	list, err := cs.ListResources(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(list.Resources) == 0 {
		t.Fatal("no skill resources mounted")
	}
	var found bool
	for _, r := range list.Resources {
		if r.URI == "lattice-skill://session-bootstrap" {
			found = true
			if r.MIMEType != "text/markdown" {
				t.Errorf("session-bootstrap MIME = %q, want text/markdown", r.MIMEType)
			}
		}
	}
	if !found {
		t.Fatal("lattice-skill://session-bootstrap resource not mounted")
	}

	read, err := cs.ReadResource(context.Background(), &sdkmcp.ReadResourceParams{
		URI: "lattice-skill://session-bootstrap",
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(read.Contents) == 0 {
		t.Fatal("session-bootstrap read returned no contents")
	}
	c := read.Contents[0]
	if c.MIMEType != "text/markdown" {
		t.Errorf("content MIME = %q, want text/markdown", c.MIMEType)
	}
	if !strings.Contains(c.Text, "session-bootstrap") {
		t.Errorf("session-bootstrap body looks wrong; got %q", truncate(c.Text, 80))
	}
}

// textOf concatenates the text of a result's TextContent parts.
func textOf(res *sdkmcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
