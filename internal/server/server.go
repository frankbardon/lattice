package server

import (
	"embed"
	"encoding/json"
	stderrors "errors"
	"html/template"
	"io"
	"io/fs"
	"net/http"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/resolver"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// ResolveFunc loads and resolves a dashboard document into a resolved tree,
// applying the given unified runtime overrides (E4): the map keys are
// addresses — a bare name targets a settable variable (a widget selection or
// URL query param), a "<node-id>.<field>" name targets a node config field.
// Both kinds are routed verbatim into the resolver's addressable override set;
// nil/empty means defaults only. It is injected by the caller (the CLI `serve`
// command) so the server package does not own resolver wiring. On failure it
// returns a CodedError.
type ResolveFunc func(overrides map[string]any) (*resolver.ResolvedTree, error)

// Server is the lattice reference-renderer web layer. It serves an HTML page
// wired with AlpineJS plus a JSON endpoint exposing the resolved tree. The
// document is re-resolved on every request so edits are reflected on reload;
// resolution failures render an HTML error page rather than crashing.
type Server struct {
	resolve ResolveFunc
	tmpl    *template.Template
	static  http.Handler
}

// New constructs a Server backed by resolve. It parses the embedded templates
// and wires the embedded static asset handler; an error is returned only if the
// embedded assets fail to load (a programming/build error).
func New(resolve ResolveFunc) (*Server, error) {
	tmpl, err := template.ParseFS(templateFS, "templates/*.tmpl")
	if err != nil {
		return nil, errors.WrapCodedError(err, errors.SERVE_INTERNAL, "failed parsing embedded templates")
	}
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, errors.WrapCodedError(err, errors.SERVE_INTERNAL, "failed mounting embedded static assets")
	}
	return &Server{
		resolve: resolve,
		tmpl:    tmpl,
		static:  http.StripPrefix("/static/", http.FileServer(http.FS(sub))),
	}, nil
}

// Handler returns the configured HTTP handler (routes mounted on a fresh mux).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handlePage)
	mux.HandleFunc("/api/tree", s.handleTree)
	mux.HandleFunc("/api/resolve", s.handleResolve)
	mux.Handle("/static/", s.static)
	return mux
}

// pageData is the base page template payload.
type pageData struct {
	Title string
}

// errorPageData renders a CodedError on the HTML error page.
type errorPageData struct {
	Code    string
	Message string
	Details map[string]any
}

// handlePage serves the base AlpineJS page. Resolution is attempted so a broken
// document yields the error page directly (rather than a blank shell that only
// fails once the JSON fetch runs). Only "/" is served here; anything else 404s.
func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	tree, err := s.resolve(queryOverrides(r))
	if err != nil {
		s.renderError(w, err)
		return
	}

	title := "Dashboard"
	if t, ok := tree.Manifest["title"].(string); ok && t != "" {
		title = t
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if execErr := s.tmpl.ExecuteTemplate(w, "page.html.tmpl", pageData{Title: title}); execErr != nil {
		// Headers may already be flushed; best effort.
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

// handleTree exposes the resolved tree as JSON for the Alpine front end. On
// resolution failure it returns the CodedError envelope with a 422 status so the
// client can surface it without the page crashing.
func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	tree, err := s.resolve(queryOverrides(r))
	if err != nil {
		s.writeJSONError(w, err)
		return
	}
	s.writeTree(w, tree)
}

// handleResolve is the live re-resolve endpoint. It accepts a JSON object of
// unified runtime overrides in the POST body — variable values keyed by name
// ({"region":"eu", ...}) and/or config-field values keyed by "<node-id>.<field>"
// ({"summary.title":"Pinned", ...}) — resolves the document with those overrides
// applied (override > default; computed variables stay computed; config
// overrides are ephemeral and validated against the node's surface), and returns
// the freshly resolved tree as JSON. A bad value (wrong type / out-of-enum /
// unknown field) surfaces as the usual VAR_*/CONFIG_OVERRIDE_* error envelope
// with a 422, so the client can show it without the page crashing.
func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.writeJSONError(w, errors.NewCodedErrorWithDetails(errors.SERVE_INVALID,
			"the re-resolve endpoint requires POST", map[string]any{"method": r.Method}))
		return
	}

	overrides := map[string]any{}
	// An empty body is allowed (re-resolve with defaults). Decode only when there
	// is content so a missing body is not an error.
	if r.Body != nil {
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&overrides); err != nil && !stderrors.Is(err, io.EOF) {
			s.writeJSONError(w, errors.WrapCodedError(err, errors.SERVE_INVALID,
				"re-resolve request body is not a JSON object of variable values"))
			return
		}
	}

	tree, err := s.resolve(overrides)
	if err != nil {
		s.writeJSONError(w, err)
		return
	}
	s.writeTree(w, tree)
}

// writeTree encodes the resolved tree as indented JSON.
func (s *Server) writeTree(w http.ResponseWriter, tree *resolver.ResolvedTree) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if encErr := enc.Encode(tree); encErr != nil {
		s.writeJSONError(w, errors.WrapCodedError(encErr, errors.SERVE_INTERNAL, "failed encoding resolved tree"))
	}
}

// queryOverrides extracts unified runtime overrides from the request's URL
// query params (E4). Each single-valued param becomes a string override keyed
// by its name: a bare name (?region=eu) targets a variable; a "<node-id>.<field>"
// name (?summary.title=Pinned) targets a node config field. The resolver coerces
// each string to the target's declared type. A repeated param keeps its first
// value. A bare name that is not a declared variable is ignored; an unknown
// config-field address fails fast (CONFIG_OVERRIDE_FIELD_UNKNOWN).
func queryOverrides(r *http.Request) map[string]any {
	q := r.URL.Query()
	if len(q) == 0 {
		return nil
	}
	out := make(map[string]any, len(q))
	for name, vals := range q {
		if len(vals) > 0 {
			out[name] = vals[0]
		}
	}
	return out
}

// renderError writes the HTML error page for err, coercing non-coded errors into
// a CodedError envelope so the page always has a code + message.
func (s *Server) renderError(w http.ResponseWriter, err error) {
	ce := asCoded(err)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnprocessableEntity)
	data := errorPageData{Code: string(ce.Code), Message: ce.Message, Details: ce.Details}
	if execErr := s.tmpl.ExecuteTemplate(w, "error.html.tmpl", data); execErr != nil {
		http.Error(w, ce.Error(), http.StatusInternalServerError)
	}
}

// writeJSONError writes the CodedError JSON envelope with a 422 status.
func (s *Server) writeJSONError(w http.ResponseWriter, err error) {
	ce := asCoded(err)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnprocessableEntity)
	b, mErr := json.MarshalIndent(ce, "", "  ")
	if mErr != nil {
		b = []byte(`{"code":"` + string(errors.SERVE_INTERNAL) + `","message":"failed encoding error"}`)
	}
	_, _ = w.Write(b)
}

// asCoded returns err as a *CodedError, wrapping non-coded errors so callers
// always have a code + message to render.
func asCoded(err error) *errors.CodedError {
	var ce *errors.CodedError
	if stderrors.As(err, &ce) {
		return ce
	}
	return errors.WrapCodedError(err, errors.SERVE_RESOLVE, err.Error())
}
