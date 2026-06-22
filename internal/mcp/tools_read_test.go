package mcp_test

// End-to-end proof of the two read tools (E1-S2). It builds a *service.Service
// over an in-memory store (afero MemMapFs via service.NewStore) seeded with a
// known-good example document and the repo's real schema catalog, runs the MCP
// server through the SDK's in-memory transport pair, and drives list_dashboards
// and get_document with a real MCP client session — exercising the full
// facade→tool→host path including reflection-generated schemas and the
// tool-error packing of a *errors.CodedError.

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/internal/mcp"
	"github.com/frankbardon/lattice/service"
)

// fixtureID is the manifest.id of the seeded example document.
const fixtureID = "example-minimal"

// newTestSession builds the service over an in-memory store seeded with the
// minimal example document, starts the MCP server over an in-memory transport,
// and returns a connected client session. The session is closed via t.Cleanup.
func newTestSession(t *testing.T) *sdkmcp.ClientSession {
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
	svc := service.New(store, res)

	srv := mcp.NewServer(svc, "test")

	ctx := context.Background()
	clientT, serverT := sdkmcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "v0"}, nil)
	clientSession, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client Connect: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	return clientSession
}

// metadataFixtureID is the manifest.id of the metadata-carrying fixture seeded by
// newMetadataTestSession.
const metadataFixtureID = "example-metadata"

// metadataFixtureDoc is a legal dashboard that attaches freeform element metadata
// (element-metadata) to two of the eligible node kinds — the document root
// container and a block wrapper — while leaving the body container and the bare
// table leaves metadata-free. It exercises the read surface's per-node metadata
// exposure (present where carried, omitted where absent) without touching the
// shared minimal fixture.
const metadataFixtureDoc = `{
  "manifest": {
    "formatVersion": "1.0.0",
    "id": "example-metadata",
    "title": "Metadata Example Dashboard",
    "author": "Lattice"
  },
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": { "grid": { "columns": [1] } },
    "metadata": { "owner": "platform-team", "revision": 7 },
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
        "id": "body",
        "config": { "grid": { "columns": [1, 1], "rows": [1], "gap": 1 } },
        "children": [
          {
            "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
            "id": "fruits-block",
            "placement": { "colStart": 1, "colSpan": 1, "rowStart": 1, "rowSpan": 1 },
            "metadata": { "source": "produce-api" },
            "config": {
              "id": "fruits-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
                "id": "fruits",
                "config": {
                  "title": "Fruits",
                  "columns": [ { "header": "Name" } ],
                  "rows": [ ["Apple"] ]
                }
              }
            }
          },
          {
            "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
            "id": "metrics-block",
            "placement": { "colStart": 2, "colSpan": 1, "rowStart": 1, "rowSpan": 1 },
            "config": {
              "id": "metrics-block",
              "content": {
                "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
                "id": "metrics",
                "config": {
                  "title": "Metrics",
                  "columns": [ { "header": "Metric" } ],
                  "rows": [ ["Requests"] ]
                }
              }
            }
          }
        ]
      }
    ]
  }
}`

// newMetadataTestSession stands up the MCP server over an in-memory store seeded
// with metadataFixtureDoc (rather than the minimal example), returning a connected
// client session closed via t.Cleanup. It mirrors newTestSession but seeds the
// metadata-carrying document.
func newMetadataTestSession(t *testing.T) *sdkmcp.ClientSession {
	t.Helper()

	store, err := service.NewStore(service.BackendFS, afero.NewMemMapFs(), "docs")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Save([]byte(metadataFixtureDoc)); err != nil {
		t.Fatalf("store.Save: %v", err)
	}
	res, err := service.NewResolver(os.DirFS("../../schemas"))
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	svc := service.New(store, res)

	srv := mcp.NewServer(svc, "test")

	ctx := context.Background()
	clientT, serverT := sdkmcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "v0"}, nil)
	clientSession, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client Connect: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	return clientSession
}

// TestToolsListed asserts both read tools appear in the host's tool list with
// reflection-generated input schemas.
func TestToolsListed(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	want := map[string]bool{"list_dashboards": false, "get_document": false}
	for _, tool := range res.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %q has nil InputSchema (expected reflection-generated)", tool.Name)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("tool %q not listed by host", name)
		}
	}
}

