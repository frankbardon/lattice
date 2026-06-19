package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"

	cli "github.com/urfave/cli/v3"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/resolver"
	"github.com/frankbardon/lattice/internal/server"
)

// defaultServePort is the listen port used when --port is not supplied.
const defaultServePort = 8080

// ServeCommand returns the `serve` subcommand.
//
// It starts a net/http server that loads and resolves the given dashboard
// document via the E1 resolver and serves an HTML page wired with AlpineJS plus
// a JSON endpoint exposing the resolved tree. The document is re-resolved on
// every request, so resolution errors surface as an HTML error page (a rendered
// CodedError) rather than crashing the server. This story (E2-S2) delivers the
// plumbing and a placeholder page; the real sketch view is E2-S3.
func ServeCommand() *cli.Command {
	return &cli.Command{
		Name:      "serve",
		Usage:     "Serve a dashboard document as an HTML structural sketch",
		ArgsUsage: "<document>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "schemas",
				Usage: "Directory holding the dashboard schema and item-type catalog",
				Value: defaultSchemasDir,
			},
			&cli.IntFlag{
				Name:  "port",
				Usage: "TCP port to listen on",
				Value: defaultServePort,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			asJSON := cmd.Bool("json")

			docPath := cmd.Args().First()
			if docPath == "" {
				return reportError(cmd, asJSON, errors.NewCodedError(errors.SERVE_INVALID,
					"serve requires a dashboard document path argument"))
			}

			port := cmd.Int("port")
			if port < 1 || port > 65535 {
				return reportError(cmd, asJSON, errors.NewCodedErrorWithDetails(errors.SERVE_INVALID,
					"--port must be in the range 1-65535", map[string]any{"port": port}))
			}

			schemasDir := cmd.String("schemas")
			// Re-resolve on every request so a document edit is reflected on
			// reload and resolution errors render as the HTML error page.
			resolve := func() (*resolver.ResolvedTree, error) {
				return runResolve(schemasDir, docPath)
			}

			srv, err := server.New(resolve)
			if err != nil {
				return reportError(cmd, asJSON, err)
			}

			addr := net.JoinHostPort("", strconv.Itoa(int(port)))
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return reportError(cmd, asJSON, errors.WrapCodedError(err, errors.SERVE_INTERNAL,
					"failed to listen on "+addr))
			}

			fmt.Fprintf(cmd.Writer, "lattice serving %s on http://localhost:%d (Ctrl-C to stop)\n", docPath, port)

			httpSrv := &http.Server{Handler: srv.Handler()}
			// Shut down gracefully when the CLI context is cancelled (e.g. SIGINT
			// wired by urfave) so Serve returns instead of blocking forever.
			go func() {
				<-ctx.Done()
				_ = httpSrv.Close()
			}()

			if serveErr := httpSrv.Serve(ln); serveErr != nil && serveErr != http.ErrServerClosed {
				return reportError(cmd, asJSON, errors.WrapCodedError(serveErr, errors.SERVE_INTERNAL,
					"http server error"))
			}
			return nil
		},
	}
}
