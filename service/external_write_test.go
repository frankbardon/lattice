package service_test

// This file is the EXTERNAL-CALLER proof for the WRITE + CAPABILITY surface
// (E2-S4). Like external_read_test.go it lives in the black-box `service_test`
// package and imports ONLY the public facade
// (github.com/frankbardon/lattice/service), the public errors package (itself at
// the module root, not internal/), stdlib, the test framework, and afero —
// exactly the dependency set a future WASM/MCP frontend has available. NO
// internal/... import appears here; if one were needed to drive a write or a
// capability check, the facade would have a gap.
//
// Backends: the FS path is hermetic via afero.NewMemMapFs(); the git path uses
// afero.NewOsFs() + t.TempDir(), which go-git requires for a real worktree
// (mirroring internal/storage and internal/changeset git tests).

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/service"
)

// newFSService wires an in-memory FS-backed Service seeded with the minimal
// fixture, returning the service, the live store (so a test can read it back or
// inject a concurrent write), and the exact seed bytes the store now holds. The
// seed is the fixture's canonical form — what the store holds after Save — so a
// byte-identity assertion after a rejected Patch compares like with like.
func newFSService(t *testing.T) (*service.Service, service.Store, []byte) {
	t.Helper()
	store, err := service.NewStore(service.BackendFS, afero.NewMemMapFs(), "docs")
	if err != nil {
		t.Fatalf("NewStore(FS): %v", err)
	}
	return seedService(t, store)
}

// newGitService wires a git-backed Service seeded with the minimal fixture. The
// git backend needs a real worktree, so it is rooted at t.TempDir() over an
// OsFs (MemMapFs is not enough for go-git).
func newGitService(t *testing.T) (*service.Service, service.Store, []byte) {
	t.Helper()
	store, err := service.NewStore(service.BackendGit, afero.NewOsFs(), t.TempDir())
	if err != nil {
		t.Fatalf("NewStore(git): %v", err)
	}
	return seedService(t, store)
}

// seedService saves the minimal fixture into the given store and pairs it with a
// resolver over the repo's real schema catalog. The seed bytes returned are the
// store's own post-Save bytes so later byte-equality assertions are exact.
func seedService(t *testing.T, store service.Store) (*service.Service, service.Store, []byte) {
	t.Helper()
	doc := loadFixtureDoc(t)
	if err := store.Save(doc); err != nil {
		t.Fatalf("seed store.Save: %v", err)
	}
	seed, err := store.Load(fixtureID)
	if err != nil {
		t.Fatalf("seed store.Load: %v", err)
	}
	res, err := service.NewResolver(os.DirFS("../schemas"))
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	return service.New(store, res), store, seed
}

