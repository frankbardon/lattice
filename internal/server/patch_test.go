package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/resolver"
	"github.com/frankbardon/lattice/service"
)

const patchFixtureID = "example-minimal"

// newPatchTestServer wires a write-enabled server over an in-memory FS-backed
// service seeded with the minimal fixture. The injected PatchFunc mirrors the
// closure the `lattice serve` command builds (ParseChangeset → Patch → Revision),
// so the handler exercises the real apply pipeline against a live store. It
// returns the handler and the store so a test can read the persisted bytes back.
func newPatchTestServer(t *testing.T) (http.Handler, service.Store) {
	t.Helper()

	store, err := service.NewStore(service.BackendFS, afero.NewMemMapFs(), "docs")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	doc, err := os.ReadFile("../../examples/minimal-dashboard.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := store.Save(doc); err != nil {
		t.Fatalf("seed store.Save: %v", err)
	}
	res, err := service.NewResolver(os.DirFS("../../schemas"))
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	svc := service.New(store, res)

	patch := func(id string, ops json.RawMessage, expectedRevision *string) (*PatchResult, error) {
		cs, err := svc.ParseChangeset(ops)
		if err != nil {
			return nil, err
		}
		var applyOpts []service.ApplyOption
		if expectedRevision != nil {
			applyOpts = append(applyOpts, service.WithExpectedRevision(*expectedRevision))
		}
		result, err := svc.Patch(id, cs, applyOpts...)
		if err != nil {
			return nil, err
		}
		rev, err := svc.Revision(id)
		if err != nil {
			return nil, err
		}
		return &PatchResult{Revision: rev, Result: result.Resolved}, nil
	}

	s, err := New(func(map[string]any) (*resolver.ResolvedTree, error) { return okTree(), nil }, WithPatch(patch))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s.Handler(), store
}

