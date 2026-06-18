// Command server is the lattice binary entry point. For the foundation story
// it constructs the persistence layer and logs startup; later stories wire the
// HTTP server, realtime transport, render pipeline, and agent hub.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/frankbardon/lattice/store"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	ctx := context.Background()
	const dsn = "file:lattice.db"

	st, err := store.NewSQLiteStore(ctx, store.SQLiteOptions{DSN: dsn, Logger: logger})
	if err != nil {
		logger.Error("failed to open store", "error", err)
		os.Exit(1)
	}
	defer func() { _ = st.Close() }()

	logger.Info("lattice server starting", "store", "sqlite", "dsn", dsn)
}
