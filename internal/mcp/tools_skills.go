package mcp

import (
	"context"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/mcp/skills"
	"github.com/frankbardon/lattice/service"
)

// The two skill tools registered here — list_skills and get_skill — expose
// lattice's embedded skill corpus (the mcp/skills package) to an MCP
// host: a guide/reference catalog the host enumerates to discover how to drive
// the rest of the tool surface, then fetches on demand. They read ONLY the pure
// embedded skills package (which itself imports nothing from internal/* and is
// boundary-safe) and the module-root errors package; an unknown skill name
// surfaces an *errors.CodedError verbatim as a tool error (IsError set), never a
// flattened plain string.

func init() {
	registrars = append(registrars, registerListSkills, registerGetSkill)
}

// listSkillsInput is the input for list_skills: it takes no arguments. AddTool
// still requires an object-typed input schema, so this is an empty struct.
type listSkillsInput struct{}

// listSkillsOutput is the structured result of list_skills: the full frontmatter
// metadata for every embedded skill, sorted by name.
type listSkillsOutput struct {
	// Skills are the corpus's catalog entries — each skill's frontmatter
	// metadata (name, description, type, kind, applies_to, covers) — sorted by
	// name. Metadata is a flat (non-recursive) struct, so the reflective
	// output-schema generator handles it directly.
	Skills []skills.Metadata `json:"skills" jsonschema:"the embedded skills, each with its frontmatter metadata, sorted by name"`
}

// registerListSkills registers the list_skills tool: it enumerates the embedded
// skill corpus via skills.List (already sorted by name) and returns every
// skill's frontmatter metadata, so a host can pick the right skill without
// fetching any body.
func registerListSkills(s *sdkmcp.Server, _ *service.Service) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "list_skills",
		Description: "List the embedded skills (workflow guides and references), each with its frontmatter metadata (name, description, type, kind, applies_to, covers), sorted by name. The name is the key get_skill accepts.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ listSkillsInput) (*sdkmcp.CallToolResult, listSkillsOutput, error) {
		return nil, listSkillsOutput{Skills: skills.List()}, nil
	})
}

// getSkillInput is the input for get_skill: the name of the skill to fetch (the
// file stem, as listed by list_skills — without the .md extension).
type getSkillInput struct {
	// Name is the skill's stable identifier (its name in list_skills / its file
	// stem), without the .md extension.
	Name string `json:"name" jsonschema:"the name of the skill to fetch (as listed by list_skills, without the .md extension)"`
}

// getSkillOutput is the structured result of get_skill: the requested skill's
// raw markdown body, served verbatim.
type getSkillOutput struct {
	// Name echoes the requested skill name.
	Name string `json:"name" jsonschema:"the requested skill name"`

	// Body is the skill's raw markdown content (including its frontmatter block),
	// returned as-is — not wrapped or re-rendered.
	Body string `json:"body" jsonschema:"the skill's raw markdown body, served verbatim"`
}

// registerGetSkill registers the get_skill tool: it returns the named skill's
// raw markdown body via skills.Get, served verbatim (not wrapped or
// re-rendered). An unknown name surfaces an MCP_SKILL_NOT_FOUND *errors.CodedError
// verbatim as a tool error (IsError set), with the requested name in
// Details["name"].
func registerGetSkill(s *sdkmcp.Server, _ *service.Service) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "get_skill",
		Description: "Fetch a skill's raw markdown body by name (as listed by list_skills). The body is returned verbatim. An unknown name is a tool error (MCP_SKILL_NOT_FOUND).",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, input getSkillInput) (*sdkmcp.CallToolResult, getSkillOutput, error) {
		body, ok := skills.Get(input.Name)
		if !ok {
			// Unknown name surfaces MCP_SKILL_NOT_FOUND verbatim as a tool error.
			return nil, getSkillOutput{}, errors.NewCodedErrorWithDetails(
				errors.MCP_SKILL_NOT_FOUND,
				"no skill named "+input.Name,
				map[string]any{"name": input.Name},
			)
		}
		return nil, getSkillOutput{Name: input.Name, Body: body}, nil
	})
}
