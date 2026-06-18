package brickagent

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

// a parameterized {pulse_request, prism_spec} template the agent might emit:
// the row limit and region are ${var} placeholders, so a later variable change
// re-renders without re-invoking the agent (the E3-S2 path).
const paramTemplate = `{
  "pulse_request": {
    "cohort": {"filename": "sales.pulse"},
    "filterers": [{"field": "region", "op": "OP_EQ", "value": "${region}"}],
    "aggregations": [{"type": "AGG_SUM", "field": "amount", "label": "revenue"}],
    "groups": [{"field": "month"}]
  },
  "prism_spec": {
    "mark": "bar",
    "encoding": {
      "x": {"field": "month", "type": "nominal"},
      "y": {"field": "revenue", "type": "quantitative"}
    }
  }
}`

// stubDriver returns a canned agent reply (or an error) without any LLM.
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

// stubScene resolves a brick's agent_id from a fixed dashboard and records the
// intent applied to it. It avoids depending on a real scene.Manager so the
// build loop is unit-testable on its own.
type stubScene struct {
	doc        *dashboard.Dashboard
	snapErr    error
	applyErr   error
	gotIntent  scene.Intent
	gotApplied bool
}

func (s *stubScene) Snapshot(_ context.Context, _ string) (*dashboard.Dashboard, error) {
	if s.snapErr != nil {
		return nil, s.snapErr
	}
	return s.doc, nil
}

func (s *stubScene) HandleIntent(_ context.Context, _ string, raw json.RawMessage) (json.RawMessage, error) {
	if s.applyErr != nil {
		return nil, s.applyErr
	}
	if err := json.Unmarshal(raw, &s.gotIntent); err != nil {
		return nil, err
	}
	s.gotApplied = true
	return json.RawMessage(`[{"op":"replace","path":"/bricks/0/template","value":"x"}]`), nil
}

func newBuilder(t *testing.T, d AgentDriver, sc SceneStore) *Builder {
	t.Helper()
	b, err := NewBuilder(d, sc, Options{Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatalf("NewBuilder: %v", err)
	}
	return b
}

func brickDoc(agentID string) *dashboard.Dashboard {
	return &dashboard.Dashboard{
		ID: "d1",
		Bricks: []dashboard.Brick{
			{ID: "b1", Kind: "pulse_prism", AgentID: agentID},
		},
	}
}

// TestBuild_AppliesParameterizedTemplate is the core of the loop: a canned agent
// reply is validated, applied as an edit_template intent, and the applied
// template is the parameterized one (so a later variable change re-renders
// without the agent). No LLM is involved.
func TestBuild_AppliesParameterizedTemplate(t *testing.T) {
	d := &stubDriver{reply: paramTemplate}
	sc := &stubScene{doc: brickDoc("agent-b1")}
	b := newBuilder(t, d, sc)

	res, err := b.Build(context.Background(), "d1", "b1", "Show me revenue by month")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// The agent was driven with the right agent_id and message.
	if d.gotID != "agent-b1" {
		t.Fatalf("driven agent id = %q, want agent-b1", d.gotID)
	}
	if d.gotMsg != "Show me revenue by month" {
		t.Fatalf("driven message = %q", d.gotMsg)
	}

	// An edit_template intent for the right brick was applied through scene.
	if !sc.gotApplied {
		t.Fatal("no intent applied to scene")
	}
	if sc.gotIntent.Type != scene.IntentEditTemplate {
		t.Fatalf("intent type = %q, want edit_template", sc.gotIntent.Type)
	}
	if sc.gotIntent.BrickID != "b1" {
		t.Fatalf("intent brick id = %q, want b1", sc.gotIntent.BrickID)
	}

	// The applied template is parameterized and is valid JSON.
	if !isParameterized([]byte(sc.gotIntent.Template)) {
		t.Fatalf("applied template is not parameterized: %s", sc.gotIntent.Template)
	}
	if !strings.Contains(sc.gotIntent.Template, "${region}") {
		t.Fatalf("applied template dropped ${region}: %s", sc.gotIntent.Template)
	}
	if err := validateTemplate([]byte(sc.gotIntent.Template)); err != nil {
		t.Fatalf("applied template fails schema: %v", err)
	}

	// The result echoes the applied patch + template.
	if len(res.Patch) == 0 {
		t.Fatal("result missing applied patch")
	}
	if res.Template == "" {
		t.Fatal("result missing template")
	}
}

// TestBuild_AcceptsFencedAndProseWrappedOutput confirms the loop extracts the
// JSON object even when the model wraps it in a ```json fence or stray prose.
func TestBuild_AcceptsFencedAndProseWrappedOutput(t *testing.T) {
	cases := map[string]string{
		"fenced":    "```json\n" + paramTemplate + "\n```",
		"proseWrap": "Here is your brick:\n" + paramTemplate + "\nHope that helps!",
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			d := &stubDriver{reply: reply}
			sc := &stubScene{doc: brickDoc("agent-b1")}
			b := newBuilder(t, d, sc)
			if _, err := b.Build(context.Background(), "d1", "b1", "go"); err != nil {
				t.Fatalf("Build: %v", err)
			}
			if !sc.gotApplied {
				t.Fatal("template not applied")
			}
		})
	}
}

