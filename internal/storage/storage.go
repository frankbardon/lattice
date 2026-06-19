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
