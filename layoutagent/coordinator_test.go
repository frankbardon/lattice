package layoutagent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/frankbardon/lattice/dashboard"
	"github.com/frankbardon/lattice/scene"
)

// stubDriver returns a canned layout-agent reply (or an error) without an LLM.
type stubDriver struct {
	reply  string
	err    error
	gotID  string
	gotMsg string
	calls  int
}

func (s *stubDriver) Drive(_ context.Context, agentID, content string) (string, error) {
	s.calls++
	s.gotID = agentID
	s.gotMsg = content
	return s.reply, s.err
}

// stubScene records the intents the coordinator applies and serves a snapshot
// that grows as add_brick intents are applied (so freshBrickID sees prior ids).
type stubScene struct {
	doc      *dashboard.Dashboard
	snapErr  error
	applyErr error
	applied  []scene.Intent
}

func newStubScene() *stubScene {
	return &stubScene{doc: &dashboard.Dashboard{ID: "d1"}}
}

func (s *stubScene) Snapshot(_ context.Context, _ string) (*dashboard.Dashboard, error) {
	if s.snapErr != nil {
		return nil, s.snapErr
	}
	// Return a copy so the coordinator cannot mutate our state.
	raw, _ := json.Marshal(s.doc)
	var out dashboard.Dashboard
	_ = json.Unmarshal(raw, &out)
	return &out, nil
}

func (s *stubScene) HandleIntent(_ context.Context, _ string, raw json.RawMessage) (json.RawMessage, error) {
	if s.applyErr != nil {
		return nil, s.applyErr
	}
	var in scene.Intent
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	s.applied = append(s.applied, in)
	// Mirror the scene engine just enough that subsequent snapshots reflect the
	// applied structural change (add/delete), so freshBrickID stays unique.
	switch in.Type {
	case scene.IntentAddBrick:
		if in.Brick != nil {
			s.doc.Bricks = append(s.doc.Bricks, *in.Brick)
		}
	case scene.IntentDeleteBrick:
		for i := range s.doc.Bricks {
			if s.doc.Bricks[i].ID == in.BrickID {
				s.doc.Bricks = append(s.doc.Bricks[:i], s.doc.Bricks[i+1:]...)
				break
			}
		}
	}
	return json.RawMessage(`[{"op":"test"}]`), nil
}

// mockBuilder records each delegated brick build and can be made to fail.
type mockBuilder struct {
	calls []struct{ brickID, prompt string }
	err   error
}

func (m *mockBuilder) BuildBrick(_ context.Context, _, brickID, prompt string) error {
	m.calls = append(m.calls, struct{ brickID, prompt string }{brickID, prompt})
	return m.err
}

