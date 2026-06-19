package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/resolver"
)

// okTree returns a minimal resolved tree for the success-path tests.
func okTree() *resolver.ResolvedTree {
	return &resolver.ResolvedTree{
		Manifest: map[string]any{"title": "Test Dashboard"},
		Root: &resolver.ResolvedInstance{
			ID:        "root",
			Container: true,
			Type:      resolver.ResolvedTypeRef{Ref: "x", ID: "x", Name: "container", Version: "1.0.0"},
		},
	}
}

func newTestServer(t *testing.T, resolve ResolveFunc) http.Handler {
	t.Helper()
	s, err := New(resolve)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s.Handler()
}

func TestPageServedOnSuccess(t *testing.T) {
	h := newTestServer(t, func() (*resolver.ResolvedTree, error) { return okTree(), nil })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "x-data=\"dashboard()\"") {
		t.Errorf("page missing Alpine mount; body=%q", body)
	}
	if !strings.Contains(body, "alpinejs") {
		t.Errorf("page missing AlpineJS script")
	}
	if !strings.Contains(body, "Test Dashboard") {
		t.Errorf("page missing manifest title")
	}
}

func TestPageRendersSketchAndInspector(t *testing.T) {
	h := newTestServer(t, func() (*resolver.ResolvedTree, error) { return okTree(), nil })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	// Sketch view: recursive node renderer + grid translation must be present.
	for _, want := range []string{
		`class="sketch"`,
		"renderNode(",
		"gridStyle(",
		"placementStyle(",
		"grid-template-columns:",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing sketch markup %q", want)
		}
	}

	// Inspector panel: collapsible JSON view.
	for _, want := range []string{
		`class="inspector"`,
		"inspectorOpen",
		"treeJSON",
		`class="inspector-json"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing inspector markup %q", want)
		}
	}
}

func TestTreeEndpointReturnsJSON(t *testing.T) {
	h := newTestServer(t, func() (*resolver.ResolvedTree, error) { return okTree(), nil })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tree", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q, want application/json", ct)
	}

	var got resolver.ResolvedTree
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if got.Root == nil || got.Root.Type.Name != "container" {
		t.Errorf("unexpected tree payload: %+v", got)
	}
}

func TestErrorPageRendersCodedError(t *testing.T) {
	ce := errors.NewCodedErrorWithDetails(errors.RESOLVE_CONFIG_INVALID,
		"config failed validation", map[string]any{"path": "root.children[0]"})
	h := newTestServer(t, func() (*resolver.ResolvedTree, error) { return nil, ce })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, string(errors.RESOLVE_CONFIG_INVALID)) {
		t.Errorf("error page missing code; body=%q", body)
	}
	if !strings.Contains(body, "config failed validation") {
		t.Errorf("error page missing message")
	}
	if !strings.Contains(body, "root.children[0]") {
		t.Errorf("error page missing details")
	}
}

func TestTreeEndpointReturnsErrorEnvelope(t *testing.T) {
	ce := errors.NewCodedError(errors.SERVE_RESOLVE, "boom")
	h := newTestServer(t, func() (*resolver.ResolvedTree, error) { return nil, ce })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tree", nil))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	var env struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("error envelope not JSON: %v", err)
	}
	if env.Code != string(errors.SERVE_RESOLVE) || env.Message != "boom" {
		t.Errorf("unexpected envelope: %+v", env)
	}
}

func TestStaticAssetServed(t *testing.T) {
	h := newTestServer(t, func() (*resolver.ResolvedTree, error) { return okTree(), nil })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/app.css", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "lattice") && rec.Body.Len() == 0 {
		t.Errorf("static css not served")
	}
}

func TestUnknownPathNotFound(t *testing.T) {
	h := newTestServer(t, func() (*resolver.ResolvedTree, error) { return okTree(), nil })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
