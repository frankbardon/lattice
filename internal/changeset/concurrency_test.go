package changeset

// E4-S3 — the consolidated CONCURRENCY / PRECONDITION suite. It proves the two
// precondition levers of the apply pipeline end to end across BOTH storage
// backends:
//
//   - RFC 6902 `test` ops as preconditions: a satisfied `test` is a transparent
//     gate the apply flows past and persists; a violated `test` aborts the whole
//     changeset (PATCH_APPLY_FAILED) and leaves the store byte-for-byte unchanged.
//   - The optimistic-concurrency revision precondition (WithExpectedRevision):
//     a matching expected revision persists; a STALE expected revision — after a
//     concurrent Save lands between load and the pre-Save revision re-read — is
//     rejected with CHANGESET_REVISION_CONFLICT, and the store reflects the
//     concurrent writer's bytes, NOT the rejected stale edit; the no-precondition
//     path is unchanged single-writer behavior.
//
// Each scenario runs against a table of backends: FS (content-hash revision over a
// hermetic afero.NewMemMapFs) and git (commit-hash revision over afero.NewOsFs +
// t.TempDir, which go-git requires for a real worktree). The git conflict path is
// the substantive addition over E4-S2 (which exercised the conflict only via FS).
//
// The store-level Revision tokens themselves (stability, change-on-Save, the git
// commit-hash vs FS content-hash distinction, unknown-id not-found) are unit-proven
// in internal/storage/revision_test.go; this suite proves how ApplyChangeset USES
// those tokens. The test-double cases that need no real backend (an incapable store,
// a satisfied test op on FS) stay in precondition_test.go; this file owns the
// backend-parametrized end-to-end scenarios.

import (
	"bytes"
	"testing"

	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/storage"
)

// revisionedStore is the storage contract these scenarios need: a Store that can
// also report its current revision (so the pipeline's precondition can be
// enforced). Both *storage.FS and *storage.Git satisfy it.
type revisionedStore interface {
	storage.Store
	storage.RevisionedStore
}

// backendCase names a storage backend and a constructor that seeds it with the
// canonical form of the minimal fixture, returning the live store plus the exact
// seed bytes it now holds. The two cases differ only in the revision token shape
// (FS content hash vs git commit hash); every concurrency scenario runs over both.
type backendCase struct {
	name string
	seed func(t *testing.T) (revisionedStore, []byte)
}

// backends is the FS-and-git table every backend-parametrized concurrency scenario
// iterates. FS is hermetic (MemMapFs); git uses OsFs + t.TempDir for a real
// go-git worktree.
var backends = []backendCase{
	{
		name: "fs",
		seed: func(t *testing.T) (revisionedStore, []byte) {
			t.Helper()
			seed := canonicalSeed(t, minimalDocPath)
			store := storage.NewFS(afero.NewMemMapFs(), ".")
			if err := store.Save(seed); err != nil {
				t.Fatalf("seed fs store: %v", err)
			}
			return store, seed
		},
	},
	{
		name: "git",
		seed: func(t *testing.T) (revisionedStore, []byte) {
			t.Helper()
			seed := canonicalSeed(t, minimalDocPath)
			store, err := storage.NewGit(afero.NewOsFs(), t.TempDir())
			if err != nil {
				t.Fatalf("new git store: %v", err)
			}
			if err := store.Save(seed); err != nil {
				t.Fatalf("seed git store: %v", err)
			}
			return store, seed
		},
	},
}

// TestConcurrency_TestOpPrecondition proves the RFC 6902 `test` op as a
// precondition lever across both backends: a satisfied `test` lets the following
// `replace` apply and persist, while a violated `test` aborts the whole changeset
// with PATCH_APPLY_FAILED and leaves the stored bytes byte-for-byte identical to
// the seed. The replace and the test address the same surfaced field, so the only
// difference between the passing and failing case is whether the asserted value
// holds.
func TestConcurrency_TestOpPrecondition(t *testing.T) {
	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			t.Run("passing test persists", func(t *testing.T) {
				res := newResolver(t)
				store, _ := b.seed(t)

				cs := parse(t, `[
					{"op":"test","path":"/$manifest/title","value":"Minimal Example Dashboard"},
					{"op":"replace","path":"/$manifest/title","value":"Renamed"}
				]`)
				result, err := ApplyChangeset(store, res, fixtureID, cs)
				if err != nil {
					t.Fatalf("ApplyChangeset with a satisfied test op: %v", err)
				}
				if got := readField(t, result.Document, "manifest", "title"); got != "Renamed" {
					t.Fatalf("satisfied test op did not let the replace apply, got %v", got)
				}
				// The persisted store reflects the edit.
				reloaded, err := store.Load(fixtureID)
				if err != nil {
					t.Fatalf("reload: %v", err)
				}
				if got := readField(t, reloaded, "manifest", "title"); got != "Renamed" {
					t.Fatalf("satisfied-test apply did not persist, reloaded title = %v", got)
				}
			})

			t.Run("failing test rejects and persists nothing", func(t *testing.T) {
				res := newResolver(t)
				store, seed := b.seed(t)

				// The `test` asserts a value the document does not hold, so the changeset
				// aborts at the applier before the replace — nothing reaches Save.
				cs := parse(t, `[
					{"op":"test","path":"/$manifest/title","value":"Wrong Title"},
					{"op":"replace","path":"/$manifest/title","value":"Renamed"}
				]`)
				_, err := ApplyChangeset(store, res, fixtureID, cs)
				hasCode(t, err, errors.PATCH_APPLY_FAILED)

				after, err := store.Load(fixtureID)
				if err != nil {
					t.Fatalf("reload after rejected apply: %v", err)
				}
				if !bytes.Equal(after, seed) {
					t.Fatalf("violated test op mutated the store:\n--- want (seed) ---\n%s\n--- got ---\n%s", seed, after)
				}
			})
		})
	}
}

