package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cli "github.com/urfave/cli/v3"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/storage"
)

// writeChangeset writes body to a changeset.json file under a fresh temp dir and
// returns its path.
func writeChangeset(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "changeset.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write changeset: %v", err)
	}
	return p
}

// TestPatchCommand_AppliesAndPersists drives the actual `patch` command against a
// fixture document saved through the FS backend under a temp root, proving the
// changeset is applied, validated, and persisted (the reloaded document carries
// the edit).
func TestPatchCommand_AppliesAndPersists(t *testing.T) {
	store, root := seedFSStore(t)
	csPath := writeChangeset(t, `[{"op":"replace","path":"/$manifest/title","value":"Renamed"}]`)

	var out, errOut bytes.Buffer
	cmd := PatchCommand()
	cmd.Writer = &out
	cmd.ErrWriter = &errOut

	args := []string{
		"patch", "--schemas", repoSchemasDir, "--store", "fs", "--root", root,
		"--changeset", csPath, minimalID,
	}
	if err := cmd.Run(context.Background(), args); err != nil {
		t.Fatalf("patch: %v (stderr=%s)", err, errOut.String())
	}
	if !strings.Contains(out.String(), minimalID) {
		t.Fatalf("expected confirmation to mention id %q; got:\n%s", minimalID, out.String())
	}

	// The persisted document carries the edit.
	reloaded, err := store.Load(minimalID)
	if err != nil {
		t.Fatalf("reload patched doc: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(reloaded, &doc); err != nil {
		t.Fatalf("unmarshal reloaded doc: %v", err)
	}
	manifest, _ := doc["manifest"].(map[string]any)
	if got := manifest["title"]; got != "Renamed" {
		t.Fatalf("reloaded manifest title = %v, want Renamed", got)
	}
}

// TestPatchCommand_ReadsStdin proves --changeset - reads the changeset from the
// command's reader (stdin), applying and persisting the edit.
func TestPatchCommand_ReadsStdin(t *testing.T) {
	store, root := seedFSStore(t)

	var out, errOut bytes.Buffer
	cmd := PatchCommand()
	cmd.Writer = &out
	cmd.ErrWriter = &errOut
	cmd.Reader = strings.NewReader(`[{"op":"replace","path":"/$manifest/title","value":"FromStdin"}]`)

	args := []string{
		"patch", "--schemas", repoSchemasDir, "--store", "fs", "--root", root,
		"--changeset", "-", minimalID,
	}
	if err := cmd.Run(context.Background(), args); err != nil {
		t.Fatalf("patch via stdin: %v (stderr=%s)", err, errOut.String())
	}

	reloaded, err := store.Load(minimalID)
	if err != nil {
		t.Fatalf("reload patched doc: %v", err)
	}
	if !strings.Contains(string(reloaded), "FromStdin") {
		t.Fatalf("expected persisted doc to carry stdin edit; got:\n%s", reloaded)
	}
}

// TestPatchCommand_InvalidEditPersistsNothing proves a changeset that fails the
// pipeline (a wrong-typed value here) surfaces a coded error, exits non-zero, and
// leaves the stored document byte-for-byte unchanged.
func TestPatchCommand_InvalidEditPersistsNothing(t *testing.T) {
	store, root := seedFSStore(t)
	before, err := store.Load(minimalID)
	if err != nil {
		t.Fatalf("load before: %v", err)
	}
	csPath := writeChangeset(t, `[{"op":"replace","path":"/$manifest/title","value":42}]`)

	var code int
	prev := cli.OsExiter
	cli.OsExiter = func(c int) { code = c }
	t.Cleanup(func() { cli.OsExiter = prev })

	var out, errOut bytes.Buffer
	cmd := PatchCommand()
	cmd.Writer = &out
	cmd.ErrWriter = &errOut

	args := []string{
		"patch", "--schemas", repoSchemasDir, "--store", "fs", "--root", root,
		"--changeset", csPath, minimalID,
	}
	_ = cmd.Run(context.Background(), args)
	if code != 1 {
		t.Fatalf("expected exit code 1 for an invalid edit, got %d", code)
	}

	after, err := store.Load(minimalID)
	if err != nil {
		t.Fatalf("load after: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("expected stored document unchanged after a rejected patch")
	}
}

// TestPatchCommand_ExpectRevisionMatchApplies proves --expect-revision wired to the
// optimistic-concurrency precondition: when the supplied token matches the stored
// document's current revision, the patch applies and persists normally.
func TestPatchCommand_ExpectRevisionMatchApplies(t *testing.T) {
	store, root := seedFSStore(t)
	current, err := store.(storage.RevisionedStore).Revision(minimalID)
	if err != nil {
		t.Fatalf("read current revision: %v", err)
	}
	csPath := writeChangeset(t, `[{"op":"replace","path":"/$manifest/title","value":"Renamed"}]`)

	var out, errOut bytes.Buffer
	cmd := PatchCommand()
	cmd.Writer = &out
	cmd.ErrWriter = &errOut

	args := []string{
		"patch", "--schemas", repoSchemasDir, "--store", "fs", "--root", root,
		"--changeset", csPath, "--expect-revision", current, minimalID,
	}
	if err := cmd.Run(context.Background(), args); err != nil {
		t.Fatalf("patch with matching --expect-revision: %v (stderr=%s)", err, errOut.String())
	}

	reloaded, err := store.Load(minimalID)
	if err != nil {
		t.Fatalf("reload patched doc: %v", err)
	}
	if !strings.Contains(string(reloaded), "Renamed") {
		t.Fatalf("expected matching-revision patch to persist the edit; got:\n%s", reloaded)
	}
}

// TestPatchCommand_ExpectRevisionStaleConflicts proves a stale --expect-revision
// token rejects the patch with CHANGESET_REVISION_CONFLICT, exits non-zero, and
// leaves the stored document unchanged.
func TestPatchCommand_ExpectRevisionStaleConflicts(t *testing.T) {
	store, root := seedFSStore(t)
	before, err := store.Load(minimalID)
	if err != nil {
		t.Fatalf("load before: %v", err)
	}
	csPath := writeChangeset(t, `[{"op":"replace","path":"/$manifest/title","value":"Renamed"}]`)

	var code int
	prev := cli.OsExiter
	cli.OsExiter = func(c int) { code = c }
	t.Cleanup(func() { cli.OsExiter = prev })

	var out, errOut bytes.Buffer
	cmd := PatchCommand()
	cmd.Writer = &out
	cmd.ErrWriter = &errOut

	args := []string{
		"patch", "--schemas", repoSchemasDir, "--store", "fs", "--root", root,
		"--changeset", csPath, "--expect-revision", "stale-token", minimalID,
	}
	_ = cmd.Run(context.Background(), args)
	if code != 1 {
		t.Fatalf("expected exit code 1 for a stale revision, got %d", code)
	}
	if !strings.Contains(errOut.String(), string(errors.CHANGESET_REVISION_CONFLICT)) {
		t.Fatalf("expected CHANGESET_REVISION_CONFLICT in error output; got:\n%s", errOut.String())
	}

	after, err := store.Load(minimalID)
	if err != nil {
		t.Fatalf("load after: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("expected stored document unchanged after a conflicted patch")
	}
}

// TestPatchCommand_MissingChangeset proves an absent --changeset flag is a
// PATCH_INVALID coded error reported through the command's error path.
func TestPatchCommand_MissingChangeset(t *testing.T) {
	_, root := seedFSStore(t)

	var code int
	prev := cli.OsExiter
	cli.OsExiter = func(c int) { code = c }
	t.Cleanup(func() { cli.OsExiter = prev })

	var out, errOut bytes.Buffer
	cmd := PatchCommand()
	cmd.Writer = &out
	cmd.ErrWriter = &errOut

	args := []string{"patch", "--schemas", repoSchemasDir, "--store", "fs", "--root", root, minimalID}
	_ = cmd.Run(context.Background(), args)
	if code != 1 {
		t.Fatalf("expected exit code 1 for a missing changeset, got %d", code)
	}
	if !strings.Contains(errOut.String(), string(errors.PATCH_INVALID)) {
		t.Fatalf("expected PATCH_INVALID in error output; got:\n%s", errOut.String())
	}
}
