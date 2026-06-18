// Package store persists lattice dashboard documents. The Store interface is
// the seam that decouples callers from the backing engine: v1 ships a SQLite
// implementation that serializes each document as a JSON column, and a future
// Postgres implementation can drop in without touching callers. No SQLite (or
// any backend) specifics leak past this interface.
package store

import (
	"context"

	"github.com/frankbardon/lattice/dashboard"
)

// Store is the persistence seam for dashboard documents. Implementations must
// be safe for concurrent use. Errors are *Error with a typed Code; callers
// should branch on CodeOf rather than the backend.
type Store interface {
	// Create persists a new dashboard. It returns Exists if a dashboard with
	// the same id already exists.
	Create(ctx context.Context, doc *dashboard.Dashboard) error

	// Load returns the dashboard for id, or NotFound if none exists.
	Load(ctx context.Context, id string) (*dashboard.Dashboard, error)

	// Save upserts a dashboard, replacing any existing document with the same
	// id and creating it otherwise.
	Save(ctx context.Context, doc *dashboard.Dashboard) error

	// List returns all stored dashboards ordered by id.
	List(ctx context.Context) ([]*dashboard.Dashboard, error)

	// Delete removes the dashboard for id. It returns NotFound if none exists.
	Delete(ctx context.Context, id string) error
}
