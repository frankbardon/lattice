package changeset

import (
	"encoding/json"
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// hasCode asserts err carries the given coded-error code.
func hasCode(t *testing.T, err error, code errors.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %s, got nil", code)
	}
	if !errors.HasCode(err, code) {
		t.Fatalf("expected error code %s, got %v", code, err)
	}
}

func TestParse_ValidMixedOps(t *testing.T) {
	data := []byte(`[
		{"op":"replace","path":"/m/config/title","value":"Pinned"},
		{"op":"remove","path":"/m/config/visibility"},
		{"op":"add","path":"/$variables/-","value":{"name":"x","type":"string"}},
		{"op":"move","from":"/a/config/title","path":"/b/config/title"},
		{"op":"copy","from":"/a/config/title","path":"/b/config/label"},
		{"op":"test","path":"/$theme/density","value":"compact"}
	]`)
	cs, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cs.Ops) != 6 {
		t.Fatalf("want 6 ops, got %d", len(cs.Ops))
	}
	if cs.Ops[0].Op != OpReplace || cs.Ops[0].Path != "/m/config/title" {
		t.Fatalf("op0 unexpected: %+v", cs.Ops[0])
	}
	if !cs.Ops[0].HasValue {
		t.Fatalf("op0 should have a value")
	}
	if cs.Ops[1].HasValue {
		t.Fatalf("remove op should not record a value")
	}
	if !cs.Ops[3].HasFrom || cs.Ops[3].From != "/a/config/title" {
		t.Fatalf("move op from unexpected: %+v", cs.Ops[3])
	}
}

func TestParse_EmptyArrayIsValid(t *testing.T) {
	cs, err := Parse([]byte(`[]`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cs.Ops) != 0 {
		t.Fatalf("want 0 ops, got %d", len(cs.Ops))
	}
}

func TestParse_ValuePreservedVerbatim(t *testing.T) {
	cs, err := Parse([]byte(`[{"op":"replace","path":"/m/config/title","value":{"a":[1,2],"b":null}}]`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(cs.Ops[0].Value, &got); err != nil {
		t.Fatalf("value not valid JSON: %v", err)
	}
	if _, ok := got["b"]; !ok {
		t.Fatalf("explicit null member lost: %v", got)
	}
}

func TestParse_Malformed(t *testing.T) {
	cases := map[string]string{
		"not an array":          `{"op":"add","path":"/x"}`,
		"garbage":               `not json`,
		"missing op":            `[{"path":"/x","value":1}]`,
		"non-string op":         `[{"op":5,"path":"/x"}]`,
		"unknown op":            `[{"op":"frobnicate","path":"/x"}]`,
		"missing path":          `[{"op":"add","value":1}]`,
		"add without value":     `[{"op":"add","path":"/x"}]`,
		"replace without value": `[{"op":"replace","path":"/x"}]`,
		"test without value":    `[{"op":"test","path":"/x"}]`,
		"move without from":     `[{"op":"move","path":"/x"}]`,
		"copy without from":     `[{"op":"copy","path":"/x"}]`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(doc))
			hasCode(t, err, errors.CHANGESET_INVALID)
		})
	}
}

func TestParse_RemoveAndMoveNeedNoValue(t *testing.T) {
	cs, err := Parse([]byte(`[
		{"op":"remove","path":"/x"},
		{"op":"move","from":"/a","path":"/b"},
		{"op":"copy","from":"/a","path":"/b"}
	]`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, op := range cs.Ops {
		if op.HasValue {
			t.Fatalf("op %s should not carry a value", op.Op)
		}
	}
}
