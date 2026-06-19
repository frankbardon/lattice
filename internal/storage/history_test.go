package storage

import (
	"bytes"
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
	// Newest-first: the second Save is revs[0], the first is revs[1].
	if revs[0].Timestamp.Before(revs[1].Timestamp) {
		t.Fatalf("History not newest-first: revs[0]=%v before revs[1]=%v", revs[0].Timestamp, revs[1].Timestamp)
	}
	if revs[0].Message == "" || revs[0].Hash == "" {
		t.Fatalf("History revision missing metadata: %+v", revs[0])
	}

	// LoadAt the older revision returns the original bytes, byte-faithfully.
	gotOld, err := s.LoadAt("alpha", revs[1].Hash)
	if err != nil {
		t.Fatalf("LoadAt(old): %v", err)
	}
	if !bytes.Equal(gotOld, v1) {
		t.Fatalf("LoadAt(old) mismatch:\n got %q\nwant %q", gotOld, v1)
	}

	// LoadAt the newest revision returns the latest bytes.
	gotNew, err := s.LoadAt("alpha", revs[0].Hash)
	if err != nil {
		t.Fatalf("LoadAt(new): %v", err)
	}
	if !bytes.Equal(gotNew, v2) {
		t.Fatalf("LoadAt(new) mismatch:\n got %q\nwant %q", gotNew, v2)
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
