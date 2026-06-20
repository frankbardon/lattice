// Package storage provides whole-document persistence for dashboard documents.
//
// A Store is a DUMB BLOB STORE: it reads and writes whole dashboard documents as
// raw bytes, addressed by the document's manifest.id. It has no JSON Patch
// awareness and performs no schema validation — it sits upstream of the resolver
// and treats every document as an opaque blob. Operating on []byte keeps the
// store byte-faithful: a document Saved then Loaded is byte-identical, which
// matters for clean git diffs in later efforts.
//
// The addressing key is the document's manifest.id, never a separate argument:
// Save derives the key from the document itself. This is what makes backends
// interchangeable — callers persist a document without choosing where its bytes
// land.
package storage

import "time"

// Store is the contract every persistence backend satisfies. Backends (the
// filesystem backend here, a git backend later) read and write whole dashboard
// documents addressed by manifest.id.
//
// All methods return errors as *errors.CodedError from the lattice errors
// package (STORAGE_* codes), so failures carry a stable code and structured
// details for the --json CLI path.
type Store interface {
	// Load returns the stored document bytes for the given manifest id. A
	// missing id returns a STORAGE_NOT_FOUND coded error.
	Load(id string) ([]byte, error)

	// Save persists a whole dashboard document. The addressing key is derived
	// from the document's manifest.id (not a separate argument); an absent or
	// filename-unsafe id returns a STORAGE_ID_INVALID coded error. The write is
	// atomic: a crash never leaves a partially written document.
	Save(document []byte) error

	// List returns the manifest ids of all stored documents.
	List() ([]string, error)

	// Exists reports whether a document with the given manifest id is stored.
	Exists(id string) (bool, error)

	// Delete removes the stored document with the given manifest id. A missing
	// id returns a STORAGE_NOT_FOUND coded error.
	Delete(id string) error
}

// Revision identifies one stored version of a document and carries the minimum
// metadata a history view needs. The Hash is the revision identifier (a git
// commit hash for the git backend) accepted by VersionedStore.LoadAt. It is the
// FULL hash — short hashes are not produced here, though LoadAt also accepts a
// short hash that unambiguously resolves to a commit.
type Revision struct {
	// Hash is the full revision identifier (a 40-char git commit hash).
	Hash string
	// Message is the commit message that recorded this revision.
	Message string
	// Timestamp is when this revision was committed.
	Timestamp time.Time
}

// VersionedStore is an OPTIONAL capability a backend may implement on top of the
// core Store contract to expose read-side version history. Only version-capable
// backends (the git backend) implement it; the filesystem backend does not.
// Callers detect the capability with a type assertion:
//
//	if vs, ok := store.(storage.VersionedStore); ok { … }
//
// All methods return errors as *errors.CodedError from the lattice errors
// package (STORAGE_* codes), matching the core Store.
type VersionedStore interface {
	Store

	// History returns the revisions that touched the document with the given
	// manifest id, newest-first. An id that no stored document ever matched
	// returns a STORAGE_NOT_FOUND coded error (an empty, never-committed
	// history is reported as not-found, not an empty slice).
	History(id string) ([]Revision, error)

	// LoadAt returns the document bytes as of the given revision. The revision
	// is a git commit hash (full, or a short hash that unambiguously resolves).
	// A revision that resolves to no commit returns a STORAGE_NOT_FOUND coded
	// error; a revision at which the document does not exist also returns
	// STORAGE_NOT_FOUND.
	LoadAt(id, revision string) ([]byte, error)
}

// RevisionedStore is an OPTIONAL capability a backend may implement on top of the
// core Store contract to expose a single "current revision" token for a stored
// document. Unlike VersionedStore (git-only, read-side history), BOTH backends
// implement RevisionedStore — the filesystem backend derives a content hash and
// the git backend reuses its commit history — so a precondition check ("the
// document I edited is still the one on disk") works uniformly across backends.
//
// Callers detect the capability with a type assertion:
//
//	if rs, ok := store.(storage.RevisionedStore); ok { … }
//
// The token is OPAQUE: callers must treat it as compare-only and never parse it.
// It is stable for an unchanged document and changes whenever the document's
// stored bytes change. Its concrete form differs per backend (a git commit hash
// for the git backend, a content hash for the filesystem backend); the only
// guarantees are stability and change-detection, not a particular format.
//
// All methods return errors as *errors.CodedError from the lattice errors
// package (STORAGE_* codes), matching the core Store.
type RevisionedStore interface {
	Store

	// Revision returns the current revision token for the document with the
	// given manifest id. An id that no stored document matches returns a
	// STORAGE_NOT_FOUND coded error. The returned token is stable across reads
	// of an unchanged document and changes after any Save that alters the
	// document's stored bytes.
	Revision(id string) (string, error)
}
