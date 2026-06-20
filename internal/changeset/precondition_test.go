package changeset

// E4-S2 unit coverage for the apply pipeline's two concurrency preconditions:
//   - RFC 6902 `test` ops as preconditions (a passing test proceeds; a failing test
//     aborts — the failing case is in e2e_test.go's rejected-edits table, the
//     passing case is here).
//   - The optimistic-concurrency revision precondition (WithExpectedRevision):
//     matching revision proceeds; a mismatch — simulating a concurrent Save between
//     load and write — rejects with CHANGESET_REVISION_CONFLICT and persists
//     nothing; a store that cannot report a revision rejects with
//     CHANGESET_REVISION_UNSUPPORTED.
//
// These drive the public ApplyChangeset over a real RevisionedStore (FS over
// MemMapFs) plus small test doubles that let a test pin the current-revision token
// and intercept Save.

import (
	"bytes"
	"testing"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/storage"
)

// TestApplyChangeset_PassingTestOpProceeds proves a satisfied RFC 6902 `test` op is
// a no-op precondition the apply flows past: a `test` asserting the current title,
// followed by a `replace`, applies and persists the replacement.
func TestApplyChangeset_PassingTestOpProceeds(t *testing.T) {
	res := newResolver(t)
	store, _ := seedStore(t, res)

	cs := parse(t, `[
		{"op":"test","path":"/$manifest/title","value":"Minimal Example Dashboard"},
		{"op":"replace","path":"/$manifest/title","value":"Renamed"}
	]`)
	result, err := ApplyChangeset(store, res, fixtureID, cs)
	if err != nil {
		t.Fatalf("ApplyChangeset with a passing test op: %v", err)
	}
	if title := readField(t, result.Document, "manifest", "title"); title != "Renamed" {
		t.Fatalf("expected the replace to apply after the satisfied test op, got %v", title)
	}
}

// TestApplyChangeset_MatchingRevisionProceeds proves the precondition is satisfied
// when the supplied expected revision equals the store's current revision: the
// apply persists normally. The expected token is read from the seeded store's own
// current revision.
func TestApplyChangeset_MatchingRevisionProceeds(t *testing.T) {
	res := newResolver(t)
	store, _ := seedStore(t, res)

	rs := store.(storage.RevisionedStore)
	current, err := rs.Revision(fixtureID)
	if err != nil {
		t.Fatalf("read current revision: %v", err)
	}

	cs := parse(t, `[{"op":"replace","path":"/$manifest/title","value":"Renamed"}]`)
	result, err := ApplyChangeset(store, res, fixtureID, cs, WithExpectedRevision(current))
	if err != nil {
		t.Fatalf("ApplyChangeset with a matching expected revision: %v", err)
	}
	if title := readField(t, result.Document, "manifest", "title"); title != "Renamed" {
		t.Fatalf("matching-revision apply did not persist the edit, got %v", title)
	}
}

// concurrentSaveStore wraps a RevisionedStore to simulate a concurrent writer: the
// FIRST Revision() call (the pipeline's pre-Save re-read) triggers an injected Save
// of `intruder` bytes, so the revision the pipeline observes reflects a document
// that changed AFTER the apply loaded it — exactly the race the precondition
// guards. Subsequent reads see the post-intrusion revision.
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

// TestApplyChangeset_MismatchedRevisionRejected proves a stale edit is rejected:
// the caller's expected revision is the document's revision at load time, but a
// concurrent Save lands between load/apply and the pre-Save revision re-read, so the
// current revision differs. The apply is rejected with CHANGESET_REVISION_CONFLICT
// and the concurrent writer's bytes remain — the stale edit persisted nothing.
func TestApplyChangeset_MismatchedRevisionRejected(t *testing.T) {
	res := newResolver(t)
	base, seed := seedStore(t, res)

	rs := base.(storage.RevisionedStore)
	expected, err := rs.Revision(fixtureID)
	if err != nil {
		t.Fatalf("read load-time revision: %v", err)
	}

	// The intruder is the seed with a different manifest title — a real byte change,
	// so the FS content-hash revision differs from `expected`.
	intruder := bytes.Replace(seed, []byte(`"Minimal Example Dashboard"`), []byte(`"Intruder Edit"`), 1)
	if bytes.Equal(intruder, seed) {
		t.Fatalf("test setup: intruder bytes did not differ from the seed")
	}
	store := &concurrentSaveStore{RevisionedStore: rs, intruder: intruder}

	cs := parse(t, `[{"op":"replace","path":"/$manifest/title","value":"Stale Edit"}]`)
	_, err = ApplyChangeset(store, res, fixtureID, cs, WithExpectedRevision(expected))
	hasCode(t, err, errors.CHANGESET_REVISION_CONFLICT)

	// Nothing the stale apply produced was persisted: the store still holds the
	// concurrent writer's bytes, and the stale edit's title is absent.
	after, err := base.Load(fixtureID)
	if err != nil {
		t.Fatalf("reload after rejected apply: %v", err)
	}
	if !bytes.Equal(after, intruder) {
		t.Fatalf("rejected stale apply overwrote the concurrent writer's bytes")
	}
	if bytes.Contains(after, []byte(`"Stale Edit"`)) {
		t.Fatalf("the rejected stale edit was persisted")
	}
}

// nonRevisionedStore is a Store that does NOT implement RevisionedStore, used to
// prove an expected revision against an incapable store is rejected rather than
// silently ignored.
type nonRevisionedStore struct{ storage.Store }

// TestApplyChangeset_UnsupportedRevisionStoreRejected proves that supplying an
// expected revision to a store that cannot report a current revision fails with
// CHANGESET_REVISION_UNSUPPORTED — the precondition the caller asked for cannot be
// enforced, so the apply is rejected rather than skipping the check.
func TestApplyChangeset_UnsupportedRevisionStoreRejected(t *testing.T) {
	res := newResolver(t)
	base, _ := seedStore(t, res)
	store := nonRevisionedStore{Store: base}

	cs := parse(t, `[{"op":"replace","path":"/$manifest/title","value":"Renamed"}]`)
	_, err := ApplyChangeset(store, res, fixtureID, cs, WithExpectedRevision("any-token"))
	hasCode(t, err, errors.CHANGESET_REVISION_UNSUPPORTED)
}
