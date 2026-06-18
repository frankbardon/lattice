package brickagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/frankbardon/lattice/dashboard"
	"github.com/frankbardon/lattice/render"
	"github.com/frankbardon/lattice/resolve"
	"github.com/frankbardon/lattice/scene"
	"github.com/frankbardon/pulse/types"
)

// memStore is an in-memory scene.Store for the end-to-end test.
type memStore struct {
	mu   sync.Mutex
	docs map[string]*dashboard.Dashboard
}

func newMemStore() *memStore { return &memStore{docs: make(map[string]*dashboard.Dashboard)} }

func (m *memStore) Load(_ context.Context, id string) (*dashboard.Dashboard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.docs[id]
	if !ok {
		return nil, errNotFound{}
	}
	return cloneDoc(d), nil
}

func (m *memStore) Save(_ context.Context, doc *dashboard.Dashboard) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.docs[doc.ID] = cloneDoc(doc)
	return nil
}

type errNotFound struct{}

func (errNotFound) Error() string { return "not found" }

func cloneDoc(d *dashboard.Dashboard) *dashboard.Dashboard {
	raw, _ := json.Marshal(d)
	var out dashboard.Dashboard
	_ = json.Unmarshal(raw, &out)
	return &out
}

// nopBroadcaster swallows patch broadcasts.
type nopBroadcaster struct{}

func (nopBroadcaster) BroadcastPatch(context.Context, string, json.RawMessage) error { return nil }

// stubPulse returns canned rows and records each request so the test can assert
// the renderer ran on the resolved template.
type stubPulse struct {
	mu   sync.Mutex
	reqs []*types.Request
}

func (s *stubPulse) Process(_ context.Context, req *types.Request) (*types.Response, error) {
	s.mu.Lock()
	s.reqs = append(s.reqs, req)
	s.mu.Unlock()
	return &types.Response{Data: []map[string]any{
		{"month": "jan", "revenue": 10.0},
		{"month": "feb", "revenue": 20.0},
	}}, nil
}

func (s *stubPulse) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reqs)
}

// TestBuild_EndToEnd_RenderAndReRender drives the build loop through a REAL
// scene.Manager wired to the REAL pulse_prism renderer (with a stubbed Pulse
// data source — no child process, no LLM). It asserts:
//   - the agent's parameterized template is applied as an edit_template patch;
//   - the scene RenderHook fires and produces an SVG fragment (render ran);
//   - a subsequent set_variable change re-renders the brick WITHOUT re-invoking
//     the agent (the E3-S2 parameterization payoff).
func TestBuild_EndToEnd_RenderAndReRender(t *testing.T) {
	ctx := context.Background()
	st := newMemStore()
	// Seed a board with a pulse_prism brick + the region variable the template
	// parameterizes.
	st.docs["d1"] = &dashboard.Dashboard{
		ID: "d1",
		Variables: []dashboard.Variable{
			{Name: "region", Type: dashboard.VarString, Value: "west"},
		},
		Bricks: []dashboard.Brick{
			{ID: "b1", Kind: render.KindPulsePrism, AgentID: "agent-b1"},
		},
	}

	pulse := &stubPulse{}
	reg := render.NewRegistry(render.Options{Logger: slog.New(slog.DiscardHandler)})
	if err := reg.Register(render.KindPulsePrism, render.NewPulsePrism(pulse)); err != nil {
		t.Fatalf("register: %v", err)
	}

	var (
		mu        sync.Mutex
		fragments []string
	)
	hook := func(_ context.Context, _ string, brick dashboard.Brick, vars []dashboard.Variable) {
		vals := resolve.FromVariables(vars)
		resolved := resolve.Substitute(brick.Template, vals, brick.Kind == render.KindPulsePrism)
		html, err := reg.Render(brick.Kind, resolved, render.ResolvedVars{})
		if err != nil {
			t.Errorf("render hook: %v", err)
			return
		}
		mu.Lock()
		fragments = append(fragments, html)
		mu.Unlock()
	}

	mgr, err := scene.NewManager(st, nopBroadcaster{}, scene.Options{
		Logger:     slog.New(slog.DiscardHandler),
		RenderHook: hook,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	driver := &stubDriver{reply: paramTemplate}
	b := newBuilder(t, driver, SceneManagerStore{Manager: mgr})

	// Build: drive the agent (canned) and apply its template.
	if _, err := b.Build(ctx, "d1", "b1", "Show me revenue by month"); err != nil {
		t.Fatalf("Build: %v", err)
	}

	mu.Lock()
	gotFrags := len(fragments)
	var lastFrag string
	if gotFrags > 0 {
		lastFrag = fragments[gotFrags-1]
	}
	mu.Unlock()

	if gotFrags != 1 {
		t.Fatalf("render hook fired %d times after build, want 1", gotFrags)
	}
	if !strings.Contains(lastFrag, "<svg") {
		t.Fatalf("rendered fragment is not SVG: %q", lastFrag)
	}
	if pulse.count() != 1 {
		t.Fatalf("pulse queried %d times, want 1", pulse.count())
	}
	// The agent was driven exactly once for the build.
	if driver.calls != 1 {
		t.Fatalf("agent driven %d times, want 1", driver.calls)
	}

	// The template stored on the brick must be the parameterized one (the
	// ${region} placeholder survives into scene, so re-render can resolve it).
	snap, _ := mgr.Doc(ctx, "d1")
	stored := snap.Snapshot().Bricks[0].Template
	if !strings.Contains(stored, "${region}") {
		t.Fatalf("stored template not parameterized: %s", stored)
	}

	// Now change the variable. This must re-render the brick (E3-S2) WITHOUT
	// driving the agent again — proving parameterization decouples re-render
	// from the LLM.
	setVar := scene.Intent{Type: scene.IntentSetVariable, Name: "region", Value: "east"}
	raw, _ := json.Marshal(setVar)
	if _, err := mgr.HandleIntent(ctx, "d1", raw); err != nil {
		t.Fatalf("set_variable: %v", err)
	}

	mu.Lock()
	gotFrags = len(fragments)
	mu.Unlock()
	if gotFrags != 2 {
		t.Fatalf("render hook fired %d times after var change, want 2", gotFrags)
	}
	if driver.calls != 1 {
		t.Fatalf("agent driven %d times after var change, want 1 (no re-invoke)", driver.calls)
	}
	if pulse.count() != 2 {
		t.Fatalf("pulse queried %d times after var change, want 2", pulse.count())
	}
}
