package changeset

// This file owns the PUBLIC APPLY PIPELINE (E1-S3): the single reusable entry
// point — ApplyChangeset — that every touchpoint (the `lattice patch` CLI, future
// callers) wraps. It closes the loop the pure apply engine (apply.go) leaves open:
// it owns the load, the read-only resolve that yields the surfaces the field-edit
// guardrail checks against, the apply, the RE-RESOLVE of the mutated bytes (the
// structural/constraint guardrail "for free"), and the Save — all under one
// ATOMIC contract.
//
// THE PIPELINE. Store.Load(id) -> resolve the CURRENT bytes read-only (to get the
// configurable surfaces) -> apply the translated changeset under the field-edit
// guardrail + canonical re-marshal (applyToBytes) -> RE-RESOLVE the mutated bytes
// (ResolveBytesWithValues): the full two-pass resolver runs again over the patched
// document, so the structural guardrail (grammar), the schema, referential
// integrity, and variable/interpolation validity are all re-checked. ON ANY CODED
// ERROR at any step the whole apply is rejected and NOTHING is persisted; only a
// fully validated result reaches Store.Save. Because the store is touched exactly
// once, on success, after every check passes, an invalid changeset or an invalid
// result leaves the stored document BYTE-FOR-BYTE unchanged.
//
// THE REVISION-PRECONDITION SEAM (E4). Concurrency control — rejecting an apply
// whose expected revision no longer matches the store's current revision — is a
// later epic. This story leaves a clean seam (WithExpectedRevision) that records
// the caller's expected revision on the apply but DOES NOT check it: E4 wires the
// check in just before Save without changing this signature.

import (
	"github.com/frankbardon/lattice/internal/resolver"
	"github.com/frankbardon/lattice/internal/storage"
	"github.com/frankbardon/lattice/internal/variables"
)

// DocumentResolver is the resolver capability the apply pipeline needs: re-run the
// full two-pass validation over in-memory document bytes. *resolver.Resolver
// satisfies it (via ResolveBytesWithValues). It is an interface so the pipeline
// depends only on the validation contract — not on resolver construction — and so
// tests can drive it with a stub. source is carried into error Details (the
// manifest id); overrides are the runtime-override set (nil for the persist path).
type DocumentResolver interface {
	ResolveBytesWithValues(docBytes []byte, source string, overrides variables.OverrideSet) (*resolver.ResolvedTree, error)
}

// ApplyResult is the outcome of a successful ApplyChangeset: the validated,
// canonically-serialized document bytes that were persisted, plus the resolved
// tree of that persisted document (the re-resolution the pipeline already computed,
// returned so callers need not resolve again). Both describe the document AS
// PERSISTED — the same bytes Store.Save received.
type ApplyResult struct {
	// Document is the validated, canonically-marshaled document bytes that were
	// persisted via Store.Save.
	Document []byte
	// Resolved is the resolved tree of the persisted document — the result of the
	// re-resolution the pipeline ran before saving.
	Resolved *resolver.ResolvedTree
}

// applyOptions carries the optional inputs of an apply. It is populated by the
// functional ApplyOption set and read by ApplyChangeset.
type applyOptions struct {
	// expectedRevision is the revision the caller expects the stored document to be
	// at (the optimistic-concurrency precondition). RECORDED but NOT checked here —
	// the revision-precondition check is E4. An empty string means "no precondition."
	expectedRevision string
}

// ApplyOption configures an ApplyChangeset call. Options are the stable seam for
// additive apply inputs (the E4 revision precondition is the first) so the
// ApplyChangeset signature does not churn as the pipeline grows.
type ApplyOption func(*applyOptions)

// WithExpectedRevision records the revision the caller expects the stored document
// to currently be at — the optimistic-concurrency precondition. It is the E4 seam:
// the expected revision is CARRIED on the apply but NOT yet checked (E4 wires the
// expected-vs-current comparison in just before Save). Passing it today is a no-op
// beyond being recorded.
func WithExpectedRevision(revision string) ApplyOption {
	return func(o *applyOptions) { o.expectedRevision = revision }
}

// ApplyChangeset is the single reusable entry point of the patch-write pipeline:
// it loads the document addressed by id from store, applies the changeset cs under
// the field-edit guardrail, re-resolves the mutated document for the structural and
// constraint guardrails, and — only if every check passes — persists the validated
// bytes back to store. It returns the persisted document and its resolved tree, or
// a coded error.
//
// It is ATOMIC: on any error — a not-found id, a malformed/off-surface/ill-typed
// changeset, an apply failure, or a mutated document that fails re-resolution —
// the apply is rejected and NOTHING is written, so the stored document is left
// byte-for-byte unchanged. res re-runs the two-pass resolver (over the current
// bytes for the surfaces, and over the mutated bytes for the structural guardrail);
// *resolver.Resolver satisfies it.
//
// Coded errors propagate verbatim from the step that produced them and identify the
// offending op/path in Details where the step can (the guardrail, the translator,
// and the applier all carry the pointer and operation index). The optional
// ApplyOptions carry the E4 revision precondition (recorded, not yet checked).
func ApplyChangeset(store storage.Store, res DocumentResolver, id string, cs *Changeset, opts ...ApplyOption) (*ApplyResult, error) {
	var options applyOptions
	for _, opt := range opts {
		opt(&options)
	}
	// options.expectedRevision is the E4 precondition seam: recorded above, checked
	// by a later story just before Save. Intentionally unused here.
	_ = options.expectedRevision

	// Load the current document bytes. A missing id surfaces as the store's
	// STORAGE_NOT_FOUND coded error.
	current, err := store.Load(id)
	if err != nil {
		return nil, err
	}

	// Resolve the CURRENT bytes read-only: this yields the configurable surfaces the
	// field-edit guardrail is checked against, and it must be the resolution of the
	// exact bytes being patched. A current document that no longer resolves fails
	// fast here (nothing is patched or persisted).
	currentTree, err := res.ResolveBytesWithValues(current, id, nil)
	if err != nil {
		return nil, err
	}

	// Apply the changeset under the field-edit guardrail and canonically re-marshal.
	// Pure: produces the mutated bytes without touching the store.
	mutated, err := applyToBytes(current, cs, currentTree)
	if err != nil {
		return nil, err
	}

	// Re-resolve the MUTATED document: the full two-pass resolver runs again, so the
	// grammar (structural guardrail), the schema, referential integrity, and
	// variable/interpolation validity are all re-checked on the patched result. Any
	// coded error rejects the whole apply — nothing is persisted.
	mutatedTree, err := res.ResolveBytesWithValues(mutated, id, nil)
	if err != nil {
		return nil, err
	}

	// Every guardrail has passed: persist the validated bytes. Save is reached
	// EXACTLY ONCE, only on full success, so a rejected apply leaves the stored
	// document byte-for-byte unchanged.
	if err := store.Save(mutated); err != nil {
		return nil, err
	}

	return &ApplyResult{Document: mutated, Resolved: mutatedTree}, nil
}
