package skills

import (
	"reflect"
	"sort"
	"testing"
)

// TestListOrderingAndParse asserts List returns the embedded corpus sorted by
// Name, with the keystone session-bootstrap skill's frontmatter parsed into the
// expected Metadata.
func TestListOrderingAndParse(t *testing.T) {
	got := List()
	if len(got) == 0 {
		t.Fatal("List() returned no skills; expected at least session-bootstrap")
	}

	// Sorted-by-Name invariant (deterministic ordering across builds).
	if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i].Name < got[j].Name }) {
		names := make([]string, len(got))
		for i, m := range got {
			names[i] = m.Name
		}
		t.Errorf("List() not sorted by Name: %v", names)
	}

	// The keystone skill must be present with fully parsed metadata.
	var boot *Metadata
	for i := range got {
		if got[i].Name == "session-bootstrap" {
			boot = &got[i]
			break
		}
	}
	if boot == nil {
		t.Fatal("session-bootstrap not present in List()")
	}
	if boot.Type != "guide" {
		t.Errorf("session-bootstrap type = %q, want %q", boot.Type, "guide")
	}
	if boot.Kind != "workflow" {
		t.Errorf("session-bootstrap kind = %q, want %q", boot.Kind, "workflow")
	}
	if boot.Description == "" {
		t.Error("session-bootstrap description is empty")
	}
	wantApplies := []string{"get_manifest", "list_skills", "get_skill"}
	if !reflect.DeepEqual(boot.AppliesTo, wantApplies) {
		t.Errorf("session-bootstrap applies_to = %v, want %v", boot.AppliesTo, wantApplies)
	}
}

// TestGet covers the hit/miss contract of Get.
func TestGet(t *testing.T) {
	tests := []struct {
		name    string
		skill   string
		wantOK  bool
		wantSub string // substring expected in the body on a hit
	}{
		{name: "hit returns body", skill: "session-bootstrap", wantOK: true, wantSub: "# Session bootstrap"},
		{name: "miss returns false", skill: "does-not-exist", wantOK: false},
		{name: "extension not stripped twice", skill: "session-bootstrap.md", wantOK: false},
		{name: "empty name", skill: "", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, ok := Get(tc.skill)
			if ok != tc.wantOK {
				t.Fatalf("Get(%q) ok = %v, want %v", tc.skill, ok, tc.wantOK)
			}
			if tc.wantOK {
				if body == "" {
					t.Errorf("Get(%q) returned empty body on a hit", tc.skill)
				}
				if tc.wantSub != "" && !contains(body, tc.wantSub) {
					t.Errorf("Get(%q) body missing %q", tc.skill, tc.wantSub)
				}
			} else if body != "" {
				t.Errorf("Get(%q) returned non-empty body on a miss: %q", tc.skill, body)
			}
		})
	}
}

// TestParseMetadata is a table-driven check of the frontmatter parser, including
// malformed/edge-case input that must NOT panic.
func TestParseMetadata(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantOK bool
		want   Metadata
	}{
		{
			name: "full frontmatter, bracket list",
			input: "---\n" +
				"name: foo\n" +
				"description: a thing\n" +
				"type: reference\n" +
				"kind: items\n" +
				"applies_to: [get_node, get_schema]\n" +
				"covers: [markdown, image]\n" +
				"---\n\n# Foo\n",
			wantOK: true,
			want: Metadata{
				Name:        "foo",
				Description: "a thing",
				Type:        "reference",
				Kind:        "items",
				AppliesTo:   []string{"get_node", "get_schema"},
				Covers:      []string{"markdown", "image"},
			},
		},
		{
			name:   "csv list form, no covers",
			input:  "---\nname: bar\napplies_to: a, b , c\n---\nbody",
			wantOK: true,
			want: Metadata{
				Name:      "bar",
				AppliesTo: []string{"a", "b", "c"},
			},
		},
		{
			name:   "no frontmatter at all",
			input:  "# Just a heading\nno frontmatter here",
			wantOK: false,
		},
		{
			name:   "unterminated frontmatter block",
			input:  "---\nname: oops\ndescription: never closes\n",
			wantOK: false,
		},
		{
			name:   "empty string",
			input:  "",
			wantOK: false,
		},
		{
			name:   "only opening fence",
			input:  "---\n",
			wantOK: false,
		},
		{
			name:   "line without colon is ignored",
			input:  "---\nname: ok\nthis line has no colon\n---\n",
			wantOK: true,
			want:   Metadata{Name: "ok"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseMetadata(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("parseMetadata ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseMetadata = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestParseListForms exercises the list parser's accepted forms and edge cases.
func TestParseListForms(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{name: "empty", in: "", want: nil},
		{name: "bracket", in: `["a", "b"]`, want: []string{"a", "b"}},
		{name: "csv", in: "a, b, c", want: []string{"a", "b", "c"}},
		{name: "single quotes", in: "['x', 'y']", want: []string{"x", "y"}},
		{name: "trailing comma dropped", in: "a, b,", want: []string{"a", "b"}},
		{name: "only commas", in: ", ,", want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseList(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseList(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
