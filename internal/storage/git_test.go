package storage

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
)

// TestGitSaveCommitsAndRoundTrips covers the git backend's core Save behaviour:
// pointing at a fresh (non-repo) directory initialises a repo, the first Save
// writes <id>.json to the working tree, round-trips byte-faithfully through
// Load, and produces exactly one commit; a second Save of the same id produces
// a second commit (so the repo accrues a revision per Save). Counts and content
// are asserted directly — never timestamps — so the test is deterministic.
func TestGitSaveCommitsAndRoundTrips(t *testing.T) {
	// t.TempDir() is an empty, non-repo directory: NewGit must git-init it
	// (init-on-absent) and the first Save must commit successfully.
	root := t.TempDir()
	s, err := NewGit(afero.NewOsFs(), root)
	if err != nil {
		t.Fatalf("NewGit: %v", err)
	}

	doc := minimalDoc("example-minimal")
	if err := s.Save(doc); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Round-trips byte-faithfully from the working tree.
	got, err := s.Load("example-minimal")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(got, doc) {
		t.Fatalf("round-trip mismatch:\n got %q\nwant %q", got, doc)
	}

	// The first Save produced exactly one commit with the generated message.
	commits := commitMessages(t, s.repo)
	if want := []string{"Save dashboard example-minimal"}; !equalStrings(commits, want) {
		t.Fatalf("commits after first Save: want %v, got %v", want, commits)
	}

	// The committed tree actually contains the document (the path was staged,
	// not just an empty commit recorded).
	if !committedFiles(t, s.repo)["example-minimal.json"] {
		t.Fatalf("commit tree missing example-minimal.json; got %v", committedFiles(t, s.repo))
	}

	// A second Save of the SAME id records a second commit (it does not amend or
	// replace the first). The new content is committed; the count is exactly two.
	doc2 := docFor("example-minimal")
	if err := s.Save(doc2); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	commits = commitMessages(t, s.repo)
	want := []string{"Save dashboard example-minimal", "Save dashboard example-minimal"}
	if !equalStrings(commits, want) {
		t.Fatalf("commits after second Save: want %v, got %v", want, commits)
	}
	// HEAD's tree carries the second Save's bytes, round-tripped byte-faithfully.
	got2, err := s.Load("example-minimal")
	if err != nil {
		t.Fatalf("Load after second Save: %v", err)
	}
	if !bytes.Equal(got2, doc2) {
		t.Fatalf("second Save round-trip mismatch:\n got %q\nwant %q", got2, doc2)
	}
}

// TestGitInitOnAbsent proves NewGit creates the root and initialises a git
// repository when pointed at a directory that does not yet exist, and that the
// first Save commits successfully into the freshly-initialised repo.
func TestGitInitOnAbsent(t *testing.T) {
	// A path NESTED under TempDir that does not exist yet: NewGit must MkdirAll
	// it and git-init it.
	root := filepath.Join(t.TempDir(), "nested", "store")
	s, err := NewGit(afero.NewOsFs(), root)
	if err != nil {
		t.Fatalf("NewGit on absent dir: %v", err)
	}

	if err := s.Save(docFor("alpha")); err != nil {
		t.Fatalf("first Save into init-on-absent repo: %v", err)
	}
	if commits := commitMessages(t, s.repo); !equalStrings(commits, []string{"Save dashboard alpha"}) {
		t.Fatalf("commits after init-on-absent Save: want one Save commit, got %v", commits)
	}
	if !committedFiles(t, s.repo)["alpha.json"] {
		t.Fatalf("init-on-absent commit tree missing alpha.json; got %v", committedFiles(t, s.repo))
	}
}

