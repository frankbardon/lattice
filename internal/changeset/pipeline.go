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
// THE REVISION PRECONDITION (E4-S2). Optimistic concurrency: when the caller
// supplies an expected revision (WithExpectedRevision), the pipeline re-reads the
// store's CURRENT revision (storage.RevisionedStore.Revision) IMMEDIATELY BEFORE
// Save — as close to the write as possible to minimize the race window — and
// rejects with CHANGESET_REVISION_CONFLICT if it no longer matches, so a stale
// edit cannot clobber a document changed since it was loaded. The conflict code is
// distinct so callers can retry. When no expected revision is supplied the
// behavior is unchanged from E1 (single-writer); a store that cannot expose a
// revision but was handed an expected one fails with CHANGESET_REVISION_UNSUPPORTED
// rather than silently skipping the precondition. RFC 6902 `test` ops are the
// SECOND precondition lever: they are evaluated by the applier during apply.go and
// a failing `test` aborts the whole changeset (PATCH_APPLY_FAILED) — nothing
// persisted — just like any other apply failure.

import (
	"github.com/frankbardon/lattice/errors"
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
	// at (the optimistic-concurrency precondition). An empty string means "no
	// precondition." When set, the pipeline re-reads the store's current revision
	// just before Save and rejects on mismatch (CHANGESET_REVISION_CONFLICT).
	expectedRevision string
	// expectRevisionSet records whether WithExpectedRevision was applied at all, so
	// an explicitly-supplied EMPTY token is still a precondition (and is enforced)
	// rather than being indistinguishable from "no precondition."
	expectRevisionSet bool
}

// ApplyOption configures an ApplyChangeset call. Options are the stable seam for
// additive apply inputs (the E4 revision precondition is the first) so the
// ApplyChangeset signature does not churn as the pipeline grows.
type ApplyOption func(*applyOptions)

// WithExpectedRevision records the revision the caller expects the stored document
// to currently be at — the optimistic-concurrency precondition (E4-S2). When
// supplied, the pipeline re-reads the store's current revision immediately before
// Save and rejects the apply with CHANGESET_REVISION_CONFLICT if it differs, so a
// stale edit cannot overwrite a document changed since it was loaded. Omitting it
// leaves behavior unchanged (single-writer). The token is OPAQUE and compared as-is
// against the store's RevisionedStore.Revision output.
func WithExpectedRevision(revision string) ApplyOption {
	return func(o *applyOptions) {
		o.expectedRevision = revision
		o.expectRevisionSet = true
	}
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
// ApplyOptions carry the E4 revision precondition (WithExpectedRevision): when set,
// the store's current revision is re-read immediately before Save and a mismatch
// rejects the apply with CHANGESET_REVISION_CONFLICT.
func ApplyChangeset(store storage.Store, res DocumentResolver, id string, cs *Changeset, opts ...ApplyOption) (*ApplyResult, error) {
	var options applyOptions
	for _, opt := range opts {
		opt(&options)
	}

	// Run the shared validate core (load → resolve → apply → re-resolve under every
	// guardrail). On any coded error nothing is persisted; the result is the
	// would-be-mutated bytes and their resolved tree.
	result, err := validate(store, res, id, cs)
	if err != nil {
		return nil, err
	}

	// Optimistic-concurrency precondition (E4-S2): if the caller supplied an expected
	// revision, re-read the store's CURRENT revision NOW — immediately before Save, as
	// close to the write as possible to minimize the race window — and reject if it no
	// longer matches. This is the last gate before the only write, so a stale edit
	// cannot clobber a document that changed since it was loaded.
	if err := checkRevisionPrecondition(store, id, options); err != nil {
		return nil, err
	}

	// Every guardrail has passed: persist the validated bytes. Save is reached
	// EXACTLY ONCE, only on full success, so a rejected apply leaves the stored
	// document byte-for-byte unchanged.
	if err := store.Save(result.Document); err != nil {
		return nil, err
	}

	return result, nil
}

// DryRunChangeset runs the SAME validate core as ApplyChangeset — load the stored
// document, resolve the current bytes for the field-edit surfaces, apply the
// changeset under the field-edit guardrail, and re-resolve the mutated document for
// the structural/constraint/schema guardrails and RFC 6902 `test` ops — but STOPS
// before persisting. It NEVER reaches Store.Save and NEVER reads or enforces a
// revision precondition (which exists only to guard the write), so the store is
// touched exactly once, by the read-only Load, and is left byte-for-byte unchanged
// no matter the outcome.
//
// It returns the would-be-mutated, canonically-serialized document bytes plus their
// resolved tree — the result a real ApplyChangeset WOULD have persisted — or, on any
// coded error from a guardrail, the SAME *errors.CodedError that ApplyChangeset
// would have returned at that step, propagated verbatim. This is the engine behind
// the facade's dry-run/validate primitive: callers can preview and validate an edit
// without any risk of mutating the stored document.
func DryRunChangeset(store storage.Store, res DocumentResolver, id string, cs *Changeset) (*ApplyResult, error) {
	return validate(store, res, id, cs)
}

// validate is the shared apply+re-resolve core of the patch pipeline, factored out
// so the real save path (ApplyChangeset) and the dry-run path (DryRunChangeset) run
// IDENTICAL guardrails without duplicating the load→resolve→apply→re-resolve dance.
// It performs NO persistence and enforces NO revision precondition — those belong to
// the write and stay in ApplyChangeset. On full success it returns the validated,
// canonically-marshaled document bytes and the resolved tree of those bytes; on any
// coded error it returns nil and the error verbatim, having touched the store only
// via the read-only Load.
func validate(store storage.Store, res DocumentResolver, id string, cs *Changeset) (*ApplyResult, error) {
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

	return &ApplyResult{Document: mutated, Resolved: mutatedTree}, nil
}

// checkRevisionPrecondition enforces the optional optimistic-concurrency
// precondition just before Save. When no expected revision was supplied it is a
// no-op (single-writer behavior, unchanged from E1). When one was supplied it
// requires the store to implement storage.RevisionedStore (else
// CHANGESET_REVISION_UNSUPPORTED — the caller asked for a precondition that cannot
// be enforced), re-reads the current revision token, and rejects with
// CHANGESET_REVISION_CONFLICT if it differs from the expected token. Tokens are
// opaque and compared verbatim.
func checkRevisionPrecondition(store storage.Store, id string, options applyOptions) error {
	if !options.expectRevisionSet {
		return nil
	}

	revStore, ok := store.(storage.RevisionedStore)
	if !ok {
		return errors.NewCodedErrorWithDetails(errors.CHANGESET_REVISION_UNSUPPORTED,
			"an expected revision was supplied but the store cannot report a current revision to enforce it",
			map[string]any{"id": id})
	}

	current, err := revStore.Revision(id)
	if err != nil {
		return err
	}
	if current != options.expectedRevision {
		return errors.NewCodedErrorWithDetails(errors.CHANGESET_REVISION_CONFLICT,
			"the stored document's revision no longer matches the expected revision; reload and retry",
			map[string]any{"id": id, "expected": options.expectedRevision, "current": current})
	}
	return nil
}
