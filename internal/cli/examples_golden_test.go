package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// update regenerates the example golden files instead of comparing against them.
// Run: go test ./internal/cli -run TestExamplesGolden -update
//
// This mirrors the resolver package's golden harness (internal/resolver): drop a
// new *.json dashboard into examples/, run with -update to mint its golden, and
// the directory walk below picks it up automatically. The examples double as the
// spec's living documentation (E5-S2) and the renderer's regression suite, so
// every example is kept paired with a resolved-tree golden here.
var update = flag.Bool("update", false, "update golden files")

const (
	// examplesDir holds the shipped example dashboards, relative to this package.
	examplesDir = "../../examples"
	// repoSchemasDir is the real on-disk schema catalog, relative to this package.
	// Resolving against it exercises the exact schemas the binary ships with.
	repoSchemasDir = "../../schemas"
	// goldenDir holds the expected resolved-tree golden per example.
	goldenDir = "testdata/golden"

	// secretEnvName/secretEnvValue back the $secret references some examples carry
	// (e.g. the kitchen-sink and binding dashboards). The test sets the variable so
	// those examples resolve deterministically; the resolver records the secret by
	// name only and the VALUE must never appear in the resolved-tree golden.
	secretEnvName  = "METRICS_API_TOKEN"
	secretEnvValue = "super-secret-token-value"
)

// TestExamplesGolden resolves every example under examples/ through the real
// resolve path (the same code the CLI runs) and diffs the resolved tree against a
// sibling golden under testdata/golden/. With -update the goldens are (re)written.
// It is data-driven over the examples directory, so new examples (and E5-S2 docs
// examples) are auto-covered. It also asserts that no example leaks a resolved
// secret value into its golden.
func TestExamplesGolden(t *testing.T) {
	// Set the secret so $secret-bearing examples resolve. t.Setenv restores it.
	t.Setenv(secretEnvName, secretEnvValue)

	docs, err := filepath.Glob(filepath.Join(examplesDir, "*.json"))
	if err != nil {
		t.Fatalf("glob examples: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("no example dashboards found under examples/")
	}

	if *update {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
	}

	for _, doc := range docs {
		doc := doc
		t.Run(filepath.Base(doc), func(t *testing.T) {
			tree, err := runResolve(repoSchemasDir, doc)
			if err != nil {
				t.Fatalf("resolve %s: %v", doc, err)
			}

			got, err := json.MarshalIndent(tree, "", "  ")
			if err != nil {
				t.Fatalf("marshal tree: %v", err)
			}
			got = append(got, '\n')

			// Redaction guard: a resolved secret value must never reach the golden.
			if bytes.Contains(got, []byte(secretEnvValue)) {
				t.Fatalf("resolved tree for %s leaked the secret value %q", filepath.Base(doc), secretEnvValue)
			}

			golden := goldenPath(doc)
			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}

			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden %s (run -update to create): %v", golden, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("resolved tree mismatch for %s\n--- got ---\n%s\n--- want ---\n%s",
					filepath.Base(doc), got, want)
			}
		})
	}
}

// goldenPath maps an example document path to its golden file under goldenDir,
// e.g. examples/minimal-dashboard.json -> testdata/golden/minimal-dashboard.golden.json.
func goldenPath(doc string) string {
	base := filepath.Base(doc)
	name := strings.TrimSuffix(base, ".json")
	return filepath.Join(goldenDir, name+".golden.json")
}
