package mcp

// Typed-handler + descriptor tests for the two skill tools (E2-S3), ported from
// the legacy internal/mcp/tools_skills_test.go but driven WITHOUT a server. They
// confirm: list_skills enumerates the embedded corpus sorted by name with the
// keystone bootstrap skill present and populated, get_skill returns the verbatim
// markdown body of a hit (frontmatter intact), and an unknown name surfaces the
// MCP_SKILL_NOT_FOUND coded error verbatim (carrying the requested name).

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// bootstrapSkill is the keystone skill that ships in the embedded corpus; the
// list/get tests anchor on it.
const bootstrapSkill = "session-bootstrap"

// TestListSkillsRegistered asserts list_skills is present in the Tools() catalog
// with a reflection-generated input+output schema and the legacy description.
func TestListSkillsRegistered(t *testing.T) {
	d := findDescriptor(t, "list_skills")
	if d.Description != listSkillsDescription {
		t.Errorf("description mismatch:\n got %q\nwant %q", d.Description, listSkillsDescription)
	}
	if len(d.InputSchema) == 0 {
		t.Errorf("list_skills has empty InputSchema (expected reflection-generated)")
	}
	if len(d.OutputSchema) == 0 {
		t.Errorf("list_skills has empty OutputSchema (expected reflection-generated)")
	}
}

// TestListSkills asserts list_skills returns the embedded corpus sorted by name,
// with the keystone bootstrap skill present and its frontmatter populated. It
// drives the descriptor's erased Invoke so the wire shape is exercised end to end.
func TestListSkills(t *testing.T) {
	svc := newTestService(t)
	d := findDescriptor(t, "list_skills")

	raw, err := d.Invoke(context.Background(), svc, nil)
	if err != nil {
		t.Fatalf("Invoke list_skills: %v", err)
	}

	var out struct {
		Skills []struct {
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Type        string   `json:"type"`
			Kind        string   `json:"kind"`
			AppliesTo   []string `json:"applies_to"`
			Covers      []string `json:"covers"`
		} `json:"skills"`
	}
	remarshal(t, raw, &out)

	if len(out.Skills) == 0 {
		t.Fatalf("skills is empty, want at least the bootstrap skill")
	}
	if !sort.SliceIsSorted(out.Skills, func(i, j int) bool { return out.Skills[i].Name < out.Skills[j].Name }) {
		t.Errorf("skills not sorted by name: %v", out.Skills)
	}

	var found bool
	for i := range out.Skills {
		if out.Skills[i].Name != bootstrapSkill {
			continue
		}
		found = true
		if out.Skills[i].Description == "" {
			t.Errorf("bootstrap skill has empty description")
		}
		if out.Skills[i].Type == "" {
			t.Errorf("bootstrap skill has empty type")
		}
		if len(out.Skills[i].AppliesTo) == 0 {
			t.Errorf("bootstrap skill has empty applies_to")
		}
		break
	}
	if !found {
		t.Fatalf("skill %q not in catalog", bootstrapSkill)
	}
}

// TestGetSkillRegistered asserts get_skill is present in the Tools() catalog with a
// reflection-generated schema and the legacy description.
func TestGetSkillRegistered(t *testing.T) {
	d := findDescriptor(t, "get_skill")
	if d.Description != getSkillDescription {
		t.Errorf("description mismatch:\n got %q\nwant %q", d.Description, getSkillDescription)
	}
	if len(d.InputSchema) == 0 {
		t.Errorf("get_skill has empty InputSchema (expected reflection-generated)")
	}
	if len(d.OutputSchema) == 0 {
		t.Errorf("get_skill has empty OutputSchema (expected reflection-generated)")
	}
}

// TestGetSkillHit asserts get_skill returns the named skill's raw markdown body
// verbatim, with the leading frontmatter block intact and the name echoed.
func TestGetSkillHit(t *testing.T) {
	svc := newTestService(t)
	d := findDescriptor(t, "get_skill")

	raw, err := d.Invoke(context.Background(), svc, json.RawMessage(`{"name":"`+bootstrapSkill+`"}`))
	if err != nil {
		t.Fatalf("Invoke get_skill: %v", err)
	}

	var out struct {
		Name string `json:"name"`
		Body string `json:"body"`
	}
	remarshal(t, raw, &out)

	if out.Name != bootstrapSkill {
		t.Errorf("name = %q, want %q", out.Name, bootstrapSkill)
	}
	if len(out.Body) == 0 {
		t.Errorf("body is empty, want the raw markdown")
	}
	// Served verbatim: the body keeps its leading frontmatter block.
	if !strings.Contains(out.Body, "name: "+bootstrapSkill) {
		t.Errorf("body does not carry the raw frontmatter (not verbatim): %q", out.Body)
	}
}

// TestGetSkillMissIsCodedError asserts an unknown skill name surfaces the
// MCP_SKILL_NOT_FOUND coded error verbatim (not flattened to a string), with the
// requested name captured in its details.
func TestGetSkillMissIsCodedError(t *testing.T) {
	svc := newTestService(t)

	_, err := getSkill(context.Background(), svc, getSkillInput{Name: "does-not-exist"})
	if err == nil {
		t.Fatalf("expected an error for unknown skill name, got success")
	}
	if !errors.HasCode(err, errors.MCP_SKILL_NOT_FOUND) {
		t.Errorf("error = %v, want it to carry MCP_SKILL_NOT_FOUND", err)
	}
	coded, ok := err.(*errors.CodedError)
	if !ok {
		t.Fatalf("error is not a *errors.CodedError: %v", err)
	}
	if coded.Details["name"] != "does-not-exist" {
		t.Errorf("details name = %v, want %q", coded.Details["name"], "does-not-exist")
	}
}
