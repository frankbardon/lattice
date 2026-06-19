package storage

import (
	"encoding/json"
	"path"
	"strings"

	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
)

// fileExt is the on-disk extension for a stored document. A document with
// manifest.id "example-minimal" is stored as "example-minimal.json".
const fileExt = ".json"

// tempPrefix prefixes the temporary file used during an atomic Save. The temp
// file is written in full, then renamed over the destination so a crash never
// leaves a partially written document.
const tempPrefix = ".lattice-tmp-"

// FS is a filesystem-backed Store. It maps a document's manifest.id to
// <root>/<id>.json on an injected afero.Fs and writes atomically (temp file +
// rename). All filesystem access goes through the afero.Fs; the backend never
// calls os.* directly.
type FS struct {
	fs   afero.Fs
	root string
}

// NewFS constructs a filesystem-backed Store over fs rooted at root. fs is the
// filesystem abstraction (afero.NewOsFs() in production, afero.NewMemMapFs() in
// tests); root is the directory under which documents are stored.
func NewFS(fs afero.Fs, root string) *FS {
	return &FS{fs: fs, root: root}
}

// manifestEnvelope is the minimal shape parsed from a document to extract its
// addressing key. It is intentionally narrow — Save does not validate the
// document against the dashboard schema, it only reads manifest.id.
type manifestEnvelope struct {
	Manifest struct {
		ID string `json:"id"`
	} `json:"manifest"`
}

// Save persists document under <root>/<id>.json where id is the document's
// manifest.id. The id is validated for filename safety; the write is atomic via
// a temp file + rename.
func (s *FS) Save(document []byte) error {
	id, err := extractID(document)
	if err != nil {
		return err
	}
	if err := validateID(id); err != nil {
		return err
	}

	if err := s.fs.MkdirAll(s.root, 0o755); err != nil {
		return errors.WrapCodedErrorWithDetails(err, errors.STORAGE_IO,
			"failed creating storage root "+s.root, map[string]any{"id": id})
	}

	dest := s.pathFor(id)
	tmp, err := afero.TempFile(s.fs, s.root, tempPrefix)
	if err != nil {
		return errors.WrapCodedErrorWithDetails(err, errors.STORAGE_IO,
			"failed creating temp file for document "+id, map[string]any{"id": id})
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(document); err != nil {
		_ = tmp.Close()
		_ = s.fs.Remove(tmpName)
		return errors.WrapCodedErrorWithDetails(err, errors.STORAGE_IO,
			"failed writing document "+id, map[string]any{"id": id, "path": tmpName})
	}
	if err := tmp.Close(); err != nil {
		_ = s.fs.Remove(tmpName)
		return errors.WrapCodedErrorWithDetails(err, errors.STORAGE_IO,
			"failed closing document "+id, map[string]any{"id": id, "path": tmpName})
	}

	if err := s.fs.Rename(tmpName, dest); err != nil {
		_ = s.fs.Remove(tmpName)
		return errors.WrapCodedErrorWithDetails(err, errors.STORAGE_IO,
			"failed renaming document "+id+" into place", map[string]any{"id": id, "path": dest})
	}
	return nil
}

// Load returns the stored document bytes for id. A missing id returns a
// STORAGE_NOT_FOUND coded error.
func (s *FS) Load(id string) ([]byte, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	dest := s.pathFor(id)
	data, err := afero.ReadFile(s.fs, dest)
	if err != nil {
		if isNotExist(s.fs, dest) {
			return nil, errors.WrapCodedErrorWithDetails(err, errors.STORAGE_NOT_FOUND,
				"no stored document for id "+id, map[string]any{"id": id})
		}
		return nil, errors.WrapCodedErrorWithDetails(err, errors.STORAGE_IO,
			"failed reading document "+id, map[string]any{"id": id, "path": dest})
	}
	return data, nil
}

// List returns the manifest ids of all stored documents. Implemented in E1-S2.
func (s *FS) List() ([]string, error) {
	return nil, errors.NewCodedError(errors.STORAGE_INTERNAL, "List not implemented (E1-S2)")
}

// Exists reports whether a document with the given id is stored. Implemented in
// E1-S2.
func (s *FS) Exists(id string) (bool, error) {
	return false, errors.NewCodedError(errors.STORAGE_INTERNAL, "Exists not implemented (E1-S2)")
}

// Delete removes the stored document with the given id. Implemented in E1-S2.
func (s *FS) Delete(id string) error {
	return errors.NewCodedError(errors.STORAGE_INTERNAL, "Delete not implemented (E1-S2)")
}

// pathFor maps a (validated) id to its on-disk path under root.
func (s *FS) pathFor(id string) string {
	return path.Join(s.root, id+fileExt)
}

// extractID does a minimal parse of document to read manifest.id. It does not
// validate the document against the dashboard schema.
func extractID(document []byte) (string, error) {
	var env manifestEnvelope
	if err := json.Unmarshal(document, &env); err != nil {
		return "", errors.WrapCodedError(err, errors.STORAGE_INVALID,
			"failed parsing document to extract manifest.id")
	}
	return env.Manifest.ID, nil
}

// validateID rejects an id that cannot serve as a filename stem: empty or
// whitespace-only, containing a path separator, or a relative path element
// (".", ".."). A valid id maps directly to <id>.json.
func validateID(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.NewCodedError(errors.STORAGE_ID_INVALID,
			"document manifest.id is missing or empty")
	}
	if id == "." || id == ".." {
		return errors.NewCodedErrorWithDetails(errors.STORAGE_ID_INVALID,
			"document manifest.id is a relative path element", map[string]any{"id": id})
	}
	if strings.ContainsAny(id, `/\`) {
		return errors.NewCodedErrorWithDetails(errors.STORAGE_ID_INVALID,
			"document manifest.id contains a path separator", map[string]any{"id": id})
	}
	return nil
}

// isNotExist reports whether dest does not exist on fs, used to distinguish a
// not-found Load from a genuine I/O failure.
func isNotExist(fs afero.Fs, dest string) bool {
	ok, err := afero.Exists(fs, dest)
	return err == nil && !ok
}
