package storage

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
)

// minimalDoc is a small dashboard-document fixture, shaped like
// examples/minimal-dashboard.json (a manifest carrying the addressing id plus a
// root container holding a block-wrapped table). The store is a dumb blob store,
// so the body need not be schema-valid — but a realistic shape guards against
// accidental coupling to a trivial payload, and exercises byte-faithful
// round-tripping of nested JSON.
func minimalDoc(id string) []byte {
	return []byte(`{
  "manifest": {
    "formatVersion": "1.0.0",
    "id": ` + jsonString(id) + `,
    "title": "Minimal Example Dashboard",
    "author": "Lattice"
  },
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "id": "root",
    "config": { "grid": { "columns": [1] } },
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/block/1.0.0",
        "id": "fruits-block",
        "config": {
          "id": "fruits-block",
          "content": {
            "$ref": "https://lattice.dev/schemas/items/table/1.0.0",
            "id": "fruits",
            "config": {
              "title": "Fruits",
              "columns": [{ "header": "Name" }, { "header": "Color" }],
              "rows": [["Apple", "Red"], ["Banana", "Yellow"]]
            }
          }
        }
      }
    ]
  }
}`)
}

// docFor is a terser fixture used where the document body is irrelevant and only
// the addressing id matters (List/Exists ordering, overwrite, etc.).
func docFor(id string) []byte {
	return []byte(`{"manifest":{"id":` + jsonString(id) + `},"root":{}}`)
}

// TestFSRoundTrip proves Save then Load returns byte-identical content and that
// the bytes land at <root>/<id>.json. Uses the realistic minimal-dashboard
// fixture so the round-trip covers nested JSON, not a trivial blob.
func TestFSRoundTrip(t *testing.T) {
	fs := afero.NewMemMapFs()
	s := NewFS(fs, "/store")

	doc := minimalDoc("example-minimal")
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

	// Bytes land at <root>/<id>.json (the id is the addressing key).
	onDisk, err := afero.ReadFile(fs, "/store/example-minimal.json")
	if err != nil {
		t.Fatalf("expected document at /store/example-minimal.json: %v", err)
	}
	if !bytes.Equal(onDisk, doc) {
		t.Fatalf("on-disk bytes differ from saved document")
	}
}

// TestFSSaveKeysByManifestID proves the addressing key is the document's
// manifest.id: two different ids yield two distinct files, and saving the same
// id twice overwrites in place (no second file, latest bytes win).
func TestFSSaveKeysByManifestID(t *testing.T) {
	fs := afero.NewMemMapFs()
	s := NewFS(fs, "/store")

	// Two different ids → two files.
	if err := s.Save(docFor("alpha")); err != nil {
		t.Fatalf("Save(alpha): %v", err)
	}
	if err := s.Save(docFor("beta")); err != nil {
		t.Fatalf("Save(beta): %v", err)
	}
	for _, id := range []string{"alpha", "beta"} {
		if ok, _ := afero.Exists(fs, "/store/"+id+".json"); !ok {
			t.Fatalf("expected file /store/%s.json after Save(%s)", id, id)
		}
	}
	if ids, err := s.List(); err != nil {
		t.Fatalf("List: %v", err)
	} else if want := []string{"alpha", "beta"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("List: want %v, got %v", want, ids)
	}

	// Same id twice → overwrite, not a second file. The second Save carries
	// different bytes; Load must return the latest, and List must still report
	// a single "alpha".
	first := []byte(`{"manifest":{"id":"alpha"},"v":1}`)
	second := []byte(`{"manifest":{"id":"alpha"},"v":2}`)
	if err := s.Save(first); err != nil {
		t.Fatalf("Save(alpha v1): %v", err)
	}
	if err := s.Save(second); err != nil {
		t.Fatalf("Save(alpha v2): %v", err)
	}
	got, err := s.Load("alpha")
	if err != nil {
		t.Fatalf("Load(alpha): %v", err)
	}
	if !bytes.Equal(got, second) {
		t.Fatalf("overwrite: want latest bytes %q, got %q", second, got)
	}
	ids, err := s.List()
	if err != nil {
		t.Fatalf("List after overwrite: %v", err)
	}
	if want := []string{"alpha", "beta"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("List after overwrite: want %v (no duplicate alpha), got %v", want, ids)
	}
}

// TestFSListExistsDelete exercises the full operation set: List enumerates
// stored ids in stable sorted order and reflects saves and deletes, Exists is a
// cheap presence check (true after Save, false after Delete and for unknown
// ids), and Delete removes a document.
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

	// Save out of order; List must return them sorted.
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

	// Exists: true for stored, false for unknown.
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

// TestFSLoadMissing proves Load of an unknown id returns the coded not-found
// error.
func TestFSLoadMissing(t *testing.T) {
	s := NewFS(afero.NewMemMapFs(), "/store")
	_, err := s.Load("nope")
	if !errors.HasCode(err, errors.STORAGE_NOT_FOUND) {
		t.Fatalf("Load(missing): want STORAGE_NOT_FOUND, got %v", err)
	}
}

// TestFSDeleteMissing proves Delete of an unknown id returns the coded
// not-found error.
func TestFSDeleteMissing(t *testing.T) {
	s := NewFS(afero.NewMemMapFs(), "/store")
	if err := s.Delete("nope"); !errors.HasCode(err, errors.STORAGE_NOT_FOUND) {
		t.Fatalf("Delete(missing): want STORAGE_NOT_FOUND, got %v", err)
	}
}

