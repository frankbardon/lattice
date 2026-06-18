package agenthub

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/frankbardon/nexus/pkg/events"
)

// stubEngine is an in-memory engineHandle for lifecycle tests — no LLM, no
// subprocess. It records boot/stop and serves a canned io.output in response to
// io.input so Drive can be exercised without creds.
type stubEngine struct {
	mu        sync.Mutex
	booted    bool
	stopped   bool
	reply     string // assistant content echoed back on io.input
	subs      map[string][]func(any)
	bootErr   error
	stopCalls *int32
}

func newStub(reply string) *stubEngine {
	return &stubEngine{reply: reply, subs: map[string][]func(any){}}
}

func (s *stubEngine) Boot(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bootErr != nil {
		return s.bootErr
	}
	s.booted = true
	return nil
}

func (s *stubEngine) Stop(context.Context) error {
	s.mu.Lock()
	s.stopped = true
	c := s.stopCalls
	s.mu.Unlock()
	if c != nil {
		atomic.AddInt32(c, 1)
	}
	return nil
}

func (s *stubEngine) Subscribe(t string, h func(any)) func() {
	s.mu.Lock()
	s.subs[t] = append(s.subs[t], h)
	s.mu.Unlock()
	return func() {}
}

func (s *stubEngine) Emit(t string, payload any) error {
	if t != "io.input" {
		return nil
	}
	// Echo a canned assistant message asynchronously, mimicking the agent.
	s.mu.Lock()
	handlers := append([]func(any){}, s.subs["io.output"]...)
	reply := s.reply
	s.mu.Unlock()
	go func() {
		for _, h := range handlers {
			h(events.AgentOutput{SchemaVersion: events.AgentOutputVersion, Role: "assistant", Content: reply})
		}
	}()
	return nil
}

// testConfig returns a minimal valid Config (binary paths satisfy validation;
// the stub engine never execs them).
func testConfig() Config {
	c := DefaultConfig()
	c.PulseBinaryPath = "/usr/bin/true"
	c.PulseDataDir = "/tmp/pulse-data"
	c.IdleTimeout = 0 // reaper off unless a test opts in
	return c
}

func TestNewHub_ValidatesConfig(t *testing.T) {
	if _, err := NewHub(DefaultConfig(), Options{}); err == nil {
		t.Fatal("expected error for missing pulse_binary_path")
	} else if CodeOf(err) != InvalidConfig {
		t.Fatalf("code = %s, want %s", CodeOf(err), InvalidConfig)
	}

	c := DefaultConfig()
	c.PulseBinaryPath = "/usr/bin/true"
	c.PulseDataDir = "/tmp/x"
	c.MaxConcurrent = -1
	if _, err := NewHub(c, Options{}); CodeOf(err) != InvalidConfig {
		t.Fatalf("negative max_concurrent: code = %s, want %s", CodeOf(err), InvalidConfig)
	}
}

