package resolve

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/frankbardon/lattice/dashboard"
)

// vars is a test helper building Values from inline variable definitions.
func vars(defs ...dashboard.Variable) Values { return FromVariables(defs) }

// TestSubstituteMarkdown covers raw (non-JSON) substitution: the value is
// spliced verbatim, an empty Value falls back to Default, and a missing variable
// is left intact.
func TestSubstituteMarkdown(t *testing.T) {
	v := vars(
		dashboard.Variable{Name: "env", Type: dashboard.VarString, Value: "prod"},
		dashboard.Variable{Name: "region", Type: dashboard.VarString, Default: "us", Value: ""},
	)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"value", "deploy to ${env}", "deploy to prod"},
		{"falls back to default", "region ${region}", "region us"},
		{"missing left intact", "hello ${ghost}", "hello ${ghost}"},
		{"repeated", "${env}/${env}", "prod/prod"},
		{"no placeholder", "plain text", "plain text"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Substitute(tc.in, v, false); got != tc.want {
				t.Fatalf("Substitute(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestSubstituteJSONInString covers a placeholder used inside a JSON string
// literal: the value is escaped into the string and the result stays valid JSON.
func TestSubstituteJSONInString(t *testing.T) {
	v := vars(
		dashboard.Variable{Name: "table", Type: dashboard.VarString, Value: "sales"},
		dashboard.Variable{Name: "weird", Type: dashboard.VarString, Value: `a"b\c`},
	)

	in := `{"from":"${table}","note":"${weird}"}`
	got := Substitute(in, v, true)

	var out map[string]string
	if err := json.Unmarshal([]byte(got), &out); err != nil {
		t.Fatalf("substituted JSON invalid: %v\n got=%s", err, got)
	}
	if out["from"] != "sales" {
		t.Fatalf("from = %q, want sales", out["from"])
	}
	if out["note"] != `a"b\c` {
		t.Fatalf("note = %q, want %q (escaping broke)", out["note"], `a"b\c`)
	}
}

// TestSubstituteJSONStandalone covers placeholders that stand alone as a JSON
// value: a number variable inlines bare (parses as a JSON number) while a
// string/enum inlines as a quoted JSON string.
func TestSubstituteJSONStandalone(t *testing.T) {
	v := vars(
		dashboard.Variable{Name: "limit", Type: dashboard.VarNumber, Value: "10"},
		dashboard.Variable{Name: "tier", Type: dashboard.VarEnum, Value: "pro", Options: []string{"free", "pro"}},
	)

	in := `{"limit":${limit},"tier":${tier}}`
	got := Substitute(in, v, true)

	var out struct {
		Limit float64 `json:"limit"`
		Tier  string  `json:"tier"`
	}
	if err := json.Unmarshal([]byte(got), &out); err != nil {
		t.Fatalf("substituted JSON invalid: %v\n got=%s", err, got)
	}
	if out.Limit != 10 {
		t.Fatalf("limit = %v, want 10 (number must inline bare)", out.Limit)
	}
	if out.Tier != "pro" {
		t.Fatalf("tier = %q, want pro (enum must inline quoted)", out.Tier)
	}
}

// TestSubstituteJSONNumberFallbackToDefault proves a number with an empty Value
// resolves from its Default and still inlines as a bare JSON number.
func TestSubstituteJSONNumberFallbackToDefault(t *testing.T) {
	v := vars(dashboard.Variable{Name: "n", Type: dashboard.VarNumber, Default: "5", Value: ""})
	got := Substitute(`{"n":${n}}`, v, true)
	var out struct {
		N float64 `json:"n"`
	}
	if err := json.Unmarshal([]byte(got), &out); err != nil {
		t.Fatalf("invalid JSON: %v got=%s", err, got)
	}
	if out.N != 5 {
		t.Fatalf("n = %v, want 5", out.N)
	}
}

// TestSubstituteJSONMissingLeftIntact proves an undefined variable token is left
// in place (it is not silently dropped or quoted).
func TestSubstituteJSONMissingLeftIntact(t *testing.T) {
	v := vars(dashboard.Variable{Name: "env", Type: dashboard.VarString, Value: "prod"})
	got := Substitute(`{"env":"${env}","x":"${ghost}"}`, v, true)
	if got != `{"env":"prod","x":"${ghost}"}` {
		t.Fatalf("got %q", got)
	}
}

// TestReferences proves reference detection returns distinct names in first-seen
// order and that a placeholder-free template references nothing.
func TestReferences(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"none", "no vars here", nil},
		{"one", "hello ${env}", []string{"env"}},
		{"distinct ordered", "${b}-${a}-${b}-${c}", []string{"b", "a", "c"}},
		{"in json", `{"from":"${table}","limit":${n}}`, []string{"table", "n"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := References(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("References(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
