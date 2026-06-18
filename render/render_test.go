package render

import (
	"log/slog"
	"strings"
	"testing"
)

func testOpts() Options { return Options{Logger: slog.New(slog.DiscardHandler)} }

// TestMarkdownRender confirms the markdown kind turns markdown into an HTML
// fragment (headings, emphasis, lists, GFM tables).
func TestMarkdownRender(t *testing.T) {
	m := NewMarkdown()

	tests := []struct {
		name     string
		in       string
		contains []string
	}{
		{"heading", "# Title", []string{"<h1", "Title", "</h1>"}},
		{"emphasis", "this is **bold**", []string{"<strong>bold</strong>"}},
		{"list", "- a\n- b", []string{"<ul>", "<li>a</li>", "<li>b</li>"}},
		{"gfm table", "| a | b |\n|---|---|\n| 1 | 2 |", []string{"<table>", "<th>a</th>", "<td>1</td>"}},
		{"link", "[home](https://example.com)", []string{`href="https://example.com"`, ">home</a>"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := m.Render(tc.in, ResolvedVars{})
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Fatalf("fragment %q missing %q", got, want)
				}
			}
		})
	}
}

// TestMarkdownSanitizes confirms the renderer is the safety boundary: script
// tags and javascript: URLs in the markdown source are stripped from output.
func TestMarkdownSanitizes(t *testing.T) {
	m := NewMarkdown()

	got, err := m.Render("Hello <script>alert('xss')</script> world", ResolvedVars{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(got, "<script") || strings.Contains(got, "alert(") {
		t.Fatalf("script not sanitized: %q", got)
	}

	got, err = m.Render("[click](javascript:alert(1))", ResolvedVars{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(got, "javascript:") {
		t.Fatalf("javascript: URL not sanitized: %q", got)
	}
}

// TestRegistryDispatch confirms a registered kind routes to its renderer.
func TestRegistryDispatch(t *testing.T) {
	reg := NewRegistry(testOpts())
	if err := reg.Register(KindMarkdown, NewMarkdown()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, err := reg.Render(KindMarkdown, "# hi", ResolvedVars{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "<h1") {
		t.Fatalf("dispatch did not produce markdown HTML: %q", got)
	}
}

// TestRegistryUnknownKind confirms an unregistered kind errors cleanly with the
// typed UnknownKind code.
func TestRegistryUnknownKind(t *testing.T) {
	reg := NewRegistry(testOpts())
	_, err := reg.Render("pulse_prism", "spec", ResolvedVars{})
	if err == nil {
		t.Fatal("expected an error for an unregistered kind")
	}
	if CodeOf(err) != UnknownKind {
		t.Fatalf("code = %s, want %s", CodeOf(err), UnknownKind)
	}
}

// TestRegisterValidation confirms empty kind / nil renderer are rejected.
func TestRegisterValidation(t *testing.T) {
	reg := NewRegistry(testOpts())
	if err := reg.Register("", NewMarkdown()); CodeOf(err) != InvalidArgument {
		t.Fatalf("empty kind: code = %s, want %s", CodeOf(err), InvalidArgument)
	}
	if err := reg.Register(KindMarkdown, nil); CodeOf(err) != InvalidArgument {
		t.Fatalf("nil renderer: code = %s, want %s", CodeOf(err), InvalidArgument)
	}
}
