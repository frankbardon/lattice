package variables

import (
	stderrors "errors"
	"reflect"
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// env builds an Environment from declarations whose Default is the value to
// substitute. DeclaredAt is irrelevant to interpolation, so it is left empty.
func env(decls ...ResolvedVar) Environment {
	e := make(Environment, len(decls))
	for _, d := range decls {
		e[d.Name] = d
	}
	return e
}

// TestInterpolate covers the two reference forms, recursion, type preservation,
// and value pass-through. Numbers arrive as float64 (decoded JSON).
func TestInterpolate(t *testing.T) {
	e := env(
		ResolvedVar{Name: "region", Type: VarTypeString, Default: "eu"},
		ResolvedVar{Name: "limit", Type: VarTypeInteger, Default: float64(10)},
		ResolvedVar{Name: "ratio", Type: VarTypeNumber, Default: 1.5},
		ResolvedVar{Name: "live", Type: VarTypeBoolean, Default: true},
		ResolvedVar{Name: "tags", Type: VarTypeArray, Default: []any{"a", "b"}},
	)

	tests := []struct {
		name string
		in   any
		want any
	}{
		{
			name: "typed binding preserves integer type",
			in:   map[string]any{"$var": "limit"},
			want: float64(10),
		},
		{
			name: "typed binding preserves boolean type",
			in:   map[string]any{"$var": "live"},
			want: true,
		},
		{
			name: "typed binding preserves array type",
			in:   map[string]any{"$var": "tags"},
			want: []any{"a", "b"},
		},
		{
			name: "string template interpolates within a string",
			in:   "region=${region}",
			want: "region=eu",
		},
		{
			name: "string template stringifies an integer value",
			in:   "limit is ${limit}",
			want: "limit is 10",
		},
		{
			name: "string template renders a fractional number",
			in:   "ratio=${ratio}",
			want: "ratio=1.5",
		},
		{
			name: "multiple templates in one string",
			in:   "${region}/${limit}",
			want: "eu/10",
		},
		{
			name: "string without a template is unchanged",
			in:   "plain text",
			want: "plain text",
		},
		{
			name: "typed binding nested inside a map and slice",
			in: map[string]any{
				"page": map[string]any{"$var": "limit"},
				"list": []any{map[string]any{"$var": "region"}, "static"},
			},
			want: map[string]any{
				"page": float64(10),
				"list": []any{"eu", "static"},
			},
		},
		{
			name: "object that is not a binding is walked as data",
			in:   map[string]any{"$var": "region", "extra": 1},
			want: map[string]any{"$var": "region", "extra": 1},
		},
		{
			name: "non-string scalars pass through",
			in:   float64(42),
			want: float64(42),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Interpolate(tt.in, e, "root")
			if err != nil {
				t.Fatalf("Interpolate: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestInterpolateMissing asserts that an undeclared reference in either form
// fails fast with VAR_UNDEFINED naming the offending instance path.
func TestInterpolateMissing(t *testing.T) {
	e := env(ResolvedVar{Name: "known", Type: VarTypeString, Default: "x"})

	tests := []struct {
		name string
		in   any
	}{
		{name: "missing typed binding", in: map[string]any{"$var": "ghost"}},
		{name: "missing string template", in: "hello ${ghost}"},
		{name: "missing nested binding", in: map[string]any{"k": map[string]any{"$var": "ghost"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Interpolate(tt.in, e, "root.children[2]")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.HasCode(err, errors.VAR_UNDEFINED) {
				t.Fatalf("expected VAR_UNDEFINED, got %v", err)
			}
			var ce *errors.CodedError
			if stderrors.As(err, &ce) {
				if ce.Details["path"] != "root.children[2]" {
					t.Errorf("path = %v, want root.children[2]", ce.Details["path"])
				}
				if ce.Details["name"] != "ghost" {
					t.Errorf("name = %v, want ghost", ce.Details["name"])
				}
			}
		})
	}
}

// TestInterpolateDoesNotMutate confirms the input value is never modified.
func TestInterpolateDoesNotMutate(t *testing.T) {
	e := env(ResolvedVar{Name: "n", Type: VarTypeInteger, Default: float64(5)})
	in := map[string]any{"v": map[string]any{"$var": "n"}}

	if _, err := Interpolate(in, e, "root"); err != nil {
		t.Fatalf("Interpolate: %v", err)
	}
	inner, ok := in["v"].(map[string]any)
	if !ok || inner["$var"] != "n" {
		t.Errorf("input was mutated: %#v", in)
	}
}
