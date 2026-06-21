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

// PatchResult is the successful outcome of a committed patch: the document's new
// opaque revision token (re-read from the store after the write) and the resolved
// tree of the persisted document. It is the payload the POST /api/patch handler
// serializes on success.
type PatchResult struct {
	// Revision is the document's revision token AFTER the write — the value a
	// client pairs with a subsequent expectedRevision precondition.
	Revision string `json:"revision"`
	// Result is the resolved tree of the persisted document (the re-resolution the
	// apply pipeline already computed), returned so the caller need not re-resolve.
	Result *resolver.ResolvedTree `json:"result"`
}

// PatchFunc commits an id-rooted RFC 6902 changeset to the stored document
// addressed by id and returns the new revision plus the resolved persisted tree.
// ops is the raw JSON Patch array as received in the request body (parsed by the
// closure via service.ParseChangeset so the server package owns no changeset
// wiring). expectedRevision, when non-nil, is threaded as an optimistic-
// concurrency precondition (service.WithExpectedRevision); a nil pointer means no
// precondition. It is injected by the caller (the CLI `serve` command) so the
// server package builds only on the service facade and never imports an internal
// core. The apply is atomic: any failure persists nothing and surfaces as a
// *errors.CodedError. A nil PatchFunc disables the write route (read-only server).
type PatchFunc func(id string, ops json.RawMessage, expectedRevision *string) (*PatchResult, error)

// Server is the lattice reference-renderer web layer. It serves an HTML page
// wired with AlpineJS plus a JSON endpoint exposing the resolved tree. The
// document is re-resolved on every request so edits are reflected on reload;
// resolution failures render an HTML error page rather than crashing.
type Server struct {
	resolve ResolveFunc
	patch   PatchFunc
	tmpl    *template.Template
	static  http.Handler
}

// Option configures a Server at construction. It is the additive seam for
// optional capabilities (the write route is the first) so New's signature does
// not churn as the server grows.
type Option func(*Server)

// WithPatch enables the POST /api/patch write route, wiring the injected commit
// function. Omitting it leaves the server read-only (the route returns 404 for an
// absent capability). The caller (the CLI `serve` command) owns the closure, so
// the server package never imports a changeset/store core.
func WithPatch(patch PatchFunc) Option {
	return func(s *Server) { s.patch = patch }
}