// TestGitDeleteCommits proves Delete removes the file and records a commit, and
// that List reflects the deletion.
func TestGitDeleteCommits(t *testing.T) {
	root := t.TempDir()
	s, err := NewGit(afero.NewOsFs(), root)
	if err != nil {
		t.Fatalf("NewGit: %v", err)
	}

	if err := s.Save(docFor("alpha")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.Delete("alpha"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := s.Load("alpha"); !errors.HasCode(err, errors.STORAGE_NOT_FOUND) {
		t.Fatalf("Load after Delete: want STORAGE_NOT_FOUND, got %v", err)
	}
	if ok, err := s.Exists("alpha"); err != nil || ok {
		t.Fatalf("Exists after Delete: want false, got %v (err %v)", ok, err)
	}
	ids, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("List after Delete: want empty, got %v", ids)
	}

	// Delete recorded a commit that removed the file from the tree: HEAD's tree
	// no longer contains alpha.json.
	if committedFiles(t, s.repo)["alpha.json"] {
		t.Fatalf("Delete commit tree still contains alpha.json; got %v", committedFiles(t, s.repo))
	}

	commits := commitMessages(t, s.repo)
	want := []string{"Delete dashboard alpha", "Save dashboard alpha"}
	if !equalStrings(commits, want) {
		t.Fatalf("commits after Delete: want %v, got %v", want, commits)
	}
}

// TestGitStagingIsPathScoped proves staging never sweeps in unrelated files: an
// untracked sibling file present in the working tree stays uncommitted after a
// Save.
func TestGitStagingIsPathScoped(t *testing.T) {
	root := t.TempDir()
	fs := afero.NewOsFs()
	s, err := NewGit(fs, root)
	if err != nil {
		t.Fatalf("NewGit: %v", err)
	}

	// An unrelated untracked file in the working tree.
	if err := afero.WriteFile(fs, root+"/unrelated.txt", []byte("noise"), 0o644); err != nil {
		t.Fatalf("write unrelated file: %v", err)
	}

	if err := s.Save(docFor("alpha")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// The commit tree contains only the dashboard document — the unrelated file
	// was never staged or committed.
	files := committedFiles(t, s.repo)
	if !files["alpha.json"] {
		t.Fatalf("commit tree should contain alpha.json, got %v", files)
	}
	if files["unrelated.txt"] {
		t.Fatalf("commit swept in the unrelated file; tree=%v", files)
	}

	// And the unrelated file remains untracked in the working tree.
	wt, err := s.repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	status, err := wt.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st := status.File("unrelated.txt"); st.Worktree != git.Untracked {
		t.Fatalf("unrelated.txt should remain untracked, got worktree=%q", st.Worktree)
	}
}

// TestGitReopenExistingRepo proves NewGit opens an existing repository rather
// than re-initialising it, preserving prior commits.
func TestGitReopenExistingRepo(t *testing.T) {
	root := t.TempDir()
	first, err := NewGit(afero.NewOsFs(), root)
	if err != nil {
		t.Fatalf("NewGit (init): %v", err)
	}
	if err := first.Save(docFor("alpha")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	second, err := NewGit(afero.NewOsFs(), root)
	if err != nil {
		t.Fatalf("NewGit (reopen): %v", err)
	}
	if ok, err := second.Exists("alpha"); err != nil || !ok {
		t.Fatalf("Exists(alpha) after reopen: want true, got %v (err %v)", ok, err)
	}
	if commits := commitMessages(t, second.repo); len(commits) != 1 {
		t.Fatalf("reopen lost history: want 1 commit, got %v", commits)
	}
}

// TestGitDefaultAuthorFallback proves a commit in a repo with no configured git
// user identity uses the fixed lattice fallback identity.
//
// signature() resolves the author from ConfigScoped(GlobalScope), which merges
// the repo's local config under the user's global config. A freshly-initialised
// repo has no local user, and pointing HOME / XDG_CONFIG_HOME at empty temp dirs
// guarantees the global config carries none either — so the lattice fallback is
// the only possible author, and the assertion is unconditional and
// deterministic (it does not depend on the developer's machine git identity).
func TestGitDefaultAuthorFallback(t *testing.T) {
	// Isolate global git config: GlobalScope reads $XDG_CONFIG_HOME/git/config,
	// $HOME/.gitconfig and $HOME/.config/git/config. Pointing both env vars at
	// empty temp dirs means none of those files exist, so no global identity.
	emptyHome := t.TempDir()
	t.Setenv("HOME", emptyHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(emptyHome, "xdg"))

	root := t.TempDir()
	s, err := NewGit(afero.NewOsFs(), root)
	if err != nil {
		t.Fatalf("NewGit: %v", err)
	}
	if err := s.Save(docFor("alpha")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	head, err := s.repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	commit, err := s.repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatalf("CommitObject: %v", err)
	}
	if commit.Author.Name != defaultAuthorName || commit.Author.Email != defaultAuthorEmail {
		t.Fatalf("author fallback: want %s <%s>, got %s <%s>",
			defaultAuthorName, defaultAuthorEmail, commit.Author.Name, commit.Author.Email)
	}
}

// commitMessages returns the repo's commit messages newest-first.
func commitMessages(t *testing.T, repo *git.Repository) []string {
	t.Helper()
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	iter, err := repo.Log(&git.LogOptions{From: head.Hash()})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	var msgs []string
	if err := iter.ForEach(func(c *object.Commit) error {
		msgs = append(msgs, c.Message)
		return nil
	}); err != nil {
		t.Fatalf("Log ForEach: %v", err)
	}
	return msgs
}

// committedFiles returns the set of paths in HEAD's tree.
func committedFiles(t *testing.T, repo *git.Repository) map[string]bool {
	t.Helper()
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatalf("CommitObject: %v", err)
	}
	tree, err := commit.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	files := map[string]bool{}
	if err := tree.Files().ForEach(func(f *object.File) error {
		files[f.Name] = true
		return nil
	}); err != nil {
		t.Fatalf("Files ForEach: %v", err)
	}
	return files
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
