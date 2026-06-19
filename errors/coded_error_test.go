package errors

import (
	"encoding/json"
	stderrors "errors"
	"testing"
)

func TestCodedErrorError(t *testing.T) {
	e := NewCodedError(RESOLVE_INVALID, "bad document")
	if got, want := e.Error(), "RESOLVE_INVALID: bad document"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestUnwrap(t *testing.T) {
	cause := stderrors.New("disk full")
	e := WrapCodedError(cause, RESOLVE_IO, "read failed")
	if got := e.Unwrap(); got != cause {
		t.Errorf("Unwrap() = %v, want %v", got, cause)
	}
	if !stderrors.Is(e, cause) {
		t.Error("errors.Is did not find the wrapped cause")
	}
}

func TestHasCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code Code
		want bool
	}{
		{"nil", nil, RESOLVE_INVALID, false},
		{"direct match", NewCodedError(RESOLVE_INVALID, "x"), RESOLVE_INVALID, true},
		{"no match", NewCodedError(RESOLVE_INVALID, "x"), SCHEMA_INVALID, false},
		{"wrapped match", WrapCodedError(NewCodedError(SCHEMA_REF, "y"), RESOLVE_IO, "z"), SCHEMA_REF, true},
		{"plain error", stderrors.New("plain"), RESOLVE_INVALID, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasCode(tc.err, tc.code); got != tc.want {
				t.Errorf("HasCode = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMarshalJSONNoDetails(t *testing.T) {
	e := NewCodedError(VAR_UNDEFINED, "missing var")
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got, want := string(b), `{"code":"VAR_UNDEFINED","message":"missing var"}`; got != want {
		t.Errorf("Marshal = %s, want %s", got, want)
	}
}

func TestMarshalJSONWithDetails(t *testing.T) {
	e := NewCodedErrorWithDetails(VAR_TYPE, "wrong type", map[string]any{"field": "x"})
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got, want := string(b), `{"code":"VAR_TYPE","message":"wrong type","details":{"field":"x"}}`; got != want {
		t.Errorf("Marshal = %s, want %s", got, want)
	}
}

func TestWrapCodedErrorWithDetails(t *testing.T) {
	cause := stderrors.New("schema invalid")
	src := map[string]any{"path": "root.children[0]"}
	e := WrapCodedErrorWithDetails(cause, RESOLVE_CONFIG_INVALID, "config invalid", src)

	if got := e.Unwrap(); got != cause {
		t.Errorf("Unwrap() = %v, want %v", got, cause)
	}
	if !HasCode(e, RESOLVE_CONFIG_INVALID) {
		t.Error("HasCode did not find RESOLVE_CONFIG_INVALID")
	}
	// Defensive copy: mutating the source must not affect the error.
	src["path"] = "mutated"
	if e.Details["path"] != "root.children[0]" {
		t.Errorf("Details not defensively copied: got %v", e.Details["path"])
	}
}

func TestNewCodedErrorWithDetailsCopiesMap(t *testing.T) {
	src := map[string]any{"k": 1}
	e := NewCodedErrorWithDetails(CONNECTION_INVALID, "bad", src)
	src["k"] = 2
	if e.Details["k"] != 1 {
		t.Errorf("Details not defensively copied: got %v", e.Details["k"])
	}
}
