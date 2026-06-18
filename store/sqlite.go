package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	_ "modernc.org/sqlite" // pure-Go driver, CGO_ENABLED=0 compatible

	"github.com/frankbardon/lattice/dashboard"
)

// sqliteDriver is the database/sql driver name registered by modernc.org/sqlite.
const sqliteDriver = "sqlite"

// SQLiteOptions configures a SQLiteStore.
type SQLiteOptions struct {
	// DSN is the modernc.org/sqlite data source name, e.g. a file path or
	// "file::memory:?cache=shared". Required.
	DSN string
	// Logger is the structured logger. Defaults to slog.Default() when nil.
	Logger *slog.Logger
}

// SQLiteStore is the SQLite-backed Store. It stores each dashboard as a single
// JSON document column keyed by id. SQLite specifics never leak past the Store
// interface this type satisfies.
type SQLiteStore struct {
	db  *sql.DB
	log *slog.Logger
}

// compile-time assertion that SQLiteStore satisfies Store.
var _ Store = (*SQLiteStore)(nil)

// NewSQLiteStore opens the database at opts.DSN, applies the schema, and
// returns a ready Store. The caller owns the returned store and must Close it.
func NewSQLiteStore(ctx context.Context, opts SQLiteOptions) (*SQLiteStore, error) {
	if opts.DSN == "" {
		return nil, newError(InvalidArgument, "sqlite dsn is required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	db, err := sql.Open(sqliteDriver, opts.DSN)
	if err != nil {
		return nil, wrapError(Internal, "open sqlite", err)
	}

	s := &SQLiteStore{db: db, log: logger}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	logger.Debug("sqlite store ready", "dsn", opts.DSN)
	return s, nil
}

// Close releases the underlying database handle.
func (s *SQLiteStore) Close() error {
	if err := s.db.Close(); err != nil {
		return wrapError(Internal, "close sqlite", err)
	}
	return nil
}

// migrate applies the forward schema. It is idempotent.
func (s *SQLiteStore) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS dashboards (
	id  TEXT PRIMARY KEY,
	doc TEXT NOT NULL
);`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return wrapError(Internal, "migrate schema", err)
	}
	return nil
}

// Create persists a new dashboard, returning Exists on id collision.
func (s *SQLiteStore) Create(ctx context.Context, doc *dashboard.Dashboard) error {
	if doc == nil || doc.ID == "" {
		return newError(InvalidArgument, "dashboard id is required")
	}
	blob, err := json.Marshal(doc)
	if err != nil {
		return wrapError(Internal, "marshal dashboard", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO dashboards (id, doc) VALUES (?, ?)`, doc.ID, string(blob))
	if err != nil {
		if isUniqueViolation(err) {
			return newError(Exists, fmt.Sprintf("dashboard %q already exists", doc.ID))
		}
		return wrapError(Internal, "insert dashboard", err)
	}
	return nil
}

// Load returns the dashboard for id, or NotFound.
func (s *SQLiteStore) Load(ctx context.Context, id string) (*dashboard.Dashboard, error) {
	if id == "" {
		return nil, newError(InvalidArgument, "dashboard id is required")
	}
	var blob string
	err := s.db.QueryRowContext(ctx, `SELECT doc FROM dashboards WHERE id = ?`, id).Scan(&blob)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, newError(NotFound, fmt.Sprintf("dashboard %q not found", id))
	}
	if err != nil {
		return nil, wrapError(Internal, "select dashboard", err)
	}
	return decodeDoc(blob)
}

// Save upserts a dashboard.
func (s *SQLiteStore) Save(ctx context.Context, doc *dashboard.Dashboard) error {
	if doc == nil || doc.ID == "" {
		return newError(InvalidArgument, "dashboard id is required")
	}
	blob, err := json.Marshal(doc)
	if err != nil {
		return wrapError(Internal, "marshal dashboard", err)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO dashboards (id, doc) VALUES (?, ?)
ON CONFLICT(id) DO UPDATE SET doc = excluded.doc`, doc.ID, string(blob))
	if err != nil {
		return wrapError(Internal, "upsert dashboard", err)
	}
	return nil
}

// List returns all dashboards ordered by id.
func (s *SQLiteStore) List(ctx context.Context) ([]*dashboard.Dashboard, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT doc FROM dashboards ORDER BY id`)
	if err != nil {
		return nil, wrapError(Internal, "list dashboards", err)
	}
	defer rows.Close()

	docs := make([]*dashboard.Dashboard, 0)
	for rows.Next() {
		var blob string
		if err := rows.Scan(&blob); err != nil {
			return nil, wrapError(Internal, "scan dashboard", err)
		}
		doc, err := decodeDoc(blob)
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapError(Internal, "iterate dashboards", err)
	}
	return docs, nil
}

// Delete removes the dashboard for id, returning NotFound when absent.
func (s *SQLiteStore) Delete(ctx context.Context, id string) error {
	if id == "" {
		return newError(InvalidArgument, "dashboard id is required")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM dashboards WHERE id = ?`, id)
	if err != nil {
		return wrapError(Internal, "delete dashboard", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return wrapError(Internal, "delete dashboard rows", err)
	}
	if n == 0 {
		return newError(NotFound, fmt.Sprintf("dashboard %q not found", id))
	}
	return nil
}

// decodeDoc unmarshals a stored JSON document.
func decodeDoc(blob string) (*dashboard.Dashboard, error) {
	var doc dashboard.Dashboard
	if err := json.Unmarshal([]byte(blob), &doc); err != nil {
		return nil, wrapError(Internal, "unmarshal dashboard", err)
	}
	return &doc, nil
}

// isUniqueViolation reports whether err is a SQLite UNIQUE/PRIMARY KEY
// constraint failure. Kept here so the detection stays behind the Store.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// modernc.org/sqlite surfaces constraint failures in the error string;
	// matching the message keeps us off the driver's internal types.
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}
