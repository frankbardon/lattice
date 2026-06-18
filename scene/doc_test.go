package scene

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"

	"github.com/frankbardon/lattice/dashboard"
)

// memStore is a minimal in-memory Store for tests, satisfying scene.Store.
type memStore struct {
	mu   sync.Mutex
	docs map[string]*dashboard.Dashboard
}

func newMemStore() *memStore {
	return &memStore{docs: make(map[string]*dashboard.Dashboard)}
}

func (m *memStore) Load(_ context.Context, id string) (*dashboard.Dashboard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.docs[id]
	if !ok {
		return nil, newError(Internal, "not found")
	}
	return cloneDoc(d), nil
}

func (m *memStore) Save(_ context.Context, doc *dashboard.Dashboard) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.docs[doc.ID] = cloneDoc(doc)
	return nil
}

// recBroadcaster records broadcast patches in order, keyed by dashboard id.
type recBroadcaster struct {
	mu      sync.Mutex
	patches map[string][]json.RawMessage
	fail    bool
}

func newRecBroadcaster() *recBroadcaster {
	return &recBroadcaster{patches: make(map[string][]json.RawMessage)}
}

func (r *recBroadcaster) BroadcastPatch(_ context.Context, id string, patch json.RawMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return newError(Internal, "broadcast failed")
	}
	cp := make(json.RawMessage, len(patch))
	copy(cp, patch)
	r.patches[id] = append(r.patches[id], cp)
	return nil
}

func (r *recBroadcaster) count(id string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.patches[id])
}

func testOpts() Options { return Options{Logger: slog.New(slog.DiscardHandler)} }

func seedDoc(t *testing.T, st *memStore, id string) *dashboard.Dashboard {
	t.Helper()
	d := &dashboard.Dashboard{
		ID:        id,
		Name:      "Board",
		Variables: []dashboard.Variable{{Name: "env", Value: "prod"}},
		Bricks: []dashboard.Brick{
			{ID: "b1", Kind: "markdown", Template: "# one", Layout: dashboard.Layout{Pos: dashboard.Position{X: 0, Y: 0}, Size: dashboard.Size{Width: 2, Height: 2}}},
			{ID: "b2", Kind: "markdown", Template: "# two", Layout: dashboard.Layout{Pos: dashboard.Position{X: 2, Y: 0}, Size: dashboard.Size{Width: 2, Height: 2}}},
		},
	}
	if err := st.Save(context.Background(), d); err != nil {
		t.Fatalf("seed save: %v", err)
	}
	return d
}

func openDoc(t *testing.T, st *memStore, bc Broadcaster, id string) *Doc {
	t.Helper()
	d, err := Open(context.Background(), id, st, bc, testOpts())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return d
}

