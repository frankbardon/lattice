package storage

import (
	"bytes"
	"testing"

	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
)

// TestFSRoundTrip is a smoke test: a document Saved is Loaded back byte-for-byte
// under <id>.json. The comprehensive suite is E1-S3.
func TestFSRoundTrip(t *testing.T) {
	fs := afero.NewMemMapFs()
	s := NewFS(fs, "/store")

	doc := []byte(`{"manifest":{"id":"example-minimal"},"root":{}}`)
	if err := s.Save(doc); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Load("example-minimal")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(got, doc) {
		t.Fatalf("round-trip mismatch:\n got %q\nwant %q", got, doc)
	}

	// Bytes land at <root>/<id>.json.
	if ok, _ := afero.Exists(fs, "/store/example-minimal.json"); !ok {
		t.Fatalf("expected document at /store/example-minimal.json")
	}
}

func TestFSSaveRejectsUnsafeID(t *testing.T) {
	s := NewFS(afero.NewMemMapFs(), "/store")
	cases := []string{"", "../escape", "a/b", `a\b`, "."}
	for _, id := range cases {
		doc := []byte(`{"manifest":{"id":` + jsonString(id) + `}}`)
		err := s.Save(doc)
		if !errors.HasCode(err, errors.STORAGE_ID_INVALID) {
			t.Fatalf("id %q: want STORAGE_ID_INVALID, got %v", id, err)
		}
	}
}

func TestFSLoadMissing(t *testing.T) {
	s := NewFS(afero.NewMemMapFs(), "/store")
	_, err := s.Load("nope")
	if !errors.HasCode(err, errors.STORAGE_NOT_FOUND) {
		t.Fatalf("want STORAGE_NOT_FOUND, got %v", err)
	}
}

// jsonString quotes a string as a JSON string literal for inline fixtures.
func jsonString(s string) string {
	var b bytes.Buffer
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}
