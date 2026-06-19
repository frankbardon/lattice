package resolver

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// secretEnvName/secretEnvValue are the env-resolved secret used by the
// valid-binding fixture. The value must NEVER appear in the resolved-tree dump.
const (
	secretEnvName  = "METRICS_API_TOKEN"
	secretEnvValue = "super-secret-token-value"
)

// TestResolveBindingValid resolves the happy-path fixture (a table bound to an
// http connection by id, a variable-filled query, and a $secret in the
// connection config) and asserts: the connectionId resolves, the query params
// are filled from variables with their JSON types preserved, the secret is
// resolved (recorded by name), and the secret VALUE is redacted from the dump.
func TestResolveBindingValid(t *testing.T) {
	t.Setenv(secretEnvName, secretEnvValue)

	res := newRepoResolver(t)
	tree, err := res.Resolve("testdata/binding/valid-binding.json")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// --- connection-id resolution: the item binds to a declared connection. ---
	// Under the E3-S2 grammar the table is block-wrapped inside a body region:
	// root -> body region -> block -> table.
	if len(tree.Root.Children) != 1 {
		t.Fatalf("len(root.children) = %d, want 1", len(tree.Root.Children))
	}
	table := tree.Root.Children[0].Children[0].Children[0]
	if table.Binding == nil {
		t.Fatalf("table.Binding is nil, want a resolved binding")
	}
	if table.Binding.ConnectionID != "metrics-api" {
		t.Errorf("binding.ConnectionID = %q, want %q", table.Binding.ConnectionID, "metrics-api")
	}

	// --- var-filled query: typed $var preserves JSON type; ${} renders text. ---
	q := table.Binding.Query
	if q == nil {
		t.Fatalf("binding.Query is nil, want filled query")
	}
	if got := q["region"]; got != "us-east" {
		t.Errorf("query[region] = %v (%T), want \"us-east\" (typed $var)", got, got)
	}
	// "hours" comes from an integer variable via { "$var": "window" } — the JSON
	// type is preserved (decoded as float64), not stringified.
	if got, ok := q["hours"].(float64); !ok || got != 24 {
		t.Errorf("query[hours] = %v (%T), want 24 (float64, typed $var)", q["hours"], q["hours"])
	}
	if got := q["label"]; got != "last 24h" {
		t.Errorf("query[label] = %v, want \"last 24h\" (${} template)", got)
	}
	// The table's title template was interpolated too.
	if got := table.Config["title"]; got != "Metrics for us-east" {
		t.Errorf("table title = %v, want \"Metrics for us-east\"", got)
	}

	// --- secret resolution: the connection records the secret by NAME only. ---
	if len(tree.Connections) != 1 {
		t.Fatalf("len(Connections) = %d, want 1", len(tree.Connections))
	}
	conn := tree.Connections[0]
	if len(conn.Secrets) != 1 || conn.Secrets[0] != secretEnvName {
		t.Errorf("conn.Secrets = %v, want [%q]", conn.Secrets, secretEnvName)
	}

	// --- redaction: the resolved value must NOT appear anywhere in the dump,
	// and the config must retain the { "$secret": "name" } reference object. ---
	dump, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		t.Fatalf("marshal tree: %v", err)
	}
	if bytes.Contains(dump, []byte(secretEnvValue)) {
		t.Errorf("resolved-tree dump leaked the secret value %q:\n%s", secretEnvValue, dump)
	}
	if !bytes.Contains(dump, []byte(`"$secret"`)) {
		t.Errorf("resolved-tree dump dropped the $secret reference object; want it retained:\n%s", dump)
	}
	// Defensive: the in-memory config retains the reference object, not the value.
	headers, _ := conn.Config["headers"].(map[string]any)
	auth, _ := headers["Authorization"].(map[string]any)
	if name, _ := auth["$secret"].(string); name != secretEnvName {
		t.Errorf("conn config Authorization = %v, want a {$secret: %q} reference", headers["Authorization"], secretEnvName)
	}
}