// TestListDashboards asserts list_dashboards returns the seeded id with its
// manifest title in structured output.
func TestListDashboards(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "list_dashboards"})
	if err != nil {
		t.Fatalf("CallTool list_dashboards: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_dashboards returned tool error: %v", res.Content)
	}

	var out struct {
		Dashboards []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"dashboards"`
	}
	decodeStructured(t, res, &out)

	if len(out.Dashboards) != 1 {
		t.Fatalf("dashboards = %d, want 1", len(out.Dashboards))
	}
	if out.Dashboards[0].ID != fixtureID {
		t.Errorf("id = %q, want %q", out.Dashboards[0].ID, fixtureID)
	}
	if out.Dashboards[0].Title == "" {
		t.Errorf("title is empty, want the manifest title")
	}
}

// TestGetDocumentRaw asserts get_document returns the raw stored document and,
// without the resolved flag, no resolved tree.
func TestGetDocumentRaw(t *testing.T) {
	cs := newTestSession(t)

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
		ID       string          `json:"id"`
		Document json.RawMessage `json:"document"`
		Resolved json.RawMessage `json:"resolved"`
	}
	decodeStructured(t, res, &out)

	if out.ID != fixtureID {
		t.Errorf("id = %q, want %q", out.ID, fixtureID)
	}
	if len(out.Document) == 0 {
		t.Errorf("document is empty, want raw stored bytes")
	}
	if len(out.Resolved) != 0 {
		t.Errorf("resolved present without the resolved flag: %s", out.Resolved)
	}
}

// TestGetDocumentMetadataRoundTrip asserts get_document's RAW output carries the
// stored element metadata through unchanged (element-metadata E2-S2): the bytes a
// host reads back are the bytes that were stored, so the metadata rides along for
// free. It re-parses the returned raw document and confirms the seeded root and
// block metadata are present verbatim, and that the resolved tree (with
// resolved=true) carries them too.
func TestGetDocumentMetadataRoundTrip(t *testing.T) {
	cs := newMetadataTestSession(t)

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "get_document",
		Arguments: map[string]any{"id": metadataFixtureID, "resolved": true},
	})
	if err != nil {
		t.Fatalf("CallTool get_document: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_document returned tool error: %v", res.Content)
	}

	var out struct {
		Document json.RawMessage `json:"document"`
		Resolved *struct {
			Root map[string]any `json:"root"`
		} `json:"resolved"`
	}
	decodeStructured(t, res, &out)

	// Raw round-trip: the returned document re-parses to the stored metadata.
	var doc struct {
		Root struct {
			Metadata map[string]any    `json:"metadata"`
			Children []json.RawMessage `json:"children"`
		} `json:"root"`
	}
	if err := json.Unmarshal(out.Document, &doc); err != nil {
		t.Fatalf("unmarshal returned raw document: %v", err)
	}
	if got, want := doc.Root.Metadata["owner"], "platform-team"; got != want {
		t.Errorf("raw root metadata owner = %v, want %v", got, want)
	}
	// The block's metadata must survive verbatim too (it is two levels down under
	// body); confirm via a substring of the raw bytes.
	if !contains(string(out.Document), "produce-api") {
		t.Errorf("raw document dropped the block metadata value %q:\n%s", "produce-api", out.Document)
	}

	// Resolved tree carries the root metadata too.
	if out.Resolved == nil {
		t.Fatalf("resolved tree absent with resolved=true")
	}
	rootMeta, _ := out.Resolved.Root["metadata"].(map[string]any)
	if rootMeta == nil || rootMeta["owner"] != "platform-team" {
		t.Errorf("resolved root metadata = %v, want owner=platform-team", out.Resolved.Root["metadata"])
	}
}

// TestGetDocumentResolved asserts get_document with resolved=true returns the
// resolved tree alongside the raw document.
func TestGetDocumentResolved(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "get_document",
		Arguments: map[string]any{"id": fixtureID, "resolved": true},
	})
	if err != nil {
		t.Fatalf("CallTool get_document: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_document(resolved) returned tool error: %v", res.Content)
	}

	var out struct {
		Resolved *struct {
			Manifest map[string]any `json:"manifest"`
			Root     map[string]any `json:"root"`
		} `json:"resolved"`
	}
	decodeStructured(t, res, &out)

	if out.Resolved == nil {
		t.Fatalf("resolved tree absent with resolved=true")
	}
	if out.Resolved.Root == nil {
		t.Errorf("resolved tree has no root")
	}
}

// TestGetDocumentUnknownIDIsToolError asserts an unknown id surfaces the store's
// STORAGE_NOT_FOUND coded error as an MCP tool error (IsError), not a protocol
// error, and that the code text reaches the host content.
func TestGetDocumentUnknownIDIsToolError(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "get_document",
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

// decodeStructured re-marshals the tool result's StructuredContent and unmarshals
// it into v, so a test can assert against the structured output the AddTool path
// emits.
func decodeStructured(t *testing.T, res *sdkmcp.CallToolResult, v any) {
	t.Helper()
	if res.StructuredContent == nil {
		t.Fatalf("result has no StructuredContent")
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("unmarshal StructuredContent: %v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
