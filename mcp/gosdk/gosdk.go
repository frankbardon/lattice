// Package gosdk is the SDK-coupled adapter for lattice's MCP surface: the ONLY
// package in the repo (tests aside) that imports the protocol SDK
// (github.com/modelcontextprotocol/go-sdk). It mounts the SDK-free tool catalog
// and skill corpus from the mcp core onto a caller-supplied *mcp.Server.
//
// THE GRAFT POINT. Register does NOT construct or return a server — it takes one
// the caller already built. That is what lets a downstream MCP host graft
// lattice's tools and skill resources beside its own: build a server, Register
// lattice onto it, add your own tools, run it. A downstream that runs on this
// exact SDK uses Register; a downstream on a different SDK ignores this package
// and mounts the mcp.ToolDescriptor catalog itself.
//
// THE FIREWALL. All go-sdk usage lives here so the mcp core stays SDK-free (a
// test enforces it). Register replicates, verbatim, the legacy
// internal/mcp server's behavior: the low-level (type-erased) tool path and the
// lattice-skill://<name> resource registrations.
package gosdk

import (
	"context"
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/frankbardon/lattice/mcp"
	"github.com/frankbardon/lattice/mcp/skills"
	"github.com/frankbardon/lattice/service"
)

// skillURIScheme is the URI scheme under which every embedded skill is exposed
// as an MCP resource: each skill becomes lattice-skill://<name>, mirroring the
// get_skill tool's name argument (the skill's file stem). This matches the
// legacy internal/mcp registration exactly.
const skillURIScheme = "lattice-skill://"

// Register mounts the full lattice MCP surface — every tool descriptor from
// mcp.Tools(cfg) and one lattice-skill://<name> resource per embedded skill —
// onto the caller-supplied server. It does not construct or return a server: the
// caller owns server lifecycle (and may mount its own tools alongside).
//
// version flows into mcp.Config so version-aware tools (the manifest) report the
// build version; it retires the legacy serverVersion process-global.
func Register(server *sdkmcp.Server, svc *service.Service, version string) {
	for _, d := range mcp.Tools(mcp.Config{Version: version}) {
		server.AddTool(toolFor(d), handlerFor(d, svc))
	}
	registerSkillResources(server)
}

// toolFor builds the SDK Tool value from a type-erased descriptor. The
// descriptor carries already-marshaled JSON Schema documents (json.RawMessage);
// the SDK's Tool wants *jsonschema.Schema, so each is unmarshaled into one. The
// reflected struct schemas are objects ("type": "object"), which the MCP spec
// requires for an input schema.
func toolFor(d mcp.ToolDescriptor) *sdkmcp.Tool {
	return &sdkmcp.Tool{
		Name:         d.Name,
		Description:  d.Description,
		InputSchema:  schemaFor(d.InputSchema),
		OutputSchema: schemaFor(d.OutputSchema),
	}
}

// schemaFor unmarshals a raw JSON Schema document into the SDK's *jsonschema.Schema.
// A nil/empty raw schema yields a nil schema. A malformed schema is a programming
// error baked into a descriptor at construction time (the mcp core reflects these
// from Go structs), so it panics rather than silently mounting a broken tool.
func schemaFor(raw json.RawMessage) *jsonschema.Schema {
	if len(raw) == 0 {
		return nil
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		panic("mcp/gosdk: unmarshaling reflected schema: " + err.Error())
	}
	return &s
}

// handlerFor builds the low-level ToolHandler for one descriptor. The low-level
// Server.AddTool does NO pre/post-processing, so this closure replicates exactly
// what the SDK's ToolHandlerFor does for the typed path the legacy code used:
//
//   - call the descriptor's erased Invoke with the raw arguments;
//   - on error: pack it into the result as a TOOL error (IsError + the error
//     text as Content), returning a nil Go error — NOT a protocol error. The
//     facade's coded-error text reaches the host verbatim, matching legacy
//     ToolHandlerFor semantics;
//   - on success: set StructuredContent to the output and mirror it as JSON
//     text in Content (as ToolHandlerFor does when Content is unset).
func handlerFor(d mcp.ToolDescriptor, svc *service.Service) sdkmcp.ToolHandler {
	return func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		out, err := d.Invoke(ctx, svc, req.Params.Arguments)
		if err != nil {
			// Tool error, not protocol error: the coded-error text surfaces
			// verbatim in Content with IsError set; the Go error is nil.
			return &sdkmcp.CallToolResult{
				IsError: true,
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: err.Error()}},
			}, nil
		}
		text, marshalErr := json.Marshal(out)
		if marshalErr != nil {
			// An output that won't marshal is a tool-side fault; surface it as a
			// tool error rather than crashing the session.
			return &sdkmcp.CallToolResult{
				IsError: true,
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: marshalErr.Error()}},
			}, nil
		}
		return &sdkmcp.CallToolResult{
			StructuredContent: out,
			Content:           []sdkmcp.Content{&sdkmcp.TextContent{Text: string(text)}},
		}, nil
	}
}

// registerSkillResources registers one MCP resource per embedded skill, driven
// by skills.List so a newly added skill file appears automatically. Each
// resource is URI lattice-skill://<name>, MIME text/markdown, with the skill's
// frontmatter description as the resource description. This replicates the legacy
// internal/mcp/resources_skills.go registration exactly.
func registerSkillResources(server *sdkmcp.Server) {
	for _, meta := range skills.List() {
		server.AddResource(&sdkmcp.Resource{
			URI:         skillURIScheme + meta.Name,
			Name:        meta.Name,
			Description: meta.Description,
			MIMEType:    "text/markdown",
		}, makeSkillReader(meta.Name))
	}
}

// makeSkillReader builds the ResourceHandler for one skill resource: it returns
// the skill's raw markdown body via skills.Get, served verbatim (the same source
// get_skill returns). The name is captured per resource. A miss surfaces the
// SDK's standard resource-not-found error, as the legacy reader did.
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
