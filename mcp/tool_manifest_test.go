package mcp

// Typed-handler + descriptor tests for the get_manifest bootstrap tool (E2-S4),
// ported from the legacy internal/mcp/tools_manifest_test.go but driven WITHOUT a
// server. They confirm: get_manifest is registered with reflection-generated schemas
// and the legacy description, every section of the single-call payload is populated
// (server name + version, tool catalog, item types, connection types, skills index,
// capability split), AND — the anti-drift guarantee this story buys — the manifest's
// tool list is EXACTLY the catalog Tools(cfg) produced and reports cfg.Version.

import (
	"context"
	"strings"
	"testing"
)

// manifestPayload mirrors getManifestOutput for decoding an Invoke result.
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

// TestGetManifestRegistered asserts get_manifest is present in the Tools() catalog
// with reflection-generated schemas and the legacy CALL-FIRST description.
func TestGetManifestRegistered(t *testing.T) {
	d := findDescriptor(t, "lattice_get_manifest")
	if d.Description != getManifestDescription {
		t.Errorf("description mismatch:\n got %q\nwant %q", d.Description, getManifestDescription)
	}
	if !strings.Contains(d.Description, "CALL FIRST") {
		t.Errorf("description = %q, want it to mark the tool CALL FIRST", d.Description)
	}
	if len(d.InputSchema) == 0 {
		t.Errorf("get_manifest has empty InputSchema (expected reflection-generated)")
	}
	if len(d.OutputSchema) == 0 {
		t.Errorf("get_manifest has empty OutputSchema (expected reflection-generated)")
	}
}

// TestGetManifestSections asserts every section of the bootstrap payload is
// populated as the story requires, driving the descriptor's erased Invoke so the
// wire shape is exercised end to end.
func TestGetManifestSections(t *testing.T) {
	svc := newTestService(t)
	d := findDescriptor(t, "lattice_get_manifest")

	raw, err := d.Invoke(context.Background(), svc, nil)
	if err != nil {
		t.Fatalf("Invoke get_manifest: %v", err)
	}

	var out manifestPayload
	remarshal(t, raw, &out)

	tests := []struct {
		name string
		ok   bool
		msg  string
	}{
		{"server name set", out.Server == "lattice", "server should be \"lattice\""},
		{"version threaded", out.Version == "test", "version should be Config.Version (\"test\")"},
		{"tool catalog non-empty", len(out.Tools) > 0, "tool catalog should list registered tools"},
		{"tool catalog includes get_manifest", manifestHasTool(out, "lattice_get_manifest"), "tool catalog should include get_manifest itself"},
		{"item types non-empty", len(out.ItemTypes) > 0, "item types should come from service.ListSchemas"},
		{"item types include dashboard envelope", containsStr(out.ItemTypes, "dashboard"), "item types should include the dashboard envelope token"},
		{"connection types present", len(out.ConnectionTypes) > 0, "connection types (http, static) should be listed"},
		{"skills index non-empty", len(out.Skills) > 0, "skills index should come from skills.List()"},
		{"skills index includes session-bootstrap", manifestHasSkill(out, "session-bootstrap"), "skills index should include the keystone session-bootstrap skill"},
		{"capability read", out.Capabilities.Read, "capabilities.read should be true"},
		{"capability simulate", out.Capabilities.Simulate, "capabilities.simulate should be true"},
		{"capability never persists", !out.Capabilities.Persist, "capabilities.persist should be false (MCP never persists)"},
		{"capability write endpoint", out.Capabilities.WriteEndpoint == patchWriteEndpoint, "capabilities.writeEndpoint should name the HTTP patch route"},
	}
	for _, tc := range tests {
		if !tc.ok {
			t.Errorf("%s: %s", tc.name, tc.msg)
		}
	}
}

// TestGetManifestDerivedFromCatalog is the anti-drift guarantee this story buys: the
// manifest's tool list is EXACTLY the set of tools Tools(cfg) produced (no more, no
// fewer), and the reported version is the cfg one — not a process-global. It builds
// the catalog with a distinctive version, derives the expected tool-name set from the
// live descriptors, then invokes get_manifest and asserts equality.
func TestGetManifestDerivedFromCatalog(t *testing.T) {
	svc := newTestService(t)

	const wantVersion = "v-anti-drift-9.9.9"
	catalog := Tools(Config{Version: wantVersion})

	// Expected: exactly the names of every descriptor in the live catalog (which
	// includes get_manifest itself).
	want := make(map[string]bool, len(catalog))
	var manifest ToolDescriptor
	var foundManifest bool
	for _, d := range catalog {
		want[d.Name] = true
		if d.Name == "lattice_get_manifest" {
			manifest = d
			foundManifest = true
		}
	}
	if !foundManifest {
		t.Fatalf("get_manifest not present in Tools() catalog")
	}

	raw, err := manifest.Invoke(context.Background(), svc, nil)
	if err != nil {
		t.Fatalf("Invoke get_manifest: %v", err)
	}
	var out manifestPayload
	remarshal(t, raw, &out)

	if out.Version != wantVersion {
		t.Errorf("version = %q, want %q (must come from Config.Version, not a global)", out.Version, wantVersion)
	}

	got := make(map[string]bool, len(out.Tools))
	for _, tool := range out.Tools {
		if got[tool.Name] {
			t.Errorf("manifest lists tool %q more than once", tool.Name)
		}
		got[tool.Name] = true
		if !want[tool.Name] {
			t.Errorf("manifest lists tool %q that is not in the live catalog (drift)", tool.Name)
		}
		if tool.Description == "" {
			t.Errorf("manifest tool %q has an empty description", tool.Name)
		}
	}
	for name := range want {
		if !got[name] {
			t.Errorf("manifest is missing catalog tool %q (drift)", name)
		}
	}
}

// TestManifestCatalogIncludesReadTools is the E2-S5 corrective guarantee: the
// derived manifest catalog now lists all 10 tools, and in particular the two read
// tools (list_dashboards, get_document) the original plan omitted. It invokes
// get_manifest and asserts both are present with the legacy descriptions, and that
// the full catalog has exactly 10 entries (no drop, no duplication).
func TestManifestCatalogIncludesReadTools(t *testing.T) {
	svc := newTestService(t)
	d := findDescriptor(t, "lattice_get_manifest")

	raw, err := d.Invoke(context.Background(), svc, nil)
	if err != nil {
		t.Fatalf("Invoke get_manifest: %v", err)
	}
	var out manifestPayload
	remarshal(t, raw, &out)

	const wantCount = 10
	if len(out.Tools) != wantCount {
		t.Errorf("manifest catalog has %d tools, want %d", len(out.Tools), wantCount)
	}

	byName := map[string]string{}
	for _, tool := range out.Tools {
		byName[tool.Name] = tool.Description
	}
	for _, tc := range []struct {
		name string
		desc string
	}{
		{"lattice_list_dashboards", listDashboardsDescription},
		{"lattice_get_document", getDocumentDescription},
	} {
		got, ok := byName[tc.name]
		if !ok {
			t.Errorf("manifest catalog is missing read tool %q", tc.name)
			continue
		}
		if got != tc.desc {
			t.Errorf("%s manifest description mismatch:\n got %q\nwant %q", tc.name, got, tc.desc)
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