func TestApplyIntents(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		intent Intent
		verify func(t *testing.T, d *dashboard.Dashboard)
	}{
		{
			name: "add_brick",
			intent: Intent{Type: IntentAddBrick, Brick: &dashboard.Brick{
				ID: "b3", Kind: "markdown", Template: "# three",
			}},
			verify: func(t *testing.T, d *dashboard.Dashboard) {
				if len(d.Bricks) != 3 || d.Bricks[2].ID != "b3" {
					t.Fatalf("brick not appended: %+v", d.Bricks)
				}
			},
		},
		{
			name:   "move_brick",
			intent: Intent{Type: IntentMoveBrick, BrickID: "b1", Pos: &dashboard.Position{X: 5, Y: 7}},
			verify: func(t *testing.T, d *dashboard.Dashboard) {
				if d.Bricks[0].Layout.Pos != (dashboard.Position{X: 5, Y: 7}) {
					t.Fatalf("pos not moved: %+v", d.Bricks[0].Layout.Pos)
				}
			},
		},
		{
			name:   "resize_brick",
			intent: Intent{Type: IntentResizeBrick, BrickID: "b2", Size: &dashboard.Size{Width: 6, Height: 3}},
			verify: func(t *testing.T, d *dashboard.Dashboard) {
				if d.Bricks[1].Layout.Size != (dashboard.Size{Width: 6, Height: 3}) {
					t.Fatalf("size not changed: %+v", d.Bricks[1].Layout.Size)
				}
			},
		},
		{
			name:   "delete_brick",
			intent: Intent{Type: IntentDeleteBrick, BrickID: "b1"},
			verify: func(t *testing.T, d *dashboard.Dashboard) {
				if len(d.Bricks) != 1 || d.Bricks[0].ID != "b2" {
					t.Fatalf("brick not deleted: %+v", d.Bricks)
				}
			},
		},
		{
			name:   "edit_template",
			intent: Intent{Type: IntentEditTemplate, BrickID: "b1", Template: "# edited"},
			verify: func(t *testing.T, d *dashboard.Dashboard) {
				if d.Bricks[0].Template != "# edited" {
					t.Fatalf("template not edited: %q", d.Bricks[0].Template)
				}
			},
		},
		{
			name:   "set_variable_existing",
			intent: Intent{Type: IntentSetVariable, Name: "env", Value: "staging"},
			verify: func(t *testing.T, d *dashboard.Dashboard) {
				if len(d.Variables) != 1 || d.Variables[0].Value != "staging" {
					t.Fatalf("variable not replaced: %+v", d.Variables)
				}
			},
		},
		{
			name:   "set_variable_new",
			intent: Intent{Type: IntentSetVariable, Name: "region", Value: "us"},
			verify: func(t *testing.T, d *dashboard.Dashboard) {
				if len(d.Variables) != 2 || d.Variables[1].Name != "region" {
					t.Fatalf("variable not added: %+v", d.Variables)
				}
			},
		},
		{
			name:   "rearrange",
			intent: Intent{Type: IntentRearrange, Order: []string{"b2", "b1"}},
			verify: func(t *testing.T, d *dashboard.Dashboard) {
				if d.Bricks[0].ID != "b2" || d.Bricks[1].ID != "b1" {
					t.Fatalf("bricks not reordered: %+v", d.Bricks)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := newMemStore()
			bc := newRecBroadcaster()
			seedDoc(t, st, "d1")
			d := openDoc(t, st, bc, "d1")

			patch, err := d.Apply(ctx, tc.intent)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if len(patch) == 0 {
				t.Fatal("expected a non-empty applied patch")
			}
			// In-memory doc reflects the change.
			tc.verify(t, d.Snapshot())
			// Snapshotted to the store.
			loaded, err := st.Load(ctx, "d1")
			if err != nil {
				t.Fatalf("store load: %v", err)
			}
			tc.verify(t, loaded)
			// Broadcast exactly once on the patches topic.
			if got := bc.count("d1"); got != 1 {
				t.Fatalf("broadcast count = %d, want 1", got)
			}
		})
	}
}

func TestApplyRejectsInvalidIntent(t *testing.T) {
	ctx := context.Background()
	st := newMemStore()
	bc := newRecBroadcaster()
	seedDoc(t, st, "d1")

	bad := []struct {
		name   string
		intent Intent
		code   Code
	}{
		{"unknown type", Intent{Type: "nope"}, InvalidIntent},
		{"move missing brick", Intent{Type: IntentMoveBrick, BrickID: "ghost", Pos: &dashboard.Position{}}, InvalidIntent},
		{"add nil brick", Intent{Type: IntentAddBrick}, InvalidIntent},
		{"add duplicate id", Intent{Type: IntentAddBrick, Brick: &dashboard.Brick{ID: "b1"}}, InvalidIntent},
		{"delete missing brick", Intent{Type: IntentDeleteBrick, BrickID: "ghost"}, InvalidIntent},
		{"set variable no name", Intent{Type: IntentSetVariable, Value: "x"}, InvalidIntent},
		{"rearrange wrong length", Intent{Type: IntentRearrange, Order: []string{"b1"}}, InvalidIntent},
		{"rearrange unknown id", Intent{Type: IntentRearrange, Order: []string{"b1", "ghost"}}, InvalidIntent},
		{"rearrange duplicate", Intent{Type: IntentRearrange, Order: []string{"b1", "b1"}}, InvalidIntent},
	}

	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			d := openDoc(t, st, bc, "d1")
			before := d.Snapshot()
			_, err := d.Apply(ctx, tc.intent)
			if err == nil {
				t.Fatal("expected rejection")
			}
			if CodeOf(err) != tc.code {
				t.Fatalf("code = %s, want %s", CodeOf(err), tc.code)
			}
			// No state change, no snapshot mutation, no broadcast.
			after := d.Snapshot()
			if !jsonEqual(t, before, after) {
				t.Fatal("document mutated on a rejected intent")
			}
			if bc.count("d1") != 0 {
				t.Fatal("a rejected intent must not broadcast")
			}
		})
	}
}

// TestApplyRejectsInvalidPatch drives an out-of-range RFC6902 op directly to
// confirm patch-level rejection (InvalidPatch), independent of intent checks.
func TestApplyRejectsInvalidPatch(t *testing.T) {
	st := newMemStore()
	seedDoc(t, st, "d1")
	doc, _ := st.Load(context.Background(), "d1")

	raw := []byte(`[{"op":"replace","path":"/bricks/99/template","value":"x"}]`)
	patch, err := jsonpatch.DecodePatch(raw)
	if err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if _, _, err := applyPatch(doc, patch); err == nil || CodeOf(err) != InvalidPatch {
		t.Fatalf("expected InvalidPatch, got %v", err)
	}
}

