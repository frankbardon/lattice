package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"

	"github.com/spf13/afero"
	cli "github.com/urfave/cli/v3"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/resolver"
	"github.com/frankbardon/lattice/internal/server"
	"github.com/frankbardon/lattice/service"
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

			port := cmd.Int("port")
			if port < 1 || port > 65535 {
				return reportError(cmd, asJSON, errors.NewCodedErrorWithDetails(errors.SERVE_INVALID,
					"--port must be in the range 1-65535", map[string]any{"port": port}))
			}

			schemasDir := cmd.String("schemas")

			// Build the service facade ONCE and reuse it across every request
			// (the intended pattern: schema catalog + store are loaded a single
			// time, then each request re-resolves through the same Service). The
			// ResolveFunc closure below is what the server package injects.
			//
			// Two modes, mirroring the resolve command's service adapters:
			//
			//   - Backend mode (--store/--root set): the argument is a manifest.id
			//     loaded through the store. service.Open assembles the store
			//     (backend over OsFs rooted at --root) and the resolver (catalog
			//     rooted at --schemas), then svc.Resolve(id, overrides) re-reads
			//     the stored document and resolves it on every request — so an edit
			//     to the stored document is reflected on reload, render read-only.
			//
			//   - Path mode (default): the argument is a filesystem path. The
			//     facade has no path-mode method, so the closure owns the document
			//     I/O — it reads the file off the real filesystem (same RESOLVE_IO
			//     wrap the resolver's path read uses) on every request, then feeds
			//     the bytes to svc.ResolveBytes (which never touches the store, so
			//     the Service is wired with a nil store). Re-reading per request
			//     keeps the reflect-on-reload behavior of the prior path wiring.
			//
			// Re-resolving per request also means resolution errors render as the
			// HTML error page / 422 envelope rather than crashing the server. The
			// per-request override map is the UNIFIED override set (E4): a bare key
			// names a variable (widget selection / URL query param), a
			// "<node-id>.<field>" key names a node config field. Both kinds flow
			// straight into the addressable OverrideSet, so serve routes both to
			// the resolver without distinguishing them.
			var resolve server.ResolveFunc
			if backendConfigured(cmd) {
				svc, err := service.Open(service.Options{
					Backend: service.Backend(cmd.String("store")),
					Root:    cmd.String("root"),
					Schemas: os.DirFS(schemasDir),
				})
				if err != nil {
					return reportError(cmd, asJSON, err)
				}
				resolve = func(overrides map[string]any) (*resolver.ResolvedTree, error) {
					return svc.Resolve(arg, overrides)
				}
			} else {
				res, err := service.NewResolver(os.DirFS(schemasDir))
				if err != nil {
					return reportError(cmd, asJSON, err)
				}
				svc := service.New(nil, res)
				resolve = func(overrides map[string]any) (*resolver.ResolvedTree, error) {
					data, err := afero.ReadFile(afero.NewOsFs(), arg)
					if err != nil {
						return nil, errors.WrapCodedError(err, errors.RESOLVE_IO, "failed reading dashboard document "+arg)
					}
					return svc.ResolveBytes(data, arg, overrides)
				}
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
