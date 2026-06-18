package render

import (
	"context"
	"errors"
	"strings"
	"testing"

	prismspec "github.com/frankbardon/prism/spec"
	"github.com/frankbardon/pulse/types"
)

// stubData is a PulseDataSource that returns canned rows (or an error) without
// spawning a Pulse child process, so the pulse_prism pipeline is unit-testable.
type stubData struct {
	rows []map[string]any
	err  error
	got  *types.Request
}

func (s *stubData) Process(_ context.Context, req *types.Request) (*types.Response, error) {
	s.got = req
	if s.err != nil {
		return nil, s.err
	}
	return &types.Response{Data: s.rows}, nil
}

// a minimal bar-chart spec referencing fields the stub rows carry.
const barSpec = `{
  "mark": "bar",
  "encoding": {
    "x": {"field": "region", "type": "nominal"},
    "y": {"field": "amount", "type": "quantitative"}
  }
}`

func barTemplate(t *testing.T) string {
	t.Helper()
	return `{"pulse_request": {"cohort": {"filename": "sales.pulse"}}, "prism_spec": ` + barSpec + `}`
}

// TestPulsePrismRenderProducesSVG confirms the full row→spec→SVG mapping yields
// an <svg> fragment, using a stubbed data source (no Pulse child).
func TestPulsePrismRenderProducesSVG(t *testing.T) {
	data := &stubData{rows: []map[string]any{
		{"region": "west", "amount": 40.0},
		{"region": "east", "amount": 20.0},
	}}
	r := NewPulsePrism(data)

	got, err := r.Render(barTemplate(t), ResolvedVars{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "<svg") {
		t.Fatalf("fragment is not SVG: %q", got)
	}
	// The Pulse request from the template must reach the data source intact.
	if data.got == nil || data.got.Cohort == nil || data.got.Cohort.Filename != "sales.pulse" {
		t.Fatalf("pulse request not forwarded as authored: %+v", data.got)
	}
}

// TestPulsePrismRowsInjected confirms the Pulse rows become the spec's inline
// data: an empty result and a populated result render differently.
func TestPulsePrismRowsInjected(t *testing.T) {
	spec, err := prismspec.DecodeBytes([]byte(barSpec))
	if err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	r := NewPulsePrism(&stubData{})

	rows := []map[string]any{{"region": "west", "amount": 10.0}, {"region": "east", "amount": 30.0}}
	withRows, err := r.renderRows(spec, rows)
	if err != nil {
		t.Fatalf("renderRows: %v", err)
	}
	if spec.Data == nil || len(spec.Data.Values) != 2 {
		t.Fatalf("rows not injected as inline values: %+v", spec.Data)
	}
	if !strings.Contains(withRows, "<svg") {
		t.Fatalf("expected svg, got %q", withRows)
	}
}

// TestPulsePrismInvalidTemplate covers the template-parsing failure modes, which
// must surface the typed InvalidTemplate code.
func TestPulsePrismInvalidTemplate(t *testing.T) {
	r := NewPulsePrism(&stubData{})
	tests := []struct {
		name, template string
	}{
		{"not json", "{not json"},
		{"missing pulse_request", `{"prism_spec": ` + barSpec + `}`},
		{"missing prism_spec", `{"pulse_request": {}}`},
		{"bad prism_spec", `{"pulse_request": {}, "prism_spec": {"mark": 123}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := r.Render(tc.template, ResolvedVars{}); CodeOf(err) != InvalidTemplate {
				t.Fatalf("code = %s, want %s (err=%v)", CodeOf(err), InvalidTemplate, err)
			}
		})
	}
}

// TestPulsePrismDataSourceError confirms a Pulse failure is wrapped as DataSource.
func TestPulsePrismDataSourceError(t *testing.T) {
	r := NewPulsePrism(&stubData{err: errors.New("boom")})
	if _, err := r.Render(barTemplate(t), ResolvedVars{}); CodeOf(err) != DataSource {
		t.Fatalf("code = %s, want %s (err=%v)", CodeOf(err), DataSource, err)
	}
}

// TestPulsePrismRegistryDispatch confirms the kind routes through the Registry,
// proving it integrates with the same dispatch seam as markdown.
func TestPulsePrismRegistryDispatch(t *testing.T) {
	reg := NewRegistry(testOpts())
	data := &stubData{rows: []map[string]any{{"region": "west", "amount": 5.0}}}
	if err := reg.Register(KindPulsePrism, NewPulsePrism(data)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, err := reg.Render(KindPulsePrism, barTemplate(t), ResolvedVars{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "<svg") {
		t.Fatalf("dispatch did not produce SVG: %q", got)
	}
}