// readTitle digests document bytes to manifest.title — the surfaced field the
// happy-path patch edits.
func readTitle(t *testing.T, b []byte) string {
	t.Helper()
	var doc struct {
		Manifest struct {
			Title string `json:"title"`
		} `json:"manifest"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("unmarshal title: %v", err)
	}
	return doc.Manifest.Title
}

// TestPatchPersistsAndReturnsRevision proves the happy path: a surfaced
// $manifest/title edit posted to /api/patch is applied, persisted, and the
// response carries the new revision plus the resolved tree — and the store now
// holds the edited bytes.
func TestPatchPersistsAndReturnsRevision(t *testing.T) {
	h, store := newPatchTestServer(t)

	body := strings.NewReader(`{"id":"example-minimal","ops":[{"op":"replace","path":"/$manifest/title","value":"Renamed"}]}`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/patch", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q, want application/json", ct)
	}

	var resp PatchResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if resp.Revision == "" {
		t.Error("response missing revision token")
	}
	if resp.Result == nil {
		t.Fatal("response missing resolved result tree")
	}
	if got, _ := resp.Result.Manifest["title"].(string); got != "Renamed" {
		t.Errorf("resolved title = %q, want %q", got, "Renamed")
	}

	// The change is actually saved.
	reloaded, err := store.Load(patchFixtureID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := readTitle(t, reloaded); got != "Renamed" {
		t.Errorf("persisted title = %q, want %q", got, "Renamed")
	}
}

// TestPatchStaleRevisionConflict proves the optimistic-concurrency precondition:
// a patch carrying a now-stale expectedRevision (after a concurrent write bumped
// the store's revision) is rejected with 409 + CHANGESET_REVISION_CONFLICT, and
// nothing the stale edit proposed is persisted.
func TestPatchStaleRevisionConflict(t *testing.T) {
	h, store := newPatchTestServer(t)

	// The expected revision is the document's current revision...
	stale, err := store.(service.RevisionedStore).Revision(patchFixtureID)
	if err != nil {
		t.Fatalf("Revision: %v", err)
	}
	// ...then a concurrent writer bumps it past `stale`.
	seed, err := store.Load(patchFixtureID)
	if err != nil {
		t.Fatalf("seed load: %v", err)
	}
	intruder := bytes.Replace(seed, []byte(`"Minimal Example Dashboard"`), []byte(`"Intruder Edit"`), 1)
	if bytes.Equal(intruder, seed) {
		t.Fatal("test setup: intruder bytes did not differ from the seed")
	}
	if err := store.Save(intruder); err != nil {
		t.Fatalf("concurrent Save: %v", err)
	}

	body := strings.NewReader(`{"id":"example-minimal","expectedRevision":"` + stale +
		`","ops":[{"op":"replace","path":"/$manifest/title","value":"Stale Edit"}]}`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/patch", body))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	assertEnvelopeCode(t, rec.Body.Bytes(), errors.CHANGESET_REVISION_CONFLICT)

	// The stale edit was not persisted; the store reflects the concurrent write.
	after, err := store.Load(patchFixtureID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if bytes.Contains(after, []byte(`"Stale Edit"`)) {
		t.Error("the rejected stale edit was persisted")
	}
}

// TestPatchUnknownID proves an id no stored document matches surfaces as 404 +
// STORAGE_NOT_FOUND.
func TestPatchUnknownID(t *testing.T) {
	h, store := newPatchTestServer(t)

	seed, err := store.Load(patchFixtureID)
	if err != nil {
		t.Fatalf("seed load: %v", err)
	}

	body := strings.NewReader(`{"id":"does-not-exist","ops":[{"op":"replace","path":"/$manifest/title","value":"X"}]}`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/patch", body))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	assertEnvelopeCode(t, rec.Body.Bytes(), errors.STORAGE_NOT_FOUND)

	// The seeded document is untouched.
	after, err := store.Load(patchFixtureID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !bytes.Equal(after, seed) {
		t.Error("a patch to an unknown id mutated an existing document")
	}
}

// TestPatchOffSurfaceRejected proves an off-surface field edit (the table's `rows`
// is a real config field but not on its configurable surface) is rejected with a
// 422 + the verbatim coded error, and the store is left byte-for-byte unchanged.
func TestPatchOffSurfaceRejected(t *testing.T) {
	h, store := newPatchTestServer(t)

	seed, err := store.Load(patchFixtureID)
	if err != nil {
		t.Fatalf("seed load: %v", err)
	}

	body := strings.NewReader(`{"id":"example-minimal","ops":[{"op":"replace","path":"/fruits/config/rows","value":[]}]}`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/patch", body))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	assertEnvelopeCode(t, rec.Body.Bytes(), errors.CONFIG_OVERRIDE_FIELD_UNKNOWN)

	after, err := store.Load(patchFixtureID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !bytes.Equal(after, seed) {
		t.Errorf("rejected patch mutated the store:\n--- want (seed) ---\n%s\n--- got ---\n%s", seed, after)
	}
}

// TestPatchRejectsNonPOST proves the write route requires POST.
func TestPatchRejectsNonPOST(t *testing.T) {
	h, _ := newPatchTestServer(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/patch", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// TestPatchDisabledWhenReadOnly proves a server built WITHOUT WithPatch reports
// the write route as unavailable (404 + coded error) rather than panicking on a
// nil PatchFunc. This is the read-only-server path (serve's path mode).
func TestPatchDisabledWhenReadOnly(t *testing.T) {
	h := newTestServer(t, func(map[string]any) (*resolver.ResolvedTree, error) { return okTree(), nil })

	body := strings.NewReader(`{"id":"example-minimal","ops":[{"op":"replace","path":"/$manifest/title","value":"X"}]}`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/patch", body))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a read-only server; body=%s", rec.Code, rec.Body.String())
	}
}

// assertEnvelopeCode fails unless body is a CodedError JSON envelope (not a plain
// string) carrying want.
func assertEnvelopeCode(t *testing.T, body []byte, want errors.Code) {
	t.Helper()
	var env struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("error body is not a JSON envelope: %v (body=%s)", err, body)
	}
	if env.Code != string(want) {
		t.Errorf("envelope code = %q, want %q (body=%s)", env.Code, want, body)
	}
	if env.Message == "" {
		t.Error("envelope missing message")
	}
}
