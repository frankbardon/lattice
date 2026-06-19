package storage

import (
	stderrors "errors"
	"io"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/frankbardon/lattice/errors"
)

// Git implements the optional VersionedStore capability: the working-tree repo
// records a commit per Save/Delete, so the read side can list a document's
// revisions and load it as of any one of them.
//
// The revision identifier is the FULL git commit hash. History returns full
// hashes; LoadAt accepts a full hash, or a short hash that go-git can resolve
// unambiguously to a single commit.

// History returns the revisions that touched <id>.json, newest-first. It walks
// the commit log filtered to the document's path. An id that no commit ever
// touched (never saved, or an unknown id) returns a STORAGE_NOT_FOUND coded
// error rather than an empty slice — an empty history is indistinguishable from
// an unknown document, and callers expect not-found for "nothing here".
func (g *Git) History(id string) ([]Revision, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}

	rel := id + fileExt
	head, err := g.repo.Head()
	if err != nil {
		// No HEAD means an empty repository: nothing has ever been committed, so
		// no document has any history.
		if stderrors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil, errors.WrapCodedErrorWithDetails(err, errors.STORAGE_NOT_FOUND,
				"no history for id "+id, map[string]any{"id": id})
		}
		return nil, errors.WrapCodedErrorWithDetails(err, errors.STORAGE_IO,
			"failed reading git HEAD", map[string]any{"id": id, "path": g.root})
	}

	path := rel
	iter, err := g.repo.Log(&git.LogOptions{From: head.Hash(), PathFilter: func(p string) bool {
		return p == path
	}})
	if err != nil {
		return nil, errors.WrapCodedErrorWithDetails(err, errors.STORAGE_IO,
			"failed reading git log for "+rel, map[string]any{"id": id, "path": rel})
	}
	defer iter.Close()

	var revs []Revision
	err = iter.ForEach(func(c *object.Commit) error {
		revs = append(revs, Revision{
			Hash:      c.Hash.String(),
			Message:   c.Message,
			Timestamp: c.Author.When,
		})
		return nil
	})
	if err != nil {
		return nil, errors.WrapCodedErrorWithDetails(err, errors.STORAGE_IO,
			"failed walking git log for "+rel, map[string]any{"id": id, "path": rel})
	}

	if len(revs) == 0 {
		return nil, errors.NewCodedErrorWithDetails(errors.STORAGE_NOT_FOUND,
			"no history for id "+id, map[string]any{"id": id})
	}
	return revs, nil
}

// LoadAt returns the bytes of <id>.json as of the given revision (a git commit
// hash, full or unambiguous-short). A revision that resolves to no commit
// returns STORAGE_NOT_FOUND; a revision at which the document does not exist in
// the commit's tree also returns STORAGE_NOT_FOUND.
func (g *Git) LoadAt(id, revision string) ([]byte, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}

	hash, err := g.repo.ResolveRevision(plumbing.Revision(revision))
	if err != nil {
		return nil, errors.WrapCodedErrorWithDetails(err, errors.STORAGE_NOT_FOUND,
			"unknown revision "+revision, map[string]any{"id": id, "revision": revision})
	}

	commit, err := g.repo.CommitObject(*hash)
	if err != nil {
		return nil, errors.WrapCodedErrorWithDetails(err, errors.STORAGE_NOT_FOUND,
			"unknown revision "+revision, map[string]any{"id": id, "revision": revision})
	}

	rel := id + fileExt
	file, err := commit.File(rel)
	if err != nil {
		if stderrors.Is(err, object.ErrFileNotFound) {
			return nil, errors.WrapCodedErrorWithDetails(err, errors.STORAGE_NOT_FOUND,
				"no document "+id+" at revision "+revision,
				map[string]any{"id": id, "revision": revision, "path": rel})
		}
		return nil, errors.WrapCodedErrorWithDetails(err, errors.STORAGE_IO,
			"failed reading "+rel+" at revision "+revision,
			map[string]any{"id": id, "revision": revision, "path": rel})
	}

	reader, err := file.Reader()
	if err != nil {
		return nil, errors.WrapCodedErrorWithDetails(err, errors.STORAGE_IO,
			"failed opening "+rel+" at revision "+revision,
			map[string]any{"id": id, "revision": revision, "path": rel})
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, errors.WrapCodedErrorWithDetails(err, errors.STORAGE_IO,
			"failed reading "+rel+" at revision "+revision,
			map[string]any{"id": id, "revision": revision, "path": rel})
	}
	return data, nil
}

// Static assertion: Git satisfies the optional VersionedStore capability (and,
// transitively, the core Store).
var _ VersionedStore = (*Git)(nil)
