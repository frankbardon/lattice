package scene

import (
	"context"
	"encoding/json"
	"sync"

	"log/slog"
)

// Manager owns the set of live, server-authoritative dashboard documents,
// opening one Doc per dashboard on first use (load-on-open) and routing inbound
// intents to it. It is the bridge between the realtime intent intake and the
// per-dashboard Doc engine, and is safe for concurrent use.
type Manager struct {
	store       Store
	broadcaster Broadcaster
	logger      *slog.Logger

	mu   sync.Mutex
	docs map[string]*Doc
}

// NewManager constructs a Manager over a store and a broadcaster. Both are
// required.
func NewManager(st Store, bc Broadcaster, opts Options) (*Manager, error) {
	if st == nil {
		return nil, newError(Internal, "store required")
	}
	if bc == nil {
		return nil, newError(Internal, "broadcaster required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		store:       st,
		broadcaster: bc,
		logger:      logger,
		docs:        make(map[string]*Doc),
	}, nil
}

// Doc returns the live Doc for id, opening (rehydrating from the store) it on
// first request. Subsequent calls return the same in-memory authority.
func (m *Manager) Doc(ctx context.Context, id string) (*Doc, error) {
	m.mu.Lock()
	if d, ok := m.docs[id]; ok {
		m.mu.Unlock()
		return d, nil
	}
	m.mu.Unlock()

	// Open outside the lock (it does store I/O); a racing opener is resolved by
	// a re-check under the lock so the map holds a single Doc per id.
	d, err := Open(ctx, id, m.store, m.broadcaster, Options{Logger: m.logger})
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.docs[id]; ok {
		return existing, nil
	}
	m.docs[id] = d
	return d, nil
}

// HandleIntent decodes and applies a raw client intent against the dashboard's
// authoritative Doc, returning the applied RFC6902 patch. It is the function
// wired into the realtime Hub's IntentHandler.
func (m *Manager) HandleIntent(ctx context.Context, dashboardID string, raw json.RawMessage) (json.RawMessage, error) {
	in, err := DecodeIntent(raw)
	if err != nil {
		return nil, err
	}
	d, err := m.Doc(ctx, dashboardID)
	if err != nil {
		return nil, err
	}
	return d.Apply(ctx, in)
}