// TestConcurrency_RevisionPreconditionHappyPath proves the load → get revision →
// apply-with-that-revision → persists flow across both backends: the expected
// revision is read from the seeded store's OWN current revision, so the
// pre-Save re-read matches and the apply persists normally.
func TestConcurrency_RevisionPreconditionHappyPath(t *testing.T) {
	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			res := newResolver(t)
			store, _ := b.seed(t)

			// Load + read the current revision, exactly as a caller would before
			// composing an edit.
			if _, err := store.Load(fixtureID); err != nil {
				t.Fatalf("load: %v", err)
			}
			expected, err := store.Revision(fixtureID)
			if err != nil {
				t.Fatalf("read current revision: %v", err)
			}

			cs := parse(t, `[{"op":"replace","path":"/$manifest/title","value":"Renamed"}]`)
			result, err := ApplyChangeset(store, res, fixtureID, cs, WithExpectedRevision(expected))
			if err != nil {
				t.Fatalf("ApplyChangeset with a matching expected revision: %v", err)
			}
			if got := readField(t, result.Document, "manifest", "title"); got != "Renamed" {
				t.Fatalf("matching-revision apply did not produce the edit, got %v", got)
			}

			reloaded, err := store.Load(fixtureID)
			if err != nil {
				t.Fatalf("reload: %v", err)
			}
			if got := readField(t, reloaded, "manifest", "title"); got != "Renamed" {
				t.Fatalf("matching-revision apply did not persist, reloaded title = %v", got)
			}
		})
	}
}

// TestConcurrency_RevisionConflict proves the headline conflict scenario across
// both backends: the caller reads the load-time revision, a CONCURRENT writer Saves
// (bumping the revision) before the pipeline's pre-Save re-read, and the apply with
// the now-stale expected revision is rejected with CHANGESET_REVISION_CONFLICT. The
// store must reflect the concurrent write, not the rejected stale edit.
//
// The concurrent Save is injected via concurrentSaveStore (defined in
// precondition_test.go): its first Revision() call — the pipeline's pre-Save
// re-read — triggers a Save of intruder bytes, so the revision the pipeline
// observes belongs to a document that changed after this apply loaded it. This is
// the git conflict path's first end-to-end exercise (E4-S2 covered only FS).
func TestConcurrency_RevisionConflict(t *testing.T) {
	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			res := newResolver(t)
			base, seed := b.seed(t)

			// The caller's expected revision is the document's revision at load time.
			expected, err := base.Revision(fixtureID)
			if err != nil {
				t.Fatalf("read load-time revision: %v", err)
			}

			// The intruder is the seed with a different manifest title — a real byte
			// change, so both the FS content hash and the git commit hash differ from
			// `expected` once it lands.
			intruder := bytes.Replace(seed, []byte(`"Minimal Example Dashboard"`), []byte(`"Intruder Edit"`), 1)
			if bytes.Equal(intruder, seed) {
				t.Fatalf("test setup: intruder bytes did not differ from the seed")
			}
			store := &concurrentSaveStore{RevisionedStore: base, intruder: intruder}

			cs := parse(t, `[{"op":"replace","path":"/$manifest/title","value":"Stale Edit"}]`)
			_, err = ApplyChangeset(store, res, fixtureID, cs, WithExpectedRevision(expected))
			hasCode(t, err, errors.CHANGESET_REVISION_CONFLICT)

			// The store reflects the CONCURRENT writer's bytes — the working-tree title
			// is the intruder's — and the rejected stale edit persisted nothing.
			after, err := base.Load(fixtureID)
			if err != nil {
				t.Fatalf("reload after rejected apply: %v", err)
			}
			if got := readField(t, after, "manifest", "title"); got != "Intruder Edit" {
				t.Fatalf("store should reflect the concurrent write, got title = %v", got)
			}
			if bytes.Contains(after, []byte(`"Stale Edit"`)) {
				t.Fatalf("the rejected stale edit was persisted")
			}
		})
	}
}

// TestConcurrency_NoExpectedRevisionUnchanged proves the no-precondition path is
// unchanged single-writer behavior across both backends: omitting WithExpectedRevision
// applies and persists regardless of the store's current revision — there is no
// optimistic-concurrency gate, so the edit always lands.
func TestConcurrency_NoExpectedRevisionUnchanged(t *testing.T) {
	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			res := newResolver(t)
			store, seed := b.seed(t)

			cs := parse(t, `[{"op":"replace","path":"/$manifest/title","value":"Renamed"}]`)
			result, err := ApplyChangeset(store, res, fixtureID, cs)
			if err != nil {
				t.Fatalf("ApplyChangeset without an expected revision: %v", err)
			}
			if bytes.Equal(result.Document, seed) {
				t.Fatalf("single-writer apply produced no change")
			}

			reloaded, err := store.Load(fixtureID)
			if err != nil {
				t.Fatalf("reload: %v", err)
			}
			if got := readField(t, reloaded, "manifest", "title"); got != "Renamed" {
				t.Fatalf("single-writer apply did not persist, reloaded title = %v", got)
			}
		})
	}
}
