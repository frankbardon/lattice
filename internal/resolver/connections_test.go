package resolver

import (
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// TestResolveConnectionsValid resolves a document declaring one connection of
// each shipped type (static + http) and asserts both appear in the resolved
// tree with their connection-type identity resolved. Connections are declared
// and validated only — never dialed.
func TestResolveConnectionsValid(t *testing.T) {
	res := newRepoResolver(t)
	tree, err := res.Resolve("testdata/connections/valid-connections.json")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if got := len(tree.Connections); got != 2 {
		t.Fatalf("len(Connections) = %d, want 2", got)
	}

	tests := []struct {
		idx         int
		wantID      string
		wantName    string
		wantVersion string
	}{
		{0, "inline-fruits", "static", "1.0.0"},
		{1, "metrics-api", "http", "1.0.0"},
	}
	for _, tc := range tests {
		c := tree.Connections[tc.idx]
		if c.ID != tc.wantID {
			t.Errorf("conn[%d].ID = %q, want %q", tc.idx, c.ID, tc.wantID)
		}
		if c.Type.Name != tc.wantName {
			t.Errorf("conn[%d].Type.Name = %q, want %q", tc.idx, c.Type.Name, tc.wantName)
		}
		if c.Type.Version != tc.wantVersion {
			t.Errorf("conn[%d].Type.Version = %q, want %q", tc.idx, c.Type.Version, tc.wantVersion)
		}
		if c.Config == nil {
			t.Errorf("conn[%d].Config is nil, want passthrough config", tc.idx)
		}
	}

	// secretRefs pass through verbatim on the http connection.
	if got := tree.Connections[1].SecretRefs["token"]; got != "vault://lattice/metrics-api#token" {
		t.Errorf("http connection secretRefs[token] = %q, want passthrough", got)
	}
}

// TestResolveConnectionErrors drives intentionally-broken connection documents
// and asserts the first CodedError carries the expected code (fail-fast). For
// connection-scoped errors it also asserts the reported path.
func TestResolveConnectionErrors(t *testing.T) {
	tests := []struct {
		name     string
		doc      string
		wantCode errors.Code
		wantPath string // expected Details["path"]; "" to skip
	}{
		{
			name:     "connection config violates type schema",
			doc:      "testdata/connections/invalid-config.json",
			wantCode: errors.CONNECTION_CONFIG_INVALID,
			wantPath: "connections[0]",
		},
		{
			name:     "duplicate connection id",
			doc:      "testdata/connections/duplicate-id.json",
			wantCode: errors.CONNECTION_DUPLICATE_ID,
			wantPath: "connections[1]",
		},
		{
			name:     "unknown connection type",
			doc:      "testdata/connections/unknown-type.json",
			wantCode: errors.CONNECTION_TYPE_UNRESOLVED,
			wantPath: "connections[0]",
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
			if tc.wantPath != "" {
				var ce *errors.CodedError
				if !asCoded(err, &ce) {
					t.Fatalf("error is not a CodedError: %v", err)
				}
				if got, _ := ce.Details["path"].(string); got != tc.wantPath {
					t.Errorf("error path = %q, want %q", got, tc.wantPath)
				}
			}
		})
	}
}
