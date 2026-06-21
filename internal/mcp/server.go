// Package mcp is lattice's MCP (Model Context Protocol) layer: a
// transport-agnostic registry that builds a *mcp.Server exposing lattice's
// read/dry-run capabilities as MCP tools.
//
// Boundary rule (non-negotiable): this package builds ONLY on the public
// ./service facade (and the module-root errors package for coded errors). It
// must never import an internal/* core directly — the facade is the stable
// surface every tool calls through.
//
// The package is deliberately transport-agnostic. NewServer assembles the
// *mcp.Server and registers the full tool set; the caller chooses how to run it
// (stdio via server.Run(ctx, &mcp.StdioTransport{}) today, a Streamable HTTP
// handler later) without touching tool code.
package mcp

import (
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/frankbardon/lattice/service"
)

// serverName is the MCP implementation name advertised in the initialize
// handshake. Hosts use it to identify the server.
const serverName = "lattice"

// registrar appends one tool's registration to the given *mcp.Server. Every
// lattice MCP tool is expressed as a registrar bound to the shared
// *service.Service, which keeps tool wiring on the facade alone.
//
// This is the SINGLE registration seam: later stories add a tool by writing a
// registrar (typically a thin closure over mcp.AddTool[In,Out]) and listing it
// in registrars below. Because a registrar is transport-agnostic, adding an
// HTTP transport never touches tool code.
type registrar func(s *sdkmcp.Server, svc *service.Service)

// registrars is the ordered list of tool registrations applied to every server
// NewServer builds. It is the one place tools are enumerated.
//
// It is intentionally EMPTY in this scaffolding story (E1-S1). Subsequent
// stories (E1-S2, E2-*, E3-S2) append their tool's registrar here; nothing else
// in this package changes.
var registrars = []registrar{}

// NewServer constructs the lattice MCP server over the given service facade and
// registers the full tool set through the single registration seam (registrars).
//
// The returned *mcp.Server is transport-agnostic: run it over stdio with
// server.Run(ctx, &mcp.StdioTransport{}) or mount it behind a Streamable HTTP
// handler — the registered tools are identical either way.
//
// version is the lattice build version, advertised to MCP hosts in the
// initialize handshake.
func NewServer(svc *service.Service, version string) *sdkmcp.Server {
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    serverName,
		Version: version,
	}, nil)

	for _, register := range registrars {
		register(srv, svc)
	}

	return srv
}
