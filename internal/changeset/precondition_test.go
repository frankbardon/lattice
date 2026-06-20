package changeset

// Precondition test DOUBLES and the cases that need no real backend. The
// backend-parametrized end-to-end precondition scenarios (the passing/failing
// `test` op, the matching-revision happy path, and the stale-revision conflict
// across both FS and git) live in concurrency_test.go (E4-S3); this file owns the
// two stubs those scenarios reuse plus the one case that is purely about an
// incapable store, where no real backend is involved.

import (
	"testing"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/storage"
)

// concurrentSaveStore wraps a RevisionedStore to simulate a concurrent writer: the
// FIRST Revision() call (the pipeline's pre-Save re-read) triggers an injected Save
// of `intruder` bytes, so the revision the pipeline observes reflects a document
// that changed AFTER the apply loaded it — exactly the race the precondition
// guards. Subsequent reads see the post-intrusion revision. It is driven by
// TestConcurrency_RevisionConflict over both backends.
type concurrentSaveStore struct {
	storage.RevisionedStore
	intruder []byte
	tripped  bool
}

func (c *concurrentSaveStore) Revision(id string) (string, error) {
	if !c.tripped {
		c.tripped = true
		if err := c.RevisionedStore.Save(c.intruder); err != nil {
			return "", err
		}
	}
	return c.RevisionedStore.Revision(id)
}

// nonRevisionedStore is a Store that does NOT implement RevisionedStore, used to
// prove an expected revision against an incapable store is rejected rather than
// silently ignored.
type nonRevisionedStore struct{ storage.Store }

// TestApplyChangeset_UnsupportedRevisionStoreRejected proves that supplying an
// expected revision to a store that cannot report a current revision fails with
// CHANGESET_REVISION_UNSUPPORTED — the precondition the caller asked for cannot be
// enforced, so the apply is rejected rather than skipping the check. This is purely
// about the store's capability, so it needs no real backend: an FS store wrapped to
// hide its RevisionedStore implementation suffices.
func TestApplyChangeset_UnsupportedRevisionStoreRejected(t *testing.T) {
	res := newResolver(t)
	base, _ := seedStore(t, res)
	store := nonRevisionedStore{Store: base}

	cs := parse(t, `[{"op":"replace","path":"/$manifest/title","value":"Renamed"}]`)
	_, err := ApplyChangeset(store, res, fixtureID, cs, WithExpectedRevision("any-token"))
	hasCode(t, err, errors.CHANGESET_REVISION_UNSUPPORTED)
}
