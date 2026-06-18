package render

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/frankbardon/lattice/pulsemcp"
)

// pulseBinary locates the pulse binary for the integration test, mirroring
// pulsemcp/integration_test.go: LATTICE_PULSE_BIN first, then PATH, else skip so
// CI stays green when Pulse is absent.
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

// buildCohort imports a tiny CSV into a .pulse cohort, returning its filename.
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

// TestIntegration_PulsePrismRoundTrip runs the full Pulse→Prism→SVG pipeline
// against a real Pulse child: it aggregates summed amount by region, feeds the
// rows into a bar spec, and confirms a visible <svg> chart comes out.
func TestIntegration_PulsePrismRoundTrip(t *testing.T) {
	bin := pulseBinary(t)
	dataDir := t.TempDir()
	cohort := buildCohort(t, bin, dataDir)

	mgr, err := pulsemcp.NewManager(pulsemcp.Config{
		BinaryPath:   bin,
		DataDir:      dataDir,
		StartTimeout: 30 * time.Second,
		CallTimeout:  15 * time.Second,
	}, pulsemcp.Options{})
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
		_ = mgr.Stop(sctx)
	})

	// Sum amount grouped by region → two rows the bar spec plots.
	template := `{
	  "pulse_request": {
	    "cohort": {"filename": "` + cohort + `"},
	    "groups": [{"type": "GROUP_CATEGORY", "field": "region"}],
	    "aggregations": [{"type": "AGG_SUM", "field": "amount", "label": "amount"}]
	  },
	  "prism_spec": ` + barSpec + `
	}`

	r := NewPulsePrism(mgr)
	got, err := r.Render(template, ResolvedVars{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "<svg") {
		t.Fatalf("expected an SVG fragment, got %q", got)
	}
}
