// Package agenthub manages the AI builder agents that build dashboard bricks.
//
// Each brick has its own builder agent (Bricks carry an agent_id). Following
// the Nexus desktop-shell pattern (pkg/desktop/shell.go), the Hub owns a
// concurrency-safe map of agentID → engine and lazily boots one Nexus engine
// per agent on first use, tearing it down when idle (timeout or explicit Close)
// to bound the number of concurrent LLM sessions per dashboard. One Hub
// instance corresponds to one dashboard; MaxConcurrent bounds that dashboard's
// engines.
//
// The brick agent reaches Pulse (and optionally Prism) through Nexus'
// nexus.mcp.client over stdio, pointed at the SAME pulse binary the E2-S1
// pulsemcp.Manager spawns — the hub wires the binary path + data dir into the
// agent config rather than standing up a parallel process.
//
// This package is lifecycle-only (E4-S1): it boots, drives a trivial message
// round-trip, and tears down. The build loop and structured widget-spec output
// are E4-S2.
package agenthub

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/frankbardon/nexus/pkg/engine"
	"github.com/frankbardon/nexus/pkg/engine/allplugins"
	"github.com/frankbardon/nexus/pkg/events"
)

// engineHandle is the minimal slice of *engine.Engine the hub drives. It is an
// interface so the lifecycle (lazy boot / idle teardown / concurrency bound)
// can be unit-tested with a stub that needs no LLM, while production uses the
// real Nexus engine via newNexusEngine.
type engineHandle interface {
	// Boot starts the engine's session and plugins. Non-blocking.
	Boot(ctx context.Context) error
	// Stop tears the engine down and releases its resources.
	Stop(ctx context.Context) error
	// Emit publishes an event on the engine's bus.
	Emit(eventType string, payload any) error
	// Subscribe registers a handler and returns an unsubscribe func.
	Subscribe(eventType string, handler func(payload any)) (unsubscribe func())
}

// engineFactory builds an engineHandle for an agentID from rendered config
// bytes. Swappable in tests.
type engineFactory func(agentID string, configYAML []byte) (engineHandle, error)

// nexusEngine adapts *engine.Engine to engineHandle.
type nexusEngine struct{ eng *engine.Engine }

func (n *nexusEngine) Boot(ctx context.Context) error { return n.eng.Boot(ctx) }
func (n *nexusEngine) Stop(ctx context.Context) error { return n.eng.Stop(ctx) }
func (n *nexusEngine) Emit(t string, p any) error     { return n.eng.Bus.Emit(t, p) }
func (n *nexusEngine) Subscribe(t string, h func(any)) func() {
	return n.eng.Bus.Subscribe(t, func(e engine.Event[any]) { h(e.Payload) })
}

// newNexusEngine is the production engineFactory: it constructs a real Nexus
// engine from the rendered config and registers all built-in plugin factories
// (ReAct agent, Anthropic provider, MCP client, etc.) before boot.
func newNexusEngine(_ string, configYAML []byte) (engineHandle, error) {
	eng, err := engine.NewFromBytes(configYAML)
	if err != nil {
		return nil, wrapError(Boot, "construct engine", err)
	}
	allplugins.RegisterAll(eng.Registry)
	return &nexusEngine{eng: eng}, nil
}

// Options configures non-essential Hub dependencies.
type Options struct {
	// Logger is the structured logger. Defaults to slog.Default() when nil.
	Logger *slog.Logger

	// newEngine overrides the engine factory. Unexported so external callers
	// always get the real Nexus engine; tests in this package set it to a stub.
	newEngine engineFactory

	// now overrides the clock for idle-reaper tests. Defaults to time.Now.
	now func() time.Time
}

// agentState tracks one agent's engine and idle bookkeeping.
type agentState struct {
	eng        engineHandle
	lastUsed   time.Time
	booting    bool       // true while a boot is in flight (slot reserved)
	bootDone   chan error // closed when boot completes; readers wait on it
	bootResult error
}

// Hub manages brick-builder engines keyed by agentID for one dashboard.
// Construct with NewHub; call Get to lazily boot an engine, Drive to round-trip
// a message, and Close on shutdown. Safe for concurrent use.
type Hub struct {
	cfg    Config
	logger *slog.Logger
	now    func() time.Time
	mkEng  engineFactory

	mu     sync.Mutex
	agents map[string]*agentState
	closed bool

	reaperStop chan struct{}
	reaperDone chan struct{}
}

