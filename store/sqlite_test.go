package store_test

import (
	"context"
	"testing"

	"github.com/frankbardon/lattice/store"
	"github.com/frankbardon/lattice/store/storetest"
)

// newSQLiteStore returns a fresh in-memory SQLite store for one test.
func newSQLiteStore(t *testing.T) (store.Store, func()) {
	t.Helper()
	// A unique shared-cache in-memory DSN per test keeps tests isolated while
	// surviving across the multiple connections database/sql may open.
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	s, err := store.NewSQLiteStore(context.Background(), store.SQLiteOptions{DSN: dsn})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	return s, func() { _ = s.Close() }
}

// TestSQLiteConformance runs the shared store conformance suite against the
// SQLite implementation.
func TestSQLiteConformance(t *testing.T) {
	storetest.Run(t, newSQLiteStore)
}

// TestNewSQLiteStoreRequiresDSN verifies the constructor rejects an empty DSN.
func TestNewSQLiteStoreRequiresDSN(t *testing.T) {
	_, err := store.NewSQLiteStore(context.Background(), store.SQLiteOptions{})
	if store.CodeOf(err) != store.InvalidArgument {
		t.Fatalf("code = %q, want %q (err=%v)", store.CodeOf(err), store.InvalidArgument, err)
	}
}
