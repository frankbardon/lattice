package variables

import (
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// TestValidateDeclaration covers type validation, enum option rules, array
// typing, and default-value checking for a single declaration.
func TestValidateDeclaration(t *testing.T) {
	tests := []struct {
		name    string
		decl    Declaration
		wantErr errors.Code // "" means no error
	}{
		{
			name: "string ok",
			decl: Declaration{Name: "region", Type: VarTypeString, Default: "us"},
		},
		{
			name: "number accepts fractional",
			decl: Declaration{Name: "ratio", Type: VarTypeNumber, Default: 1.5},
		},
		{
			name: "integer accepts whole float",
			decl: Declaration{Name: "limit", Type: VarTypeInteger, Default: float64(10)},
		},
		{
			name:    "integer rejects fractional",
			decl:    Declaration{Name: "limit", Type: VarTypeInteger, Default: 10.5},
			wantErr: errors.VAR_TYPE,
		},
		{
			name: "boolean ok",
			decl: Declaration{Name: "live", Type: VarTypeBoolean, Default: true},
		},
		{
			name:    "boolean rejects string default",
			decl:    Declaration{Name: "live", Type: VarTypeBoolean, Default: "true"},
			wantErr: errors.VAR_TYPE,
		},
		{
			name:    "string rejects number default",
			decl:    Declaration{Name: "region", Type: VarTypeString, Default: float64(1)},
			wantErr: errors.VAR_TYPE,
		},
		{
			name: "array ok",
			decl: Declaration{Name: "tags", Type: VarTypeArray, Default: []any{"a", "b"}},
		},
		{
			name:    "array rejects non-array default",
			decl:    Declaration{Name: "tags", Type: VarTypeArray, Default: "a"},
			wantErr: errors.VAR_TYPE,
		},
		{
			name: "enum ok with member default",
			decl: Declaration{Name: "env", Type: VarTypeEnum, Options: []any{"dev", "prod"}, Default: "prod"},
		},
		{
			name:    "enum missing options",
			decl:    Declaration{Name: "env", Type: VarTypeEnum},
			wantErr: errors.VAR_OPTIONS_INVALID,
		},
		{
			name:    "enum default not a member",
			decl:    Declaration{Name: "env", Type: VarTypeEnum, Options: []any{"dev", "prod"}, Default: "stage"},
			wantErr: errors.VAR_OPTIONS_INVALID,
		},
		{
			name:    "enum non-string option",
			decl:    Declaration{Name: "env", Type: VarTypeEnum, Options: []any{"dev", float64(1)}},
			wantErr: errors.VAR_OPTIONS_INVALID,
		},
		{
			name:    "options on non-enum rejected",
			decl:    Declaration{Name: "region", Type: VarTypeString, Options: []any{"a"}},
			wantErr: errors.VAR_OPTIONS_INVALID,
		},
		{
			name:    "missing name",
			decl:    Declaration{Type: VarTypeString},
			wantErr: errors.VAR_DECLARATION_INVALID,
		},
		{
			name:    "unknown type",
			decl:    Declaration{Name: "x", Type: VarType("date")},
			wantErr: errors.VAR_DECLARATION_INVALID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDeclaration(tt.decl, "doc", 0)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %s, got nil", tt.wantErr)
			}
			if !errors.HasCode(err, tt.wantErr) {
				t.Fatalf("expected code %s, got %v", tt.wantErr, err)
			}
		})
	}
}

// TestExtendShadowing verifies that an inner declaration shadows an outer one of
// the same name and that DeclaredAt records the shadowing winner.
func TestExtendShadowing(t *testing.T) {
	doc, err := Environment(nil).Extend([]Declaration{
		{Name: "region", Type: VarTypeString, Default: "us"},
		{Name: "limit", Type: VarTypeInteger, Default: float64(10)},
	}, "doc")
	if err != nil {
		t.Fatalf("doc extend: %v", err)
	}

	child, err := doc.Extend([]Declaration{
		{Name: "region", Type: VarTypeString, Default: "eu"},
	}, "root.children[0]")
	if err != nil {
		t.Fatalf("child extend: %v", err)
	}

	// region is shadowed by the inner declaration.
	region, ok := child.Lookup("region")
	if !ok {
		t.Fatal("region not visible in child")
	}
	if region.Default != "eu" {
		t.Errorf("region default = %v, want eu", region.Default)
	}
	if region.DeclaredAt != "root.children[0]" {
		t.Errorf("region declaredAt = %q, want root.children[0]", region.DeclaredAt)
	}

	// limit is inherited unchanged from doc scope.
	limit, ok := child.Lookup("limit")
	if !ok {
		t.Fatal("limit not visible in child")
	}
	if limit.DeclaredAt != "doc" {
		t.Errorf("limit declaredAt = %q, want doc", limit.DeclaredAt)
	}

	// The parent environment must NOT be mutated by the child's shadowing.
	parentRegion, _ := doc.Lookup("region")
	if parentRegion.Default != "us" {
		t.Errorf("parent region mutated: default = %v, want us", parentRegion.Default)
	}
	if got := doc.Names(); len(got) != 2 {
		t.Errorf("parent env should still have 2 vars, got %v", got)
	}
}

// TestExtendDuplicateNameSameScope rejects two declarations of the same name on
// one node (as opposed to shadowing across scopes, which is allowed).
func TestExtendDuplicateNameSameScope(t *testing.T) {
	_, err := Environment(nil).Extend([]Declaration{
		{Name: "region", Type: VarTypeString},
		{Name: "region", Type: VarTypeString},
	}, "doc")
	if err == nil {
		t.Fatal("expected duplicate-name error")
	}
	if !errors.HasCode(err, errors.VAR_DECLARATION_INVALID) {
		t.Fatalf("expected VAR_DECLARATION_INVALID, got %v", err)
	}
}

// TestExtendEmptyReturnsParent confirms a node with no declarations sees exactly
// the parent scope.
func TestExtendEmptyReturnsParent(t *testing.T) {
	doc, err := Environment(nil).Extend([]Declaration{
		{Name: "a", Type: VarTypeString},
	}, "doc")
	if err != nil {
		t.Fatalf("doc extend: %v", err)
	}
	child, err := doc.Extend(nil, "root")
	if err != nil {
		t.Fatalf("child extend: %v", err)
	}
	if _, ok := child.Lookup("a"); !ok {
		t.Error("child should inherit a from doc scope")
	}
}

// TestExtendFailFast ensures the first invalid declaration aborts the extend.
func TestExtendFailFast(t *testing.T) {
	_, err := Environment(nil).Extend([]Declaration{
		{Name: "ok", Type: VarTypeString},
		{Name: "bad", Type: VarTypeEnum}, // missing options
	}, "doc")
	if !errors.HasCode(err, errors.VAR_OPTIONS_INVALID) {
		t.Fatalf("expected VAR_OPTIONS_INVALID, got %v", err)
	}
}

// TestNamesSorted checks deterministic ordering.
func TestNamesSorted(t *testing.T) {
	env, err := Environment(nil).Extend([]Declaration{
		{Name: "zeta", Type: VarTypeString},
		{Name: "alpha", Type: VarTypeString},
		{Name: "mu", Type: VarTypeString},
	}, "doc")
	if err != nil {
		t.Fatalf("extend: %v", err)
	}
	got := env.Names()
	want := []string{"alpha", "mu", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
