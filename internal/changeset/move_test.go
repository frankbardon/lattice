package changeset

// Engine-level coverage for STRUCTURAL REORDER + MOVE ACROSS PARENTS (E3-S2):
// RFC 6902 `move` ops with id-rooted `from`/`path`. These cases drive the public
// ApplyChangeset over a seeded store so the RFC 6902 sequential-index semantics,
// the cross-parent placement fix-up, and the re-resolve grammar guardrail are all
// exercised exactly as a caller observes them.
//
// The grids fixture (examples/grids-dashboard.json) supplies a richer tree than
// minimal: root → body (2-col×1-row) holding `sidebar-block` (a block) and `panel`
// (a nested container, 1-col×2-row) holding `main-block` (row 1) and `footer-block`
// (row 2). The two grids differ, so a cross-parent move's stale placement is the
// case the fix-up must reconcile.

import (
	"bytes"
	"testing"

	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/storage"
)

// gridsDocPath is a shipped fixture with a nested-container tree (a body region
// holding a block and a sub-container of two blocks) and DIFFERING parent grids —
// the shape a cross-parent move's placement fix-up needs.
const gridsDocPath = "../../examples/grids-dashboard.json"

// gridsID is the manifest.id of gridsDocPath, the key it is addressed by.
const gridsID = "example-grids"

// seedGridsStore builds a hermetic MemMapFs-backed store holding the canonical form
// of the grids fixture, returning the store and the seed bytes.
func seedGridsStore(t *testing.T) (storage.Store, []byte) {
	t.Helper()
	raw, err := afero.ReadFile(afero.NewOsFs(), gridsDocPath)
	if err != nil {
		t.Fatalf("read grids fixture: %v", err)
	}
	canonical, err := canonicalize(raw)
	if err != nil {
		t.Fatalf("canonicalize grids fixture: %v", err)
	}
	store := storage.NewFS(afero.NewMemMapFs(), ".")
	if err := store.Save(canonical); err != nil {
		t.Fatalf("seed grids store: %v", err)
	}
	return store, canonical
}

// childIDs reads the ordered child ids of the panel container (root → body →
// panel) from a canonical document.
func panelChildIDs(t *testing.T, doc []byte) []string {
	t.Helper()
	body := readField(t, doc, "root", "children").([]any)[0].(map[string]any)
	// body children: [sidebar-block, panel]; panel is the nested container.
	panel := body[childrenKey].([]any)[1].(map[string]any)
	return childIDList(t, panel)
}

// bodyChildIDs reads the ordered child ids of the body container (root → body).
func bodyChildIDs(t *testing.T, doc []byte) []string {
	t.Helper()
	body := readField(t, doc, "root", "children").([]any)[0].(map[string]any)
	return childIDList(t, body)
}

// childIDList extracts the ordered `id` of each child of a decoded instance.
func childIDList(t *testing.T, inst map[string]any) []string {
	t.Helper()
	children, _ := inst[childrenKey].([]any)
	ids := make([]string, 0, len(children))
	for _, c := range children {
		ids = append(ids, c.(map[string]any)[idKey].(string))
	}
	return ids
}

// TestE2E_ReorderWithinParent proves an in-parent reorder via `move` honors RFC
// 6902 sequential index semantics: the target index is interpreted AFTER the
// implied removal of the source. Moving `main-block` (index 0 in panel) to index 1
// places it AFTER `footer-block` — the off-by-one case (a naive non-sequential read
// would leave it at index 0). Placement is kept (same parent, same grid), and the
// result persists.
func TestE2E_ReorderWithinParent(t *testing.T) {
	res := newResolver(t)
	store, seed := seedGridsStore(t)

	if got := panelChildIDs(t, seed); got[0] != "main-block" || got[1] != "footer-block" {
		t.Fatalf("seed panel order = %v, want [main-block footer-block]", got)
	}

	cs := parse(t, `[{"op":"move","from":"/main-block","path":"/panel/children/1"}]`)
	r, err := ApplyChangeset(store, res, gridsID, cs)
	if err != nil {
		t.Fatalf("reorder within parent: %v", err)
	}

	got := panelChildIDs(t, r.Document)
	if len(got) != 2 || got[0] != "footer-block" || got[1] != "main-block" {
		t.Fatalf("reordered panel order = %v, want [footer-block main-block] (sequential index semantics)", got)
	}

	// Same-parent reorder keeps the explicit placement (the grid is unchanged).
	body := readField(t, r.Document, "root", "children").([]any)[0].(map[string]any)
	panel := body[childrenKey].([]any)[1].(map[string]any)
	moved := panel[childrenKey].([]any)[1].(map[string]any)
	if _, ok := moved[placementKey]; !ok {
		t.Fatalf("same-parent reorder should preserve the moved node's placement, got: %v", moved)
	}
}

