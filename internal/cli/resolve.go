package cli

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"os"

	"github.com/spf13/afero"
	cli "github.com/urfave/cli/v3"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/resolver"
	"github.com/frankbardon/lattice/internal/storage"
	"github.com/frankbardon/lattice/service"
)

// defaultSchemasDir is the catalog directory scanned for the dashboard schema
// and item-type schemas when --schemas is not supplied. It is relative to the
// process working directory (config is via flags, not config files).
const defaultSchemasDir = "schemas"

// defaultStore is the storage backend selected when --store is not supplied. It
// matches the existing direct-path behavior: a filesystem backend.
const defaultStore = string(storage.BackendFS)

// defaultStoreRoot is the storage root used when --root is not supplied. It is
// the process working directory (config is via flags, not config files).
const defaultStoreRoot = "."

// storeFlags returns the --store/--root flags shared by the resolve and serve
// commands so backend selection is declared in one place.
func storeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  "store",
			Usage: "Storage backend to construct: fs or git",
			Value: defaultStore,
		},
		&cli.StringFlag{
			Name:  "root",
			Usage: "Root directory for the storage backend",
			Value: defaultStoreRoot,
		},
	}
}

// backendConfigured reports whether the user explicitly selected a storage
// backend by setting --store or --root. When true, the positional argument is a
// manifest.id loaded through the backend; when false, it is a filesystem path
// resolved directly (the pre-existing, default behavior that keeps every
// `resolve <path>` invocation, example, and golden test working unchanged).
func backendConfigured(cmd *cli.Command) bool {
	return cmd.IsSet("store") || cmd.IsSet("root")
}

// ResolveCommand returns the `resolve` subcommand.
//
// It runs the two-pass resolver against the given document and prints the
// resolved-tree JSON on success. On failure it prints the first CodedError and
// exits non-zero; with --json the error is emitted as a machine-readable JSON
// envelope ({code,message,details}) on stderr.
func ResolveCommand() *cli.Command {
	return &cli.Command{
		Name:      "resolve",
		Usage:     "Resolve and validate a dashboard document, emitting the resolved tree",
		ArgsUsage: "<document>",
		Flags: append([]cli.Flag{
			&cli.StringFlag{
				Name:  "schemas",
				Usage: "Directory holding the dashboard schema and item-type catalog",
				Value: defaultSchemasDir,
			},
		}, storeFlags()...),
		Action: func(_ context.Context, cmd *cli.Command) error {
			asJSON := cmd.Bool("json")
			arg := cmd.Args().First()
			if arg == "" {
				return reportError(cmd, asJSON, errors.NewCodedError(errors.RESOLVE_INVALID,
					"resolve requires a dashboard document path or manifest id argument"))
			}

			schemasDir := cmd.String("schemas")

			var tree *resolver.ResolvedTree
			var err error
			if backendConfigured(cmd) {
				// The user selected a backend (--store/--root): the argument is a
				// manifest.id loaded through the store, then resolved from its bytes.
				// service.Open assembles the same store (backend over OsFs rooted at
				// --root) and resolver (schema catalog rooted at --schemas) the
				// inline wiring did; Resolve runs the identical load→two-pass pipeline.
				tree, err = resolveByIDViaService(cmd, schemasDir, arg, nil)
			} else {
				// Default, path-friendly behavior: the argument is a filesystem path
				// resolved directly. No backend is constructed.
				tree, err = resolvePathViaService(schemasDir, arg, nil)
			}
			if err != nil {
				return reportError(cmd, asJSON, err)
			}

			out, err := json.MarshalIndent(tree, "", "  ")
			if err != nil {
				return reportError(cmd, asJSON, errors.WrapCodedError(err, errors.RESOLVE_INTERNAL,
					"failed encoding resolved tree"))
			}
			fmt.Fprintln(cmd.Writer, string(out))
			return nil
		},
	}
}

// resolveByIDViaService is the by-id resolve path routed through the service
// facade. It assembles a Service via service.Open — a store over the real
// filesystem (the --store backend rooted at --root) and a resolver over the
// --schemas catalog (os.DirFS rooted directly at the schemas dir, matching the
// facade's schemasRoot assumption) — then resolves the manifest id. Open's store
// is built lazily here, only once the argument is known to be an id, preserving
// the no-backend-on-path-mode property. A not-found id surfaces as the store's
// STORAGE_NOT_FOUND coded error; schema/backend construction failures surface as
// the facade's SCHEMA_*/storage coded errors. Behavior matches the prior inline
// store-construction + ResolveBytesWithValues wiring.
func resolveByIDViaService(cmd *cli.Command, schemasDir, id string, overrides map[string]any) (*resolver.ResolvedTree, error) {
	svc, err := service.Open(service.Options{
		Backend: service.Backend(cmd.String("store")),
		Root:    cmd.String("root"),
		Schemas: os.DirFS(schemasDir),
	})
	if err != nil {
		return nil, err
	}
	return svc.Resolve(id, overrides)
}

// resolvePathViaService is the default path-mode resolve routed through the
// service facade. The facade has no path-mode method (its resolver reads schemas
// from the supplied fs.FS, not the document), so the adapter owns the document
// I/O: it reads the file off the real filesystem with the same RESOLVE_IO error
// wrapping the resolver's path read used, then feeds the bytes to
// Service.ResolveBytes with docPath as the source label. ResolveBytes never
// touches the store, so the Service is wired with a nil store via service.New
// over a resolver built from the --schemas catalog. This reproduces the prior
// resolver.ResolveWithValues(docPath, ...) behavior byte-for-byte.
func resolvePathViaService(schemasDir, docPath string, overrides map[string]any) (*resolver.ResolvedTree, error) {
	res, err := service.NewResolver(os.DirFS(schemasDir))
	if err != nil {
		return nil, err
	}

	data, err := afero.ReadFile(afero.NewOsFs(), docPath)
	if err != nil {
		return nil, errors.WrapCodedError(err, errors.RESOLVE_IO, "failed reading dashboard document "+docPath)
	}

	svc := service.New(nil, res)
	return svc.ResolveBytes(data, docPath, overrides)
}

// reportError prints err and returns a non-nil error so the CLI exits non-zero.
// With asJSON, a CodedError is emitted as its JSON envelope; otherwise the
// "CODE: message" form is printed. The returned error is silenced from the
// default urfave handler (which would double-print) via cli.Exit with code 1 and
// an empty message.
func reportError(cmd *cli.Command, asJSON bool, err error) error {
	if asJSON {
		var ce *errors.CodedError
		if stderrors.As(err, &ce) {
			b, mErr := json.MarshalIndent(ce, "", "  ")
			if mErr == nil {
				fmt.Fprintln(cmd.ErrWriter, string(b))
				return cli.Exit("", 1)
			}
		}
		// Fallback: wrap non-coded errors so --json still yields an envelope.
		b, _ := json.MarshalIndent(errors.WrapCodedError(err, errors.RESOLVE_INTERNAL, err.Error()), "", "  ")
		fmt.Fprintln(cmd.ErrWriter, string(b))
		return cli.Exit("", 1)
	}
	fmt.Fprintln(cmd.ErrWriter, "error: "+err.Error())
	return cli.Exit("", 1)
}
