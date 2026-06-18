package layoutagent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/frankbardon/lattice/agenthub"
	"github.com/frankbardon/lattice/brickagent"
	"github.com/frankbardon/lattice/dashboard"
	"github.com/frankbardon/lattice/render"
	"github.com/frankbardon/lattice/resolve"
	"github.com/frankbardon/lattice/scene"
	"github.com/frankbardon/pulse/types"
)

// liveCreds gates the live end-to-end test. It needs an Anthropic API key (both
// the layout and brick agents make real LLM calls) and a pulse binary (the
// brick agents' MCP client execs it). When either is absent the test skips,
// keeping CI green without creds or external tools (mirrors the agenthub /
// brickagent skip pattern).
func liveCreds(t *testing.T) (pulseBin, dataDir string) {
	t.Helper()
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY unset; skipping live layout coordinator round-trip")
	}
	if p := os.Getenv("LATTICE_PULSE_BIN"); p != "" {
		if _, err := os.Stat(p); err != nil {
			t.Skipf("LATTICE_PULSE_BIN=%q not found; skipping", p)
		}
		pulseBin = p
	} else if p, err := exec.LookPath("pulse"); err == nil {
		pulseBin = p
	} else {
		t.Skip("pulse binary not found (set LATTICE_PULSE_BIN or install on PATH); skipping")
	}
	dataDir = os.Getenv("PULSE_DATA_DIR")
	if dataDir == "" {
		dataDir = t.TempDir()
	}
	return pulseBin, dataDir
}

// memStore is an in-memory scene.Store for the end-to-end test.
type memStore struct {
	mu   sync.Mutex
	docs map[string]*dashboard.Dashboard
}

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

type nopBroadcaster struct{}

func (nopBroadcaster) BroadcastPatch(context.Context, string, json.RawMessage) error { return nil }

// stubPulse returns canned rows so the brick renderer can produce an SVG without
// a real Pulse query (the brick agent still reaches the live pulse MCP child for
// its tool calls; this only backs the renderer).
type stubPulse struct{}

func (stubPulse) Process(_ context.Context, _ *types.Request) (*types.Response, error) {
	return &types.Response{Data: []map[string]any{{"region": "west", "AGG_SUM_amount": 10.0}}}, nil
}

// brickBuilderAdapter bridges *brickagent.Builder to BrickBuilder for the test.
type brickBuilderAdapter struct{ b *brickagent.Builder }

func (a brickBuilderAdapter) BuildBrick(ctx context.Context, dashboardID, brickID, prompt string) error {
	_, err := a.b.Build(ctx, dashboardID, brickID, prompt)
	return err
}

// TestIntegration_SalesOverview is the acceptance check for E5-S1: "Build me a
// sales overview with three charts" must yield an arranged board whose bricks
// are filled in by brick agents. It boots a REAL layout coordinator + real brick
// agents through one agenthub.Hub, applies the plan through a real scene.Manager
// wired to the pulse_prism renderer, and asserts the board ends up arranged with
// rendered fragments. It skips cleanly without creds/binaries.
func TestIntegration_SalesOverview(t *testing.T) {
	pulseBin, dataDir := liveCreds(t)

	st := &memStore{docs: map[string]*dashboard.Dashboard{
		"d1": {ID: "d1", Variables: []dashboard.Variable{}, Bricks: []dashboard.Brick{}},
	}}

	reg := render.NewRegistry(render.Options{})
	if err := reg.Register(render.KindPulsePrism, render.NewPulsePrism(stubPulse{})); err != nil {
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
			return
		}
		mu.Lock()
		fragments = append(fragments, html)
		mu.Unlock()
	}

	mgr, err := scene.NewManager(st, nopBroadcaster{}, scene.Options{RenderHook: hook})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ahCfg := agenthub.DefaultConfig()
	ahCfg.PulseBinaryPath = pulseBin
	ahCfg.PulseDataDir = dataDir
	ahCfg.SessionsRoot = t.TempDir()
	ahCfg.IdleTimeout = 0
	ahCfg.DriveTimeout = 2 * time.Minute
	ahCfg.OutputSchema = brickagent.TemplateSchema
	ahCfg.LayoutOutputSchema = PlanSchema
	hub, err := agenthub.NewHub(ahCfg, agenthub.Options{})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	t.Cleanup(func() { _ = hub.Close() })

	sceneStore := brickagent.SceneManagerStore{Manager: mgr}
	builder, err := brickagent.NewBuilder(hub, sceneStore, brickagent.Options{})
	if err != nil {
		t.Fatalf("NewBuilder: %v", err)
	}
	coord, err := NewCoordinator(hub, sceneStore, brickBuilderAdapter{builder}, Options{})
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	res, err := coord.Coordinate(ctx, "d1", "Build me a sales overview with three charts")
	if err != nil {
		t.Fatalf("Coordinate: %v", err)
	}
	if len(res.Created) == 0 {
		t.Fatal("layout coordinator created no bricks")
	}

	snap, _ := mgr.Doc(ctx, "d1")
	doc := snap.Snapshot()
	if len(doc.Bricks) == 0 {
		t.Fatal("board has no bricks after coordination")
	}
	for _, b := range doc.Bricks {
		if b.AgentID == "" {
			t.Fatalf("brick %s has no server-assigned agent_id", b.ID)
		}
		if strings.TrimSpace(b.Template) == "" {
			t.Errorf("brick %s was not filled in by its agent (empty template)", b.ID)
		}
	}
	t.Logf("created %d bricks; rendered %d fragments", len(doc.Bricks), len(fragments))
}
