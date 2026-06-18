package render

import "log/slog"

// Options configures a Registry.
type Options struct {
	// Logger receives registry events. Defaults to slog.Default().
	Logger *slog.Logger
}

// Registry dispatches a render request to the Renderer registered for a brick
// kind. It is the pluggable seam: kinds register at startup and every render
// routes through Render(kind, ...). A Registry is not mutated after wiring, so
// it is safe for concurrent reads.
type Registry struct {
	logger    *slog.Logger
	renderers map[string]Renderer
}

// NewRegistry constructs an empty Registry. Register kinds on it before serving.
func NewRegistry(opts Options) *Registry {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Registry{
		logger:    logger,
		renderers: make(map[string]Renderer),
	}
}

// Register binds a Renderer to a brick kind. kind must be non-empty and r
// non-nil; re-registering a kind replaces the previous Renderer.
func (reg *Registry) Register(kind string, r Renderer) error {
	if kind == "" {
		return newError(InvalidArgument, "kind required")
	}
	if r == nil {
		return newError(InvalidArgument, "renderer required for kind "+kind)
	}
	reg.renderers[kind] = r
	return nil
}

// Render dispatches by kind: it looks up the registered Renderer and produces
// the fragment. An unregistered kind yields a coded UnknownKind error so callers
// can fail cleanly (e.g. surface a placeholder rather than crash a board).
func (reg *Registry) Render(kind, template string, vars ResolvedVars) (string, error) {
	r, ok := reg.renderers[kind]
	if !ok {
		return "", newError(UnknownKind, "no renderer registered for kind "+kind)
	}
	return r.Render(template, vars)
}
