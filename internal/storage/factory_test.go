package storage

import (
	stderrors "errors"
	"testing"

	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
)

func TestNewSelectsFSBackend(t *testing.T) {
	store, err := New(BackendFS, afero.NewMemMapFs(), "data")
	if err != nil {
		t.Fatalf("New(fs) returned error: %v", err)
	}
	if _, ok := store.(*FS); !ok {
		t.Fatalf("New(fs) = %T, want *FS", store)
	}
}

func TestNewSelectsGitBackend(t *testing.T) {
	// The git backend initialises a working-tree repo under root; an OS-backed
	// temp dir is required because go-git operates on the real filesystem.
	root := t.TempDir()
	store, err := New(BackendGit, afero.NewOsFs(), root)
	if err != nil {
		t.Fatalf("New(git) returned error: %v", err)
	}
	if _, ok := store.(*Git); !ok {
		t.Fatalf("New(git) = %T, want *Git", store)
	}
}

func TestNewUnknownBackendIsCodedError(t *testing.T) {
	store, err := New(Backend("postgres"), afero.NewMemMapFs(), "data")
	if err == nil {
		t.Fatalf("New(unknown) returned nil error (store=%v), want coded error", store)
	}
	if store != nil {
		t.Fatalf("New(unknown) returned non-nil store %T alongside error", store)
	}

	var ce *errors.CodedError
	if !stderrors.As(err, &ce) {
		t.Fatalf("New(unknown) error %T is not a *CodedError", err)
	}
	if ce.Code != errors.STORAGE_BACKEND_UNKNOWN {
		t.Fatalf("New(unknown) code = %q, want %q", ce.Code, errors.STORAGE_BACKEND_UNKNOWN)
	}
	if got := ce.Details["store"]; got != "postgres" {
		t.Fatalf("New(unknown) Details[store] = %v, want %q", got, "postgres")
	}
}
