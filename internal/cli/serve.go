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
	"github.com/frankbardon/lattice/internal/storage"
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
		Flags: append([]cli.Flag{
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
		}, storeFlags()...),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			asJSON := cmd.Bool("json")

			arg := cmd.Args().First()
			if arg == "" {
				return reportError(cmd, asJSON, errors.NewCodedError(errors.SERVE_INVALID,
					"serve requires a dashboard document path or manifest id argument"))
			}

			// When the user selected a backend (--store/--root), the argument is a
			// manifest.id served through the store; otherwise it is a filesystem
			// path served directly (the pre-existing default). The store is
			// constructed once here, lazily and only in backend mode, so a plain
			// path-mode serve never incurs a backend side-effect (e.g. git init).
			var store storage.Store
			if backendConfigured(cmd) {
				var err error
				if store, err = newStore(cmd); err != nil {
					return reportError(cmd, asJSON, err)
				}
			}

			port := cmd.Int("port")
			if port < 1 || port > 65535 {
				return reportError(cmd, asJSON, errors.NewCodedErrorWithDetails(errors.SERVE_INVALID,
					"--port must be in the range 1-65535", map[string]any{"port": port}))
			}

			schemasDir := cmd.String("schemas")
			// Re-resolve on every request so a document edit is reflected on
			// reload and resolution errors render as the HTML error page. The
			// per-request override map is the UNIFIED override set (E4): a bare
			// key names a variable (widget selection / URL query param), a
			// "<node-id>.<field>" key names a node config field. Both kinds flow
			// straight into the addressable OverrideSet, so serve routes both to
			// the resolver without distinguishing them.
			//
			// In backend mode the store is re-read per request too (Load(arg) then
			// resolve the bytes), so editing the stored document is reflected on
			// reload exactly as a path-mode edit is; the render stays read-only.
			resolve := func(overrides map[string]any) (*resolver.ResolvedTree, error) {
				if store != nil {
					return resolveBytesByID(store, schemasDir, arg, overrides)
				}
				return runResolveWithValues(schemasDir, arg, overrides)
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

			fmt.Fprintf(cmd.Writer, "lattice serving %s on http://localhost:%d (Ctrl-C to stop)\n", arg, port)

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