// TestBuild_RejectsInvalidOutput confirms a non-conforming agent reply is
// rejected (InvalidOutput) and NO intent is applied to scene.
func TestBuild_RejectsInvalidOutput(t *testing.T) {
	cases := map[string]string{
		"notJSON":          "I could not build that.",
		"missingPrismSpec": `{"pulse_request": {"cohort": {"filename": "x.pulse"}}}`,
		"prismNoMark":      `{"pulse_request": {}, "prism_spec": {"encoding": {}}}`,
		"extraKey":         `{"pulse_request": {}, "prism_spec": {"mark": "bar"}, "evil": 1}`,
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			d := &stubDriver{reply: reply}
			sc := &stubScene{doc: brickDoc("agent-b1")}
			b := newBuilder(t, d, sc)
			_, err := b.Build(context.Background(), "d1", "b1", "go")
			if err == nil {
				t.Fatal("expected an error")
			}
			if CodeOf(err) != InvalidOutput {
				t.Fatalf("code = %s, want %s (%v)", CodeOf(err), InvalidOutput, err)
			}
			if sc.gotApplied {
				t.Fatal("invalid output must not be applied to scene")
			}
		})
	}
}

// TestBuild_BrickResolution covers the agent-id resolution failures.
func TestBuild_BrickResolution(t *testing.T) {
	t.Run("unknownBrick", func(t *testing.T) {
		d := &stubDriver{reply: paramTemplate}
		sc := &stubScene{doc: brickDoc("agent-b1")}
		b := newBuilder(t, d, sc)
		_, err := b.Build(context.Background(), "d1", "missing", "go")
		if CodeOf(err) != BrickNotFound {
			t.Fatalf("code = %s, want %s", CodeOf(err), BrickNotFound)
		}
		if d.calls != 0 {
			t.Fatal("agent must not be driven for an unknown brick")
		}
	})
	t.Run("noAgentID", func(t *testing.T) {
		doc := brickDoc("")
		d := &stubDriver{reply: paramTemplate}
		sc := &stubScene{doc: doc}
		b := newBuilder(t, d, sc)
		_, err := b.Build(context.Background(), "d1", "b1", "go")
		if CodeOf(err) != BrickNotFound {
			t.Fatalf("code = %s, want %s", CodeOf(err), BrickNotFound)
		}
	})
}

// TestBuild_AgentAndApplyErrors confirms driver and scene failures surface with
// the right codes.
func TestBuild_AgentAndApplyErrors(t *testing.T) {
	t.Run("agentFailed", func(t *testing.T) {
		d := &stubDriver{err: errors.New("boom")}
		sc := &stubScene{doc: brickDoc("agent-b1")}
		b := newBuilder(t, d, sc)
		_, err := b.Build(context.Background(), "d1", "b1", "go")
		if CodeOf(err) != AgentFailed {
			t.Fatalf("code = %s, want %s", CodeOf(err), AgentFailed)
		}
	})
	t.Run("applyFailed", func(t *testing.T) {
		d := &stubDriver{reply: paramTemplate}
		sc := &stubScene{doc: brickDoc("agent-b1"), applyErr: errors.New("bad patch")}
		b := newBuilder(t, d, sc)
		_, err := b.Build(context.Background(), "d1", "b1", "go")
		if CodeOf(err) != ApplyFailed {
			t.Fatalf("code = %s, want %s", CodeOf(err), ApplyFailed)
		}
	})
}

// TestBuild_RejectsBadRequest covers the request-level validation.
func TestBuild_RejectsBadRequest(t *testing.T) {
	d := &stubDriver{reply: paramTemplate}
	sc := &stubScene{doc: brickDoc("agent-b1")}
	b := newBuilder(t, d, sc)
	for _, tc := range []struct{ dash, brick, msg string }{
		{"", "b1", "go"},
		{"d1", "", "go"},
		{"d1", "b1", "   "},
	} {
		if _, err := b.Build(context.Background(), tc.dash, tc.brick, tc.msg); CodeOf(err) != InvalidRequest {
			t.Fatalf("Build(%q,%q,%q) code = %s, want %s", tc.dash, tc.brick, tc.msg, CodeOf(err), InvalidRequest)
		}
	}
}