// readManifestTitle digests document bytes to manifest.title — the surfaced,
// document-scope field the write tests edit via the $manifest scope.
func readManifestTitle(t *testing.T, b []byte) string {
	t.Helper()
	var doc struct {
		Manifest struct {
			Title string `json:"title"`
		} `json:"manifest"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("unmarshal manifest title: %v", err)
	}
	return doc.Manifest.Title
}

// assertCode fails unless err is a *errors.CodedError carrying want.
func assertCode(t *testing.T, err error, want errors.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %s, got nil", want)
	}
	if !errors.HasCode(err, want) {
		t.Fatalf("expected error code %s, got %v", want, err)
	}
}

// TestExternalPatchHappyPath proves the write happy path through the facade: a
// surfaced $manifest/title edit is parsed via ParseChangeset, applied via Patch,
// and the returned ApplyResult carries both the new document bytes and a resolved
// tree. The store's persisted bytes change to match the returned document.
func TestExternalPatchHappyPath(t *testing.T) {
	svc, store, seed := newFSService(t)

	cs, err := svc.ParseChangeset([]byte(`[{"op":"replace","path":"/$manifest/title","value":"Renamed"}]`))
	if err != nil {
		t.Fatalf("ParseChangeset: %v", err)
	}

	result, err := svc.Patch(fixtureID, cs)
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}

	// ApplyResult carries the persisted document with the new value...
	if got := readManifestTitle(t, result.Document); got != "Renamed" {
		t.Fatalf("ApplyResult.Document title = %q, want %q", got, "Renamed")
	}
	// ...and the resolved tree of that persisted document.
	if result.Resolved == nil {
		t.Fatal("ApplyResult.Resolved is nil, want a resolved tree")
	}
	if got, _ := result.Resolved.Manifest["id"].(string); got != fixtureID {
		t.Fatalf("ApplyResult.Resolved manifest id = %v, want %q", got, fixtureID)
	}
	if result.Resolved.Root == nil || !result.Resolved.Root.Container {
		t.Fatal("ApplyResult.Resolved root should be a container")
	}

	// The store now holds the returned bytes, and they differ from the seed.
	reloaded, err := store.Load(fixtureID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !bytes.Equal(reloaded, result.Document) {
		t.Fatalf("persisted bytes differ from returned ApplyResult.Document")
	}
	if bytes.Equal(reloaded, seed) {
		t.Fatalf("store still holds the seed; the edit was not persisted")
	}
	if got := readManifestTitle(t, reloaded); got != "Renamed" {
		t.Fatalf("reloaded title = %q, want %q", got, "Renamed")
	}
}

// TestExternalPatchAtomicity proves a rejected Patch persists NOTHING through the
// facade: an off-surface field edit (the table's `rows` is a real config field
// but not on its configurable surface) is rejected with a coded error, and the
// stored bytes remain byte-for-byte identical to the seed.
func TestExternalPatchAtomicity(t *testing.T) {
	svc, store, seed := newFSService(t)

	cs, err := svc.ParseChangeset([]byte(`[{"op":"replace","path":"/fruits/config/rows","value":[]}]`))
	if err != nil {
		t.Fatalf("ParseChangeset: %v", err)
	}

	if _, err := svc.Patch(fixtureID, cs); err == nil {
		t.Fatal("Patch of an off-surface field should have failed, got nil error")
	}

	after, err := store.Load(fixtureID)
	if err != nil {
		t.Fatalf("reload after rejected Patch: %v", err)
	}
	if !bytes.Equal(after, seed) {
		t.Fatalf("rejected Patch mutated the store:\n--- want (seed) ---\n%s\n--- got ---\n%s", seed, after)
	}
}

// TestExternalPatchStaleRevisionConflict proves the optimistic-concurrency
// precondition through the facade across both shipped backends. The caller reads
// the document's current revision via Service.Revision, a CONCURRENT writer Saves
// (bumping the revision) before the patch, and the Patch carrying the now-stale
// expected revision is rejected with CHANGESET_REVISION_CONFLICT — and the
// rejected stale edit persists nothing (the store reflects the concurrent write).
func TestExternalPatchStaleRevisionConflict(t *testing.T) {
	cases := []struct {
		name string
		wire func(t *testing.T) (*service.Service, service.Store, []byte)
	}{
		{"fs", newFSService},
		{"git", newGitService},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, store, seed := c.wire(t)

			// The caller's expected revision is the document's revision at load time,
			// read through the facade's capability-gated Revision method.
			stale, err := svc.Revision(fixtureID)
			if err != nil {
				t.Fatalf("Revision: %v", err)
			}

			// A concurrent writer lands a real byte change, bumping the store's
			// revision past `stale` (FS content hash and git commit hash both differ).
			intruder := bytes.Replace(seed, []byte(`"Minimal Example Dashboard"`), []byte(`"Intruder Edit"`), 1)
			if bytes.Equal(intruder, seed) {
				t.Fatal("test setup: intruder bytes did not differ from the seed")
			}
			if err := store.Save(intruder); err != nil {
				t.Fatalf("concurrent Save: %v", err)
			}

			cs, err := svc.ParseChangeset([]byte(`[{"op":"replace","path":"/$manifest/title","value":"Stale Edit"}]`))
			if err != nil {
				t.Fatalf("ParseChangeset: %v", err)
			}

			_, err = svc.Patch(fixtureID, cs, service.WithExpectedRevision(stale))
			assertCode(t, err, errors.CHANGESET_REVISION_CONFLICT)

			// The store reflects the concurrent writer's bytes; the stale edit is gone.
			after, err := store.Load(fixtureID)
			if err != nil {
				t.Fatalf("reload after rejected Patch: %v", err)
			}
			if got := readManifestTitle(t, after); got != "Intruder Edit" {
				t.Fatalf("store should reflect the concurrent write, title = %q", got)
			}
			if bytes.Contains(after, []byte(`"Stale Edit"`)) {
				t.Fatalf("the rejected stale edit was persisted")
			}
		})
	}
}

// TestExternalSaveDeleteRoundTrip proves the whole-document Save/Delete
// passthrough through the facade: a fresh document Saved is then Loadable
// byte-faithfully and reported by Exists/List, and after Delete it is gone
// (Exists false, Load surfaces STORAGE_NOT_FOUND).
func TestExternalSaveDeleteRoundTrip(t *testing.T) {
	store, err := service.NewStore(service.BackendFS, afero.NewMemMapFs(), "docs")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	res, err := service.NewResolver(os.DirFS("../schemas"))
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	svc := service.New(store, res)

	doc := loadFixtureDoc(t)
	if err := svc.Save(doc); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Round-trips byte-faithfully and is addressable by manifest.id.
	raw, err := svc.Load(fixtureID)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if !bytes.Equal(raw, doc) {
		t.Fatalf("Save/Load not byte-faithful:\n got %q\nwant %q", raw, doc)
	}
	if ok, err := svc.Exists(fixtureID); err != nil || !ok {
		t.Fatalf("Exists after Save: want true, got %v (err %v)", ok, err)
	}

	if err := svc.Delete(fixtureID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ok, err := svc.Exists(fixtureID); err != nil || ok {
		t.Fatalf("Exists after Delete: want false, got %v (err %v)", ok, err)
	}
	if _, err := svc.Load(fixtureID); !errors.HasCode(err, errors.STORAGE_NOT_FOUND) {
		t.Fatalf("Load after Delete: want STORAGE_NOT_FOUND, got %v", err)
	}
}

// TestExternalCapabilityGatingFS proves the FS backend lacks the VersionedStore
// capability: History and LoadAt are rejected with STORAGE_CAPABILITY_UNSUPPORTED
// rather than silently degrading, while Revision (the RevisionedStore capability,
// implemented by BOTH shipped backends) succeeds.
func TestExternalCapabilityGatingFS(t *testing.T) {
	svc, _, _ := newFSService(t)

	if _, err := svc.History(fixtureID); !errors.HasCode(err, errors.STORAGE_CAPABILITY_UNSUPPORTED) {
		t.Fatalf("FS History: want STORAGE_CAPABILITY_UNSUPPORTED, got %v", err)
	}
	if _, err := svc.LoadAt(fixtureID, "anything"); !errors.HasCode(err, errors.STORAGE_CAPABILITY_UNSUPPORTED) {
		t.Fatalf("FS LoadAt: want STORAGE_CAPABILITY_UNSUPPORTED, got %v", err)
	}

	// Revision is supported on the FS backend.
	if _, err := svc.Revision(fixtureID); err != nil {
		t.Fatalf("FS Revision should succeed: %v", err)
	}
}

// TestExternalCapabilityGatingGit proves the git backend DOES implement the
// VersionedStore capability: History returns the seed revision and LoadAt fetches
// that historical version's bytes, and Revision (RevisionedStore) succeeds too.
// This keeps the git assertion minimal — it proves the facade delegates the
// capability correctly; the rich history semantics are unit-proven in
// internal/storage.
func TestExternalCapabilityGatingGit(t *testing.T) {
	svc, _, seed := newGitService(t)

	hist, err := svc.History(fixtureID)
	if err != nil {
		t.Fatalf("git History: %v", err)
	}
	if len(hist) != 1 {
		t.Fatalf("git History: want 1 revision after seed Save, got %d", len(hist))
	}

	at, err := svc.LoadAt(fixtureID, hist[0].Hash)
	if err != nil {
		t.Fatalf("git LoadAt: %v", err)
	}
	if !bytes.Equal(at, seed) {
		t.Fatalf("git LoadAt(seed revision) not byte-faithful:\n got %q\nwant %q", at, seed)
	}

	// Revision is supported on the git backend.
	if _, err := svc.Revision(fixtureID); err != nil {
		t.Fatalf("git Revision should succeed: %v", err)
	}
}
