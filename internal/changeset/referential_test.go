package changeset

// E3-S3 closes Epic 3 by proving the THREE structural guardrail concerns the
// per-op slices (E3-S1 add/remove, E3-S2 reorder/move — structural_test.go,
// move_test.go) do not already cover end to end:
//
//   - REFERENTIAL INTEGRITY: a structural remove that strands an in-document
//     REFERENCE (a configurator's `target` id) is rejected by RE-RESOLVE, not by
//     the apply layer — the resolver's reference pass dangles, so the whole
//     changeset is refused and nothing is persisted. There is NO cascade: the
//     referrer is not silently dropped to make the remove fit.
//
//   - EMPTIED-REGION behavior: the grammar's ACTUAL rule for a region left with no
//     children, asserted rather than assumed — emptying a region (root or a nested
//     container) is LEGAL and persists.
//
//   - ATOMICITY across a MULTI-OP structural changeset: when a LATER op in a
//     structural changeset fails, the WHOLE changeset rolls back — the earlier,
//     individually-legal ops are not persisted either.
//
// The four headline structural operations (add into a container, reorder
// siblings, move across parents legal+illegal, remove a subtree) plus the
// duplicate-id and missing-id add contracts are proven in structural_test.go and
// move_test.go; this file rounds out the epic rather than restating them.
//
// Be ADVERSARIAL: each rejection case tries to persist an illegal tree and asserts
// the store is left BYTE-FOR-BYTE identical to its seed.

import (
	"bytes"
	"testing"

	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/storage"
)

// configuratorDocPath is a shipped fixture whose body region holds TWO blocks: a
// `summary` table and a `summary-configurator` whose `target` is the id `summary`.
// The configurator -> table reference is the in-document reference whose integrity
// a structural remove can violate — the clean dangling-reference scenario the
// schema affords (connections/variables are document-scope arrays, not tree items
// a structural changeset can remove; an item id referenced from elsewhere in the
// TREE is exactly the configurator target).
const configuratorDocPath = "../../examples/configurator-dashboard.json"

// configuratorID is the manifest.id of configuratorDocPath, the store key.
const configuratorID = "example-configurator"

// seedConfiguratorStore builds a hermetic MemMapFs-backed store holding the
// canonical form of the configurator fixture, returning the store and seed bytes.
func seedConfiguratorStore(t *testing.T) (storage.Store, []byte) {
	t.Helper()
	raw, err := afero.ReadFile(afero.NewOsFs(), configuratorDocPath)
	if err != nil {
		t.Fatalf("read configurator fixture: %v", err)
	}
	canonical, err := canonicalize(raw)
	if err != nil {
		t.Fatalf("canonicalize configurator fixture: %v", err)
	}
	store := storage.NewFS(afero.NewMemMapFs(), ".")
	if err := store.Save(canonical); err != nil {
		t.Fatalf("seed configurator store: %v", err)
	}
	return store, canonical
}

// TestE2E_RemoveTargetDanglesReference is the REFERENTIAL-INTEGRITY proof:
// structurally removing `summary-block` (the wrapper holding the `summary` table a
// configurator targets) is GRAMMAR-legal on its own — it just shortens the body
// region's children — but it strands the configurator's `target: "summary"`. The
// pipeline's RE-RESOLVE catches the now-dangling reference
// (CONFIGURATOR_TARGET_NOT_FOUND), refuses the whole changeset, and persists
// nothing. The configurator is NOT cascaded away: re-resolve rejects rather than
// repairs.
func TestE2E_RemoveTargetDanglesReference(t *testing.T) {
	res := newResolver(t)
	store, seed := seedConfiguratorStore(t)

	cs := parse(t, `[{"op":"remove","path":"/summary-block"}]`)
	_, err := ApplyChangeset(store, res, configuratorID, cs)
	hasCode(t, err, errors.CONFIGURATOR_TARGET_NOT_FOUND)

	assertStoreUnchanged(t, store, configuratorID, seed)
}

// TestE2E_RemoveContentLeafEmptiesWrapper proves the OTHER half of the
// reference/shape boundary: removing the `summary` table directly (the inner
// content leaf) leaves its block wrapper holding zero content, which the block
// pass rejects fail-fast (WRAPPER_CHILD_COUNT_INVALID) on re-resolve — a wrapper
// must wrap exactly one content item. Nothing is persisted. (This is the
// shape rule that a remove of a wrapped leaf hits BEFORE the dangling reference
// would; both refusals leave the store untouched.)
func TestE2E_RemoveContentLeafEmptiesWrapper(t *testing.T) {
	res := newResolver(t)
	store, seed := seedConfiguratorStore(t)

	cs := parse(t, `[{"op":"remove","path":"/summary"}]`)
	_, err := ApplyChangeset(store, res, configuratorID, cs)
	hasCode(t, err, errors.WRAPPER_CHILD_COUNT_INVALID)

	assertStoreUnchanged(t, store, configuratorID, seed)
}

