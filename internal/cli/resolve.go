package cli

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spf13/afero"
	cli "github.com/urfave/cli/v3"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/resolver"
	"github.com/frankbardon/lattice/internal/storage"
	"github.com/frankbardon/lattice/internal/variables"
)

// defaultSchemasDir is the catalog directory scanned for the dashboard schema
// and item-type schemas when --schemas is not supplied. It is relative to the
// process working directory (config is via flags, not config files).
const defaultSchemasDir = "schemas"

// dashboardSchemaFile is the dashboard document schema's filename within the
// schemas directory; it is loaded for the structural (Pass 1) validation.
const dashboardSchemaFile = "dashboard.schema.json"

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

// newStore constructs the storage backend selected by the --store/--root flags
// over the real filesystem. Backend construction lives in the storage factory;
// this is the thin CLI adapter that reads the flags and reports an unknown
// backend as a coded error. The constructed store is not yet on the load path
// (E3-S2); this wires selection so the chosen backend is constructible.
func newStore(cmd *cli.Command) (storage.Store, error) {
	return storage.New(storage.Backend(cmd.String("store")), afero.NewOsFs(), cmd.String("root"))
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
			docPath := cmd.Args().First()
			if docPath == "" {
				return reportError(cmd, asJSON, errors.NewCodedError(errors.RESOLVE_INVALID,
					"resolve requires a dashboard document path argument"))
			}

			// Construct the selected backend so an unknown --store fails fast
			// with a coded error. The backend is not yet on the load path
			// (E3-S2 reroutes loading); existing direct-path resolution is
			// unchanged below.
			if _, err := newStore(cmd); err != nil {
				return reportError(cmd, asJSON, err)
			}

			tree, err := runResolve(cmd.String("schemas"), docPath)
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

// runResolve wires the resolver: it loads the dashboard schema from schemasDir,
// builds a resolver over the on-disk catalog, and resolves the document.
func runResolve(schemasDir, docPath string) (*resolver.ResolvedTree, error) {
	return runResolveWithValues(schemasDir, docPath, nil)
}

// runResolveWithValues is runResolve with E4-S1 runtime overrides applied: an
// addressable override set keyed by address (a bare name targets a settable
// variable as in E3-S4; a "<node-id>.<field>" address targets a node config
// field, carried for E4-S2). A nil/empty overrides map is identical to
// runResolve.
func runResolveWithValues(schemasDir, docPath string, overrides map[string]any) (*resolver.ResolvedTree, error) {
	fs := afero.NewOsFs()

	dashSch, err := loadDashboardSchema(fs, schemasDir)
	if err != nil {
		return nil, err
	}

	res, err := resolver.New(fs, dashSch, []string{schemasDir})
	if err != nil {
		return nil, err
	}
	return res.ResolveWithValues(docPath, variables.OverrideSet(overrides))
}

// loadDashboardSchema reads and parses the dashboard document schema from
// schemasDir/dashboard.schema.json.
func loadDashboardSchema(fs afero.Fs, schemasDir string) (*jsonschema.Schema, error) {
	p := schemasDir + "/" + dashboardSchemaFile
	data, err := afero.ReadFile(fs, p)
	if err != nil {
		return nil, errors.WrapCodedError(err, errors.SCHEMA_IO, "failed reading dashboard schema "+p)
	}
	var s jsonschema.Schema
	if err := s.UnmarshalJSON(data); err != nil {
		return nil, errors.WrapCodedError(err, errors.SCHEMA_INVALID, "failed parsing dashboard schema "+p)
	}
	return &s, nil
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
