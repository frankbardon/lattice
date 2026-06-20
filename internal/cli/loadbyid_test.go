package cli

import (
	"bytes"
	"context"
	stderrors "errors"
	"os"
	"strings"
	"testing"

	"github.com/spf13/afero"
	cli "github.com/urfave/cli/v3"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/storage"
	"github.com/frankbardon/lattice/service"
)

// resolveByIDViaSvc drives the backend-addressed read through the public service
// facade, mirroring the CLI's by-id wiring (resolveByIDViaService): it wires the
// already-built store and a resolver over the on-disk schema catalog into a
// Service, then resolves the manifest id. It replaces the deleted
// resolveBytesByID cli helper while preserving its exact load->resolve behavior.
func resolveByIDViaSvc(t *testing.T, store storage.Store, schemasDir, id string, overrides map[string]any) (*service.ResolvedTree, error) {
	t.Helper()
	res, err := service.NewResolver(os.DirFS(schemasDir))
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	return service.New(store, res).Resolve(id, overrides)
}

// minimalExample is the shipped example used as a known-good document for the
// load-by-id tests; its manifest.id is the load key.
const (
	minimalExample = "../../examples/minimal-dashboard.json"
	minimalID      = "example-minimal"
)

// seedFSStore copies the minimal example into a fresh temp root through the FS
// backend (so it lands as <id>.json exactly as a real save would) and returns
// the store plus its root.
func seedFSStore(t *testing.T) (storage.Store, string) {
	t.Helper()
	doc, err := os.ReadFile(minimalExample)
	if err != nil {
		t.Fatalf("read example: %v", err)
	}
	root := t.TempDir()
	store, err := storage.New(storage.BackendFS, afero.NewOsFs(), root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := store.Save(doc); err != nil {
		t.Fatalf("save doc: %v", err)
	}
	return store, root
}

// TestResolveBytesByID proves a document saved through the FS backend resolves
// when addressed by its manifest.id (store.Load -> ResolveBytesWithValues).
func TestResolveBytesByID(t *testing.T) {
	store, _ := seedFSStore(t)

	tree, err := resolveByIDViaSvc(t, store, repoSchemasDir, minimalID, nil)
	if err != nil {
		t.Fatalf("resolve by id: %v", err)
	}
	if tree == nil || tree.Root == nil {
		t.Fatal("expected a resolved tree with a root")
	}
	if got := tree.Manifest["id"]; got != minimalID {
		t.Fatalf("resolved manifest id = %v, want %s", got, minimalID)
	}
}

// TestResolveBytesByID_NotFound proves an unknown id surfaces a clear
// STORAGE_NOT_FOUND coded error through the load-by-id path.
func TestResolveBytesByID_NotFound(t *testing.T) {
	store, _ := seedFSStore(t)

	_, err := resolveByIDViaSvc(t, store, repoSchemasDir, "no-such-dashboard", nil)
	if err == nil {
		t.Fatal("expected a not-found error, got nil")
	}
	var ce *errors.CodedError
	if !stderrors.As(err, &ce) {
		t.Fatalf("expected a CodedError, got %T: %v", err, err)
	}
	if ce.Code != errors.STORAGE_NOT_FOUND {
		t.Fatalf("error code = %s, want %s", ce.Code, errors.STORAGE_NOT_FOUND)
	}
}

// TestResolveCommand_LoadByID drives the actual `resolve` command with
// --store/--root set, proving the flag-driven branch treats the positional arg
// as a manifest.id and emits the resolved tree.
func TestResolveCommand_LoadByID(t *testing.T) {
	t.Setenv(secretEnvName, secretEnvValue)
	_, root := seedFSStore(t)

	var out, errOut bytes.Buffer
	cmd := ResolveCommand()
	cmd.Writer = &out
	cmd.ErrWriter = &errOut

	args := []string{"resolve", "--schemas", repoSchemasDir, "--store", "fs", "--root", root, minimalID}
	if err := cmd.Run(context.Background(), args); err != nil {
		t.Fatalf("resolve by id: %v (stderr=%s)", err, errOut.String())
	}
	if !strings.Contains(out.String(), minimalID) {
		t.Fatalf("expected resolved tree to mention id %q; got:\n%s", minimalID, out.String())
	}
}

// TestResolveCommand_LoadByID_NotFound proves an unknown id under a configured
// backend surfaces the STORAGE_NOT_FOUND coded error through the command's error
// reporting and exits non-zero.
func TestResolveCommand_LoadByID_NotFound(t *testing.T) {
	_, root := seedFSStore(t)

	// reportError returns cli.Exit("", 1), whose default handler calls os.Exit.
	// Stub the exiter so the command run returns rather than killing the test
	// binary, and assert on both the exit code and the rendered error.
	var code int
	prev := cli.OsExiter
	cli.OsExiter = func(c int) { code = c }
	t.Cleanup(func() { cli.OsExiter = prev })

	var out, errOut bytes.Buffer
	cmd := ResolveCommand()
	cmd.Writer = &out
	cmd.ErrWriter = &errOut

	// --json is a global app flag, not declared on the subcommand, so a standalone
	// command run reports the human-readable "error: CODE: message" form; assert
	// the code appears there.
	args := []string{"resolve", "--schemas", repoSchemasDir, "--store", "fs", "--root", root, "missing-id"}
	_ = cmd.Run(context.Background(), args)
	if code != 1 {
		t.Fatalf("expected exit code 1 for a missing id, got %d", code)
	}
	if !strings.Contains(errOut.String(), string(errors.STORAGE_NOT_FOUND)) {
		t.Fatalf("expected STORAGE_NOT_FOUND in error output; got:\n%s", errOut.String())
	}
}