func TestGet_LazyBootAndReuse(t *testing.T) {
	var boots int32
	stub := newStub("ok")
	h, err := NewHub(testConfig(), Options{
		newEngine: func(string, []byte) (engineHandle, error) {
			atomic.AddInt32(&boots, 1)
			return stub, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	ctx := context.Background()
	e1, err := h.Get(ctx, "brick-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !stub.booted {
		t.Fatal("engine not booted")
	}
	// Second Get for the same agent reuses the engine (no second boot).
	e2, err := h.Get(ctx, "brick-1")
	if err != nil {
		t.Fatalf("Get reuse: %v", err)
	}
	if e1 != e2 {
		t.Fatal("expected the same engine instance on reuse")
	}
	if got := atomic.LoadInt32(&boots); got != 1 {
		t.Fatalf("boots = %d, want 1 (lazy + reuse)", got)
	}
	if h.Len() != 1 {
		t.Fatalf("Len = %d, want 1", h.Len())
	}
}

func TestGet_RendersConfigPerAgent(t *testing.T) {
	var captured string
	c := testConfig()
	c.PrismBinaryPath = "/usr/bin/prism"
	h, err := NewHub(c, Options{
		newEngine: func(agentID string, cfg []byte) (engineHandle, error) {
			captured = string(cfg)
			return newStub("ok"), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	if _, err := h.Get(context.Background(), "brick-xyz"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"agent_id: brick-xyz",
		"name: pulse",
		"/usr/bin/true",   // pulse binary
		"/tmp/pulse-data", // pulse data dir
		"name: prism",     // prism wired because PrismBinaryPath set
		"api_key_env: ANTHROPIC_API_KEY",
		"nexus.mcp.client",
	} {
		if !strings.Contains(captured, want) {
			t.Errorf("rendered config missing %q\n---\n%s", want, captured)
		}
	}
}

func TestGet_ConcurrencyBoundEvictsLRU(t *testing.T) {
	now := time.Unix(1000, 0)
	c := testConfig()
	c.MaxConcurrent = 2
	var stopped int32
	mkStub := func(string, []byte) (engineHandle, error) {
		s := newStub("ok")
		s.stopCalls = &stopped
		return s, nil
	}
	h, err := NewHub(c, Options{
		newEngine: mkStub,
		now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	ctx := context.Background()

	if _, err := h.Get(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if _, err := h.Get(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	if h.Len() != 2 {
		t.Fatalf("Len = %d, want 2", h.Len())
	}
	// Third agent at bound 2 → LRU ("a") evicted.
	now = now.Add(time.Second)
	if _, err := h.Get(ctx, "cc"); err != nil {
		t.Fatal(err)
	}

	// Eviction stops the LRU engine asynchronously; wait for it.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&stopped) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if atomic.LoadInt32(&stopped) != 1 {
		t.Fatalf("evicted engines stopped = %d, want 1", atomic.LoadInt32(&stopped))
	}
	if h.Len() != 2 {
		t.Fatalf("Len after eviction = %d, want 2", h.Len())
	}
}

func TestStop_TearsDownEngine(t *testing.T) {
	var stopped int32
	h, err := NewHub(testConfig(), Options{
		newEngine: func(string, []byte) (engineHandle, error) {
			s := newStub("ok")
			s.stopCalls = &stopped
			return s, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	ctx := context.Background()
	if _, err := h.Get(ctx, "x"); err != nil {
		t.Fatal(err)
	}
	if err := h.Stop("x"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if atomic.LoadInt32(&stopped) != 1 {
		t.Fatalf("stop calls = %d, want 1", atomic.LoadInt32(&stopped))
	}
	if h.Len() != 0 {
		t.Fatalf("Len = %d, want 0", h.Len())
	}
	// Stop of an unknown / already-stopped agent is a no-op.
	if err := h.Stop("x"); err != nil {
		t.Fatalf("idempotent Stop: %v", err)
	}
}

func TestIdleReaper_TearsDownIdleEngine(t *testing.T) {
	var mu sync.Mutex
	clock := time.Unix(0, 0)
	advance := func(d time.Duration) { mu.Lock(); clock = clock.Add(d); mu.Unlock() }
	read := func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }

	c := testConfig()
	c.IdleTimeout = 100 * time.Millisecond
	var stopped int32
	h, err := NewHub(c, Options{
		newEngine: func(string, []byte) (engineHandle, error) {
			s := newStub("ok")
			s.stopCalls = &stopped
			return s, nil
		},
		now: read,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	if _, err := h.Get(context.Background(), "idle"); err != nil {
		t.Fatal(err)
	}
	// Push the clock well past the idle timeout; the reaper (ticking on real
	// time) must observe the stale lastUsed and reap.
	advance(time.Hour)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&stopped) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt32(&stopped) != 1 {
		t.Fatalf("idle reaper stopped %d engines, want 1", atomic.LoadInt32(&stopped))
	}
	if h.Len() != 0 {
		t.Fatalf("Len after reap = %d, want 0", h.Len())
	}
}

func TestDrive_RoundTrip(t *testing.T) {
	h, err := NewHub(testConfig(), Options{
		newEngine: func(string, []byte) (engineHandle, error) { return newStub("echo: hi"), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	got, err := h.Drive(context.Background(), "brick-1", "hi")
	if err != nil {
		t.Fatalf("Drive: %v", err)
	}
	if got != "echo: hi" {
		t.Fatalf("Drive content = %q, want %q", got, "echo: hi")
	}
}

func TestClose_StopsAllAndRejectsGet(t *testing.T) {
	var stopped int32
	h, err := NewHub(testConfig(), Options{
		newEngine: func(string, []byte) (engineHandle, error) {
			s := newStub("ok")
			s.stopCalls = &stopped
			return s, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := h.Get(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Get(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if atomic.LoadInt32(&stopped) != 2 {
		t.Fatalf("Close stopped %d engines, want 2", atomic.LoadInt32(&stopped))
	}
	if _, err := h.Get(ctx, "a"); CodeOf(err) != Closed {
		t.Fatalf("Get after Close: code = %s, want %s", CodeOf(err), Closed)
	}
	// Idempotent.
	if err := h.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
