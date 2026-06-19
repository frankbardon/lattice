package variables

import (
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// TestParseOverrideTargetValid covers well-formed addresses: bare variable names
// and node+field addresses (including dotted field paths). Each valid target must
// also round-trip through String().
func TestParseOverrideTargetValid(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want OverrideTarget
	}{
		{
			name: "bare variable name",
			addr: "region",
			want: OverrideTarget{Kind: OverrideKindVariable, Name: "region"},
		},
		{
			name: "variable name with dashes",
			addr: "selected-region",
			want: OverrideTarget{Kind: OverrideKindVariable, Name: "selected-region"},
		},
		{
			name: "node and single field",
			addr: "chart-1.title",
			want: OverrideTarget{Kind: OverrideKindNodeField, Name: "chart-1", Field: "title"},
		},
		{
			name: "node and dotted field path",
			addr: "panel.grid.gap",
			want: OverrideTarget{Kind: OverrideKindNodeField, Name: "panel", Field: "grid.gap"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseOverrideTarget(tt.addr)
			if err != nil {
				t.Fatalf("ParseOverrideTarget(%q): unexpected error %v", tt.addr, err)
			}
			if got != tt.want {
				t.Fatalf("ParseOverrideTarget(%q) = %+v, want %+v", tt.addr, got, tt.want)
			}
			// Round-trip: the target renders back to the same address.
			if rt := got.String(); rt != tt.addr {
				t.Errorf("String() = %q, want %q (round-trip)", rt, tt.addr)
			}
		})
	}
}

// TestParseOverrideTargetMalformed covers addresses that are syntactically
// invalid: empty, or a node+field address missing one of its halves. Each must
// fail fast with VAR_OVERRIDE_INVALID.
func TestParseOverrideTargetMalformed(t *testing.T) {
	tests := []struct {
		name string
		addr string
	}{
		{"empty", ""},
		{"leading dot (no node id)", ".title"},
		{"trailing dot (no field path)", "chart-1."},
		{"only a dot", "."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseOverrideTarget(tt.addr)
			if err == nil {
				t.Fatalf("ParseOverrideTarget(%q): expected error, got nil", tt.addr)
			}
			if !errors.HasCode(err, errors.VAR_OVERRIDE_INVALID) {
				t.Fatalf("ParseOverrideTarget(%q): got %v, want VAR_OVERRIDE_INVALID", tt.addr, err)
			}
		})
	}
}

// TestOverrideSetVariableOverrides confirms the variable subset of a mixed set is
// extracted (bare names only), preserving the E3-S4 Overrides shape, and that
// node+field-addressed entries are excluded.
func TestOverrideSetVariableOverrides(t *testing.T) {
	set := OverrideSet{
		"region":        "apac",
		"gap":           "4",
		"chart-1.title": "Sales",
		"panel.grid.gap": 8,
	}
	vars, err := set.VariableOverrides()
	if err != nil {
		t.Fatalf("VariableOverrides: %v", err)
	}
	if len(vars) != 2 {
		t.Fatalf("VariableOverrides = %v, want exactly region and gap", vars)
	}
	if vars["region"] != "apac" || vars["gap"] != "4" {
		t.Errorf("VariableOverrides = %v, want region=apac gap=4", vars)
	}
	if _, ok := vars["chart-1.title"]; ok {
		t.Error("node+field address leaked into variable overrides")
	}
}

// TestOverrideSetVariableOverridesEmpty confirms a nil/empty set and a set with
// no variable targets both yield a nil map (identical to the no-override path).
func TestOverrideSetVariableOverridesEmpty(t *testing.T) {
	for _, set := range []OverrideSet{nil, {}, {"chart-1.title": "x"}} {
		vars, err := set.VariableOverrides()
		if err != nil {
			t.Fatalf("VariableOverrides(%v): %v", set, err)
		}
		if vars != nil {
			t.Errorf("VariableOverrides(%v) = %v, want nil", set, vars)
		}
	}
}

// TestOverrideSetNodeFieldOverrides confirms the node+field subset is parsed and
// carried (target + value), excluding bare variable names.
func TestOverrideSetNodeFieldOverrides(t *testing.T) {
	set := OverrideSet{
		"region":         "apac",
		"chart-1.title":  "Sales",
		"panel.grid.gap": 8,
	}
	nf, err := set.NodeFieldOverrides()
	if err != nil {
		t.Fatalf("NodeFieldOverrides: %v", err)
	}
	if len(nf) != 2 {
		t.Fatalf("NodeFieldOverrides = %v, want 2 node+field entries", nf)
	}
	byAddr := map[string]any{}
	for _, o := range nf {
		if o.Target.Kind != OverrideKindNodeField {
			t.Errorf("carried target %v is not a node+field target", o.Target)
		}
		byAddr[o.Target.String()] = o.Value
	}
	if byAddr["chart-1.title"] != "Sales" || byAddr["panel.grid.gap"] != 8 {
		t.Errorf("NodeFieldOverrides carried %v, want chart-1.title=Sales panel.grid.gap=8", byAddr)
	}
}

// TestOverrideSetMalformedPropagates confirms a malformed address in the set
// surfaces as a fail-fast VAR_OVERRIDE_INVALID through both classifiers.
func TestOverrideSetMalformedPropagates(t *testing.T) {
	set := OverrideSet{".title": "x"}
	if _, err := set.VariableOverrides(); !errors.HasCode(err, errors.VAR_OVERRIDE_INVALID) {
		t.Errorf("VariableOverrides: got %v, want VAR_OVERRIDE_INVALID", err)
	}
	if _, err := set.NodeFieldOverrides(); !errors.HasCode(err, errors.VAR_OVERRIDE_INVALID) {
		t.Errorf("NodeFieldOverrides: got %v, want VAR_OVERRIDE_INVALID", err)
	}
}
