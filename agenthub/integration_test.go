package agenthub

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// liveCreds gates the live round-trip test. It requires an Anthropic API key
// (the agent makes a real LLM call) and a pulse binary (the agent's MCP client
// execs it over stdio at boot). When either is absent the test skips, keeping
// CI green without creds or external tools — mirroring pulsemcp's skip pattern.
func liveCreds(t *testing.T) (pulseBin, dataDir string) {
	t.Helper()

	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY unset; skipping live agent round-trip")
	}

	if p := os.Getenv("LATTICE_PULSE_BIN"); p != "" {
		if _, err := os.Stat(p); err != nil {
			t.Skipf("LATTICE_PULSE_BIN=%q not found; skipping", p)
		}
		pulseBin = p
	} else if p, err := exec.LookPath("pulse"); err == nil {
		pulseBin = p
	} else {
		t.Skip("pulse binary not found (set LATTICE_PULSE_BIN or install on PATH); skipping")
	}

	dataDir = os.Getenv("PULSE_DATA_DIR")
	if dataDir == "" {
		dataDir = t.TempDir()
	}
	return pulseBin, dataDir
}

// TestIntegration_DriveRoundTrip boots a REAL Nexus brick agent (Anthropic
// provider + pulse MCP client over stdio) and round-trips a trivial message
// through io.input → io.output. It proves the hub drives a live engine. It
// skips cleanly when creds/binaries are absent (see liveCreds).
func TestIntegration_DriveRoundTrip(t *testing.T) {
	pulseBin, dataDir := liveCreds(t)

	cfg := DefaultConfig()
	cfg.PulseBinaryPath = pulseBin
	cfg.PulseDataDir = dataDir
	cfg.SessionsRoot = t.TempDir()
	cfg.IdleTimeout = 0 // no reaper during the test
	cfg.DriveTimeout = 90 * time.Second

	h, err := NewHub(cfg, Options{})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	reply, err := h.Drive(ctx, "brick-it", "Reply with exactly the word: pong")
	if err != nil {
		t.Fatalf("Drive: %v", err)
	}
	if reply == "" {
		t.Fatal("expected a non-empty assistant reply")
	}
	t.Logf("agent reply: %q", reply)
}

// TestIntegration_BootConfigValidates boots a real engine just far enough to
// confirm the brick-agent YAML the hub generates is accepted by Nexus
// (NewFromBytes + RegisterAll + Boot) WITHOUT requiring an LLM round-trip. It
// still needs the pulse binary because the MCP client execs it at boot, so it
// skips when that is absent — but it does NOT require an API key.
func TestIntegration_BootConfigValidates(t *testing.T) {
	var pulseBin, dataDir string
	if p := os.Getenv("LATTICE_PULSE_BIN"); p != "" {
		if _, err := os.Stat(p); err != nil {
			t.Skipf("LATTICE_PULSE_BIN=%q not found; skipping", p)
		}
		pulseBin = p
	} else if p, err := exec.LookPath("pulse"); err == nil {
		pulseBin = p
	} else {
		t.Skip("pulse binary not found; skipping boot-config-validation")
	}
	dataDir = os.Getenv("PULSE_DATA_DIR")
	if dataDir == "" {
		dataDir = t.TempDir()
	}

	cfg := DefaultConfig()
	cfg.PulseBinaryPath = pulseBin
	cfg.PulseDataDir = dataDir
	cfg.SessionsRoot = t.TempDir()
	cfg.IdleTimeout = 0
	// Point the provider at an env var that need not be set: boot wires the
	// provider but does not call the LLM, so no key is required to validate.
	cfg.AnthropicKeyEnv = "LATTICE_AGENTHUB_UNUSED_KEY"

	h, err := NewHub(cfg, Options{})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if _, err := h.Get(ctx, "brick-boot"); err != nil {
		t.Fatalf("Get (boot): %v", err)
	}
	if h.Len() != 1 {
		t.Fatalf("Len = %d, want 1", h.Len())
	}
}