// NewHub validates cfg and constructs a Hub. It does not boot any engine; that
// happens lazily on the first Get for each agentID. If IdleTimeout > 0 an idle
// reaper goroutine starts and runs until Close.
func NewHub(cfg Config, opts Options) (*Hub, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	mkEng := opts.newEngine
	if mkEng == nil {
		mkEng = newNexusEngine
	}
	clock := opts.now
	if clock == nil {
		clock = time.Now
	}

	h := &Hub{
		cfg:    cfg,
		logger: logger,
		now:    clock,
		mkEng:  mkEng,
		agents: make(map[string]*agentState),
	}

	if cfg.IdleTimeout > 0 {
		h.reaperStop = make(chan struct{})
		h.reaperDone = make(chan struct{})
		go h.reap()
	}
	return h, nil
}

// Get lazily boots (or returns the already-running) engine for agentID. The
// engine is constructed from the brick-agent YAML config, with Pulse (and
// optionally Prism) MCP tools wired in. Booting a new engine when the hub is
// already at MaxConcurrent first tries to reclaim an idle slot; if none can be
// reclaimed it returns an AtCapacity error.
//
// Concurrent Get calls for the SAME agentID collapse onto a single boot: the
// first caller boots, the rest wait for that boot's result.
func (h *Hub) Get(ctx context.Context, agentID string) (engineHandle, error) {
	for {
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return nil, newError(Closed, "hub is closed")
		}

		st, ok := h.agents[agentID]
		if ok && st.eng != nil {
			// Already running.
			st.lastUsed = h.now()
			eng := st.eng
			h.mu.Unlock()
			return eng, nil
		}
		if ok && st.booting {
			// A boot is in flight; wait for it outside the lock, then retry.
			done := st.bootDone
			h.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, wrapError(Boot, "wait for in-flight boot", ctx.Err())
			case <-done:
				continue
			}
		}

		// We need to boot. Enforce the concurrency bound, reclaiming an idle
		// engine if necessary.
		if err := h.ensureCapacityLocked(); err != nil {
			h.mu.Unlock()
			return nil, err
		}

		st = &agentState{booting: true, bootDone: make(chan error)}
		h.agents[agentID] = st
		h.mu.Unlock()

		eng, err := h.boot(ctx, agentID)

		h.mu.Lock()
		if err != nil {
			delete(h.agents, agentID)
		} else {
			st.eng = eng
			st.booting = false
			st.lastUsed = h.now()
		}
		st.bootResult = err
		close(st.bootDone)
		h.mu.Unlock()

		if err != nil {
			return nil, err
		}
		return eng, nil
	}
}

// ensureCapacityLocked guarantees there is room for one more engine, reclaiming
// the least-recently-used idle engine when at the bound. Must hold h.mu.
func (h *Hub) ensureCapacityLocked() error {
	live := 0
	for _, s := range h.agents {
		if s.eng != nil || s.booting {
			live++
		}
	}
	if live < h.cfg.MaxConcurrent {
		return nil
	}

	// At capacity: evict the least-recently-used running engine.
	var lruID string
	var lru *agentState
	for id, s := range h.agents {
		if s.eng == nil || s.booting {
			continue
		}
		if lru == nil || s.lastUsed.Before(lru.lastUsed) {
			lruID, lru = id, s
		}
	}
	if lru == nil {
		// Everything is mid-boot; cannot reclaim a slot right now.
		return newError(AtCapacity, "all engine slots are booting")
	}

	eng := lru.eng
	delete(h.agents, lruID)
	// Stop outside the lock would be cleaner, but eviction is rare and Stop is
	// bounded; do it inline with a fresh context so a cancelled caller context
	// does not abort an unrelated engine's teardown.
	go h.stopEngine(lruID, eng)
	h.logger.Info("agenthub evicted idle engine for capacity", "agent_id", lruID)
	return nil
}

// boot renders the config for agentID and boots a fresh engine.
func (h *Hub) boot(ctx context.Context, agentID string) (engineHandle, error) {
	configYAML, err := h.cfg.renderConfig(agentID)
	if err != nil {
		return nil, err
	}
	eng, err := h.mkEng(agentID, configYAML)
	if err != nil {
		return nil, err
	}
	if err := eng.Boot(ctx); err != nil {
		return nil, wrapError(Boot, "boot engine for agent "+agentID, err)
	}
	h.logger.Info("agenthub booted engine", "agent_id", agentID)
	return eng, nil
}

