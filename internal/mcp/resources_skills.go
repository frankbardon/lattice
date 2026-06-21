package mcp

import (
	"context"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/frankbardon/lattice/internal/mcp/skills"
	"github.com/frankbardon/lattice/service"
)

// skillURIScheme is the URI scheme under which every embedded skill is exposed
// as an MCP resource. Each skill becomes lattice-skill://<name>, mirroring the
// get_skill tool's name argument (the skill's file stem). Surfacing skills as
// resources lets a host that browses resources discover the skill catalog
// without issuing a tool call; reading a resource returns the same markdown body
// get_skill serves.
const skillURIScheme = "lattice-skill://"

func init() {
	registrars = append(registrars, registerSkillResources)
}

// registerSkillResources registers one MCP resource per embedded skill, driven
// by skills.List so a newly added skill file appears automatically. Each
// resource is URI lattice-skill://<name>, MIME text/markdown, with the skill's
// frontmatter description as the resource description. Resources need no service
// facade (they read only the pure embedded skills package), so the service
// argument is ignored.
//
// Resources are a fixed snapshot taken at registration: a host that expects a
// skill added after connect must reconnect to see it.
func registerSkillResources(s *sdkmcp.Server, _ *service.Service) {
	for _, meta := range skills.List() {
		s.AddResource(&sdkmcp.Resource{
			URI:         skillURIScheme + meta.Name,
			Name:        meta.Name,
			Description: meta.Description,
			MIMEType:    "text/markdown",
		}, makeSkillReader(meta.Name))
	}
}

// makeSkillReader builds the ResourceHandler for one skill resource: it returns
// the skill's raw markdown body via skills.Get, served verbatim (the same source
// get_skill returns). The name is captured per resource so every reader fetches
// its own skill. A miss (the skill file vanished between registration and the
// read — not expected for embedded data) surfaces the SDK's standard
// resource-not-found error.
func makeSkillReader(name string) sdkmcp.ResourceHandler {
	return func(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
		body, ok := skills.Get(name)
		if !ok {
			return nil, sdkmcp.ResourceNotFoundError(req.Params.URI)
		}
		return &sdkmcp.ReadResourceResult{
			Contents: []*sdkmcp.ResourceContents{{
				URI:      req.Params.URI,
				MIMEType: "text/markdown",
				Text:     body,
			}},
		}, nil
	}
}
