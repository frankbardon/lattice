package service

import (
	"github.com/frankbardon/lattice/internal/changeset"
)

// ParseChangeset parses and validates RFC 6902 JSON Patch bytes into a Changeset
// addressed to a single document — a thin wrapper over changeset.Parse. It is the
// only way external callers obtain a *Changeset, which carries unexported state
// and so cannot be constructed directly (an opaque handle); the parsed value is
// then handed to Patch.
//
// Malformed patch bytes or an invalid op set surface as the parser's PATCH_*
// *errors.CodedError, propagated verbatim from the core.
func (s *Service) ParseChangeset(b []byte) (*Changeset, error) {
	return changeset.Parse(b)
}

// Patch applies the changeset cs to the stored document addressed by id and, only
// if every guardrail passes, persists the validated result — a thin wrapper over
// the atomic changeset.ApplyChangeset pipeline. It does NOT reimplement the
// load → resolve → apply → re-resolve → save dance: the wired store and resolver
// are passed straight through, so atomicity (a rejected apply persists nothing)
// and byte-faithfulness are inherited from the pipeline. On success it returns the
// ApplyResult — the persisted document bytes plus their resolved tree.
//
// opts carry additive apply inputs; WithExpectedRevision supplies the optimistic-
// concurrency precondition and flows through unchanged. When supplied, the store's
// current revision is re-read immediately before the write and a mismatch rejects
// with CHANGESET_REVISION_CONFLICT; a store that cannot report a revision fails
// with CHANGESET_REVISION_UNSUPPORTED.
//
// All failures — a not-found id, a malformed/off-surface/ill-typed changeset, an
// apply or re-resolution failure, or a revision conflict — surface as
// *errors.CodedError (STORAGE_NOT_FOUND, PATCH_*, RESOLVE_*/SCHEMA_*/VAR_*,
// CHANGESET_REVISION_*) propagated verbatim from the pipeline; they are not
// re-wrapped or re-coded.
func (s *Service) Patch(id string, cs *Changeset, opts ...ApplyOption) (*ApplyResult, error) {
	return changeset.ApplyChangeset(s.store, s.resolver, id, cs, opts...)
}
