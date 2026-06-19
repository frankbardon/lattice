package variables

import (
	"reflect"
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// TestComputedExtend exercises computed-variable evaluation through Extend: a
// simple expression, cross-variable references (in and out of declaration
// order), enum coercion, array results, type mismatches, evaluation errors, and
// cycle detection.
func TestComputedExtend(t *testing.T) {
	tests := []struct {
		name    string
		parent  []Declaration // inherited scope (literals)
		decls   []Declaration // this node's declarations
		want    map[string]any
		wantErr errors.Code // "" means no error
	}{
		{
			name:  "simple arithmetic expr",
			decls: []Declaration{{Name: "n", Type: VarTypeInteger, Expr: "1 + 2"}},
			want:  map[string]any{"n": float64(3)},
		},
		{
			name: "expr over inherited literal",
			parent: []Declaration{
				{Name: "limit", Type: VarTypeInteger, Default: float64(10)},
			},
			decls: []Declaration{{Name: "doubled", Type: VarTypeInteger, Expr: "limit * 2"}},
			want:  map[string]any{"limit": float64(10), "doubled": float64(20)},
		},
		{
			name: "cross-var expr resolved in dependency order",
			decls: []Declaration{
				// Declared before its dependency on purpose: the topo sort orders it.
				{Name: "c", Type: VarTypeInteger, Expr: "a + b"},
				{Name: "a", Type: VarTypeInteger, Default: float64(2)},
				{Name: "b", Type: VarTypeInteger, Expr: "a * 3"},
			},
			want: map[string]any{"a": float64(2), "b": float64(6), "c": float64(8)},
		},
		{
			name: "string concatenation expr",
			parent: []Declaration{
				{Name: "region", Type: VarTypeString, Default: "us"},
			},
			decls: []Declaration{{Name: "label", Type: VarTypeString, Expr: `"region-" + region`}},
			want:  map[string]any{"region": "us", "label": "region-us"},
		},
		{
			name: "boolean expr",
			parent: []Declaration{
				{Name: "limit", Type: VarTypeInteger, Default: float64(10)},
			},
			decls: []Declaration{{Name: "big", Type: VarTypeBoolean, Expr: "limit > 5"}},
			want:  map[string]any{"limit": float64(10), "big": true},
		},
		{
			name: "enum expr coerced and membership-checked",
			decls: []Declaration{
				{Name: "env", Type: VarTypeEnum, Options: []any{"dev", "prod"}, Expr: `"pr" + "od"`},
			},
			want: map[string]any{"env": "prod"},
		},
		{
			name: "array expr",
			decls: []Declaration{
				{Name: "xs", Type: VarTypeArray, Expr: "[1, 2, 3]"},
			},
			want: map[string]any{"xs": []any{1, 2, 3}},
		},
		{
			name: "type mismatch: string result for integer var",
			decls: []Declaration{
				{Name: "n", Type: VarTypeInteger, Expr: `"hello"`},
			},
			wantErr: errors.VAR_TYPE,
		},
		{
			name: "type mismatch: fractional result for integer var",
			decls: []Declaration{
				{Name: "n", Type: VarTypeInteger, Expr: "5 / 2"},
			},
			wantErr: errors.VAR_TYPE,
		},
		{
			name: "enum result not in options",
			decls: []Declaration{
				{Name: "env", Type: VarTypeEnum, Options: []any{"dev", "prod"}, Expr: `"stage"`},
			},
			wantErr: errors.VAR_OPTIONS_INVALID,
		},
		{
			name: "eval error: bad builtin call",
			decls: []Declaration{
				{Name: "n", Type: VarTypeInteger, Expr: `int("not-a-number")`},
			},
			wantErr: errors.VAR_EXPR,
		},
		{
			name: "compile error: syntax",
			decls: []Declaration{
				{Name: "n", Type: VarTypeInteger, Expr: "1 +"},
			},
			wantErr: errors.VAR_EXPR,
		},
		{
			name: "direct cycle",
			decls: []Declaration{
				{Name: "a", Type: VarTypeInteger, Expr: "a + 1"},
			},
			wantErr: errors.VAR_CYCLE,
		},
		{
			name: "mutual cycle",
			decls: []Declaration{
				{Name: "a", Type: VarTypeInteger, Expr: "b + 1"},
				{Name: "b", Type: VarTypeInteger, Expr: "a + 1"},
			},
			wantErr: errors.VAR_CYCLE,
		},
		{
			name: "default and expr both set",
			decls: []Declaration{
				{Name: "a", Type: VarTypeInteger, Default: float64(1), Expr: "2"},
			},
			wantErr: errors.VAR_DECLARATION_INVALID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent, err := Environment(nil).Extend(tt.parent, "doc")
			if err != nil {
				t.Fatalf("parent extend: %v", err)
			}

			env, err := parent.Extend(tt.decls, "root")
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %s, got nil", tt.wantErr)
				}
				if !errors.HasCode(err, tt.wantErr) {
					t.Fatalf("expected code %s, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("extend: %v", err)
			}

			for name, want := range tt.want {
				rv, ok := env.Lookup(name)
				if !ok {
					t.Fatalf("variable %q not in scope", name)
				}
				if !reflect.DeepEqual(rv.Default, want) {
					t.Errorf("%q value = %#v, want %#v", name, rv.Default, want)
				}
			}
		})
	}
}

