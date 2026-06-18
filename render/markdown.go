package render

import (
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	gmhtml "github.com/yuin/goldmark/renderer/html"
)

// KindMarkdown is the brick kind handled by the markdown Renderer.
const KindMarkdown = "markdown"

// Markdown renders a markdown template into a sanitized HTML fragment. It is the
// first concrete kind and proves the render seam is real: the same Registry that
// dispatches markdown will dispatch the pulse_prism kind in a later epic.
//
// Output is sanitized. Markdown is authored over a collaborative channel by
// anonymous clients, so the rendered fragment is treated as untrusted user
// content: goldmark converts markdown (including any raw HTML in it) to HTML,
// then a bluemonday UGC policy strips anything that could execute (scripts,
// event handlers, javascript: URLs), leaving safe presentational markup.
type Markdown struct {
	md     goldmark.Markdown
	policy *bluemonday.Policy
}

// NewMarkdown constructs a markdown Renderer. The converter enables GitHub
// Flavored Markdown (tables, strikethrough, autolinks, task lists) and allows
// raw HTML through goldmark; the sanitizer is the safety boundary, not goldmark.
func NewMarkdown() *Markdown {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(gmhtml.WithUnsafe()),
	)
	return &Markdown{
		md:     md,
		policy: bluemonday.UGCPolicy(),
	}
}

// Render converts the markdown template to a sanitized HTML fragment. The vars
// argument is accepted for interface conformance and ignored in v1: variable
// interpolation is the DataResolver's job in a later epic.
func (m *Markdown) Render(template string, _ ResolvedVars) (string, error) {
	var buf strings.Builder
	if err := m.md.Convert([]byte(template), &buf); err != nil {
		return "", wrapError(Internal, "convert markdown", err)
	}
	return m.policy.Sanitize(buf.String()), nil
}
