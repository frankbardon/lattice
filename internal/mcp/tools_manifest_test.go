package mcp_test

// End-to-end proof of the get_manifest bootstrap tool (E1-S4). It drives the tool
// through the same in-memory transport harness the read tools use (newTestSession)
// and asserts each section of the single-call payload is populated: server name +
// version, a non-empty tool catalog, the item-type catalog (via the facade), the
// connection types, the slim skills index (including the keystone session-bootstrap
// skill), and the read/simulate/never-persist capability split.

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestGetManifestListed asserts get_manifest appears in the host's tool list with
// a reflection-generated input schema.
func TestGetManifestListed(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var found *sdkmcp.Tool
	for _, tool := range res.Tools {
		if tool.Name == "get_manifest" {
			found = tool
		}
	}
	if found == nil {
		t.Fatalf("get_manifest not listed by host")
	}
	if found.InputSchema == nil {
		t.Errorf("get_manifest has nil InputSchema (expected reflection-generated)")
	}
	if !contains(found.Description, "CALL FIRST") {
		t.Errorf("description = %q, want it to mark the tool CALL FIRST", found.Description)
	}
}

// manifestPayload mirrors getManifestOutput for decoding the structured result.
type manifestPayload struct {
	Server  string `json:"server"`
	Version string `json:"version"`
	Tools   []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"tools"`
	ItemTypes       []string `json:"itemTypes"`
	ConnectionTypes []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"connectionTypes"`
	Skills []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"skills"`
	Capabilities struct {
		Read          bool   `json:"read"`
		Simulate      bool   `json:"simulate"`
		Persist       bool   `json:"persist"`
		WriteEndpoint string `json:"writeEndpoint"`
	} `json:"capabilities"`
}

// TestGetManifestSections asserts every section of the bootstrap payload is
// populated as the story requires.
func TestGetManifestSections(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "get_manifest"})
	if err != nil {
		t.Fatalf("CallTool get_manifest: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_manifest returned tool error: %v", res.Content)
	}

	var out manifestPayload
	decodeStructured(t, res, &out)

	tests := []struct {
		name string
		ok   bool
		msg  string
	}{
		{"server name set", out.Server == "lattice", "server should be \"lattice\""},
		{"version threaded", out.Version == "test", "version should be the NewServer version (\"test\")"},
		{"tool catalog non-empty", len(out.Tools) > 0, "tool catalog should list registered tools"},
		{"tool catalog includes get_manifest", manifestHasTool(out, "get_manifest"), "tool catalog should include get_manifest itself"},
		{"item types non-empty", len(out.ItemTypes) > 0, "item types should come from service.ListSchemas"},
		{"item types include dashboard envelope", containsStr(out.ItemTypes, "dashboard"), "item types should include the dashboard envelope token"},
		{"connection types present", len(out.ConnectionTypes) > 0, "connection types (http, static) should be listed"},
		{"skills index non-empty", len(out.Skills) > 0, "skills index should come from skills.List()"},
		{"skills index includes session-bootstrap", manifestHasSkill(out, "session-bootstrap"), "skills index should include the keystone session-bootstrap skill"},
		{"capability read", out.Capabilities.Read, "capabilities.read should be true"},
		{"capability simulate", out.Capabilities.Simulate, "capabilities.simulate should be true"},
		{"capability never persists", !out.Capabilities.Persist, "capabilities.persist should be false (MCP never persists)"},
		{"capability write endpoint", out.Capabilities.WriteEndpoint != "", "capabilities.writeEndpoint should name the HTTP patch route"},
	}
	for _, tc := range tests {
		if !tc.ok {
			t.Errorf("%s: %s", tc.name, tc.msg)
		}
	}
}

// manifestHasTool reports whether the manifest tool catalog includes name.
func manifestHasTool(p manifestPayload, name string) bool {
	for _, tool := range p.Tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// manifestHasSkill reports whether the manifest skills index includes name.
func manifestHasSkill(p manifestPayload, name string) bool {
	for _, sk := range p.Skills {
		if sk.Name == name {
			return true
		}
	}
	return false
}

// containsStr reports whether ss contains s.
func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
