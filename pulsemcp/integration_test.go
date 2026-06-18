package pulsemcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/frankbardon/pulse/types"
)

// pulseBinary locates the pulse binary for the integration test. It honours the
// LATTICE_PULSE_BIN env var first (so CI / local dev can point at a built
// binary), then falls back to PATH. Returns "" when no binary is available, in
// which case the integration test skips — keeping CI green when Pulse is absent.
func pulseBinary(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("LATTICE_PULSE_BIN"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		t.Skipf("LATTICE_PULSE_BIN=%q not found; skipping integration test", p)
	}
	if p, err := exec.LookPath("pulse"); err == nil {
		return p
	}
	t.Skip("pulse binary not found (set LATTICE_PULSE_BIN or install on PATH); skipping integration test")
	return ""
}

// buildCohort imports a tiny CSV into a .pulse cohort under dir, returning the
// cohort filename. Skips the test if the import step fails (e.g. binary mismatch).
func buildCohort(t *testing.T, bin, dir string) string {
	t.Helper()
	csvPath := filepath.Join(dir, "sales.csv")
	const csv = "region,amount\nwest,10\neast,20\nwest,30\n"
	if err := os.WriteFile(csvPath, []byte(csv), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	cohort := "sales.pulse"
	cmd := exec.Command(bin, "import", "csv", "-i", csvPath, "-o", filepath.Join(dir, cohort))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("pulse import failed (binary likely incompatible): %v\n%s", err, out)
	}
	return cohort
}

func TestIntegration_ProcessRoundTrip(t *testing.T) {
	bin := pulseBinary(t)
	dataDir := t.TempDir()
	cohort := buildCohort(t, bin, dataDir)

	mgr, err := NewManager(Config{
		BinaryPath:   bin,
		DataDir:      dataDir,
		StartTimeout: 30 * time.Second,
		CallTimeout:  15 * time.Second,
	}, Options{})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ctx := t.Context()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := mgr.Stop(sctx); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})

	req := &types.Request{
		Cohort:       &types.Cohort{Filename: cohort},
		Aggregations: []*types.Aggregation{{Type: types.AGG_COUNT}},
	}
	resp, err := mgr.Process(ctx, req)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if resp.Metadata == nil {
		t.Fatalf("response metadata nil; got %+v", resp)
	}
	if resp.Metadata.TotalRows != 3 {
		t.Errorf("total_rows = %d, want 3", resp.Metadata.TotalRows)
	}
	if len(resp.Data) == 0 {
		t.Errorf("expected at least one data row, got none")
	}
}

func TestIntegration_RestartOnCrash(t *testing.T) {
	bin := pulseBinary(t)
	dataDir := t.TempDir()
	cohort := buildCohort(t, bin, dataDir)

	mgr, err := NewManager(Config{
		BinaryPath:     bin,
		DataDir:        dataDir,
		StartTimeout:   30 * time.Second,
		CallTimeout:    15 * time.Second,
		RestartBackoff: 100 * time.Millisecond,
	}, Options{})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	if err := mgr.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = mgr.Stop(sctx)
	})

	firstPID := mgr.PID()
	if firstPID <= 0 {
		t.Fatalf("expected a valid child PID, got %d", firstPID)
	}

	// Kill the child out from under the manager; the supervisor's health check
	// must notice and respawn it.
	proc, err := os.FindProcess(firstPID)
	if err != nil {
		t.Fatalf("FindProcess: %v", err)
	}
	if err := proc.Kill(); err != nil {
		t.Fatalf("kill child: %v", err)
	}

	// Wait for a respawn: a new PID + a working call.
	deadline := time.Now().Add(15 * time.Second)
	var newPID int
	for time.Now().Before(deadline) {
		newPID = mgr.PID()
		if newPID > 0 && newPID != firstPID {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if newPID == firstPID || newPID <= 0 {
		t.Fatalf("child was not restarted: firstPID=%d newPID=%d", firstPID, newPID)
	}

	// The restarted child must serve calls.
	req := &types.Request{
		Cohort:       &types.Cohort{Filename: cohort},
		Aggregations: []*types.Aggregation{{Type: types.AGG_COUNT}},
	}
	resp, err := mgr.Process(t.Context(), req)
	if err != nil {
		t.Fatalf("Process after restart: %v", err)
	}
	if resp.Metadata == nil || resp.Metadata.TotalRows != 3 {
		t.Fatalf("unexpected response after restart: %+v", resp)
	}
}
