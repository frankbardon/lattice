package changeset

// Engine-level coverage for the DRY-RUN apply primitive (E3-S1): DryRunChangeset
// runs the SAME load → resolve → apply → re-resolve guardrails as ApplyChangeset but
// must NEVER reach Store.Save. Each case wraps the seeded store in a save-counting
// guard and asserts (a) the dry run records ZERO saves and leaves the stored bytes
// byte-for-byte unchanged, and (b) the coded error (or success) is IDENTICAL to what
// a real ApplyChangeset of the same changeset would produce — the dry run is the same
// pipeline minus the write.

import (
	"bytes"
	"testing"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/storage"
)

// countingStore wraps a Store and counts Save calls, so a test can prove the dry-run
// path never writes. All other operations pass straight through to the wrapped store.
type countingStore struct {
	storage.Store
	saves int
}

func (c *countingStore) Save(document []byte) error {
	c.saves++
	return c.Store.Save(document)
}

func TestDryRunChangeset(t *testing.T) {
	cases := map[string]struct {
		patch string
		// code is the coded error both DryRunChangeset and ApplyChangeset must return;
		// the empty code means the change is expected to succeed.
		code errors.Code
	}{
		// Valid surfaced field edit: succeeds, yielding the would-be-mutated bytes.
		"valid field edit": {
			`[{"op":"replace","path":"/fruits/config/title","value":"Citrus"}]`,
			"",
		},
		// Off-surface field: `rows` is a real config field but not on the table's
		// configurable surface — rejected by the field-edit guardrail.
		"off-surface field": {
			`[{"op":"replace","path":"/fruits/config/rows","value":[]}]`,
			errors.CONFIG_OVERRIDE_FIELD_UNKNOWN,
		},
		// Bad structural add (missing id): the apply layer's structural id check
		// rejects an added node whose value omits its own id.
		"structural add missing id": {
			`[{"op":"add","path":"/body/children/-","value":{` +
				`"$ref":"https://lattice.dev/schemas/items/block/1.0.0",` +
				`"config":{"id":"x","content":{"$ref":"https://lattice.dev/schemas/items/table/1.0.0","config":{"title":"X"}}}}}]`,
			errors.CHANGESET_STRUCTURAL_ID_INVALID,
		},
		// Bad structural add (duplicate id): reusing an existing id (`fruits`) is
		// rejected by the structural uniqueness check.
		"structural add duplicate id": {
			`[{"op":"add","path":"/body/children/-","value":{` +
				`"$ref":"https://lattice.dev/schemas/items/block/1.0.0","id":"fruits",` +
				`"config":{"id":"dup","content":{"$ref":"https://lattice.dev/schemas/items/table/1.0.0","id":"dup-inner","config":{"title":"X"}}}}}]`,
			errors.CHANGESET_STRUCTURAL_ID_INVALID,
		},
		// Schema/constraint violation: `grid.gap` is a number-typed nested surface
		// entry; a string is the wrong type, so value validation rejects it — the same
		// constraint check a real ApplyChangeset runs.
		"schema violation": {
			`[{"op":"replace","path":"/body/config/grid/gap","value":"wide"}]`,
			errors.CONFIG_OVERRIDE_VALUE_INVALID,
		},
		// Failing RFC 6902 test op: a `test` whose expected value does not hold aborts
		// the apply (PATCH_APPLY_FAILED) before any write would occur.
		"failing test op": {
			`[{"op":"test","path":"/$manifest/title","value":"Wrong"}]`,
			errors.PATCH_APPLY_FAILED,
		},
	}

	res := newResolver(t)

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// The dry-run store is wrapped so any Save is counted; the parallel
			// ApplyChangeset (the oracle) runs against its own seeded store.
			base, seed := seedFSStoreWith(t, minimalDocPath)
			store := &countingStore{Store: base}

			result, err := DryRunChangeset(store, res, fixtureID, parse(t, c.patch))

			if c.code == "" {
				if err != nil {
					t.Fatalf("DryRunChangeset: unexpected error: %v", err)
				}
				if result == nil || result.Document == nil || result.Resolved == nil {
					t.Fatalf("DryRunChangeset: want a result with document + resolved tree, got %+v", result)
				}
			} else {
				hasCode(t, err, c.code)
				if result != nil {
					t.Fatalf("DryRunChangeset: want nil result on error, got %+v", result)
				}
			}

			// THE CONTRACT: the dry run never wrote, and the store is byte-identical.
			if store.saves != 0 {
				t.Fatalf("DryRunChangeset wrote to the store: Save called %d time(s), want 0", store.saves)
			}
			after, loadErr := store.Load(fixtureID)
			if loadErr != nil {
				t.Fatalf("reload after dry run: %v", loadErr)
			}
			if !bytes.Equal(after, seed) {
				t.Fatalf("dry run mutated the store:\n--- want (seed) ---\n%s\n--- got ---\n%s", seed, after)
			}

			// ORACLE: a real ApplyChangeset of the same changeset returns the same
			// outcome — identical coded error on failure, and on success the same
			// would-be-persisted document bytes the dry run reported.
			oracleStore, _ := seedFSStoreWith(t, minimalDocPath)
			oracle, oracleErr := ApplyChangeset(oracleStore, res, fixtureID, parse(t, c.patch))
			if c.code == "" {
				if oracleErr != nil {
					t.Fatalf("oracle ApplyChangeset: unexpected error: %v", oracleErr)
				}
				if !bytes.Equal(result.Document, oracle.Document) {
					t.Fatalf("dry-run document differs from the persisted document:\n--- dry run ---\n%s\n--- applied ---\n%s",
						result.Document, oracle.Document)
				}
			} else {
				hasCode(t, oracleErr, c.code)
			}
		})
	}
}
