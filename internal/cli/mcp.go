package cli

import (
	"context"
	"fmt"
	"os"

	cli "github.com/urfave/cli/v3"

	"github.com/frankbardon/lattice/errors"
	lmcp "github.com/frankbardon/lattice/internal/mcp"
	"github.com/frankbardon/lattice/service"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// McpCommand returns the `mcp` subcommand.
//
// It runs lattice's MCP (Model Context Protocol) server over stdio. The server
// is assembled from the same --store/--root/--schemas seam serve/patch use: the
// store is the --store backend over the real filesystem rooted at --root, and
// the resolver reads the --schemas catalog. That single *service.Service is
// handed to the transport-agnostic registry (internal/mcp), which registers the
// tool set and returns a *mcp.Server; the command then serves it over a
// StdioTransport, blocking until the host disconnects or the CLI context is
// cancelled.
//
// Unlike serve, mcp always operates through a backend (its tools read documents
// by manifest id, not by filesystem path), mirroring the patch command's
// service construction. The MCP server is read + dry-run only — it never
// persists.
func McpCommand() *cli.Command {
	return &cli.Command{
		Name:  "mcp",
		Usage: "Run the lattice MCP server over stdio for an MCP host",
		Flags: append([]cli.Flag{
			&cli.StringFlag{
				Name:  "schemas",
				Usage: "Directory holding the dashboard schema and item-type catalog",
				Value: defaultSchemasDir,
			},
		}, storeFlags()...),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			asJSON := cmd.Bool("json")

			// Assemble the Service via Open exactly as patch does: the store is the
			// --store backend over the real filesystem rooted at --root, and the
			// resolver reads the --schemas catalog (os.DirFS rooted directly at the
			// schemas dir). MCP tools call only through this facade.
			svc, err := service.Open(service.Options{
				Backend: service.Backend(cmd.String("store")),
				Root:    cmd.String("root"),
				Schemas: os.DirFS(cmd.String("schemas")),
			})
			if err != nil {
				return reportError(cmd, asJSON, err)
			}

			srv := lmcp.NewServer(svc, cmd.Root().Version)

			fmt.Fprintf(cmd.Writer, "lattice MCP server running on stdio (Ctrl-C to stop)\n")

			// Run blocks until the host disconnects or ctx is cancelled (SIGINT wired
			// by urfave). A nil context cancellation returns nil; any other transport
			// failure surfaces as an MCP_INTERNAL coded error.
			if runErr := srv.Run(ctx, &sdkmcp.StdioTransport{}); runErr != nil {
				if ctx.Err() != nil {
					return nil
				}
				return reportError(cmd, asJSON, errors.WrapCodedError(runErr, errors.MCP_INTERNAL,
					"MCP stdio transport error"))
			}
			return nil
		},
	}
}
