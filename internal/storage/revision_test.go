package storage

import (
	"testing"

	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
)

// TestFSRevisionStableAndChanges proves the FS backend's content-hash token is
// stable across reads of an unchanged document and changes after a Save that
// alters the bytes.
func TestFSRevisionStableAndChanges(t *testing.T) {
	s := NewFS(afero.NewMemMapFs(), "/store")
	if err := s.Save(minimalDoc("alpha")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	first, err := s.Revision("alpha")
	if err != nil {
		t.Fatalf("Revision: %v", err)
	}
	if first == "" {
		t.Fatal("Revision returned an empty token")
	}

	// Re-reading an unchanged document yields the identical token.
	again, err := s.Revision("alpha")
	if err != nil {
		t.Fatalf("Revision (re-read): %v", err)
	}
	if again != first {
		t.Fatalf("Revision not stable across reads: %q vs %q", first, again)
	}

	// A Save that changes the bytes changes the token.
	if err := s.Save(docFor("alpha")); err != nil {
		t.Fatalf("Save (changed): %v", err)
	}
	changed, err := s.Revision("alpha")
	if err != nil {
		t.Fatalf("Revision (after change): %v", err)
	}
	if changed == first {
		t.Fatalf("Revision did not change after a content-changing Save: %q", changed)
	}
}

// TestFSRevisionUnknownID proves an unknown id is reported as a coded
// not-found, not an empty token.
func TestFSRevisionUnknownID(t *testing.T) {
	s := NewFS(afero.NewMemMapFs(), "/store")
	if _, err := s.Revision("missing"); !errors.HasCode(err, errors.STORAGE_NOT_FOUND) {
		t.Fatalf("Revision(missing): want STORAGE_NOT_FOUND, got %v", err)
	}
}

// TestGitRevisionStableAndChanges proves the git backend's commit-hash token is
// stable across reads of an unchanged document, equals the newest History entry,
// and advances after a Save that records a new commit.
func TestGitRevisionStableAndChanges(t *testing.T) {
	s, err := NewGit(afero.NewOsFs(), t.TempDir())
	if err != nil {
		t.Fatalf("NewGit: %v", err)
	}
	if err := s.Save([]byte(`{"manifest":{"id":"alpha"},"root":{},"rev":0}`)); err != nil {
		t.Fatalf("Save v0: %v", err)
	}

	first, err := s.Revision("alpha")
	if err != nil {
		t.Fatalf("Revision: %v", err)
	}
	if first == "" {
		t.Fatal("Revision returned an empty token")
	}

	// The token is the newest commit hash History reports.
	revs, err := s.History("alpha")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if first != revs[0].Hash {
		t.Fatalf("Revision %q != newest History hash %q", first, revs[0].Hash)
	}

	// Re-reading an unchanged document yields the identical token.
	again, err := s.Revision("alpha")
	if err != nil {
		t.Fatalf("Revision (re-read): %v", err)
	}
	if again != first {
		t.Fatalf("Revision not stable across reads: %q vs %q", first, again)
	}

	// A Save that records a new commit advances the token.
	if err := s.Save([]byte(`{"manifest":{"id":"alpha"},"root":{},"rev":1}`)); err != nil {
		t.Fatalf("Save v1: %v", err)
	}
	changed, err := s.Revision("alpha")
	if err != nil {
		t.Fatalf("Revision (after change): %v", err)
	}
	if changed == first {
		t.Fatalf("Revision did not change after a new commit: %q", changed)
	}
}

// TestGitRevisionUnknownID proves an id with no commits is reported as a coded
// not-found.
func TestGitRevisionUnknownID(t *testing.T) {
	s, err := NewGit(afero.NewOsFs(), t.TempDir())
	if err != nil {
		t.Fatalf("NewGit: %v", err)
	}
	// Commit something unrelated so the repo has a HEAD.
	if err := s.Save(docFor("alpha")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := s.Revision("missing"); !errors.HasCode(err, errors.STORAGE_NOT_FOUND) {
		t.Fatalf("Revision(missing): want STORAGE_NOT_FOUND, got %v", err)
	}
}

// TestGitRevisionOverridesFSContentHash proves the git backend returns a commit
// hash (consistent with History), not the embedded FS backend's content hash, so
// the two tokens are different forms for the same stored document.
func TestGitRevisionOverridesFSContentHash(t *testing.T) {
	s, err := NewGit(afero.NewOsFs(), t.TempDir())
	if err != nil {
		t.Fatalf("NewGit: %v", err)
	}
	if err := s.Save(minimalDoc("alpha")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	gitTok, err := s.Revision("alpha")
	if err != nil {
		t.Fatalf("Git.Revision: %v", err)
	}
	fsTok, err := s.FS.Revision("alpha")
	if err != nil {
		t.Fatalf("FS.Revision: %v", err)
	}
	if gitTok == fsTok {
		t.Fatalf("git Revision must be a commit hash, not the FS content hash: both %q", gitTok)
	}
	revs, err := s.History("alpha")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if gitTok != revs[0].Hash {
		t.Fatalf("git Revision %q != newest commit %q", gitTok, revs[0].Hash)
	}
}
