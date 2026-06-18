// Package render is lattice's server-side rendering seam. A brick's
// parameterized Template is turned into a finished HTML/SVG fragment here, on
// the server, and the thin client only slots that fragment into the DOM — it
// never computes one.
//
// # The seam
//
// Rendering is pluggable by brick kind from day one. A Renderer turns a
// template (plus resolved variables) into a fragment; a Registry maps a
// brick.Kind to the Renderer that handles it. v1 ships a single markdown kind;
// the pulse_prism kind (Prism spec → SVG) registers the same way in a later
// epic without touching this package.
//
// # Renderer-agnostic on purpose
//
// This package has no knowledge of Pulse, Prism, or the scene/realtime layers —
// it is a pure template→fragment function behind an interface. Variable
// resolution is the DataResolver's job (a later epic); the ResolvedVars argument
// is plumbed through every Renderer now and carried empty until then, so adding
// resolution later is a wiring change, not an interface change.
package render

// ResolvedVars are the fully-resolved variable values a renderer may interpolate
// into a template. v1 carries it empty: the server-side DataResolver that
// populates ${var}/data.ref values lands in a later epic. The type and argument
// exist now so renderers and their call sites do not change when resolution is
// wired in.
type ResolvedVars map[string]string

// Renderer turns a brick's parameterized template into a finished fragment
// (HTML or SVG, as a string) ready to be slotted into the board. Implementations
// are keyed by brick kind in a Registry. A Renderer must be safe for concurrent
// use: one instance serves every brick of its kind across all dashboards.
type Renderer interface {
	// Render produces the fragment for template with vars resolved. It returns a
	// coded error (render.Error) on failure so callers can act on the Code.
	Render(template string, vars ResolvedVars) (string, error)
}