// TestE2E_EmptyRootRegionIsLegal asserts the ACTUAL emptied-region rule at the
// root: removing the only region under root (`body`) leaves root with no children,
// which the grammar ALLOWS — an empty positional region is a legal (if degenerate)
// tree. The remove persists and the reloaded root holds an empty children array.
// (The story leaves "reject or allow" to the grammar; this is the grammar's real
// answer — allow.)
func TestE2E_EmptyRootRegionIsLegal(t *testing.T) {
	res := newResolver(t)
	store, seed := seedStore(t, res)

	cs := parse(t, `[{"op":"remove","path":"/body"}]`)
	if _, err := ApplyChangeset(store, res, fixtureID, cs); err != nil {
		t.Fatalf("emptying root by removing its only region should be legal: %v", err)
	}

	reloaded, err := store.Load(fixtureID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if bytes.Equal(reloaded, seed) {
		t.Fatalf("emptying the root did not change the stored bytes")
	}
	root := readField(t, reloaded, "root").(map[string]any)
	children, ok := root[childrenKey].([]any)
	if !ok || len(children) != 0 {
		t.Fatalf("root should hold an empty children array after the remove, got: %v", root[childrenKey])
	}
}

// TestE2E_EmptyNestedContainerIsLegal asserts the same emptied-region rule for a
// NESTED region: removing BOTH of the `panel` container's children (footer-block
// then main-block, in descending physical-index order so each pointer still
// resolves) empties the panel, which the grammar ALLOWS. The result persists and
// the reloaded panel holds an empty children array — confirming a region with no
// children is legal at any depth, not only at root.
func TestE2E_EmptyNestedContainerIsLegal(t *testing.T) {
	res := newResolver(t)
	store, _ := seedGridsStore(t)

	// panel.children = [main-block(0), footer-block(1)]. The translator resolves both
	// id-rooted pointers against the ORIGINAL tree, so remove the HIGHER index first;
	// removing index 0 first would shift footer-block and strand the second pointer.
	cs := parse(t, `[{"op":"remove","path":"/footer-block"},{"op":"remove","path":"/main-block"}]`)
	r, err := ApplyChangeset(store, res, gridsID, cs)
	if err != nil {
		t.Fatalf("emptying a nested container should be legal: %v", err)
	}

	got := panelChildIDs(t, r.Document)
	if len(got) != 0 {
		t.Fatalf("panel should be empty after removing both children, got %v", got)
	}
}

// TestE2E_StructuralMultiOpAtomicRollback is the MULTI-OP ATOMICITY proof: a
// structural changeset whose FIRST op is individually legal (remove footer-block
// from panel) but whose SECOND op fails at the applier (remove panel.children[5],
// an index far past the array's end) is rejected as a WHOLE. The RFC 6902 applier
// runs the translated ops as one atomic patch, so the failing later op rolls back
// the already-applied first remove; the pipeline then Saves nothing. The store is
// left byte-for-byte identical to its seed — partial application is impossible.
func TestE2E_StructuralMultiOpAtomicRollback(t *testing.T) {
	res := newResolver(t)
	store, seed := seedGridsStore(t)

	// First op: a legal remove of footer-block from panel. Second op: remove
	// panel.children[5], an out-of-range index the applier cannot satisfy. The later
	// failure must roll the whole changeset back, so the legal first remove never
	// reaches the store.
	cs := parse(t, `[
		{"op":"remove","path":"/footer-block"},
		{"op":"remove","path":"/panel/children/5"}
	]`)
	_, err := ApplyChangeset(store, res, gridsID, cs)
	if err == nil {
		t.Fatalf("a multi-op structural changeset with a failing later op must be rejected")
	}
	hasCode(t, err, errors.PATCH_APPLY_FAILED)

	// The legal first remove must NOT survive the failed second op.
	assertStoreUnchanged(t, store, gridsID, seed)
	reloaded, err := store.Load(gridsID)
	if err != nil {
		t.Fatalf("reload after rejected multi-op: %v", err)
	}
	if !bytes.Contains(reloaded, []byte("footer-block")) {
		t.Fatalf("the rolled-back first remove leaked into the store (footer-block gone):\n%s", reloaded)
	}
}

// TestE2E_StructuralAddThenIllegalAddAtomicRollback proves atomicity when the
// failing later op is a GRAMMAR violation rather than an apply error: the first op
// legally appends a block wrapper to body; the second appends a BARE content leaf
// (unwrapped table) into the same region, which re-resolve refuses
// (GRAMMAR_REGION_CHILD_INVALID). Because re-resolve runs over the document with
// BOTH ops already applied, the legal first add is rolled back with the illegal
// second — nothing persists.
func TestE2E_StructuralAddThenIllegalAddAtomicRollback(t *testing.T) {
	res := newResolver(t)
	store, seed := seedStore(t, res)

	bareLeaf := `{
		"$ref":"https://lattice.dev/schemas/items/table/1.0.0",
		"id":"loose-table",
		"config":{"title":"Loose"}
	}`
	cs := parse(t, `[
		{"op":"add","path":"/body/children/-","value":`+notesBlock+`},
		{"op":"add","path":"/body/children/-","value":`+bareLeaf+`}
	]`)
	_, err := ApplyChangeset(store, res, fixtureID, cs)
	hasCode(t, err, errors.GRAMMAR_REGION_CHILD_INVALID)

	// The legal first add must NOT survive the failed second op.
	assertStoreUnchanged(t, store, fixtureID, seed)
	reloaded, err := store.Load(fixtureID)
	if err != nil {
		t.Fatalf("reload after rejected multi-op: %v", err)
	}
	if bytes.Contains(reloaded, []byte("notes-block")) {
		t.Fatalf("the rolled-back first add leaked into the store:\n%s", reloaded)
	}
}

// assertStoreUnchanged reloads id from store and fails if its bytes differ from
// seed — the atomicity invariant every rejected structural apply must hold
// (nothing persisted). It generalizes structural_test.go's assertUnchanged to an
// explicit store id so the configurator and grids fixtures reuse it.
func assertStoreUnchanged(t *testing.T, store storage.Store, id string, seed []byte) {
	t.Helper()
	after, err := store.Load(id)
	if err != nil {
		t.Fatalf("reload after rejected apply: %v", err)
	}
	if !bytes.Equal(after, seed) {
		t.Fatalf("rejected apply mutated the store:\n--- want (seed) ---\n%s\n--- got ---\n%s", seed, after)
	}
}
