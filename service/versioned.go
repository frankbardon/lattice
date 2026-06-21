package service

import (
	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/storage"
)

// History returns the revisions that touched the document addressed by id,
// newest-first — the read-side version history. It is a CAPABILITY-GATED method:
// version history is the OPTIONAL VersionedStore capability, implemented by the
// git backend but not the filesystem backend. The wired store is detected by
// type assertion; when it satisfies VersionedStore the call delegates and the
// core's STORAGE_* coded errors (e.g. STORAGE_NOT_FOUND for an id with no
// history) propagate verbatim. When it does not, the call is rejected with a
// STORAGE_CAPABILITY_UNSUPPORTED *errors.CodedError carrying Details["id"]
// rather than silently degrading.
func (s *Service) History(id string) ([]Revision, error) {
	vs, ok := s.store.(storage.VersionedStore)
	if !ok {
		return nil, errors.NewCodedErrorWithDetails(
			errors.STORAGE_CAPABILITY_UNSUPPORTED,
			"store does not support version history (History): the configured backend is not a VersionedStore",
			map[string]any{"id": id},
		)
	}
	return vs.History(id)
}

// LoadAt returns the document bytes for id as of the given revision — a read of a
// historical version. It is a CAPABILITY-GATED method on the same OPTIONAL
// VersionedStore capability as History (git backend only). The wired store is
// detected by type assertion; when it satisfies VersionedStore the call delegates
// and the core's STORAGE_* coded errors (e.g. STORAGE_NOT_FOUND for a revision
// that resolves to no commit, or at which the document does not exist) propagate
// verbatim. When it does not, the call is rejected with a
// STORAGE_CAPABILITY_UNSUPPORTED *errors.CodedError carrying Details["id"].
func (s *Service) LoadAt(id, revision string) ([]byte, error) {
	vs, ok := s.store.(storage.VersionedStore)
	if !ok {
		return nil, errors.NewCodedErrorWithDetails(
			errors.STORAGE_CAPABILITY_UNSUPPORTED,
			"store does not support version history (LoadAt): the configured backend is not a VersionedStore",
			map[string]any{"id": id},
		)
	}
	return vs.LoadAt(id, revision)
}

// Revision returns the current opaque revision token for the document addressed
// by id — the value a caller pairs with WithExpectedRevision for an
// optimistic-concurrency Patch. It is a CAPABILITY-GATED method: a
// current-revision token is the OPTIONAL RevisionedStore capability. Both shipped
// backends (filesystem and git) implement it, but a custom/injected store may
// not. The wired store is detected by type assertion; when it satisfies
// RevisionedStore the call delegates and the core's STORAGE_* coded errors (e.g.
// STORAGE_NOT_FOUND for an unknown id) propagate verbatim. When it does not, the
// call is rejected with a STORAGE_CAPABILITY_UNSUPPORTED *errors.CodedError
// carrying Details["id"]. The returned token is opaque: treat it as compare-only
// and never parse it.
func (s *Service) Revision(id string) (string, error) {
	rs, ok := s.store.(storage.RevisionedStore)
	if !ok {
		return "", errors.NewCodedErrorWithDetails(
			errors.STORAGE_CAPABILITY_UNSUPPORTED,
			"store does not support current-revision lookup (Revision): the configured backend is not a RevisionedStore",
			map[string]any{"id": id},
		)
	}
	return rs.Revision(id)
}
