package resolver

import (
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// TestDefaultThemeAttached asserts the document-scope default theme (E2-S2) is
// present, verbatim, on the resolved document output where downstream consumers
// read it — the default layer only, with no per-block merge.
func TestDefaultThemeAttached(t *testing.T) {
	res := newRepoResolver(t)
	tree, err := res.Resolve("testdata/valid/default-theme.json")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if tree.DefaultTheme == nil {
		t.Fatal("DefaultTheme is nil; want the declared document default theme attached")
	}
	want := map[string]string{"emphasis": "high", "spacing": "cosy", "tone": "accent"}
	for k, v := range want {
		got, ok := tree.DefaultTheme[k].(string)
		if !ok || got != v {
			t.Errorf("DefaultTheme[%q] = %v, want %q", k, tree.DefaultTheme[k], v)
		}
	}
	if len(tree.DefaultTheme) != len(want) {
		t.Errorf("DefaultTheme = %v, want exactly %v", tree.DefaultTheme, want)
	}
}

// TestNoDefaultThemeOmitted asserts a document that declares no default theme
// resolves with a nil DefaultTheme (the field is omitted from the contract), so
// the attachment is purely additive for documents that opt out.
func TestNoDefaultThemeOmitted(t *testing.T) {
	res := newRepoResolver(t)
	tree, err := res.Resolve("testdata/valid/minimal-dashboard.json")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if tree.DefaultTheme != nil {
		t.Errorf("DefaultTheme = %v, want nil for a document with no theme", tree.DefaultTheme)
	}
}

// TestDefaultThemeInvalidToken asserts a default theme using an out-of-vocabulary
// token — or an out-of-vocabulary value for a known token — fails fast with a
// coded, source-named error. The theme schema's closed token vocabulary is
// enforced by the structural (Pass 1) validation, so the existing
// RESOLVE_DOCUMENT_INVALID path covers it (no redundant theme-specific code).
func TestDefaultThemeInvalidToken(t *testing.T) {
	tests := []struct {
		name string
		doc  string
	}{
		{"unknown token key", "testdata/invalid/theme-unknown-token.json"},
		{"out-of-vocabulary token value", "testdata/invalid/theme-bad-token-value.json"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := newRepoResolver(t)
			_, err := res.Resolve(tc.doc)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.HasCode(err, errors.RESOLVE_DOCUMENT_INVALID) {
				t.Fatalf("error = %v, want code %s", err, errors.RESOLVE_DOCUMENT_INVALID)
			}
			var ce *errors.CodedError
			if !asCoded(err, &ce) {
				t.Fatalf("error is not a CodedError: %v", err)
			}
			if src, _ := ce.Details["source"].(string); src != tc.doc {
				t.Errorf("error source = %q, want %q", src, tc.doc)
			}
		})
	}
}
