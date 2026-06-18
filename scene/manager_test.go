package scene

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/frankbardon/lattice/dashboard"
)

// TestManagerHandleIntent exercises the realtime-facing path: raw JSON intent →
// decode → load-on-open → apply → persisted + broadcast.
func TestManagerHandleIntent(t *testing.T) {
	ctx := context.Background()
	st := newMemStore()
	bc := newRecBroadcaster()
	seedDoc(t, st, "d1")

	mgr, err := NewManager(st, bc, testOpts())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	raw, _ := json.Marshal(Intent{Type: IntentEditTemplate, BrickID: "b1", Template: "# hi"})
	patch, err := mgr.HandleIntent(ctx, "d1", raw)
	if err != nil {
		t.Fatalf("HandleIntent: %v", err)
	}
	if len(patch) == 0 {
		t.Fatal("expected applied patch")
	}

	// Same Doc instance returned on a second open (single authority per id).
	d1, _ := mgr.Doc(ctx, "d1")
	d2, _ := mgr.Doc(ctx, "d1")
	if d1 != d2 {
		t.Fatal("manager returned two Docs for one dashboard")
	}
	if got := d1.Snapshot().Bricks[0].Template; got != "# hi" {
		t.Fatalf("template = %q, want %q", got, "# hi")
	}
}

func TestManagerRejectsBadIntent(t *testing.T) {
	ctx := context.Background()
	st := newMemStore()
	bc := newRecBroadcaster()
	seedDoc(t, st, "d1")
	mgr, _ := NewManager(st, bc, testOpts())

	if _, err := mgr.HandleIntent(ctx, "d1", []byte("not json")); err == nil {
		t.Fatal("malformed intent must be rejected")
	}
	if _, err := mgr.HandleIntent(ctx, "d1", mustJSON(t, Intent{Type: IntentDeleteBrick, BrickID: "ghost"})); err == nil {
		t.Fatal("intent on missing brick must be rejected")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// ensure the seed brick model stays in sync with the package types.
var _ = dashboard.Brick{}