// Drive sends content to the agent via io.input and waits for the agent's final
// io.output (the assistant message), round-tripping a single message. It boots
// the engine lazily if needed. The wait is bounded by DriveTimeout (and ctx).
//
// This is the lifecycle proof for E4-S1: it shows an engine can be driven. The
// real build loop (structured widget specs, multi-turn editing) is E4-S2.
func (h *Hub) Drive(ctx context.Context, agentID, content string) (string, error) {
	eng, err := h.Get(ctx, agentID)
	if err != nil {
		return "", err
	}

	dctx, cancel := context.WithTimeout(ctx, h.cfg.DriveTimeout)
	defer cancel()

	out := make(chan string, 1)
	errc := make(chan string, 1)
	unsub := eng.Subscribe("io.output", func(payload any) {
		o, ok := payload.(events.AgentOutput)
		if !ok {
			return
		}
		switch o.Role {
		case "assistant":
			select {
			case out <- o.Content:
			default:
			}
		case "error":
			select {
			case errc <- o.Content:
			default:
			}
		}
	})
	defer unsub()

	input := events.UserInput{SchemaVersion: events.UserInputVersion, Content: content}
	if err := eng.Emit("io.input", input); err != nil {
		return "", wrapError(Drive, "emit io.input", err)
	}

	// Mark the engine as freshly used so the idle reaper does not race the
	// round-trip out from under us.
	h.touch(agentID)

	select {
	case <-dctx.Done():
		return "", wrapError(Drive, "wait for io.output", dctx.Err())
	case msg := <-errc:
		return "", newError(Drive, "agent reported error: "+msg)
	case msg := <-out:
		h.touch(agentID)
		return msg, nil
	}
}

// touch updates an agent's last-used time so the idle reaper leaves it alone.
func (h *Hub) touch(agentID string) {
	h.mu.Lock()
	if st, ok := h.agents[agentID]; ok && st.eng != nil {
		st.lastUsed = h.now()
	}
	h.mu.Unlock()
}

// Stop tears down the engine for agentID if it is running. It is a no-op for an
// unknown or already-stopped agent.
func (h *Hub) Stop(agentID string) error {
	h.mu.Lock()
	st, ok := h.agents[agentID]
	if !ok || st.eng == nil {
		h.mu.Unlock()
		return nil
	}
	eng := st.eng
	delete(h.agents, agentID)
	h.mu.Unlock()

	return h.stopEngineSync(agentID, eng)
}

// stopEngine stops an engine and logs any error (used for async eviction).
func (h *Hub) stopEngine(agentID string, eng engineHandle) {
	if err := h.stopEngineSync(agentID, eng); err != nil {
		h.logger.Warn("agenthub engine stop error", "agent_id", agentID, "error", err)
	}
}

func (h *Hub) stopEngineSync(agentID string, eng engineHandle) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := eng.Stop(ctx); err != nil {
		return wrapError(Internal, "stop engine for agent "+agentID, err)
	}
	h.logger.Info("agenthub stopped engine", "agent_id", agentID)
	return nil
}

// Len returns the number of currently running engines. Primarily for tests and
// operational observability.
func (h *Hub) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, s := range h.agents {
		if s.eng != nil {
			n++
		}
	}
	return n
}

// reap periodically tears down engines idle longer than IdleTimeout.
func (h *Hub) reap() {
	defer close(h.reaperDone)
	// Tick at a fraction of the timeout so reclamation is reasonably prompt
	// without busy-looping. Floor at one second.
	interval := h.cfg.IdleTimeout / 4
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-h.reaperStop:
			return
		case <-ticker.C:
			h.reapOnce()
		}
	}
}

// reapOnce stops every engine idle longer than IdleTimeout.
func (h *Hub) reapOnce() {
	cutoff := h.now().Add(-h.cfg.IdleTimeout)

	type victim struct {
		id  string
		eng engineHandle
	}
	var victims []victim

	h.mu.Lock()
	for id, s := range h.agents {
		if s.eng != nil && s.lastUsed.Before(cutoff) {
			victims = append(victims, victim{id, s.eng})
			delete(h.agents, id)
		}
	}
	h.mu.Unlock()

	for _, v := range victims {
		h.logger.Info("agenthub reaping idle engine", "agent_id", v.id)
		h.stopEngine(v.id, v.eng)
	}
}

// Close stops the idle reaper and tears down every running engine. After Close,
// Get returns a Closed error. Safe to call once; subsequent calls are no-ops.
func (h *Hub) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true

	type entry struct {
		id  string
		eng engineHandle
	}
	var running []entry
	for id, s := range h.agents {
		if s.eng != nil {
			running = append(running, entry{id, s.eng})
		}
	}
	h.agents = make(map[string]*agentState)
	h.mu.Unlock()

	if h.reaperStop != nil {
		close(h.reaperStop)
		<-h.reaperDone
	}

	var firstErr error
	for _, e := range running {
		if err := h.stopEngineSync(e.id, e.eng); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