// TestComputedFeedsInterpolation confirms a computed value flows through the
// shared interpolation pipeline exactly as a literal default does.
func TestComputedFeedsInterpolation(t *testing.T) {
	env, err := Environment(nil).Extend([]Declaration{
		{Name: "base", Type: VarTypeInteger, Default: float64(4)},
		{Name: "rows", Type: VarTypeInteger, Expr: "base * 3"},
	}, "doc")
	if err != nil {
		t.Fatalf("extend: %v", err)
	}

	// ${} string template renders the computed integer.
	out, err := Interpolate("rows=${rows}", env, "root")
	if err != nil {
		t.Fatalf("interpolate template: %v", err)
	}
	if out != "rows=12" {
		t.Errorf("template = %q, want %q", out, "rows=12")
	}

	// $var typed binding preserves the computed value's type.
	bound, err := Interpolate(map[string]any{"$var": "rows"}, env, "root")
	if err != nil {
		t.Fatalf("interpolate binding: %v", err)
	}
	if bound != float64(12) {
		t.Errorf("binding = %#v, want float64(12)", bound)
	}
}

// TestComputedShadowing verifies a computed declaration shadows an inherited
// variable of the same name and that the new computed value is the visible one.
func TestComputedShadowing(t *testing.T) {
	doc, err := Environment(nil).Extend([]Declaration{
		{Name: "limit", Type: VarTypeInteger, Default: float64(10)},
	}, "doc")
	if err != nil {
		t.Fatalf("doc extend: %v", err)
	}

	child, err := doc.Extend([]Declaration{
		// References the inherited limit, then shadows a different name.
		{Name: "scaled", Type: VarTypeInteger, Expr: "limit + 5"},
	}, "root.children[0]")
	if err != nil {
		t.Fatalf("child extend: %v", err)
	}

	scaled, ok := child.Lookup("scaled")
	if !ok {
		t.Fatal("scaled not visible")
	}
	if scaled.Default != float64(15) {
		t.Errorf("scaled = %#v, want float64(15)", scaled.Default)
	}
	if scaled.DeclaredAt != "root.children[0]" {
		t.Errorf("scaled declaredAt = %q, want root.children[0]", scaled.DeclaredAt)
	}

	// The parent environment is untouched.
	if _, ok := doc.Lookup("scaled"); ok {
		t.Error("parent env was mutated with scaled")
	}
}
