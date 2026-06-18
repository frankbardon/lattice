// Command server is the lattice binary entry point. It constructs the
// persistence layer, the embedded Parsec realtime hub, and an HTTP server that
// serves the realtime transport, scoped token minting, and a thin dashboard
// client. Later stories wire the render pipeline and agent hub.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/frankbardon/lattice/realtime"
	"github.com/frankbardon/lattice/store"
)

const (
	defaultDSN  = "file:lattice.db"
	defaultAddr = ":8080"
	// secretEnv holds the HMAC secret used to sign subscribe tokens. It must
	// be set in any deployment so tokens survive a restart; an unset secret
	// triggers an ephemeral random secret (logged loudly) for local dev only.
	secretEnv = "LATTICE_HMAC_SECRET"
	addrEnv   = "LATTICE_ADDR"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("server exited", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.NewSQLiteStore(ctx, store.SQLiteOptions{DSN: defaultDSN, Logger: logger})
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	secret := hmacSecret(logger)
	hub, err := realtime.NewHub(secret, realtime.Options{Logger: logger})
	if err != nil {
		return err
	}

	runErr := make(chan error, 1)
	go func() { runErr <- hub.Run(ctx) }()
	for !hub.Started() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-runErr:
			return err
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	addr := defaultAddr
	if v := os.Getenv(addrEnv); v != "" {
		addr = v
	}
	srv := &http.Server{Addr: addr, Handler: hub.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		logger.Info("lattice server starting", "store", "sqlite", "dsn", defaultDSN, "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			runErr <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-runErr:
		if err != nil {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	if err := <-runErr; err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// hmacSecret reads the signing secret from the environment, falling back to an
// ephemeral random secret for local development. A random secret means minted
// tokens do not survive a restart, so it is logged loudly.
func hmacSecret(logger *slog.Logger) []byte {
	if v := os.Getenv(secretEnv); v != "" {
		return []byte(v)
	}
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	secret := []byte(hex.EncodeToString(buf))
	logger.Warn("no " + secretEnv + " set; using ephemeral random HMAC secret (tokens will not survive restart)")
	return secret
}