// TestRehydrateFromStore proves load-on-open: a fresh Doc opened from the store
// after a mutation (simulating a server restart) carries the persisted state.
func TestRehydrateFromStore(t *testing.T) {
	ctx := context.Background()
	st := newMemStore()
	bc := newRecBroadcaster()
	seedDoc(t, st, "d1")

	d1 := openDoc(t, st, bc, "d1")
	if _, err := d1.Apply(ctx, Intent{Type: IntentEditTemplate, BrickID: "b1", Template: "# survives"}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// Discard d1 (server restart): a brand-new Doc opened from the same store
	// must reflect the persisted edit.
	d2 := openDoc(t, st, bc, "d1")
	if got := d2.Snapshot().Bricks[0].Template; got != "# survives" {
		t.Fatalf("rehydrated template = %q, want %q", got, "# survives")
	}
}

// TestConvergence proves two clients converge: applying the same intent stream
// to two independent Docs yields identical documents (and identical broadcast
// patch streams), which is the unit-level proof of A→B convergence.
func TestConvergence(t *testing.T) {
	ctx := context.Background()
	intents := []Intent{
		{Type: IntentAddBrick, Brick: &dashboard.Brick{ID: "b3", Kind: "markdown", Template: "# 3"}},
		{Type: IntentMoveBrick, BrickID: "b3", Pos: &dashboard.Position{X: 4, Y: 4}},
		{Type: IntentSetVariable, Name: "env", Value: "qa"},
		{Type: IntentResizeBrick, BrickID: "b1", Size: &dashboard.Size{Width: 8, Height: 1}},
		{Type: IntentRearrange, Order: []string{"b3", "b2", "b1"}},
		{Type: IntentDeleteBrick, BrickID: "b2"},
	}

	run := func() (*dashboard.Dashboard, []json.RawMessage) {
		st := newMemStore()
		bc := newRecBroadcaster()
		seedDoc(t, st, "d1")
		d := openDoc(t, st, bc, "d1")
		for _, in := range intents {
			if _, err := d.Apply(ctx, in); err != nil {
				t.Fatalf("apply %s: %v", in.Type, err)
			}
		}
		return d.Snapshot(), bc.patches["d1"]
	}

	docA, patchesA := run()
	docB, patchesB := run()

	if !jsonEqual(t, docA, docB) {
		t.Fatal("two clients diverged on the same intent stream")
	}
	if len(patchesA) != len(patchesB) {
		t.Fatalf("broadcast stream lengths differ: %d vs %d", len(patchesA), len(patchesB))
	}
	for i := range patchesA {
		if string(patchesA[i]) != string(patchesB[i]) {
			t.Fatalf("broadcast patch %d differs:\n a=%s\n b=%s", i, patchesA[i], patchesB[i])
		}
	}
}

// TestEditTemplateFiresRenderHook proves render-on-change: an applied
// edit_template intent invokes the render hook with the brick's new state, while
// other intents do not. The hook is what cmd/server wires to render and
// broadcast the fragment on the rendered topic.
func TestEditTemplateFiresRenderHook(t *testing.T) {
	ctx := context.Background()
	st := newMemStore()
	bc := newRecBroadcaster()
	seedDoc(t, st, "d1")

	type call struct {
		dashboardID string
		brick       dashboard.Brick
	}
	var calls []call
	hook := func(_ context.Context, dashboardID string, b dashboard.Brick) {
		calls = append(calls, call{dashboardID, b})
	}

	d, err := Open(ctx, "d1", st, bc, Options{Logger: slog.New(slog.DiscardHandler), RenderHook: hook})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// A non-template intent must not trigger a render.
	if _, err := d.Apply(ctx, Intent{Type: IntentMoveBrick, BrickID: "b1", Pos: &dashboard.Position{X: 1, Y: 1}}); err != nil {
		t.Fatalf("Apply move: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("move_brick fired the render hook: %+v", calls)
	}

	// An edit_template intent must fire the hook once with the changed brick.
	if _, err := d.Apply(ctx, Intent{Type: IntentEditTemplate, BrickID: "b1", Template: "# rendered me"}); err != nil {
		t.Fatalf("Apply edit: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("render hook call count = %d, want 1", len(calls))
	}
	if calls[0].dashboardID != "d1" {
		t.Fatalf("hook dashboard = %q, want d1", calls[0].dashboardID)
	}
	if calls[0].brick.ID != "b1" || calls[0].brick.Template != "# rendered me" {
		t.Fatalf("hook brick = %+v, want b1 with edited template", calls[0].brick)
	}
}

func jsonEqual(t *testing.T, a, b any) bool {
	t.Helper()
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	return string(ra) == string(rb)
}
