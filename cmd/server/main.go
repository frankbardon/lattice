// Command server is the lattice binary entry point. It constructs the
// persistence layer, the embedded Parsec realtime hub, and an HTTP server that
// serves the realtime transport, scoped token minting, and a thin dashboard
// client. Later stories wire the render pipeline and agent hub.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/frankbardon/lattice/agenthub"
	"github.com/frankbardon/lattice/brickagent"
	"github.com/frankbardon/lattice/dashboard"
	"github.com/frankbardon/lattice/pulsemcp"
	"github.com/frankbardon/lattice/realtime"
	"github.com/frankbardon/lattice/render"
	"github.com/frankbardon/lattice/resolve"
	"github.com/frankbardon/lattice/scene"
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
	// pulseBinEnv and pulseDataDirEnv configure the Pulse stdio MCP child the
	// pulse_prism renderer queries for data. When pulseDataDirEnv is unset the
	// pulse manager is not started and the pulse_prism kind is not registered,
	// so markdown-only boards run with no Pulse dependency.
	pulseBinEnv     = "LATTICE_PULSE_BIN"
	pulseDataDirEnv = "PULSE_DATA_DIR"
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

	// The hub needs an intent handler at construction, but the scene manager
	// needs the hub as its broadcaster — break the cycle with a holder the hub
	// dispatches through and that is bound to the manager once both exist.
	var mgr *scene.Manager
	intentHandler := func(ctx context.Context, dashboardID string, raw json.RawMessage) (realtime.IntentResult, error) {
		patch, err := mgr.HandleIntent(ctx, dashboardID, raw)
		if err != nil {
			return realtime.IntentResult{}, err
		}
		return realtime.IntentResult{Patch: patch}, nil
	}

	// The brick build loop (E4-S2) drives a brick's AI builder agent from a chat
	// message and applies the emitted parameterized template as an edit_template
	// intent. The Builder needs the scene Manager (which needs the hub), so — like
	// the intent handler — the brick_chat handler dispatches through a holder bound
	// once both exist. It stays nil (RPC rejected) when no agent hub is wired
	// (no Pulse configured, see below), since the brick agent reaches data via the
	// Pulse MCP child.
	var builder *brickagent.Builder
	brickChatHandler := func(ctx context.Context, dashboardID, brickID, message string) (realtime.BrickChatResult, error) {
		if builder == nil {
			return realtime.BrickChatResult{}, errors.New("brick agent not available: PULSE_DATA_DIR is not configured")
		}
		res, err := builder.Build(ctx, dashboardID, brickID, message)
		if err != nil {
			return realtime.BrickChatResult{}, err
		}
		return realtime.BrickChatResult{Patch: res.Patch, Template: res.Template}, nil
	}

	// docProvider backs the thin client's initial-snapshot read endpoint. A
	// dashboard the client opens for the first time is seeded as an empty board
	// (v1 has no separate create flow yet); the scene Manager then owns it as
	// the authoritative document and the client converges via the patch stream.
	docProvider := func(ctx context.Context, dashboardID string) (json.RawMessage, error) {
		if err := ensureDashboard(ctx, st, dashboardID, logger); err != nil {
			return nil, err
		}
		d, err := mgr.Doc(ctx, dashboardID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(d.Snapshot())
	}

	hub, err := realtime.NewHub(secret, realtime.Options{
		Logger:           logger,
		IntentHandler:    intentHandler,
		DocProvider:      docProvider,
		BrickChatHandler: brickChatHandler,
	})
	if err != nil {
		return err
	}

	// Render seam: a registry dispatches by brick.kind. v1 ships the markdown
	// kind; pulse_prism registers here in a later epic. The hook bridges scene
	// (which knows a template changed) to render+realtime (which know how to
	// render and broadcast), keeping render out of scene's patch path.
	registry := render.NewRegistry(render.Options{Logger: logger})
	if err := registry.Register(render.KindMarkdown, render.NewMarkdown()); err != nil {
		return err
	}

	// agentHub is the brick AI builder engine pool; it is wired only when Pulse
	// is configured (the agent fetches data via the Pulse MCP child). Left nil
	// otherwise, leaving the brick_chat RPC to report the agent unavailable.
	var agentHub *agenthub.Hub

	// pulse_prism kind: register only when a Pulse data dir is configured. The
	// renderer needs the Pulse MCP manager (E2-S1) to fetch data, so the manager
	// is started here and injected into the renderer at construction (the
	// Renderer interface signature cannot carry it). The manager is stopped on
	// shutdown. Absent PULSE_DATA_DIR, markdown boards still serve.
	if dataDir := os.Getenv(pulseDataDirEnv); dataDir != "" {
		cfg := pulsemcp.DefaultConfig()
		cfg.DataDir = dataDir
		if bin := os.Getenv(pulseBinEnv); bin != "" {
			cfg.BinaryPath = bin
		}
		pulseMgr, err := pulsemcp.NewManager(cfg, pulsemcp.Options{Logger: logger})
		if err != nil {
			return err
		}
		if err := pulseMgr.Start(ctx); err != nil {
			return err
		}
		defer func() {
			sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = pulseMgr.Stop(sctx)
		}()
		if err := registry.Register(render.KindPulsePrism, render.NewPulsePrism(pulseMgr)); err != nil {
			return err
		}
		logger.Info("pulse_prism renderer registered", "data_dir", dataDir, "binary", cfg.BinaryPath)

		// Brick AI builder (E4-S2): one agenthub.Hub per process boots a Nexus
		// builder engine per brick (keyed by agent_id) lazily on first chat. The
		// agent reaches data through the SAME pulse binary + data dir the renderer
		// uses (so it reasons over the cohorts the board renders), and is forced to
		// emit a parameterized {pulse_request, prism_spec} template via the
		// json_schema gate (brickagent.TemplateSchema). The Builder (constructed
		// after the scene Manager exists) routes brick_chat RPCs through it.
		ahCfg := agenthub.DefaultConfig()
		ahCfg.PulseBinaryPath = cfg.BinaryPath
		ahCfg.PulseDataDir = dataDir
		ahCfg.OutputSchema = brickagent.TemplateSchema
		agentHub, err = agenthub.NewHub(ahCfg, agenthub.Options{Logger: logger})
		if err != nil {
			return err
		}
		defer func() { _ = agentHub.Close() }()
		logger.Info("brick agent hub started", "data_dir", dataDir, "binary", ahCfg.PulseBinaryPath)
	} else {
		logger.Info("PULSE_DATA_DIR unset; pulse_prism renderer + brick agent disabled")
	}
	renderHook := func(ctx context.Context, dashboardID string, brick dashboard.Brick, vars []dashboard.Variable) {
		// Server-side variable resolution (E3-S2): substitute the brick's ${var}
		// placeholders with the dashboard's current variable values BEFORE the
		// renderer parses the template — the seam E2-S2 documented. pulse_prism
		// templates are JSON (substitution must stay JSON-safe); markdown is not.
		// This is pure resolve+render; no agent is invoked on a variable change.
		vals := resolve.FromVariables(vars)
		resolved := resolve.Substitute(brick.Template, vals, brick.Kind == render.KindPulsePrism)
		html, err := registry.Render(brick.Kind, resolved, render.ResolvedVars{})
		if err != nil {
			code := render.CodeOf(err)
			logger.Warn("render brick failed", "dashboard", dashboardID, "brick", brick.ID, "kind", brick.Kind, "code", code, "error", err)
			// Surface the typed render error to every subscriber on the rendered
			// topic. The edit_template intent already acked (it only stores the
			// template string), so this is the only path the failure reaches the
			// client; the thin client shows it against the brick / in the editor
			// panel without crashing the board.
			if berr := hub.BroadcastRenderError(ctx, dashboardID, brick.ID, string(code), err.Error()); berr != nil {
				logger.Warn("broadcast render error failed", "dashboard", dashboardID, "brick", brick.ID, "error", berr)
			}
			return
		}
		if err := hub.BroadcastRendered(ctx, dashboardID, brick.ID, html); err != nil {
			logger.Warn("broadcast rendered fragment failed", "dashboard", dashboardID, "brick", brick.ID, "error", err)
		}
	}

	mgr, err = scene.NewManager(st, hub, scene.Options{Logger: logger, RenderHook: renderHook})
	if err != nil {
		return err
	}

	// Now that the scene Manager exists, bind the brick build loop. The Builder
	// drives the agent hub (per-brick engines) and applies emitted templates
	// through mgr (the authoritative scene path: snapshot + broadcast + render
	// hook). Only when an agent hub was wired (Pulse configured).
	if agentHub != nil {
		builder, err = brickagent.NewBuilder(agentHub, brickagent.SceneManagerStore{Manager: mgr}, brickagent.Options{Logger: logger})
		if err != nil {
			return err
		}
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

// ensureDashboard guarantees a dashboard document exists for id, seeding an
// empty board the first time one is opened. v1 has no separate create flow, so
// opening a board link is what brings it into being. Create races (two clients
// opening the same fresh board) collapse harmlessly: a store Exists result is
// not an error here.
func ensureDashboard(ctx context.Context, st store.Store, id string, logger *slog.Logger) error {
	if _, err := st.Load(ctx, id); err == nil {
		return nil
	} else if store.CodeOf(err) != store.NotFound {
		return err
	}
	doc := &dashboard.Dashboard{
		ID:        id,
		Name:      "",
		Variables: []dashboard.Variable{},
		Bricks:    []dashboard.Brick{},
	}
	if err := st.Create(ctx, doc); err != nil {
		if store.CodeOf(err) == store.Exists {
			return nil
		}
		return err
	}
	logger.Info("seeded empty dashboard", "id", id)
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
