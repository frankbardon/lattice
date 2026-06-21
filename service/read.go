package service

import (
	"github.com/frankbardon/lattice/internal/variables"
)

// Resolve loads the document addressed by id through the wired store, then runs
// the two-pass resolver over its bytes with the given runtime overrides. It is
// the backend-addressed load path: store.Load(id) yields the whole-document
// bytes and ResolveBytesWithValues runs the identical pipeline ResolveBytes
// would, with id doubling as the error source carried into error Details.
//
// overrides is an addressable override map keyed by override address — a bare
// name targets a settable variable; a "<node-id>.<field>" address targets a node
// config field — carried verbatim into variables.OverrideSet. A nil/empty map
// applies no overrides.
//
// A not-found id surfaces as the store's STORAGE_NOT_FOUND *errors.CodedError;
// resolution failures surface as the resolver's RESOLVE_*/SCHEMA_*/VAR_* coded
// errors. Errors propagate verbatim from the cores — they are not re-wrapped.
func (s *Service) Resolve(id string, overrides map[string]any) (*ResolvedTree, error) {
	data, err := s.store.Load(id)
	if err != nil {
		return nil, err
	}
	return s.resolver.ResolveBytesWithValues(data, id, variables.OverrideSet(overrides))
}

// ResolveBytes resolves in-memory document bytes with the given runtime
// overrides without touching the store — the WASM/optimistic-local path where
// the document already lives in memory. It runs the identical two-pass pipeline
// Resolve uses.
//
// src is the document's origin label carried into error Details (the source
// param of the resolver); it is not read from disk. overrides is the addressable
// override map (see Resolve) carried verbatim into variables.OverrideSet; a
// nil/empty map applies no overrides.
//
// Resolution failures surface as the resolver's RESOLVE_*/SCHEMA_*/VAR_* coded
// errors, propagated verbatim from the cores.
func (s *Service) ResolveBytes(b []byte, src string, overrides map[string]any) (*ResolvedTree, error) {
	return s.resolver.ResolveBytesWithValues(b, src, variables.OverrideSet(overrides))
}

// Load returns the raw stored document bytes for the given manifest id — a
// passthrough to the wired store. A missing id surfaces as the store's
// STORAGE_NOT_FOUND *errors.CodedError, propagated verbatim.
func (s *Service) Load(id string) ([]byte, error) {
	return s.store.Load(id)
}

// List returns the manifest ids of all stored documents — a passthrough to the
// wired store. Failures surface as the store's STORAGE_* coded errors.
func (s *Service) List() ([]string, error) {
	return s.store.List()
}

// Exists reports whether a document with the given manifest id is stored — a
// passthrough to the wired store. Failures surface as the store's STORAGE_*
// coded errors.
func (s *Service) Exists(id string) (bool, error) {
	return s.store.Exists(id)
}