// New constructs a Server backed by resolve, applying any options. It parses the
// embedded templates and wires the embedded static asset handler; an error is
// returned only if the embedded assets fail to load (a programming/build error).
func New(resolve ResolveFunc, opts ...Option) (*Server, error) {
	tmpl, err := template.ParseFS(templateFS, "templates/*.tmpl")
	if err != nil {
		return nil, errors.WrapCodedError(err, errors.SERVE_INTERNAL, "failed parsing embedded templates")
	}
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, errors.WrapCodedError(err, errors.SERVE_INTERNAL, "failed mounting embedded static assets")
	}
	s := &Server{
		resolve: resolve,
		tmpl:    tmpl,
		static:  http.StripPrefix("/static/", http.FileServer(http.FS(sub))),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// Handler returns the configured HTTP handler (routes mounted on a fresh mux).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handlePage)
	mux.HandleFunc("/api/tree", s.handleTree)
	mux.HandleFunc("/api/resolve", s.handleResolve)
	mux.HandleFunc("/api/patch", s.handlePatch)
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

// patchRequest is the POST /api/patch request body: a single document's manifest
// id, the id-rooted RFC 6902 JSON Patch array (kept raw so the injected closure
// parses it through service.ParseChangeset), and an optional optimistic-
// concurrency precondition. ExpectedRevision is a pointer so an OMITTED field
// (no precondition) is distinguished from an explicit empty-string token.
type patchRequest struct {
	ID               string          `json:"id"`
	Ops              json.RawMessage `json:"ops"`
	ExpectedRevision *string         `json:"expectedRevision,omitempty"`
}

// handlePatch is the REAL persistence path: it commits an id-rooted changeset to
// the stored document and returns the new revision plus the resolved persisted
// tree. It mirrors the `lattice patch` CLI semantics (backend-rooted, id-rooted
// changeset, optional expect-revision precondition) and is the only write route
// in the server — the MCP layer never reaches here.
//
// SECURITY: there is NO authentication or authorization on this endpoint. The
// effort assumes a localhost/trusted deployment; exposing it on an untrusted
// network would let any caller mutate stored documents. This is a known, accepted
// gap (documented for the README) — do NOT add auth here without revisiting the
// effort's trust assumption.
//
// On success it writes 200 with {revision, result}. Coded errors are mapped to an
// HTTP status by httpStatus and returned as the CodedError JSON envelope (never a
// plain string): not-found→404, revision conflict→409, everything else (parse,
// validation, off-surface, structural)→422.
func (s *Server) handlePatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.writeJSONErrorStatus(w, http.StatusMethodNotAllowed, errors.NewCodedErrorWithDetails(errors.SERVE_INVALID,
			"the patch endpoint requires POST", map[string]any{"method": r.Method}))
		return
	}

	// A read-only server (no PatchFunc injected) has no write capability; report it
	// rather than silently 404-ing on the route itself.
	if s.patch == nil {
		s.writeJSONErrorStatus(w, http.StatusNotFound, errors.NewCodedError(errors.SERVE_INVALID,
			"the patch endpoint is not enabled on this server"))
		return
	}

	var req patchRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		s.writeJSONErrorStatus(w, http.StatusBadRequest, errors.WrapCodedError(err, errors.SERVE_INVALID,
			"patch request body is not a JSON object of {id, ops, expectedRevision?}"))
		return
	}
	if req.ID == "" {
		s.writeJSONErrorStatus(w, http.StatusBadRequest, errors.NewCodedError(errors.SERVE_INVALID,
			"patch request requires a non-empty document id"))
		return
	}
	if len(req.Ops) == 0 {
		s.writeJSONErrorStatus(w, http.StatusBadRequest, errors.NewCodedError(errors.SERVE_INVALID,
			"patch request requires an ops array (the id-rooted RFC 6902 JSON Patch)"))
		return
	}

	// The injected closure parses + applies through the atomic service.Patch
	// pipeline and re-reads the post-write revision. Any failure (parse,
	// validation, off-surface, structural, not-found, revision conflict) surfaces
	// as a *errors.CodedError, propagated verbatim and mapped to a status below.
	result, err := s.patch(req.ID, req.Ops, req.ExpectedRevision)
	if err != nil {
		ce := asCoded(err)
		s.writeJSONErrorStatus(w, httpStatus(ce), ce)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if encErr := enc.Encode(result); encErr != nil {
		s.writeJSONError(w, errors.WrapCodedError(encErr, errors.SERVE_INTERNAL, "failed encoding patch result"))
	}
}

// httpStatus maps a CodedError to the HTTP status the write endpoint reports.
// The mapping is by code: a not-found id is 404, an optimistic-concurrency
// conflict is 409 (so a client can retry), and every other coded failure — a
// malformed/off-surface/ill-typed changeset, an apply or re-resolution failure,
// or a capability/precondition rejection — is 422 (the existing CodedError
// envelope status used across the server). SERVE_INVALID input guards set their
// own 4xx status directly and do not flow through here.
func httpStatus(ce *errors.CodedError) int {
	switch ce.Code {
	case errors.STORAGE_NOT_FOUND:
		return http.StatusNotFound
	case errors.CHANGESET_REVISION_CONFLICT:
		return http.StatusConflict
	default:
		return http.StatusUnprocessableEntity
	}
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
	s.writeJSONErrorStatus(w, http.StatusUnprocessableEntity, err)
}

// writeJSONErrorStatus writes the CodedError JSON envelope with the given HTTP
// status, so callers (the write endpoint) can map a coded error to a specific
// status — 404/409/422 — without flattening the error to a plain string.
func (s *Server) writeJSONErrorStatus(w http.ResponseWriter, status int, err error) {
	ce := asCoded(err)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
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
