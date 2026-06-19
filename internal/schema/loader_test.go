package schema

import (
	"net/url"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
)

// newTestCatalog builds a catalog from the on-disk testdata over an OsFs.
func newTestCatalog(t *testing.T) (*Catalog, afero.Fs) {
	t.Helper()
	fs := afero.NewOsFs()
	cat, err := NewCatalog(fs, "testdata/catalog")
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	return cat, fs
}

func TestLoaderResolvesRefForms(t *testing.T) {
	tests := []struct {
		name     string
		docPath  string
		wantType string // canonical id expected to appear in graph.Types
		// childRef is the resolved ref key recorded for the single child node.
		childRefKey string
	}{
		{
			name:        "absolute url ref via catalog",
			docPath:     "testdata/dashboard-absolute.json",
			wantType:    "https://lattice.dev/schemas/items/table/1.0.0",
			childRefKey: "https://lattice.dev/schemas/items/table/1.0.0",
		},
		{
			name:        "relative ref via catalog normalization and relative-root file",
			docPath:     "testdata/dashboard-relative.json",
			wantType:    "https://lattice.dev/schemas/items/note/1.0.0",
			childRefKey: "https://lattice.dev/schemas/items/note/1.0.0",
		},
		{
			name:        "inline defs fragment ref",
			docPath:     "testdata/dashboard-inline.json",
			wantType:    "#/$defs/badge",
			childRefKey: "#/$defs/badge",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cat, fs := newTestCatalog(t)
			loader := NewLoader(fs, cat, nil, WithRelativeRoots("testdata/relroot"))
			g, err := loader.Load(tc.docPath)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if _, ok := g.Types[tc.wantType]; !ok {
				t.Errorf("Types missing %q; have %v", tc.wantType, keysOf(g.Types))
			}
			// Root must always be resolved to the container type.
			if got := g.Refs[g.Document.Root]; got != "https://lattice.dev/schemas/items/container/1.0.0" {
				t.Errorf("root ref = %q, want container", got)
			}
			if len(g.Document.Root.Children) != 1 {
				t.Fatalf("expected 1 child, got %d", len(g.Document.Root.Children))
			}
			child := g.Document.Root.Children[0]
			if got := g.Refs[child]; got != tc.childRefKey {
				t.Errorf("child ref key = %q, want %q", got, tc.childRefKey)
			}
		})
	}
}

func TestLoaderFailFast(t *testing.T) {
	tests := []struct {
		name     string
		docPath  string
		wantCode errors.Code
	}{
		{
			name:     "version mismatch on known type",
			docPath:  "testdata/dashboard-version-mismatch.json",
			wantCode: errors.SCHEMA_VERSION_MISMATCH,
		},
		{
			name:     "unresolved unknown ref",
			docPath:  "testdata/dashboard-unresolved.json",
			wantCode: errors.SCHEMA_REF_UNRESOLVED,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cat, fs := newTestCatalog(t)
			loader := NewLoader(fs, cat, nil)
			_, err := loader.Load(tc.docPath)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.HasCode(err, tc.wantCode) {
				t.Errorf("error = %v, want code %s", err, tc.wantCode)
			}
		})
	}
}

// TestLoaderURLFetcherPath exercises the offline/fixture-backed URL fetcher
// code path: an absolute ref the catalog does not serve and whose name is
// unknown is satisfied by the fetcher.
func TestLoaderURLFetcherPath(t *testing.T) {
	cat, _ := newTestCatalog(t)

	const remoteID = "https://remote.example/schemas/items/gauge/1.0.0"
	fetched := false
	fetcher := func(u *url.URL) (*jsonschema.Schema, error) {
		fetched = true
		if u.String() != remoteID {
			t.Errorf("fetcher got %q, want %q", u.String(), remoteID)
		}
		var s jsonschema.Schema
		if err := s.UnmarshalJSON([]byte(`{"$id":"` + remoteID + `","type":"object"}`)); err != nil {
			return nil, err
		}
		return &s, nil
	}

	fs := afero.NewMemMapFs()
	doc := `{
      "manifest": {"formatVersion":"1.0.0","id":"u","title":"URL"},
      "root": {"$ref":"` + remoteID + `","id":"root"}
    }`
	if err := afero.WriteFile(fs, "dash.json", []byte(doc), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loader := NewLoader(fs, cat, nil, WithURLFetcher(fetcher))
	g, err := loader.Load("dash.json")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !fetched {
		t.Error("expected URL fetcher to be invoked")
	}
	if _, ok := g.Types[remoteID]; !ok {
		t.Errorf("Types missing fetched %q; have %v", remoteID, keysOf(g.Types))
	}
}

// TestLoaderURLNoFetcherUnresolved confirms an unknown absolute URL with no
// fetcher fails fast as SCHEMA_REF_UNRESOLVED (not a version mismatch).
func TestLoaderURLNoFetcherUnresolved(t *testing.T) {
	cat, _ := newTestCatalog(t)
	fs := afero.NewMemMapFs()
	doc := `{
      "manifest": {"formatVersion":"1.0.0","id":"u","title":"URL"},
      "root": {"$ref":"https://remote.example/schemas/items/gauge/1.0.0","id":"root"}
    }`
	if err := afero.WriteFile(fs, "dash.json", []byte(doc), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	loader := NewLoader(fs, cat, nil)
	_, err := loader.Load("dash.json")
	if !errors.HasCode(err, errors.SCHEMA_REF_UNRESOLVED) {
		t.Errorf("error = %v, want SCHEMA_REF_UNRESOLVED", err)
	}
}

func TestLoaderMissingRoot(t *testing.T) {
	cat, _ := newTestCatalog(t)
	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "dash.json",
		[]byte(`{"manifest":{"formatVersion":"1.0.0","id":"x","title":"X"}}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	loader := NewLoader(fs, cat, nil)
	_, err := loader.Load("dash.json")
	if !errors.HasCode(err, errors.SCHEMA_INVALID) {
		t.Errorf("error = %v, want SCHEMA_INVALID", err)
	}
}

func TestLoaderMissingDocumentIO(t *testing.T) {
	cat, _ := newTestCatalog(t)
	loader := NewLoader(afero.NewMemMapFs(), cat, nil)
	_, err := loader.Load("does-not-exist.json")
	if !errors.HasCode(err, errors.SCHEMA_IO) {
		t.Errorf("error = %v, want SCHEMA_IO", err)
	}
}

func keysOf(m map[string]*ResolvedType) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
