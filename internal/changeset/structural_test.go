package changeset

// Engine-level coverage for STRUCTURAL `$root` editing (E3-S1): add & remove
// addressed by id-rooted pointers into `children` arrays, gated by re-resolve
// rather than the (empty) `$root` configurable surface. These cases drive the
// public ApplyChangeset over a seeded store so the re-resolve grammar/schema
// guardrail and the apply-layer id contract are exercised exactly as a caller
// observes them; the field-edit slice's engine cases live in apply_test.go.

import (
	"bytes"
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// A legal block wrapper (with a single table content leaf) to insert into the
// `body` container's `children` array. It carries its own stable instance id
// (`notes-block`) and the wrapper's required config id; its inner table is
// `notes`. Both ids are absent from the seed fixture, so the add is unique.
const notesBlock = `{
	"$ref":"https://lattice.dev/schemas/items/block/1.0.0",
	"id":"notes-block",
	"config":{
		"id":"notes-block",
		"content":{
			"$ref":"https://lattice.dev/schemas/items/table/1.0.0",
			"id":"notes",
			"config":{"title":"Notes","columns":[{"header":"Note"}],"rows":[]}
		}
	}
}`

// TestE2E_StructuralAddBlockPersists proves a structural ADD of a block wrapper
// into a container's `children` (append form `/body/children/-`) flows through the
// re-resolve guardrail and is persisted: the body container gains a third child
// carrying the new id, and the bytes change.
func TestE2E_StructuralAddBlockPersists(t *testing.T) {
	res := newResolver(t)
	store, seed := seedStore(t, res)

	cs := parse(t, `[{"op":"add","path":"/body/children/-","value":`+notesBlock+`}]`)
	if _, err := ApplyChangeset(store, res, fixtureID, cs); err != nil {
		t.Fatalf("structural add: %v", err)
	}

	reloaded, err := store.Load(fixtureID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if bytes.Equal(reloaded, seed) {
		t.Fatalf("structural add did not change the stored bytes")
	}
	body := readField(t, reloaded, "root", "children").([]any)[0].(map[string]any)
	children := body[childrenKey].([]any)
	if len(children) != 3 {
		t.Fatalf("body should hold 3 children after the add, got %d", len(children))
	}
	if got := children[2].(map[string]any)[idKey]; got != "notes-block" {
		t.Fatalf("appended child id = %v, want notes-block", got)
	}
}

// TestE2E_StructuralAddPositional proves the POSITIONAL insert form
// (`/body/children/0`) places the new block at the addressed index rather than
// appending.
func TestE2E_StructuralAddPositional(t *testing.T) {
	res := newResolver(t)
	store, _ := seedStore(t, res)

	cs := parse(t, `[{"op":"add","path":"/body/children/0","value":`+notesBlock+`}]`)
	if _, err := ApplyChangeset(store, res, fixtureID, cs); err != nil {
		t.Fatalf("positional structural add: %v", err)
	}

	reloaded, err := store.Load(fixtureID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	body := readField(t, reloaded, "root", "children").([]any)[0].(map[string]any)
	children := body[childrenKey].([]any)
	if len(children) != 3 {
		t.Fatalf("body should hold 3 children, got %d", len(children))
	}
	if got := children[0].(map[string]any)[idKey]; got != "notes-block" {
		t.Fatalf("index-0 child id = %v, want notes-block (positional insert)", got)
	}
}

// TestE2E_StructuralAddMissingIDRejected proves a structural add whose value omits
// its own `id` is rejected with CHANGESET_STRUCTURAL_ID_INVALID and nothing is
// persisted — re-resolve cannot supply this check, so the apply layer owns it.
func TestE2E_StructuralAddMissingIDRejected(t *testing.T) {
	res := newResolver(t)
	store, seed := seedStore(t, res)

	noID := `{
		"$ref":"https://lattice.dev/schemas/items/block/1.0.0",
		"config":{"id":"x","content":{"$ref":"https://lattice.dev/schemas/items/table/1.0.0","config":{"title":"X"}}}
	}`
	cs := parse(t, `[{"op":"add","path":"/body/children/-","value":`+noID+`}]`)
	_, err := ApplyChangeset(store, res, fixtureID, cs)
	hasCode(t, err, errors.CHANGESET_STRUCTURAL_ID_INVALID)

	assertUnchanged(t, store, seed)
}

// TestE2E_StructuralAddDuplicateIDRejected proves a structural add whose value
// reuses an id already in the document (`fruits`) is rejected with
// CHANGESET_STRUCTURAL_ID_INVALID — the resolver's id index is last-wins and would
// silently shadow it, so uniqueness is enforced here — and nothing is persisted.
func TestE2E_StructuralAddDuplicateIDRejected(t *testing.T) {
	res := newResolver(t)
	store, seed := seedStore(t, res)

	dup := `{
		"$ref":"https://lattice.dev/schemas/items/block/1.0.0",
		"id":"fruits",
		"config":{"id":"dup","content":{"$ref":"https://lattice.dev/schemas/items/table/1.0.0","id":"dup-inner","config":{"title":"X"}}}
	}`
	cs := parse(t, `[{"op":"add","path":"/body/children/-","value":`+dup+`}]`)
	_, err := ApplyChangeset(store, res, fixtureID, cs)
	hasCode(t, err, errors.CHANGESET_STRUCTURAL_ID_INVALID)

	assertUnchanged(t, store, seed)
}

// TestE2E_StructuralAddGrammarViolationRejected proves a structurally well-formed
// add that violates the TREE GRAMMAR is rejected by re-resolve, not the apply
// layer: appending a BARE content leaf (a table, with a unique id) directly into a
// container's children breaks the "content must be block-wrapped" rule
// (GRAMMAR_REGION_CHILD_INVALID). Nothing is persisted.
func TestE2E_StructuralAddGrammarViolationRejected(t *testing.T) {
	res := newResolver(t)
	store, seed := seedStore(t, res)

	bareLeaf := `{
		"$ref":"https://lattice.dev/schemas/items/table/1.0.0",
		"id":"loose-table",
		"config":{"title":"Loose"}
	}`
	cs := parse(t, `[{"op":"add","path":"/body/children/-","value":`+bareLeaf+`}]`)
	_, err := ApplyChangeset(store, res, fixtureID, cs)
	hasCode(t, err, errors.GRAMMAR_REGION_CHILD_INVALID)

	assertUnchanged(t, store, seed)
}

// TestE2E_StructuralRemovePersists proves a structural REMOVE addressing an item
// by its id (`/metrics-block`, the item root form) deletes that node and its
// subtree from the parent's children, and the result persists.
func TestE2E_StructuralRemovePersists(t *testing.T) {
	res := newResolver(t)
	store, _ := seedStore(t, res)

	cs := parse(t, `[{"op":"remove","path":"/metrics-block"}]`)
	if _, err := ApplyChangeset(store, res, fixtureID, cs); err != nil {
		t.Fatalf("structural remove: %v", err)
	}

	reloaded, err := store.Load(fixtureID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	body := readField(t, reloaded, "root", "children").([]any)[0].(map[string]any)
	children := body[childrenKey].([]any)
	if len(children) != 1 {
		t.Fatalf("body should hold 1 child after removing metrics-block, got %d", len(children))
	}
	if got := children[0].(map[string]any)[idKey]; got != "fruits-block" {
		t.Fatalf("surviving child id = %v, want fruits-block", got)
	}
	if bytes.Contains(reloaded, []byte("metrics-block")) {
		t.Fatalf("removed subtree should be gone, got:\n%s", reloaded)
	}
}

// TestE2E_StructuralRemoveUnknownIDRejected proves a structural remove whose
// leading id matches no node is rejected with the translator's
// CHANGESET_TARGET_NOT_FOUND (a structural op bypasses the field surface, so the
// off-surface code never fires) and nothing is persisted.
func TestE2E_StructuralRemoveUnknownIDRejected(t *testing.T) {
	res := newResolver(t)
	store, seed := seedStore(t, res)

	cs := parse(t, `[{"op":"remove","path":"/ghost-block"}]`)
	_, err := ApplyChangeset(store, res, fixtureID, cs)
	hasCode(t, err, errors.CHANGESET_TARGET_NOT_FOUND)

	assertUnchanged(t, store, seed)
}

// assertUnchanged reloads the store and fails if its bytes differ from the seed —
// the atomicity invariant a rejected apply must hold (nothing persisted).
func assertUnchanged(t *testing.T, store interface {
	Load(string) ([]byte, error)
}, seed []byte) {
	t.Helper()
	after, err := store.Load(fixtureID)
	if err != nil {
		t.Fatalf("reload after rejected apply: %v", err)
	}
	if !bytes.Equal(after, seed) {
		t.Fatalf("rejected apply mutated the store:\n--- want (seed) ---\n%s\n--- got ---\n%s", seed, after)
	}
}
