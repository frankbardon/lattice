package changeset

// This file is the END-TO-END slice for Epic 1 (story E1-S5): it drives the
// PUBLIC apply pipeline — changeset.ApplyChangeset — against REAL storage
// backends, proving the field-edit apply→validate→persist loop and its guardrail
// as a human running `go test` would observe them. The engine-level
// (applyToBytes), translator, and parser cases live in apply_test.go /
// translate_test.go / changeset_test.go; this file deliberately exercises only
// the public entry point over a Store, asserting the persisted result and reload,
// so the criteria are covered once, end to end, without re-testing the internals.
//
// Backends: the FS path is hermetic via afero.NewMemMapFs(); one case drives the
// git backend (afero.NewOsFs() + t.TempDir(), which go-git requires for a real
// worktree) and asserts a commit was recorded.

import (
	"bytes"
	"testing"

	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/storage"
)

// themedDocPath is a shipped fixture carrying an explicit document `theme` block
// (emphasis/spacing/tone), so a `$theme` token edit can be persisted and the
// reloaded bytes inspected for the changed value.
const themedDocPath = "../../examples/themed-dashboard.json"

const themedID = "example-themed"

// canonicalSeed reads a shipped fixture and returns it in canonical form — the
// exact bytes a successful apply would have written — so a byte-identity
// assertion after a rejected apply compares like with like and a no-op apply
// round-trips to the same bytes.
func canonicalSeed(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := afero.ReadFile(afero.NewOsFs(), path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	canonical, err := canonicalize(raw)
	if err != nil {
		t.Fatalf("canonicalize %s: %v", path, err)
	}
	return canonical
}

// seedFSStoreWith builds a hermetic MemMapFs-backed store holding the canonical
// form of the named fixture, returning the store and the seed bytes.
func seedFSStoreWith(t *testing.T, path string) (storage.Store, []byte) {
	t.Helper()
	seed := canonicalSeed(t, path)
	store := storage.NewFS(afero.NewMemMapFs(), ".")
	if err := store.Save(seed); err != nil {
		t.Fatalf("seed fs store: %v", err)
	}
	return store, seed
}

// TestE2E_FieldEditPersistsAndReloads proves the headline criterion: a `replace`
// on a SURFACED item config field (addressed by id) is persisted, and a reload
// reflects the new value. The fruits table is block-wrapped; its title is on the
// item surface and is addressed by the stable id `fruits`.
func TestE2E_FieldEditPersistsAndReloads(t *testing.T) {
	res := newResolver(t)
	store, seed := seedFSStoreWith(t, minimalDocPath)

	cs := parse(t, `[{"op":"replace","path":"/fruits/config/title","value":"Citrus"}]`)
	result, err := ApplyChangeset(store, res, fixtureID, cs)
	if err != nil {
		t.Fatalf("ApplyChangeset: %v", err)
	}

	// The store now holds the returned bytes, and they differ from the seed.
	reloaded, err := store.Load(fixtureID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !bytes.Equal(reloaded, result.Document) {
		t.Fatalf("stored bytes differ from the returned document")
	}
	if bytes.Equal(reloaded, seed) {
		t.Fatalf("store still holds the seed; the edit was not persisted")
	}

	// The reloaded document carries the new title at the fruits table's physical
	// location (root → body → fruits-block → block content), and the old value is
	// gone.
	if !bytes.Contains(reloaded, []byte(`"Citrus"`)) {
		t.Fatalf("reloaded document missing the new title, got:\n%s", reloaded)
	}
	if bytes.Contains(reloaded, []byte(`"Fruits"`)) {
		t.Fatalf("old fruits title should be gone, got:\n%s", reloaded)
	}
}

// TestE2E_NestedFieldEditPersistsAndReloads proves a NESTED-config edit (E2-S2)
// flows through the full public pipeline: a `replace` on the body container's
// declared `grid.gap` nested surface entry (addressed by id at
// `/body/config/grid/gap`) is persisted, and a reload reflects the new value with
// the sibling tracks intact.
func TestE2E_NestedFieldEditPersistsAndReloads(t *testing.T) {
	res := newResolver(t)
	store, _ := seedFSStoreWith(t, minimalDocPath)

	cs := parse(t, `[{"op":"replace","path":"/body/config/grid/gap","value":3}]`)
	if _, err := ApplyChangeset(store, res, fixtureID, cs); err != nil {
		t.Fatalf("ApplyChangeset: %v", err)
	}

	reloaded, err := store.Load(fixtureID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	children := readField(t, reloaded, "root", "children").([]any)
	grid := children[0].(map[string]any)["config"].(map[string]any)["grid"].(map[string]any)
	if grid["gap"] != float64(3) {
		t.Fatalf("reloaded body grid.gap = %v, want 3", grid["gap"])
	}
	if _, ok := grid["columns"]; !ok {
		t.Fatalf("nested edit dropped sibling grid.columns: %v", grid)
	}
}

// TestE2E_ThemeAndManifestScopeEdits proves a `$theme` token edit and a
// `$manifest` title edit are persisted within their document-scope surfaces, and
// a reload reflects both new values. Uses the themed fixture so the theme tokens
// have explicit on-disk values to overwrite.
func TestE2E_ThemeAndManifestScopeEdits(t *testing.T) {
	res := newResolver(t)
	store, _ := seedFSStoreWith(t, themedDocPath)

	cs := parse(t, `[
		{"op":"replace","path":"/$theme/emphasis","value":"high"},
		{"op":"replace","path":"/$manifest/title","value":"Retuned"}
	]`)
	if _, err := ApplyChangeset(store, res, themedID, cs); err != nil {
		t.Fatalf("ApplyChangeset: %v", err)
	}

	reloaded, err := store.Load(themedID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	// $theme/emphasis lands at /theme/emphasis; the seed value was "low".
	if got := readField(t, reloaded, "theme", "emphasis"); got != "high" {
		t.Fatalf("theme emphasis = %v, want high", got)
	}
	// $manifest/title lands at /manifest/title.
	if got := readField(t, reloaded, "manifest", "title"); got != "Retuned" {
		t.Fatalf("manifest title = %v, want Retuned", got)
	}
}

// TestE2E_RejectedEditsLeaveStoreUnchanged is the consolidated guardrail/atomicity
// table: each case is a changeset rejected at a distinct pipeline stage, asserted
// to (a) surface the expected coded error and (b) leave the stored bytes
// BYTE-FOR-BYTE identical to the seed (nothing persisted).
func TestE2E_RejectedEditsLeaveStoreUnchanged(t *testing.T) {
	res := newResolver(t)

	cases := map[string]struct {
		patch string
		code  errors.Code
	}{
		// Off-surface field: the table's `rows` is a real config field but is not on
		// its configurable surface — the field-edit guardrail rejects it.
		"off-surface field": {
			`[{"op":"replace","path":"/fruits/config/rows","value":[]}]`,
			errors.CONFIG_OVERRIDE_FIELD_UNKNOWN,
		},
		// Invalid value (type): a number into the string $manifest title.
		"bad value type": {
			`[{"op":"replace","path":"/$manifest/title","value":42}]`,
			errors.CONFIG_OVERRIDE_VALUE_INVALID,
		},
		// Invalid value (constraint): an out-of-set enum for $theme/emphasis.
		"bad enum value": {
			`[{"op":"replace","path":"/$theme/emphasis","value":"loud"}]`,
			errors.CONFIG_OVERRIDE_VALUE_INVALID,
		},
		// Unknown id: no node named `ghost`. The field-edit guardrail runs ahead of
		// pointer translation, so an id with no resolved surface is rejected as
		// off-surface (CONFIG_OVERRIDE_FIELD_UNKNOWN) before the translator's
		// CHANGESET_TARGET_NOT_FOUND can fire. The translator-level code is asserted
		// directly in translate_test.go; what matters end to end is the coded
		// rejection with nothing persisted.
		"unknown id": {
			`[{"op":"replace","path":"/ghost/config/title","value":"x"}]`,
			errors.CONFIG_OVERRIDE_FIELD_UNKNOWN,
		},
		// Unknown $scope: `$bogus` is not one of the five reserved scopes. Same as
		// above — a scope with no surface is rejected off-surface by the guardrail
		// ahead of the translator's CONFIGURATOR_TARGET_SCOPE_UNKNOWN.
		"unknown scope": {
			`[{"op":"replace","path":"/$bogus/title","value":"x"}]`,
			errors.CONFIG_OVERRIDE_FIELD_UNKNOWN,
		},
		// Failed precondition: a `test` op whose expected value does not hold. The
		// title is surfaced, so the op passes the guardrail and fails at the applier.
		"test precondition mismatch": {
			`[{"op":"test","path":"/$manifest/title","value":"Wrong"}]`,
			errors.PATCH_APPLY_FAILED,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			store, seed := seedFSStoreWith(t, minimalDocPath)

			_, err := ApplyChangeset(store, res, fixtureID, parse(t, c.patch))
			hasCode(t, err, c.code)

			after, loadErr := store.Load(fixtureID)
			if loadErr != nil {
				t.Fatalf("reload after rejected apply: %v", loadErr)
			}
			if !bytes.Equal(after, seed) {
				t.Fatalf("rejected apply mutated the store:\n--- want (seed) ---\n%s\n--- got ---\n%s", seed, after)
			}
		})
	}
}

// TestE2E_NoOpChangesetYieldsNoByteDiff proves canonical serialization is stable
// end to end: applying an empty (no-op) changeset to an already-canonical stored
// document persists bytes byte-identical to the seed — re-applying yields no diff.
func TestE2E_NoOpChangesetYieldsNoByteDiff(t *testing.T) {
	res := newResolver(t)
	store, seed := seedFSStoreWith(t, minimalDocPath)

	result, err := ApplyChangeset(store, res, fixtureID, parse(t, `[]`))
	if err != nil {
		t.Fatalf("ApplyChangeset (no-op): %v", err)
	}
	if !bytes.Equal(result.Document, seed) {
		t.Fatalf("no-op apply changed the bytes:\n--- seed ---\n%s\n--- got ---\n%s", seed, result.Document)
	}

	reloaded, err := store.Load(fixtureID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !bytes.Equal(reloaded, seed) {
		t.Fatalf("no-op apply persisted different bytes than the seed")
	}
}

// TestE2E_GitBackendCommitsOnApply is the one end-to-end pass through the GIT
// backend (OsFs + t.TempDir, which go-git needs for a real worktree). It proves a
// successful apply persists AND records a commit: the document's git history gains
// a revision beyond the seed save, and the reloaded working-tree bytes carry the
// edit.
func TestE2E_GitBackendCommitsOnApply(t *testing.T) {
	res := newResolver(t)

	root := t.TempDir()
	store, err := storage.NewGit(afero.NewOsFs(), root)
	if err != nil {
		t.Fatalf("NewGit: %v", err)
	}

	// Seed: the first Save commits the canonical fixture (one revision).
	seed := canonicalSeed(t, minimalDocPath)
	if err := store.Save(seed); err != nil {
		t.Fatalf("seed git store: %v", err)
	}

	// *storage.Git satisfies VersionedStore; History records a revision per Save.
	var versioned storage.VersionedStore = store
	before, err := versioned.History(fixtureID)
	if err != nil {
		t.Fatalf("history after seed: %v", err)
	}
	if len(before) != 1 {
		t.Fatalf("expected 1 revision after seed, got %d", len(before))
	}

	// Apply a surfaced field edit through the public pipeline; the git Save is a
	// commit, so history must gain exactly one revision.
	cs := parse(t, `[{"op":"replace","path":"/$manifest/title","value":"Committed"}]`)
	if _, err := ApplyChangeset(store, res, fixtureID, cs); err != nil {
		t.Fatalf("ApplyChangeset (git): %v", err)
	}

	after, err := versioned.History(fixtureID)
	if err != nil {
		t.Fatalf("history after apply: %v", err)
	}
	if len(after) != len(before)+1 {
		t.Fatalf("expected the apply to add exactly one commit: before %d, after %d", len(before), len(after))
	}

	// The working-tree bytes carry the edit, and the newest revision differs from
	// the seed revision (a real new commit, not an amend).
	reloaded, err := store.Load(fixtureID)
	if err != nil {
		t.Fatalf("reload after git apply: %v", err)
	}
	if got := readField(t, reloaded, "manifest", "title"); got != "Committed" {
		t.Fatalf("reloaded title = %v, want Committed", got)
	}
	if after[0].Hash == before[0].Hash {
		t.Fatalf("apply did not produce a new commit (HEAD hash unchanged)")
	}
}

// TestE2E_MissingIDRejected proves an apply against an id the store does not hold
// surfaces the store's STORAGE_NOT_FOUND coded error (and, trivially, persists
// nothing — there is nothing to mutate).
func TestE2E_MissingIDRejected(t *testing.T) {
	res := newResolver(t)
	store, _ := seedFSStoreWith(t, minimalDocPath)

	cs := parse(t, `[{"op":"replace","path":"/$manifest/title","value":"x"}]`)
	_, err := ApplyChangeset(store, res, "no-such-id", cs)
	hasCode(t, err, errors.STORAGE_NOT_FOUND)
}