// TestFSSaveRejectsUnsafeID proves every operation that derives or accepts an id
// refuses an unsafe one with the coded STORAGE_ID_INVALID error, and that a
// rejected Save writes nothing (the store stays empty).
func TestFSSaveRejectsUnsafeID(t *testing.T) {
	cases := []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"parent-escape", "../escape"},
		{"forward-slash", "a/b"},
		{"backslash", `a\b`},
		{"dot", "."},
		{"dotdot", ".."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			s := NewFS(fs, "/store")

			doc := []byte(`{"manifest":{"id":` + jsonString(tc.id) + `}}`)
			if err := s.Save(doc); !errors.HasCode(err, errors.STORAGE_ID_INVALID) {
				t.Fatalf("Save id %q: want STORAGE_ID_INVALID, got %v", tc.id, err)
			}

			// A rejected Save must write nothing — the store stays empty.
			ids, err := s.List()
			if err != nil {
				t.Fatalf("List after rejected Save: %v", err)
			}
			if len(ids) != 0 {
				t.Fatalf("rejected Save left documents behind: %v", ids)
			}
			if ok, _ := afero.DirExists(fs, "/store"); ok {
				if entries, _ := afero.ReadDir(fs, "/store"); len(entries) != 0 {
					t.Fatalf("rejected Save left files under /store: %d entries", len(entries))
				}
			}
		})
	}
}

// failOnWriteFs wraps an afero.Fs so that files opened for writing (TempFile
// uses OpenFile with O_CREATE) are created on the underlying filesystem but
// fail on the first Write. This simulates a Save that fails mid-write: the temp
// file already exists on disk, so the backend's cleanup path is what must
// guarantee no partial document and no leftover temp file remain.
type failOnWriteFs struct {
	afero.Fs
}

func (f *failOnWriteFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	if flag&(os.O_WRONLY|os.O_RDWR) != 0 {
		return &failOnWriteFile{File: file}, nil
	}
	return file, nil
}

func (f *failOnWriteFs) Create(name string) (afero.File, error) {
	file, err := f.Fs.Create(name)
	if err != nil {
		return nil, err
	}
	return &failOnWriteFile{File: file}, nil
}

// failOnWriteFile is an afero.File whose Write always fails; every other
// operation (Close, Name, ...) delegates to the real file so the temp file is
// genuinely created on the underlying filesystem.
type failOnWriteFile struct {
	afero.File
}

func (f *failOnWriteFile) Write(p []byte) (int, error) {
	return 0, os.ErrInvalid
}

func (f *failOnWriteFile) WriteString(s string) (int, error) {
	return 0, os.ErrInvalid
}

// TestFSSaveAtomicWriteFailure proves a Save that fails mid-write leaves no
// partial <id>.json and no leftover temp file. The write fails after the temp
// file is created, so this asserts the backend's temp+rename atomicity: the
// destination is never touched, and the temp file is cleaned up.
func TestFSSaveAtomicWriteFailure(t *testing.T) {
	mem := afero.NewMemMapFs()
	fs := &failOnWriteFs{Fs: mem}
	s := NewFS(fs, "/store")

	err := s.Save(minimalDoc("example-minimal"))
	if !errors.HasCode(err, errors.STORAGE_IO) {
		t.Fatalf("Save with failing write: want STORAGE_IO, got %v", err)
	}

	// No partial destination file.
	if ok, _ := afero.Exists(mem, "/store/example-minimal.json"); ok {
		t.Fatalf("failed Save left a partial /store/example-minimal.json")
	}

	// No leftover temp file under root (cleanup removed it).
	entries, err := afero.ReadDir(mem, "/store")
	if err != nil {
		// A missing root is also acceptable: nothing was committed.
		if isNotExist(mem, "/store") {
			return
		}
		t.Fatalf("ReadDir(/store): %v", err)
	}
	for _, e := range entries {
		t.Fatalf("failed Save left a file behind: %q", e.Name())
	}
}

// TestFSSaveAtomicReplaceOnFailure proves a failed Save does not corrupt or
// remove an already-stored document at the same id: the prior content survives
// intact (the temp+rename approach never truncates the destination).
func TestFSSaveAtomicReplaceOnFailure(t *testing.T) {
	mem := afero.NewMemMapFs()

	// First, a successful Save through the plain MemMapFs.
	s := NewFS(mem, "/store")
	original := []byte(`{"manifest":{"id":"keep"},"v":1}`)
	if err := s.Save(original); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	// Now attempt an overwrite through a write-failing wrapper.
	failing := NewFS(&failOnWriteFs{Fs: mem}, "/store")
	if err := failing.Save([]byte(`{"manifest":{"id":"keep"},"v":2}`)); !errors.HasCode(err, errors.STORAGE_IO) {
		t.Fatalf("overwrite with failing write: want STORAGE_IO, got %v", err)
	}

	// The original document must survive unchanged.
	got, err := s.Load("keep")
	if err != nil {
		t.Fatalf("Load(keep) after failed overwrite: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("failed overwrite corrupted the stored document:\n got %q\nwant %q", got, original)
	}

	// And no leftover temp file.
	entries, err := afero.ReadDir(mem, "/store")
	if err != nil {
		t.Fatalf("ReadDir(/store): %v", err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("want exactly the stored document, got %v", names)
	}
}

// TestFSSaveInvalidJSON proves a document whose bytes are not parseable enough
// to extract manifest.id is rejected with the coded invalid error and writes
// nothing.
func TestFSSaveInvalidJSON(t *testing.T) {
	fs := afero.NewMemMapFs()
	s := NewFS(fs, "/store")
	if err := s.Save([]byte(`not json`)); !errors.HasCode(err, errors.STORAGE_INVALID) {
		t.Fatalf("Save(invalid json): want STORAGE_INVALID, got %v", err)
	}
	if ok, _ := afero.DirExists(fs, "/store"); ok {
		if entries, _ := afero.ReadDir(fs, "/store"); len(entries) != 0 {
			t.Fatalf("rejected Save left files behind: %d entries", len(entries))
		}
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
