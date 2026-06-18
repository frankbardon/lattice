package render

import (
	"context"
	"encoding/json"

	"github.com/frankbardon/prism"
	prismrender "github.com/frankbardon/prism/render"
	"github.com/frankbardon/prism/render/svg"
	prismspec "github.com/frankbardon/prism/spec"
	"github.com/frankbardon/pulse/types"
)

// KindPulsePrism is the brick kind handled by the PulsePrism Renderer.
const KindPulsePrism = "pulse_prism"

// PulseDataSource is the slice of the Pulse MCP manager the renderer needs: run
// a declarative Pulse request and return the table-row response. *pulsemcp.Manager
// satisfies it via its Process method. Depending on this narrow interface rather
// than the concrete manager keeps render/ decoupled from the child-process
// lifecycle and lets unit tests stub the data path without spawning Pulse.
type PulseDataSource interface {
	Process(ctx context.Context, req *types.Request) (*types.Response, error)
}

// PulsePrismTemplate is the brick template shape for the pulse_prism kind. Both
// fields are raw JSON so they round-trip untouched through scene/store and so
// ${var} placeholders inside them survive until the DataResolver (E3-S2)
// substitutes them; here resolvedVars is pass-through (see Render).
//
//	{
//	  "pulse_request": { ...types.Request... },
//	  "prism_spec":    { ...spec.Spec... }
//	}
type PulsePrismTemplate struct {
	// PulseRequest is a declarative Pulse types.Request (the data query).
	PulseRequest json.RawMessage `json:"pulse_request"`
	// PrismSpec is a Prism visualization spec (Vega-Lite-style JSON). Its data
	// binding is overwritten with the Pulse response rows at render time, so any
	// data block authored in the template is advisory only.
	PrismSpec json.RawMessage `json:"prism_spec"`
}

// PulsePrism renders a pulse_prism brick into a server-side SVG fragment: it
// runs the template's Pulse request through the MCP manager, injects the
// resulting rows into the Prism spec's data binding, compiles the spec, and
// renders it to SVG. The fragment returned is the SVG string the realtime layer
// broadcasts on the rendered topic; the thin client only slots it into the DOM.
//
// One instance serves every pulse_prism brick across all dashboards; it holds
// no per-brick state and is safe for concurrent use (the SVG renderer is
// stateless and the data source is concurrency-safe).
type PulsePrism struct {
	data PulseDataSource
	svg  prismrender.Renderer
}

// NewPulsePrism constructs a pulse_prism Renderer bound to a Pulse data source.
// The data source (the E2-S1 pulse manager) is injected here because the
// Renderer interface signature is fixed at Render(template, vars) and cannot
// carry it; the constructed instance is what gets registered against the
// pulse_prism kind. data must be non-nil.
func NewPulsePrism(data PulseDataSource) *PulsePrism {
	return &PulsePrism{
		data: data,
		svg:  svg.New(),
	}
}

// Render runs the Pulse→Prism→SVG pipeline for one brick template.
//
//  1. Parse the template envelope into a Pulse request + Prism spec.
//  2. Run the Pulse request via the MCP manager → types.Response rows.
//  3. Inject the rows as the Prism spec's inline data (spec.Data.Values).
//  4. Compile the spec and render the scene to SVG bytes.
//
// The vars argument is accepted for interface conformance and is pass-through
// in this story: ${var} placeholders inside pulse_request / prism_spec are NOT
// substituted here. The DataResolver (E3-S2) is the seam that will walk the
// template's raw JSON and replace ${var} tokens with vars values BEFORE this
// renderer parses it — i.e. resolution happens on the way in, leaving this
// pipeline unchanged. Until then vars is carried empty.
func (p *PulsePrism) Render(template string, _ ResolvedVars) (string, error) {
	var tmpl PulsePrismTemplate
	if err := json.Unmarshal([]byte(template), &tmpl); err != nil {
		return "", wrapError(InvalidTemplate, "decode pulse_prism template", err)
	}
	if len(tmpl.PulseRequest) == 0 {
		return "", newError(InvalidTemplate, "pulse_prism template missing pulse_request")
	}
	if len(tmpl.PrismSpec) == 0 {
		return "", newError(InvalidTemplate, "pulse_prism template missing prism_spec")
	}

	var req types.Request
	if err := json.Unmarshal(tmpl.PulseRequest, &req); err != nil {
		return "", wrapError(InvalidTemplate, "decode pulse_request", err)
	}

	spec, err := prismspec.DecodeBytes(tmpl.PrismSpec)
	if err != nil {
		return "", wrapError(InvalidTemplate, "decode prism_spec", err)
	}

	resp, err := p.data.Process(context.Background(), &req)
	if err != nil {
		return "", wrapError(DataSource, "run pulse request", err)
	}

	return p.renderRows(spec, resp.Data)
}

// renderRows injects rows into the Prism spec's data binding and renders SVG.
// Split out so the row→spec mapping and the Prism compile/render path can be
// unit-tested without a Pulse child process.
func (p *PulsePrism) renderRows(spec *prismspec.Spec, rows []map[string]any) (string, error) {
	// Map the Pulse response rows into the spec's data as inline values. Prism
	// specs *reference* data; we supply it inline so the server does the data
	// fetch (via Pulse) rather than Prism opening a .pulse file. This overrides
	// any data block authored in the template. Normalise nil to an empty slice
	// so Prism sees an empty (not absent) dataset.
	if rows == nil {
		rows = []map[string]any{}
	}
	spec.Data = &prismspec.Data{Values: rows}

	plan, err := prism.Compile(context.Background(), spec, prism.CompileOptions{})
	if err != nil {
		return "", wrapError(Compile, "compile prism spec", err)
	}

	out, err := p.svg.Render(plan.Scene, prismrender.RenderOpts{})
	if err != nil {
		return "", wrapError(Compile, "render svg", err)
	}
	return string(out), nil
}