// TestResolveBindingErrors drives intentionally-broken binding/secret documents
// and asserts the first CodedError carries the expected code (fail-fast). For
// instance/connection-scoped errors it also asserts the reported path.
func TestResolveBindingErrors(t *testing.T) {
	// The missing-secret fixture references DEFINITELY_NOT_SET_SECRET, which is
	// intentionally never set in the test process, so os.LookupEnv reports it
	// absent and resolution fails fast with SECRET_MISSING.
	tests := []struct {
		name     string
		doc      string
		wantCode errors.Code
		wantPath string // expected Details["path"]; "" to skip
		wantKV   [2]string
	}{
		{
			name:     "missing secret in connection config",
			doc:      "testdata/binding/missing-secret.json",
			wantCode: errors.SECRET_MISSING,
			wantPath: "connections[0]",
			wantKV:   [2]string{"name", "DEFINITELY_NOT_SET_SECRET"},
		},
		{
			name:     "binding to an unknown connection id",
			doc:      "testdata/binding/unknown-connection.json",
			wantCode: errors.BINDING_CONNECTION_NOT_FOUND,
			wantPath: "root.children[0].children[0].children[0]",
			wantKV:   [2]string{"connectionId", "does-not-exist"},
		},
		{
			name:     "query declared without a connectionId",
			doc:      "testdata/binding/query-without-connection.json",
			wantCode: errors.BINDING_INVALID,
			wantPath: "root.children[0].children[0].children[0]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := newRepoResolver(t)
			_, err := res.Resolve(tc.doc)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.HasCode(err, tc.wantCode) {
				t.Fatalf("error = %v, want code %s", err, tc.wantCode)
			}
			var ce *errors.CodedError
			if !asCoded(err, &ce) {
				t.Fatalf("error is not a CodedError: %v", err)
			}
			if tc.wantPath != "" {
				if got, _ := ce.Details["path"].(string); got != tc.wantPath {
					t.Errorf("error path = %q, want %q", got, tc.wantPath)
				}
			}
			if tc.wantKV[0] != "" {
				if got, _ := ce.Details[tc.wantKV[0]].(string); got != tc.wantKV[1] {
					t.Errorf("error %s = %q, want %q", tc.wantKV[0], got, tc.wantKV[1])
				}
			}
		})
	}
}

// TestResolveSecretsUnit exercises resolveSecrets directly (table-driven) over
// the secret-substitution helper, independent of schema validation.
func TestResolveSecretsUnit(t *testing.T) {
	t.Setenv("S_ONE", "one")
	t.Setenv("S_TWO", "two")

	tests := []struct {
		name        string
		cfg         map[string]any
		wantValue   string // a value expected to appear after substitution; "" to skip
		wantSecrets []string
		wantCode    errors.Code // "" = no error
	}{
		{
			name: "single secret in a string slot",
			cfg: map[string]any{
				"headers": map[string]any{"Auth": map[string]any{"$secret": "S_ONE"}},
			},
			wantValue:   "one",
			wantSecrets: []string{"S_ONE"},
		},
		{
			name: "two secrets resolve, names sorted and de-duped",
			cfg: map[string]any{
				"a": map[string]any{"$secret": "S_TWO"},
				"b": map[string]any{"$secret": "S_ONE"},
				"c": map[string]any{"$secret": "S_ONE"},
			},
			wantSecrets: []string{"S_ONE", "S_TWO"},
		},
		{
			name:        "no secrets passes through cleanly",
			cfg:         map[string]any{"url": "https://x", "n": float64(3)},
			wantSecrets: nil,
		},
		{
			name:     "missing secret fails fast",
			cfg:      map[string]any{"x": map[string]any{"$secret": "NOPE_NOT_SET"}},
			wantCode: errors.SECRET_MISSING,
		},
		{
			name:     "non-string secret name is invalid",
			cfg:      map[string]any{"x": map[string]any{"$secret": float64(1)}},
			wantCode: errors.SECRET_INVALID,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, secrets, err := resolveSecrets(tc.cfg, "connections[0]")
			if tc.wantCode != "" {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !errors.HasCode(err, tc.wantCode) {
					t.Fatalf("error = %v, want code %s", err, tc.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveSecrets: %v", err)
			}
			if !equalStrings(secrets, tc.wantSecrets) {
				t.Errorf("secrets = %v, want %v", secrets, tc.wantSecrets)
			}
			if tc.wantValue != "" {
				dump, _ := json.Marshal(got)
				if !strings.Contains(string(dump), tc.wantValue) {
					t.Errorf("substituted config %s does not contain %q", dump, tc.wantValue)
				}
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
