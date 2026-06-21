package mcp_test

// End-to-end proof of the two skill tools (E1-S2). It reuses the shared
// newTestSession harness (which builds the full MCP server — every registrar,
// skill tools included — over the SDK's in-memory transport) and drives
// list_skills and get_skill through a real MCP client session, asserting on the
// structured catalog output, the verbatim markdown body of a hit, and the
// tool-error packing of the MCP_SKILL_NOT_FOUND *errors.CodedError on a miss.

import (
	"context"
	"sort"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// bootstrapSkill is the name of the keystone skill that ships in the embedded
// corpus; the list/get tests anchor on it.
const bootstrapSkill = "session-bootstrap"

// skillMeta mirrors the json shape of skills.Metadata for decoding the
// list_skills structured output.
type skillMeta struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Type        string   `json:"type"`
	Kind        string   `json:"kind"`
	AppliesTo   []string `json:"applies_to"`
	Covers      []string `json:"covers"`
}

func TestSkillToolsListed(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	want := map[string]bool{"list_skills": false, "get_skill": false}
	for _, tool := range res.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("tool %q not listed by host", name)
		}
	}
}

func TestListSkills(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "list_skills"})
	if err != nil {
		t.Fatalf("CallTool list_skills: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_skills returned tool error: %v", res.Content)
	}

	var out struct {
		Skills []skillMeta `json:"skills"`
	}
	decodeStructured(t, res, &out)

	if len(out.Skills) == 0 {
		t.Fatalf("skills is empty, want at least the bootstrap skill")
	}

	// Sorted by name (skills.List sorts).
	if !sort.SliceIsSorted(out.Skills, func(i, j int) bool { return out.Skills[i].Name < out.Skills[j].Name }) {
		t.Errorf("skills not sorted by name: %v", out.Skills)
	}

	// The bootstrap skill is present with its frontmatter metadata populated.
	var boot *skillMeta
	for i := range out.Skills {
		if out.Skills[i].Name == bootstrapSkill {
			boot = &out.Skills[i]
			break
		}
	}
	if boot == nil {
		t.Fatalf("skill %q not in catalog", bootstrapSkill)
	}
	if boot.Description == "" {
		t.Errorf("bootstrap skill has empty description")
	}
	if boot.Type == "" {
		t.Errorf("bootstrap skill has empty type")
	}
	if len(boot.AppliesTo) == 0 {
		t.Errorf("bootstrap skill has empty applies_to")
	}
}

func TestGetSkillHit(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "get_skill",
		Arguments: map[string]any{"name": bootstrapSkill},
	})
	if err != nil {
		t.Fatalf("CallTool get_skill: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_skill returned tool error: %v", res.Content)
	}

	var out struct {
		Name string `json:"name"`
		Body string `json:"body"`
	}
	decodeStructured(t, res, &out)

	if out.Name != bootstrapSkill {
		t.Errorf("name = %q, want %q", out.Name, bootstrapSkill)
	}
	if len(out.Body) == 0 {
		t.Errorf("body is empty, want the raw markdown")
	}
	// Served verbatim: the body keeps its leading frontmatter block.
	if !contains(out.Body, "name: "+bootstrapSkill) {
		t.Errorf("body does not carry the raw frontmatter (not verbatim): %q", out.Body)
	}
}

func TestGetSkillMissIsToolError(t *testing.T) {
	cs := newTestSession(t)

	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "get_skill",
		Arguments: map[string]any{"name": "does-not-exist"},
	})
	if err != nil {
		t.Fatalf("CallTool unexpectedly returned a protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError for unknown skill name, got success")
	}

	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			text += tc.Text
		}
	}
	if !contains(text, "MCP_SKILL_NOT_FOUND") {
		t.Errorf("tool error content = %q, want it to carry the MCP_SKILL_NOT_FOUND code", text)
	}
}
