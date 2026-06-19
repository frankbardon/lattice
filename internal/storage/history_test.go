package storage

import (
	"bytes"
	"strconv"
	"testing"

	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
)

// TestGitHistoryAndLoadAt is a smoke test for the read-side versioning
// capability: two Saves of the same id produce two revisions newest-first, and
// LoadAt at the older revision returns the older bytes. The comprehensive
// versioning suite (multi-document isolation, short hashes, deletions) is E2-S3.
func TestGitHistoryAndLoadAt(t *testing.T) {
	root := t.TempDir()
	s, err := NewGit(afero.NewOsFs(), root)
	if err != nil {
		t.Fatalf("NewGit: %v", err)
	}

	v1 := docFor("alpha")
	if err := s.Save(v1); err != nil {
		t.Fatalf("Save v1: %v", err)
	}
	v2 := minimalDoc("alpha")
	if err := s.Save(v2); err != nil {
		t.Fatalf("Save v2: %v", err)
	}

	revs, err := s.History("alpha")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(revs) != 2 {
		t.Fatalf("History: want 2 revisions, got %d (%v)", len(revs), revs)
	}
	if revs[0].Message == "" || revs[0].Hash == "" {
		t.Fatalf("History revision missing metadata: %+v", revs[0])
	}

	// Newest-first, asserted by CONTENT, not timestamps: LoadAt(revs[0]) must
	// return the second Save's bytes and LoadAt(revs[1]) the first Save's. Two
	// Saves can land in the same wall-clock second, so a timestamp comparison
	// would be flaky; ordering by which bytes each revision carries is exact.
	gotNew, err := s.LoadAt("alpha", revs[0].Hash)
	if err != nil {
		t.Fatalf("LoadAt(newest): %v", err)
	}
	if !bytes.Equal(gotNew, v2) {
		t.Fatalf("revs[0] is not newest: LoadAt(revs[0]) mismatch:\n got %q\nwant %q", gotNew, v2)
	}

	// LoadAt the older revision returns the original (distinct) bytes,
	// byte-faithfully — proving LoadAt reads historical content, not current.
	gotOld, err := s.LoadAt("alpha", revs[1].Hash)
	if err != nil {
		t.Fatalf("LoadAt(oldest): %v", err)
	}
	if !bytes.Equal(gotOld, v1) {
		t.Fatalf("LoadAt(oldest) mismatch:\n got %q\nwant %q", gotOld, v1)
	}
	// Sanity: the two revisions are genuinely distinct content, so "historical"
	// is a real claim, not an accident of identical bytes.
	if bytes.Equal(v1, v2) {
		t.Fatal("test fixture broken: v1 and v2 must differ for LoadAt-at-history to be meaningful")
	}
}

// TestGitHistoryCountAfterNSaves proves History returns exactly N revisions
// after N Saves of the same id, newest-first, and that each revision resolves
// to a loadable commit. N>2 guards against off-by-one and confirms the path
// filter counts only commits that touched this document.
func TestGitHistoryCountAfterNSaves(t *testing.T) {
	root := t.TempDir()
	s, err := NewGit(afero.NewOsFs(), root)
	if err != nil {
		t.Fatalf("NewGit: %v", err)
	}

	// Each Save must carry DISTINCT bytes: a git commit needs a tree change, so
	// re-saving byte-identical content is a no-op (ErrEmptyCommit). Vary the
	// document body per iteration to record a genuine new revision each time.
	const n = 4
	for i := 0; i < n; i++ {
		doc := []byte(`{"manifest":{"id":"alpha"},"root":{},"rev":` + strconv.Itoa(i) + `}`)
		if err := s.Save(doc); err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
	}
	// An unrelated document's Saves must NOT inflate alpha's history (the log is
	// path-filtered to alpha.json).
	if err := s.Save(docFor("beta")); err != nil {
		t.Fatalf("Save beta: %v", err)
	}

	revs, err := s.History("alpha")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(revs) != n {
		t.Fatalf("History after %d Saves: want %d revisions, got %d", n, n, len(revs))
	}
	for i, r := range revs {
		if r.Hash == "" {
			t.Fatalf("revision %d has empty hash: %+v", i, r)
		}
		// Every listed revision resolves to loadable historical content.
		if _, err := s.LoadAt("alpha", r.Hash); err != nil {
			t.Fatalf("LoadAt(revs[%d]=%s): %v", i, r.Hash, err)
		}
	}
}

// TestGitHistoryUnknownID proves an id with no commits is reported not-found,
// not as an empty slice.
func TestGitHistoryUnknownID(t *testing.T) {
	root := t.TempDir()
	s, err := NewGit(afero.NewOsFs(), root)
	if err != nil {
		t.Fatalf("NewGit: %v", err)
	}
	// Commit something unrelated so the repo has a HEAD.
	if err := s.Save(docFor("alpha")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := s.History("missing"); !errors.HasCode(err, errors.STORAGE_NOT_FOUND) {
		t.Fatalf("History(missing): want STORAGE_NOT_FOUND, got %v", err)
	}
	// Even an empty repo (no HEAD) is not-found, not an error.
	empty, err := NewGit(afero.NewOsFs(), t.TempDir())
	if err != nil {
		t.Fatalf("NewGit(empty): %v", err)
	}
	if _, err := empty.History("alpha"); !errors.HasCode(err, errors.STORAGE_NOT_FOUND) {
		t.Fatalf("History on empty repo: want STORAGE_NOT_FOUND, got %v", err)
	}
}

// TestGitLoadAtUnknownRevision proves an unresolvable revision is a coded
// not-found error.
func TestGitLoadAtUnknownRevision(t *testing.T) {
	root := t.TempDir()
	s, err := NewGit(afero.NewOsFs(), root)
	if err != nil {
		t.Fatalf("NewGit: %v", err)
	}
	if err := s.Save(docFor("alpha")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	bogus := "0000000000000000000000000000000000000000"
	if _, err := s.LoadAt("alpha", bogus); !errors.HasCode(err, errors.STORAGE_NOT_FOUND) {
		t.Fatalf("LoadAt(bogus revision): want STORAGE_NOT_FOUND, got %v", err)
	}
}

// TestFSIsNotVersioned proves the FS backend deliberately does NOT implement the
// optional VersionedStore capability, so callers' type assertions correctly find
// it absent.
func TestFSIsNotVersioned(t *testing.T) {
	var s Store = NewFS(afero.NewMemMapFs(), "/store")
	if _, ok := s.(VersionedStore); ok {
		t.Fatal("FS backend must NOT implement VersionedStore")
	}
}

// TestGitIsVersioned proves the git backend DOES advertise the capability via a
// type assertion on the core Store interface.
func TestGitIsVersioned(t *testing.T) {
	g, err := NewGit(afero.NewOsFs(), t.TempDir())
	if err != nil {
		t.Fatalf("NewGit: %v", err)
	}
	var s Store = g
	if _, ok := s.(VersionedStore); !ok {
		t.Fatal("git backend must implement VersionedStore")
	}
}
