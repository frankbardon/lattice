package storage

import (
	"bytes"
	"reflect"
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

func docFor(id string) []byte {
	return []byte(`{"manifest":{"id":` + jsonString(id) + `},"root":{}}`)
}

// TestFSListExistsDelete exercises the E1-S2 operations: List enumerates stored
// ids in stable order and reflects saves/deletes, Exists is a cheap presence
// check, and Delete removes a document (missing id → STORAGE_NOT_FOUND).
func TestFSListExistsDelete(t *testing.T) {
	fs := afero.NewMemMapFs()
	s := NewFS(fs, "/store")

	// List on an absent root is empty, not an error.
	ids, err := s.List()
	if err != nil {
		t.Fatalf("List (empty): %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("List (empty): want none, got %v", ids)
	}

	for _, id := range []string{"gamma", "alpha", "beta"} {
		if err := s.Save(docFor(id)); err != nil {
			t.Fatalf("Save %q: %v", id, err)
		}
	}

	ids, err = s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if want := []string{"alpha", "beta", "gamma"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("List: want %v, got %v", want, ids)
	}

	// Exists is true for a stored id, false otherwise.
	if ok, err := s.Exists("alpha"); err != nil || !ok {
		t.Fatalf("Exists(alpha): want true, got %v (err %v)", ok, err)
	}
	if ok, err := s.Exists("nope"); err != nil || ok {
		t.Fatalf("Exists(nope): want false, got %v (err %v)", ok, err)
	}

	// Delete removes the document; afterwards Exists is false, Load is
	// not-found, and List no longer reports it.
	if err := s.Delete("beta"); err != nil {
		t.Fatalf("Delete(beta): %v", err)
	}
	if ok, _ := s.Exists("beta"); ok {
		t.Fatalf("Exists(beta) after Delete: want false")
	}
	if _, err := s.Load("beta"); !errors.HasCode(err, errors.STORAGE_NOT_FOUND) {
		t.Fatalf("Load(beta) after Delete: want STORAGE_NOT_FOUND, got %v", err)
	}
	ids, err = s.List()
	if err != nil {
		t.Fatalf("List after Delete: %v", err)
	}
	if want := []string{"alpha", "gamma"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("List after Delete: want %v, got %v", want, ids)
	}
}

func TestFSDeleteMissing(t *testing.T) {
	s := NewFS(afero.NewMemMapFs(), "/store")
	if err := s.Delete("nope"); !errors.HasCode(err, errors.STORAGE_NOT_FOUND) {
		t.Fatalf("Delete(missing): want STORAGE_NOT_FOUND, got %v", err)
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