func newCoordinator(t *testing.T, d AgentDriver, sc SceneStore, b BrickBuilder) *Coordinator {
	t.Helper()
	c, err := NewCoordinator(d, sc, b, Options{Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	return c
}

// TestCoordinate_CreatesArrangesAndDelegates is the core of the loop: a canned
// plan with three create_brick actions produces three add_brick intents with
// SERVER-ASSIGNED agent_ids, and each brick's content is DELEGATED to the brick
// builder with the per-brick prompt. No LLM is involved.
func TestCoordinate_CreatesArrangesAndDelegates(t *testing.T) {
	plan := `{"actions":[
      {"type":"create_brick","position":{"x":0,"y":0},"size":{"width":6,"height":4},"prompt":"total sales by region"},
      {"type":"create_brick","position":{"x":6,"y":0},"size":{"width":6,"height":4},"prompt":"sales over time"},
      {"type":"create_brick","position":{"x":0,"y":4},"size":{"width":12,"height":3},"prompt":"top products"}
    ]}`
	d := &stubDriver{reply: plan}
	sc := newStubScene()
	mb := &mockBuilder{}
	c := newCoordinator(t, d, sc, mb)

	res, err := c.Coordinate(context.Background(), "d1", "Build me a sales overview with three charts")
	if err != nil {
		t.Fatalf("Coordinate: %v", err)
	}

	// The layout agent was driven with the layout-keyed agent id.
	if d.gotID != AgentID("d1") {
		t.Fatalf("driven agent id = %q, want %q", d.gotID, AgentID("d1"))
	}
	if !strings.HasPrefix(d.gotID, "layout:") {
		t.Fatalf("layout agent id not keyed distinctly: %q", d.gotID)
	}

	// Three add_brick intents applied through scene (server-authoritative).
	var adds int
	for _, in := range sc.applied {
		if in.Type != scene.IntentAddBrick {
			continue
		}
		adds++
		if in.Brick == nil {
			t.Fatal("add_brick intent has no brick")
		}
		// Server-assigned agent_id, bound to the brick (not client-seeded).
		if in.Brick.AgentID == "" {
			t.Fatalf("brick %s has no server-assigned agent_id", in.Brick.ID)
		}
		if in.Brick.AgentID != "brick:"+in.Brick.ID {
			t.Fatalf("agent_id %q not bound to brick id %q", in.Brick.AgentID, in.Brick.ID)
		}
		if in.Brick.Kind != defaultBrickKind {
			t.Fatalf("brick kind = %q, want %q", in.Brick.Kind, defaultBrickKind)
		}
	}
	if adds != 3 {
		t.Fatalf("add_brick intents = %d, want 3", adds)
	}

	// Each created brick was delegated to the brick builder with its prompt.
	if len(mb.calls) != 3 {
		t.Fatalf("delegated builds = %d, want 3", len(mb.calls))
	}
	wantPrompts := map[string]bool{"total sales by region": true, "sales over time": true, "top products": true}
	for _, call := range mb.calls {
		if !wantPrompts[call.prompt] {
			t.Fatalf("unexpected delegated prompt %q", call.prompt)
		}
	}

	// Result echoes the patches + created bricks.
	if len(res.Patches) != 3 {
		t.Fatalf("result patches = %d, want 3", len(res.Patches))
	}
	if len(res.Created) != 3 {
		t.Fatalf("result created = %d, want 3", len(res.Created))
	}
	for _, cb := range res.Created {
		if cb.DelegateError != "" {
			t.Fatalf("unexpected delegate error: %s", cb.DelegateError)
		}
		if cb.AgentID != "brick:"+cb.BrickID {
			t.Fatalf("created agent_id %q not bound to brick %q", cb.AgentID, cb.BrickID)
		}
	}
}

// TestCoordinate_ArrangeMutations maps move/resize/delete actions to the right
// scene intents (no delegation for these).
func TestCoordinate_ArrangeMutations(t *testing.T) {
	plan := `{"actions":[
      {"type":"move_brick","brick_id":"b1","position":{"x":3,"y":2}},
      {"type":"resize_brick","brick_id":"b2","size":{"width":8,"height":5}},
      {"type":"delete_brick","brick_id":"b3"}
    ]}`
	sc := newStubScene()
	sc.doc.Bricks = []dashboard.Brick{{ID: "b3"}}
	mb := &mockBuilder{}
	c := newCoordinator(t, &stubDriver{reply: plan}, sc, mb)

	if _, err := c.Coordinate(context.Background(), "d1", "tidy up the board"); err != nil {
		t.Fatalf("Coordinate: %v", err)
	}
	if len(mb.calls) != 0 {
		t.Fatalf("no delegation expected for arrange-only plan, got %d", len(mb.calls))
	}
	wantTypes := []scene.IntentType{scene.IntentMoveBrick, scene.IntentResizeBrick, scene.IntentDeleteBrick}
	if len(sc.applied) != len(wantTypes) {
		t.Fatalf("applied %d intents, want %d", len(sc.applied), len(wantTypes))
	}
	for i, want := range wantTypes {
		if sc.applied[i].Type != want {
			t.Fatalf("applied[%d].Type = %s, want %s", i, sc.applied[i].Type, want)
		}
	}
	// move/resize carried their payloads through.
	if sc.applied[0].Pos == nil || sc.applied[0].Pos.X != 3 {
		t.Fatalf("move_brick pos not carried: %+v", sc.applied[0].Pos)
	}
	if sc.applied[1].Size == nil || sc.applied[1].Size.Width != 8 {
		t.Fatalf("resize_brick size not carried: %+v", sc.applied[1].Size)
	}
}

// TestCoordinate_DelegationFailureNonFatal: a brick agent failing to build its
// content does NOT unwind the arranged board — the brick still exists (its
// add_brick applied) and the error is reported on the CreatedBrick.
func TestCoordinate_DelegationFailureNonFatal(t *testing.T) {
	plan := `{"actions":[{"type":"create_brick","position":{"x":0,"y":0},"size":{"width":6,"height":4},"prompt":"x"}]}`
	sc := newStubScene()
	mb := &mockBuilder{err: errors.New("agent boom")}
	c := newCoordinator(t, &stubDriver{reply: plan}, sc, mb)

	res, err := c.Coordinate(context.Background(), "d1", "make a chart")
	if err != nil {
		t.Fatalf("Coordinate must not fail on a delegation miss: %v", err)
	}
	if len(res.Created) != 1 {
		t.Fatalf("created = %d, want 1", len(res.Created))
	}
	if res.Created[0].DelegateError == "" {
		t.Fatal("delegate error not reported on the created brick")
	}
	// The brick was still added to the board.
	if len(sc.doc.Bricks) != 1 {
		t.Fatalf("brick not added despite delegation failure: %d bricks", len(sc.doc.Bricks))
	}
}

// TestCoordinate_RejectsBadRequest covers request-level validation.
func TestCoordinate_RejectsBadRequest(t *testing.T) {
	c := newCoordinator(t, &stubDriver{reply: "{}"}, newStubScene(), &mockBuilder{})
	for _, tc := range []struct{ dash, msg string }{
		{"", "go"},
		{"d1", "  "},
	} {
		if _, err := c.Coordinate(context.Background(), tc.dash, tc.msg); CodeOf(err) != InvalidRequest {
			t.Fatalf("Coordinate(%q,%q) code = %s, want %s", tc.dash, tc.msg, CodeOf(err), InvalidRequest)
		}
	}
}

// TestCoordinate_AgentAndApplyErrors confirms driver and scene failures surface
// with the right codes and leave the board unchanged.
func TestCoordinate_AgentAndApplyErrors(t *testing.T) {
	t.Run("agentFailed", func(t *testing.T) {
		c := newCoordinator(t, &stubDriver{err: errors.New("boom")}, newStubScene(), &mockBuilder{})
		_, err := c.Coordinate(context.Background(), "d1", "go")
		if CodeOf(err) != AgentFailed {
			t.Fatalf("code = %s, want %s", CodeOf(err), AgentFailed)
		}
	})
	t.Run("invalidOutput", func(t *testing.T) {
		c := newCoordinator(t, &stubDriver{reply: "not a plan"}, newStubScene(), &mockBuilder{})
		_, err := c.Coordinate(context.Background(), "d1", "go")
		if CodeOf(err) != InvalidOutput {
			t.Fatalf("code = %s, want %s", CodeOf(err), InvalidOutput)
		}
	})
	t.Run("applyFailed", func(t *testing.T) {
		plan := `{"actions":[{"type":"delete_brick","brick_id":"b1"}]}`
		sc := newStubScene()
		sc.applyErr = errors.New("bad patch")
		c := newCoordinator(t, &stubDriver{reply: plan}, sc, &mockBuilder{})
		_, err := c.Coordinate(context.Background(), "d1", "go")
		if CodeOf(err) != ApplyFailed {
			t.Fatalf("code = %s, want %s", CodeOf(err), ApplyFailed)
		}
	})
}
