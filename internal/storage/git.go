package storage

import (
	stderrors "errors"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
)

// Default author identity used for commits when the repo's git config carries no
// user.name / user.email. Keeping a fixed fallback means a freshly initialised
// repo (no global or local git identity) still produces valid, attributable
// commits rather than failing the Save/Delete.
const (
	defaultAuthorName  = "lattice"
	defaultAuthorEmail = "lattice@localhost"
)

// Git is a git-backed Store. It is "FS write semantics + commit": documents live
// on disk as plain <id>.json files in the repository working tree (the same
// byte-faithful, atomic, id-mapped writes the FS backend performs), and every
// Save or Delete produces a commit recording that change.
//
// Read operations (Load, List, Exists) are served straight from the working
// tree via the embedded FS backend — the latest committed-or-not bytes on disk —
// so a Git store reads identically to an FS store over the same root.
//
// Staging is always path-scoped: only the specific <id>.json is staged before a
// commit. Unrelated untracked or modified files in the repository are never
// touched, so a lattice store can safely share a working tree with other files.
type Git struct {
	*FS // embedded for Load/List/Exists and the shared id-mapping/atomic-write Save

	repo *git.Repository
	// root is the repository working-tree root. The FS backend writes under the
	// same root, so a document's path relative to the worktree is simply
	// "<id>.json".
	root string
}

// NewGit constructs a git-backed Store over the working-tree repository at root.
// If root is not already a git repository it is initialised (a non-bare,
// working-tree repo); if it already is one it is opened. fs is the filesystem
// abstraction used for the on-disk reads and writes (afero.NewOsFs() in
// production); it must address the same files go-git operates on.
//
// NewGit requires a working-tree (non-bare) repository: go-git's staging and
// commit operations act on the working tree, and documents are stored there as
// plain files.
func NewGit(fs afero.Fs, root string) (*Git, error) {
	if err := fs.MkdirAll(root, 0o755); err != nil {
		return nil, errors.WrapCodedErrorWithDetails(err, errors.STORAGE_IO,
			"failed creating git store root "+root, map[string]any{"path": root})
	}

	repo, err := git.PlainOpen(root)
	if stderrors.Is(err, git.ErrRepositoryNotExists) {
		repo, err = git.PlainInit(root, false)
		if err != nil {
			return nil, errors.WrapCodedErrorWithDetails(err, errors.STORAGE_IO,
				"failed initialising git repository at "+root, map[string]any{"path": root})
		}
	} else if err != nil {
		return nil, errors.WrapCodedErrorWithDetails(err, errors.STORAGE_IO,
			"failed opening git repository at "+root, map[string]any{"path": root})
	}

	return &Git{
		FS:   NewFS(fs, root),
		repo: repo,
		root: root,
	}, nil
}

// Save writes the document as <id>.json in the working tree (reusing the FS
// backend's id mapping, safety validation, and atomic temp-file + rename write),
// then stages only that path and commits with a generated message. Unrelated
// files in the repository are left unstaged and uncommitted.
func (g *Git) Save(document []byte) error {
	id, err := extractID(document)
	if err != nil {
		return err
	}
	if err := validateID(id); err != nil {
		return err
	}

	if err := g.FS.Save(document); err != nil {
		return err
	}

	return g.commitPath(id, "Save dashboard "+id)
}

// Delete removes the document's <id>.json from the working tree (reusing the FS
// backend's not-found semantics), then stages that path's deletion and commits.
func (g *Git) Delete(id string) error {
	if err := validateID(id); err != nil {
		return err
	}
	if err := g.FS.Delete(id); err != nil {
		return err
	}

	return g.commitPath(id, "Delete dashboard "+id)
}

// commitPath stages only <id>.json (relative to the worktree root) and commits
// it with msg. Staging is path-scoped — never `git add .` — so the commit
// records solely this document's change. The path passed to Add covers both an
// added/modified file and a deletion (go-git's status diff handles either).
func (g *Git) commitPath(id, msg string) error {
	wt, err := g.repo.Worktree()
	if err != nil {
		return errors.WrapCodedErrorWithDetails(err, errors.STORAGE_IO,
			"failed opening git worktree", map[string]any{"id": id, "path": g.root})
	}

	rel := id + fileExt
	if _, err := wt.Add(rel); err != nil {
		return errors.WrapCodedErrorWithDetails(err, errors.STORAGE_IO,
			"failed staging "+rel, map[string]any{"id": id, "path": rel})
	}

	sig := g.signature()
	if _, err := wt.Commit(msg, &git.CommitOptions{Author: sig}); err != nil {
		return errors.WrapCodedErrorWithDetails(err, errors.STORAGE_IO,
			"failed committing "+rel, map[string]any{"id": id, "path": rel})
	}
	return nil
}

// signature resolves the commit author from the repository's git config
// (user.name / user.email, merging the local repo config over the user's global
// config), falling back to the fixed lattice identity for whichever field is
// unset. The commit time is always now.
func (g *Git) signature() *object.Signature {
	name, email := defaultAuthorName, defaultAuthorEmail
	if cfg, err := g.repo.ConfigScoped(config.GlobalScope); err == nil {
		if cfg.User.Name != "" {
			name = cfg.User.Name
		}
		if cfg.User.Email != "" {
			email = cfg.User.Email
		}
	}
	return &object.Signature{Name: name, Email: email, When: time.Now()}
}
