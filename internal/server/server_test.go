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
	h := newTestServer(t, func(map[string]any) (*resolver.ResolvedTree, error) { return okTree(), nil })

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
	h := newTestServer(t, func(map[string]any) (*resolver.ResolvedTree, error) { return okTree(), nil })

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

// TestPageRendersWidgetControls asserts the serve UI carries the per-widget
// control renderers (E4-S3 carried fix): the retired dropdown-only renderer is
// gone and the unified controlHTML dispatcher plus the select/text/number/
// boolean families are present so widget leaves render interactive controls.
func TestPageRendersWidgetControls(t *testing.T) {
	h := newTestServer(t, func(map[string]any) (*resolver.ResolvedTree, error) { return okTree(), nil })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	for _, want := range []string{
		"controlHTML(",
		"widgetFamily(",
		"selectHTML(",
		"textHTML(",
		"numberHTML(",
		"booleanHTML(",
		`data-var="`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing widget-control markup %q", want)
		}
	}
	// The retired dropdown-only renderer and type check must be gone.
	for _, gone := range []string{"dropdownHTML(", "name === 'dropdown'"} {
		if strings.Contains(body, gone) {
			t.Errorf("page still carries retired dropdown markup %q", gone)
		}
	}
}

// TestPageRendersConfiguratorControls asserts the served page carries the
// renderer for a configurator's auto-generated editor form (E5-S2): the
// generatedHTML/generatedControlHTML functions that walk node.generated.widgets
// and emit one data-var="<target-id>.<field>" control per widget. Those controls
// reuse the same delegated @change path authored widgets use, so a change posts a
// config override that re-resolves the target.
func TestPageRendersConfiguratorControls(t *testing.T) {
	h := newTestServer(t, func(map[string]any) (*resolver.ResolvedTree, error) { return okTree(), nil })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	for _, want := range []string{
		"generatedHTML(",
		"generatedControlHTML(",
		"node.generated",
		"w.target",
		"w.field",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing configurator-control markup %q", want)
		}
	}
}

func TestTreeEndpointReturnsJSON(t *testing.T) {
	h := newTestServer(t, func(map[string]any) (*resolver.ResolvedTree, error) { return okTree(), nil })

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

func TestResolveEndpointAppliesOverrides(t *testing.T) {
	// Capture the overrides the handler threads into resolution so we can assert
	// the POSTed body reaches the resolver and the fresh tree is returned.
	var got map[string]any
	resolve := func(overrides map[string]any) (*resolver.ResolvedTree, error) {
		got = overrides
		tree := okTree()
		region, _ := overrides["region"].(string)
		tree.Manifest = map[string]any{"title": "Region " + region}
		return tree, nil
	}
	h := newTestServer(t, resolve)

	body := strings.NewReader(`{"region":"eu"}`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/resolve", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got["region"] != "eu" {
		t.Errorf("overrides threaded = %+v, want region=eu", got)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q, want application/json", ct)
	}
	var tree resolver.ResolvedTree
	if err := json.Unmarshal(rec.Body.Bytes(), &tree); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if title, _ := tree.Manifest["title"].(string); title != "Region eu" {
		t.Errorf("re-resolved title = %q, want %q", title, "Region eu")
	}
}

// TestResolveEndpointRoutesConfigOverride asserts a "<node-id>.<field>" config
// override posted to /api/resolve reaches the resolver verbatim alongside a bare
// variable override — proving serve routes BOTH override kinds through the
// unified map without distinguishing them (E4-S3).
func TestResolveEndpointRoutesConfigOverride(t *testing.T) {
	var got map[string]any
	resolve := func(overrides map[string]any) (*resolver.ResolvedTree, error) {
		got = overrides
		return okTree(), nil
	}
	h := newTestServer(t, resolve)

	body := strings.NewReader(`{"region":"eu","summary.title":"Pinned"}`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/resolve", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got["region"] != "eu" {
		t.Errorf("variable override = %+v, want region=eu", got)
	}
	if got["summary.title"] != "Pinned" {
		t.Errorf("config override = %+v, want summary.title=Pinned", got)
	}
}

// TestTreeEndpointRoutesConfigOverrideQuery asserts a "<node-id>.<field>" config
// override supplied as a URL query param is lifted into the unified override map
// and threaded into resolution, matching the bare-variable query path.
func TestTreeEndpointRoutesConfigOverrideQuery(t *testing.T) {
	var got map[string]any
	resolve := func(overrides map[string]any) (*resolver.ResolvedTree, error) {
		got = overrides
		return okTree(), nil
	}
	h := newTestServer(t, resolve)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tree?summary.title=Pinned", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got["summary.title"] != "Pinned" {
		t.Errorf("config-override query = %+v, want summary.title=Pinned", got)
	}
}

func TestResolveEndpointEmptyBodyUsesDefaults(t *testing.T) {
	var got map[string]any
	resolve := func(overrides map[string]any) (*resolver.ResolvedTree, error) {
		got = overrides
		return okTree(), nil
	}
	h := newTestServer(t, resolve)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/resolve", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(got) != 0 {
		t.Errorf("overrides = %+v, want empty for empty body", got)
	}
}

func TestResolveEndpointRejectsGet(t *testing.T) {
	h := newTestServer(t, func(map[string]any) (*resolver.ResolvedTree, error) { return okTree(), nil })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/resolve", nil))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422 for GET", rec.Code)
	}
}

func TestTreeEndpointAppliesQueryOverrides(t *testing.T) {
	var got map[string]any
	resolve := func(overrides map[string]any) (*resolver.ResolvedTree, error) {
		got = overrides
		return okTree(), nil
	}
	h := newTestServer(t, resolve)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tree?region=eu", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got["region"] != "eu" {
		t.Errorf("query overrides = %+v, want region=eu", got)
	}
}

func TestErrorPageRendersCodedError(t *testing.T) {
	ce := errors.NewCodedErrorWithDetails(errors.RESOLVE_CONFIG_INVALID,
		"config failed validation", map[string]any{"path": "root.children[0]"})
	h := newTestServer(t, func(map[string]any) (*resolver.ResolvedTree, error) { return nil, ce })

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
	h := newTestServer(t, func(map[string]any) (*resolver.ResolvedTree, error) { return nil, ce })

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
	h := newTestServer(t, func(map[string]any) (*resolver.ResolvedTree, error) { return okTree(), nil })

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
	h := newTestServer(t, func(map[string]any) (*resolver.ResolvedTree, error) { return okTree(), nil })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
