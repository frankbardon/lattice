package service

import (
	stderrors "errors"
	"os"
	"testing"

	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
)

func TestNewResolver(t *testing.T) {
	res, err := NewResolver(os.DirFS("../schemas"))
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	if res == nil {
		t.Fatal("NewResolver returned nil resolver")
	}
}

func TestNewResolverMissingSchemaIsCoded(t *testing.T) {
	// An empty in-memory FS lacks dashboard.schema.json -> SCHEMA_IO.
	_, err := NewResolver(afero.NewIOFS(afero.NewMemMapFs()))
	if err == nil {
		t.Fatal("expected error for missing dashboard schema")
	}
	var ce *errors.CodedError
	if !stderrors.As(err, &ce) {
		t.Fatalf("expected *errors.CodedError, got %T", err)
	}
	if !errors.HasCode(err, errors.SCHEMA_IO) {
		t.Fatalf("expected SCHEMA_IO, got %s", ce.Code)
	}
}

func TestNewStore(t *testing.T) {
	store, err := NewStore(BackendFS, afero.NewMemMapFs(), ".")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if store == nil {
		t.Fatal("NewStore returned nil store")
	}
}

func TestNewWiresPair(t *testing.T) {
	res, err := NewResolver(os.DirFS("../schemas"))
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	store, err := NewStore(BackendFS, afero.NewMemMapFs(), ".")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	svc := New(store, res)
	if svc == nil {
		t.Fatal("New returned nil service")
	}
	if svc.store != store || svc.resolver != res {
		t.Fatal("New did not wire the supplied store/resolver")
	}
}

func TestOpen(t *testing.T) {
	svc, err := Open(Options{
		Root:    t.TempDir(),
		Backend: BackendFS,
		Schemas: os.DirFS("../schemas"),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if svc == nil || svc.store == nil || svc.resolver == nil {
		t.Fatal("Open returned an unwired service")
	}
}
