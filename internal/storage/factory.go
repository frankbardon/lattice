package storage

import (
	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
)

// Backend names the persistence backend kind selected at the command line (the
// --store flag value). It is the single place the set of known backends is
// enumerated.
type Backend string

const (
	// BackendFS selects the filesystem-backed Store (NewFS). It is the default.
	BackendFS Backend = "fs"

	// BackendGit selects the git-backed Store (NewGit): FS write semantics plus a
	// commit per Save/Delete, with read-side version history.
	BackendGit Backend = "git"
)

// New constructs the Store named by backend, rooted at root over fs. fs is the
// filesystem abstraction (afero.NewOsFs() in production, afero.NewMemMapFs() in
// tests). It is the one place backend construction lives, so commands select a
// backend by name rather than wiring NewFS/NewGit themselves.
//
// An unrecognized backend returns a STORAGE_BACKEND_UNKNOWN coded error naming
// the offending value in Details["store"].
func New(backend Backend, fs afero.Fs, root string) (Store, error) {
	switch backend {
	case BackendFS:
		return NewFS(fs, root), nil
	case BackendGit:
		return NewGit(fs, root)
	default:
		return nil, errors.NewCodedErrorWithDetails(errors.STORAGE_BACKEND_UNKNOWN,
			"unknown storage backend "+string(backend)+` (expected "fs" or "git")`,
			map[string]any{"store": string(backend)})
	}
}