// TestE2E_ReorderIndexShiftToFront proves the other off-by-one boundary: moving the
// LAST child (`footer-block`, index 1) to index 0 brings it to the front. After the
// implied removal the array is [main-block]; inserting at 0 yields [footer-block,
// main-block].
func TestE2E_ReorderIndexShiftToFront(t *testing.T) {
	res := newResolver(t)
	store, _ := seedGridsStore(t)

	cs := parse(t, `[{"op":"move","from":"/footer-block","path":"/panel/children/0"}]`)
	r, err := ApplyChangeset(store, res, gridsID, cs)
	if err != nil {
		t.Fatalf("reorder to front: %v", err)
	}

	got := panelChildIDs(t, r.Document)
	if len(got) != 2 || got[0] != "footer-block" || got[1] != "main-block" {
		t.Fatalf("reordered panel order = %v, want [footer-block main-block]", got)
	}
}

// TestE2E_MoveAcrossParents proves a legal cross-parent move relocates the item +
// subtree and RE-RESOLVES: `footer-block` (in panel, placement rowStart:2) moves
// into `body` (a 2-col×1-row grid where rowStart:2 is out of bounds). The
// cross-parent placement fix-up strips the stale placement so the moved node falls
// back to the first cell and the document re-resolves; the result persists.
func TestE2E_MoveAcrossParents(t *testing.T) {
	res := newResolver(t)
	store, seed := seedGridsStore(t)

	// Seed: body holds [sidebar-block, panel]; footer-block lives under panel.
	if got := bodyChildIDs(t, seed); len(got) != 2 {
		t.Fatalf("seed body should hold 2 children, got %v", got)
	}

	cs := parse(t, `[{"op":"move","from":"/footer-block","path":"/body/children/2"}]`)
	r, err := ApplyChangeset(store, res, gridsID, cs)
	if err != nil {
		t.Fatalf("legal cross-parent move: %v", err)
	}

	body := readField(t, r.Document, "root", "children").([]any)[0].(map[string]any)
	bodyChildren := body[childrenKey].([]any)
	if len(bodyChildren) != 3 {
		t.Fatalf("body should hold 3 children after the move, got %d", len(bodyChildren))
	}
	moved := bodyChildren[2].(map[string]any)
	if moved[idKey] != "footer-block" {
		t.Fatalf("appended body child = %v, want footer-block", moved[idKey])
	}
	// The subtree traveled: the wrapped `footer` table content came along.
	content := moved[configKey].(map[string]any)[instanceContentKey].(map[string]any)
	if content[idKey] != "footer" {
		t.Fatalf("moved subtree lost its content leaf: %v", content)
	}
	// Cross-parent move dropped the stale placement (it would be out of bounds in
	// body's grid); the node now defaults to the first cell.
	if _, ok := moved[placementKey]; ok {
		t.Fatalf("cross-parent move should strip the stale placement, got: %v", moved[placementKey])
	}
	// panel now holds only main-block.
	panel := bodyChildren[1].(map[string]any)
	if got := childIDList(t, panel); len(got) != 1 || got[0] != "main-block" {
		t.Fatalf("panel after move = %v, want [main-block]", got)
	}
}

// TestE2E_IllegalCrossParentMoveRejected proves a cross-parent move that violates
// the TREE GRAMMAR is rejected by re-resolve and nothing is persisted: moving a
// block (`sidebar-block`) directly under `root` breaks the rule that root accepts
// only positional regions (GRAMMAR_ROOT_CHILD_INVALID). The placement fix-up clears
// the stale placement first, so the rejection is the grammar guardrail — not an
// incidental layout-bounds error — exactly as the structural guardrail intends.
func TestE2E_IllegalCrossParentMoveRejected(t *testing.T) {
	res := newResolver(t)
	store, seed := seedGridsStore(t)

	cs := parse(t, `[{"op":"move","from":"/sidebar-block","path":"/root/children/1"}]`)
	_, err := ApplyChangeset(store, res, gridsID, cs)
	hasCode(t, err, errors.GRAMMAR_ROOT_CHILD_INVALID)

	after, loadErr := store.Load(gridsID)
	if loadErr != nil {
		t.Fatalf("reload after rejected move: %v", loadErr)
	}
	if !bytes.Equal(after, seed) {
		t.Fatalf("rejected move mutated the store:\n--- want (seed) ---\n%s\n--- got ---\n%s", seed, after)
	}
}

// TestE2E_MoveUnknownSourceRejected proves a move whose `from` id matches no node is
// rejected at translation (CHANGESET_TARGET_NOT_FOUND) — the move classifier routes
// it structurally (bypassing the field surface), so the not-found code is what
// surfaces — and nothing is persisted.
func TestE2E_MoveUnknownSourceRejected(t *testing.T) {
	res := newResolver(t)
	store, seed := seedGridsStore(t)

	cs := parse(t, `[{"op":"move","from":"/ghost-block","path":"/body/children/2"}]`)
	_, err := ApplyChangeset(store, res, gridsID, cs)
	hasCode(t, err, errors.CHANGESET_TARGET_NOT_FOUND)

	after, loadErr := store.Load(gridsID)
	if loadErr != nil {
		t.Fatalf("reload after rejected move: %v", loadErr)
	}
	if !bytes.Equal(after, seed) {
		t.Fatalf("rejected move mutated the store")
	}
}
