package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/afero"
	cli "github.com/urfave/cli/v3"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/changeset"
)

// PatchCommand returns the `patch` subcommand.
//
// It edits a stored dashboard end to end: it loads the document addressed by the
// positional manifest id through the configured backend (--store/--root, the same
// seam resolve/serve use), parses the --changeset file (an RFC 6902 / id-rooted
// JSON Patch), and runs the patch-write pipeline (changeset.ApplyChangeset). The
// pipeline is ATOMIC — it applies under the field-edit guardrail, re-resolves the
// mutated document for the structural/schema/referential guardrails, and persists
// the validated bytes back to the store only when every check passes (for the git
// backend, the Save is a commit). On any coded error nothing is persisted and the
// command exits non-zero, reporting the error through the shared CLI error path
// (a JSON envelope under the global --json flag).
//
// Unlike resolve/serve, patch always operates through a backend: the id argument
// is a manifest id, not a filesystem path. --store defaults to fs and --root to
// the working directory.
func PatchCommand() *cli.Command {
	return &cli.Command{
		Name:      "patch",
		Usage:     "Apply a changeset to a stored dashboard document and persist the result",
		ArgsUsage: "<id>",
		Flags: append([]cli.Flag{
			&cli.StringFlag{
				Name:  "schemas",
				Usage: "Directory holding the dashboard schema and item-type catalog",
				Value: defaultSchemasDir,
			},
			&cli.StringFlag{
				Name:  "changeset",
				Usage: "Path to the changeset file (an id-rooted JSON Patch array); - reads stdin",
			},
			&cli.StringFlag{
				Name:  "expect-revision",
				Usage: "Optimistic-concurrency precondition: an opaque revision token the stored document must still be at, re-checked just before write; omit for no precondition",
			},
		}, storeFlags()...),
		Action: func(_ context.Context, cmd *cli.Command) error {
			asJSON := cmd.Bool("json")

			id := cmd.Args().First()
			if id == "" {
				return reportError(cmd, asJSON, errors.NewCodedError(errors.PATCH_INVALID,
					"patch requires a dashboard manifest id argument"))
			}

			csPath := cmd.String("changeset")
			if csPath == "" {
				return reportError(cmd, asJSON, errors.NewCodedError(errors.PATCH_INVALID,
					"patch requires a --changeset file (use - for stdin)"))
			}

			csBytes, err := readChangeset(cmd, csPath)
			if err != nil {
				return reportError(cmd, asJSON, err)
			}

			cs, err := changeset.Parse(csBytes)
			if err != nil {
				return reportError(cmd, asJSON, err)
			}

			// Construct the backend and the resolver exactly as the load-by-id resolve
			// path does: the id is a manifest id, the store is built from --store/--root,
			// and *resolver.Resolver satisfies the pipeline's DocumentResolver.
			store, err := newStore(cmd)
			if err != nil {
				return reportError(cmd, asJSON, err)
			}
			res, err := newResolver(cmd.String("schemas"))
			if err != nil {
				return reportError(cmd, asJSON, err)
			}

			// The pipeline is atomic: load, resolve, apply, re-resolve, and persist on
			// full success only. Any coded error rejects the apply and persists nothing.
			// When --expect-revision is supplied, the pipeline enforces it as an
			// optimistic-concurrency precondition re-checked immediately before write
			// (a mismatch rejects with CHANGESET_REVISION_CONFLICT, nothing persisted).
			var applyOpts []changeset.ApplyOption
			if cmd.IsSet("expect-revision") {
				applyOpts = append(applyOpts, changeset.WithExpectedRevision(cmd.String("expect-revision")))
			}
			if _, err := changeset.ApplyChangeset(store, res, id, cs, applyOpts...); err != nil {
				return reportError(cmd, asJSON, err)
			}

			fmt.Fprintf(cmd.Writer, "patched %s via %s store (changeset applied and persisted)\n",
				id, cmd.String("store"))
			return nil
		},
	}
}

// readChangeset reads the changeset bytes from csPath. The special path "-" reads
// the whole of stdin (cmd.Reader, defaulting to os.Stdin) so a changeset can be
// piped in; any other path is read from the real filesystem. A read failure is a
// PATCH_INVALID coded error naming the source.
func readChangeset(cmd *cli.Command, csPath string) ([]byte, error) {
	if csPath == "-" {
		r := cmd.Reader
		if r == nil {
			r = os.Stdin
		}
		data, err := io.ReadAll(r)
		if err != nil {
			return nil, errors.WrapCodedError(err, errors.PATCH_INVALID,
				"failed reading changeset from stdin")
		}
		return data, nil
	}

	data, err := afero.ReadFile(afero.NewOsFs(), csPath)
	if err != nil {
		return nil, errors.WrapCodedError(err, errors.PATCH_INVALID,
			"failed reading changeset file "+csPath)
	}
	return data, nil
}
