package schema

import (
	"testing"

	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
)

func TestNewCatalogIndexesByID(t *testing.T) {
	cat, _ := newTestCatalog(t)

	want := []string{
		"https://lattice.dev/schemas/items/table/1.0.0",
		"https://lattice.dev/schemas/items/container/1.0.0",
	}
	for _, id := range want {
		rt, ok := cat.lookupID(id)
		if !ok {
			t.Errorf("catalog missing id %q", id)
			continue
		}
		if rt.Version != "1.0.0" {
			t.Errorf("id %q version = %q, want 1.0.0", id, rt.Version)
		}
	}

	if !cat.hasName("table") {
		t.Error("expected catalog to know name table")
	}
	if cat.hasName("nonexistent") {
		t.Error("did not expect catalog to know name nonexistent")
	}
	if got := cat.availableVersions("table"); len(got) != 1 || got[0] != "1.0.0" {
		t.Errorf("availableVersions(table) = %v, want [1.0.0]", got)
	}
}

func TestNewCatalogRejectsInvalidSchema(t *testing.T) {
	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "cat/bad.schema.json", []byte("{not json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := NewCatalog(fs, "cat")
	if !errors.HasCode(err, errors.SCHEMA_INVALID) {
		t.Errorf("error = %v, want SCHEMA_INVALID", err)
	}
}

func TestNewCatalogRejectsMissingID(t *testing.T) {
	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "cat/noid.schema.json", []byte(`{"type":"object"}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := NewCatalog(fs, "cat")
	if !errors.HasCode(err, errors.SCHEMA_INVALID) {
		t.Errorf("error = %v, want SCHEMA_INVALID", err)
	}
}

func TestVersionParsing(t *testing.T) {
	tests := []struct {
		id          string
		wantName    string
		wantVersion string
	}{
		{"https://lattice.dev/schemas/items/table/1.0.0", "table", "1.0.0"},
		{"https://lattice.dev/schemas/items/container/2.3.4", "container", "2.3.4"},
		{"https://lattice.dev/schemas/dashboard/1.0.0", "dashboard", "1.0.0"},
		{"https://lattice.dev/schemas/items/table", "", ""},
		{"#/$defs/badge", "", ""},
	}
	for _, tc := range tests {
		if got := nameOf(tc.id); got != tc.wantName {
			t.Errorf("nameOf(%q) = %q, want %q", tc.id, got, tc.wantName)
		}
		if got := versionOf(tc.id); got != tc.wantVersion {
			t.Errorf("versionOf(%q) = %q, want %q", tc.id, got, tc.wantVersion)
		}
	}
}

func TestNormalizeRelativeRef(t *testing.T) {
	tests := []struct {
		ref  string
		want string
	}{
		{"items/table/1.0.0", "https://lattice.dev/schemas/items/table/1.0.0"},
		{"./items/table/1.0.0", "https://lattice.dev/schemas/items/table/1.0.0"},
		{"/items/table/1.0.0", "https://lattice.dev/schemas/items/table/1.0.0"},
	}
	for _, tc := range tests {
		if got := normalizeRelativeRef(tc.ref); got != tc.want {
			t.Errorf("normalizeRelativeRef(%q) = %q, want %q", tc.ref, got, tc.want)
		}
	}
}
