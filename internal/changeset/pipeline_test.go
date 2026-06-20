package changeset

import (
	"bytes"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/resolver"
	"github.com/frankbardon/lattice/internal/storage"
)

// fixtureID is the manifest.id of minimalDocPath (examples/minimal-dashboard.json),
// the key it is addressed by in the store.
const fixtureID = "example-minimal"

// newResolver builds a resolver over the shipped on-disk schema catalog — the
// exact validation the binary ships with. It is the DocumentResolver the pipeline
// re-runs over the current and mutated bytes.
func newResolver(t *testing.T) *resolver.Resolver {
	t.Helper()
	fs := afero.NewOsFs()

	schemaBytes, err := afero.ReadFile(fs, schemasDir+"/dashboard.schema.json")
	if err != nil {
		t.Fatalf("read dashboard schema: %v", err)
	}
	var dashSch jsonschema.Schema
	if err := dashSch.UnmarshalJSON(schemaBytes); err != nil {
		t.Fatalf("parse dashboard schema: %v", err)
	}
	res, err := resolver.New(fs, &dashSch, []string{schemasDir})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	return res
}

// seedStore builds a hermetic in-memory FS store and saves the fixture document
// into it (in its canonical form, so a later no-op save round-trips), returning the
// store and the canonical bytes it now holds.
func seedStore(t *testing.T, res *resolver.Resolver) (storage.Store, []byte) {
	t.Helper()
	fs := afero.NewOsFs()
	raw, err := afero.ReadFile(fs, minimalDocPath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	// Canonicalize the seed bytes so the store holds exactly what a successful apply
	// would have written — the byte-identity assertion then compares like with like.
	canonical, err := canonicalize(raw)
	if err != nil {
		t.Fatalf("canonicalize fixture: %v", err)
	}

	store := storage.NewFS(afero.NewMemMapFs(), ".")
	if err := store.Save(canonical); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	return store, canonical
}

// The headline apply→persist→reload, the rejected-edit atomicity table, and the
// missing-id case are proven end-to-end against real Stores in e2e_test.go (the
// E1-S5 slice). This file retains the bad-pointer rejection (a translation-stage
// failure not covered there) and the E4 revision-precondition seam, both driven
// through the public ApplyChangeset over the seeded MemMapFs store.

func TestApplyChangeset_MalformedPointerPersistsNothing(t *testing.T) {
	res := newResolver(t)
	store, seed := seedStore(t, res)

	// A malformed pointer (empty leading segment) is rejected at translation; the
	// store must be left byte-for-byte unchanged.
	cs := parse(t, `[{"op":"replace","path":"//x","value":"x"}]`)
	if _, err := ApplyChangeset(store, res, fixtureID, cs); err == nil {
		t.Fatalf("expected an error for a malformed pointer, got nil")
	}

	after, err := store.Load(fixtureID)
	if err != nil {
		t.Fatalf("reload after rejected apply: %v", err)
	}
	if !bytes.Equal(after, seed) {
		t.Fatalf("rejected apply mutated the store:\n--- want (seed) ---\n%s\n--- got ---\n%s", seed, after)
	}
}

func TestApplyChangeset_StaleExpectedRevisionConflicts(t *testing.T) {
	res := newResolver(t)
	store, seed := seedStore(t, res)

	// E4-S2: the precondition is now enforced. An expected revision that does not
	// match the store's current revision (here an arbitrary token that is not the
	// document's content hash) rejects the apply with CHANGESET_REVISION_CONFLICT and
	// persists nothing.
	cs := parse(t, `[{"op":"replace","path":"/$manifest/title","value":"Renamed"}]`)
	_, err := ApplyChangeset(store, res, fixtureID, cs, WithExpectedRevision("deadbeef"))
	hasCode(t, err, errors.CHANGESET_REVISION_CONFLICT)

	after, err := store.Load(fixtureID)
	if err != nil {
		t.Fatalf("reload after rejected apply: %v", err)
	}
	if !bytes.Equal(after, seed) {
		t.Fatalf("rejected stale-revision apply mutated the store")
	}
}
