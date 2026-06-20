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

func TestApplyChangeset_PersistsValidEdit(t *testing.T) {
	res := newResolver(t)
	store, seed := seedStore(t, res)

	// A surfaced field edit: rename the fruits table's title and the document title.
	cs := parse(t, `[
		{"op":"replace","path":"/fruits/config/title","value":"Citrus"},
		{"op":"replace","path":"/$manifest/title","value":"Renamed"}
	]`)

	result, err := ApplyChangeset(store, res, fixtureID, cs)
	if err != nil {
		t.Fatalf("ApplyChangeset: %v", err)
	}

	// The returned document carries the edit...
	if !bytes.Contains(result.Document, []byte(`"Citrus"`)) {
		t.Fatalf("returned document missing the new title, got:\n%s", result.Document)
	}
	if result.Resolved == nil {
		t.Fatalf("expected a resolved tree for the persisted document")
	}

	// ...and the STORE now holds exactly those bytes (the apply persisted, and a
	// reload returns the changed document, not the seed).
	reloaded, err := store.Load(fixtureID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !bytes.Equal(reloaded, result.Document) {
		t.Fatalf("stored bytes differ from the returned document")
	}
	if bytes.Equal(reloaded, seed) {
		t.Fatalf("store still holds the seed document; the edit was not persisted")
	}
	if title := readField(t, reloaded, "manifest", "title"); title != "Renamed" {
		t.Fatalf("reloaded manifest title = %v, want Renamed", title)
	}
}

func TestApplyChangeset_InvalidEditPersistsNothing(t *testing.T) {
	res := newResolver(t)

	// Each case is a changeset rejected at a different pipeline stage; in every case
	// the store must be left byte-for-byte unchanged.
	cases := map[string]string{
		// Off-surface field (guardrail): the table's `rows` is not on its surface.
		"off-surface field": `[{"op":"replace","path":"/fruits/config/rows","value":[]}]`,
		// Ill-typed value (guardrail): a number into a string title.
		"bad value type": `[{"op":"replace","path":"/$manifest/title","value":42}]`,
		// Unknown id (translation): no node named `ghost`.
		"unknown target": `[{"op":"replace","path":"/ghost/config/title","value":"x"}]`,
		// Failed apply (applier): a `test` precondition that does not hold.
		"test mismatch": `[{"op":"test","path":"/$manifest/title","value":"Wrong"}]`,
		// Malformed pointer (translation): empty leading segment.
		"bad pointer": `[{"op":"replace","path":"//x","value":"x"}]`,
	}

	for name, patch := range cases {
		t.Run(name, func(t *testing.T) {
			store, seed := seedStore(t, res)

			_, err := ApplyChangeset(store, res, fixtureID, parse(t, patch))
			if err == nil {
				t.Fatalf("expected an error, got nil")
			}

			// Atomicity: the stored document is byte-for-byte the seed.
			after, loadErr := store.Load(fixtureID)
			if loadErr != nil {
				t.Fatalf("reload after rejected apply: %v", loadErr)
			}
			if !bytes.Equal(after, seed) {
				t.Fatalf("rejected apply mutated the store:\n--- want (seed) ---\n%s\n--- got ---\n%s", seed, after)
			}
		})
	}
}

func TestApplyChangeset_MissingIDRejected(t *testing.T) {
	res := newResolver(t)
	store, _ := seedStore(t, res)

	cs := parse(t, `[{"op":"replace","path":"/$manifest/title","value":"Renamed"}]`)
	_, err := ApplyChangeset(store, res, "no-such-id", cs)
	hasCode(t, err, errors.STORAGE_NOT_FOUND)
}

func TestApplyChangeset_ExpectedRevisionSeamIsNoOp(t *testing.T) {
	res := newResolver(t)
	store, _ := seedStore(t, res)

	// The E4 precondition seam is recorded but not yet checked: passing any expected
	// revision today neither rejects nor alters a valid apply.
	cs := parse(t, `[{"op":"replace","path":"/$manifest/title","value":"Renamed"}]`)
	result, err := ApplyChangeset(store, res, fixtureID, cs, WithExpectedRevision("deadbeef"))
	if err != nil {
		t.Fatalf("ApplyChangeset with expected revision: %v", err)
	}
	if title := readField(t, result.Document, "manifest", "title"); title != "Renamed" {
		t.Fatalf("expected the edit to apply despite the (unchecked) precondition")
	}
}
