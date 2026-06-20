package changeset

import (
	"encoding/json"
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// testDoc mirrors examples/minimal-dashboard.json's physical shape: a root
// container -> body container -> two block wrappers, each wrapping a table at
// config/content. It exercises the id walk through children slots AND block
// config/content.
const testDoc = `{
  "manifest": { "id": "example", "title": "T" },
  "variables": [{ "name": "region", "type": "string", "default": "us" }],
  "theme": { "density": "comfortable" },
  "connections": [{ "id": "c1", "$ref": "x" }],
  "root": {
    "$ref": "container", "id": "root", "config": { "grid": { "columns": [1] } },
    "children": [
      {
        "$ref": "container", "id": "body", "config": {},
        "children": [
          {
            "$ref": "block", "id": "fruits-block",
            "config": {
              "id": "fruits-block",
              "content": { "$ref": "table", "id": "fruits", "config": { "title": "Fruits" } }
            }
          },
          {
            "$ref": "block", "id": "metrics-block",
            "config": {
              "id": "metrics-block",
              "content": { "$ref": "table", "id": "metrics", "config": { "title": "Metrics" } }
            }
          }
        ]
      }
    ]
  }
}`

func decode(t *testing.T) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(testDoc), &doc); err != nil {
		t.Fatalf("decode test doc: %v", err)
	}
	return doc
}

func TestTranslate_ItemID(t *testing.T) {
	tr := NewTranslator(decode(t))

	cases := map[string]struct {
		in   string
		want string
	}{
		"root container":        {"/root/config/grid/columns/0", "/root/config/grid/columns/0"},
		"nested container body": {"/body/config", "/root/children/0/config"},
		"block wrapper":         {"/fruits-block/config/title", "/root/children/0/children/0/config/title"},
		"second block wrapper":  {"/metrics-block/config/visibility", "/root/children/0/children/1/config/visibility"},
		"block content via id":  {"/fruits/config/title", "/root/children/0/children/0/config/content/config/title"},
		"second content via id": {"/metrics/config/title", "/root/children/0/children/1/config/content/config/title"},
		"id only no remainder":  {"/fruits", "/root/children/0/children/0/config/content"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := tr.Translate(c.in, 0)
			if err != nil {
				t.Fatalf("Translate(%q): %v", c.in, err)
			}
			if got != c.want {
				t.Fatalf("Translate(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestTranslate_Scopes(t *testing.T) {
	tr := NewTranslator(decode(t))

	cases := map[string]struct {
		in   string
		want string
	}{
		"$manifest title":    {"/$manifest/title", "/manifest/title"},
		"$theme token":       {"/$theme/density", "/theme/density"},
		"$variables append":  {"/$variables/-", "/variables/-"},
		"$connections index": {"/$connections/0/config", "/connections/0/config"},
		"$root child":        {"/$root/children/0", "/root/children/0"},
		"$root only":         {"/$root", "/root"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := tr.Translate(c.in, 0)
			if err != nil {
				t.Fatalf("Translate(%q): %v", c.in, err)
			}
			if got != c.want {
				t.Fatalf("Translate(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestTranslate_UnknownID(t *testing.T) {
	tr := NewTranslator(decode(t))
	_, err := tr.Translate("/nope/config/title", 3)
	hasCode(t, err, errors.CHANGESET_TARGET_NOT_FOUND)
}

func TestTranslate_UnknownScope(t *testing.T) {
	tr := NewTranslator(decode(t))
	_, err := tr.Translate("/$bogus/title", 0)
	hasCode(t, err, errors.CONFIGURATOR_TARGET_SCOPE_UNKNOWN)
}

func TestTranslate_MalformedPointer(t *testing.T) {
	tr := NewTranslator(decode(t))
	for _, p := range []string{"", "no-slash", "//empty-leading"} {
		_, err := tr.Translate(p, 0)
		hasCode(t, err, errors.CHANGESET_POINTER_INVALID)
	}
}

func TestTranslateOperation_FromAndPath(t *testing.T) {
	tr := NewTranslator(decode(t))
	op := Operation{Op: OpMove, Path: "/metrics/config/title", From: "/fruits/config/title", HasFrom: true}
	out, err := tr.TranslateOperation(op, 0)
	if err != nil {
		t.Fatalf("TranslateOperation: %v", err)
	}
	if out.Path != "/root/children/0/children/1/config/content/config/title" {
		t.Fatalf("path = %q", out.Path)
	}
	if out.From != "/root/children/0/children/0/config/content/config/title" {
		t.Fatalf("from = %q", out.From)
	}
}

func TestTranslateOperation_FromUntranslatable(t *testing.T) {
	tr := NewTranslator(decode(t))
	op := Operation{Op: OpCopy, Path: "/fruits/config/title", From: "/ghost/config/title", HasFrom: true}
	_, err := tr.TranslateOperation(op, 7)
	hasCode(t, err, errors.CHANGESET_TARGET_NOT_FOUND)
}

func TestTranslateChangeset_FailFast(t *testing.T) {
	tr := NewTranslator(decode(t))
	cs := &Changeset{Ops: []Operation{
		{Op: OpReplace, Path: "/fruits/config/title", HasValue: true, Value: json.RawMessage(`"X"`)},
		{Op: OpReplace, Path: "/missing/config/title", HasValue: true, Value: json.RawMessage(`"Y"`)},
	}}
	_, err := tr.TranslateChangeset(cs)
	hasCode(t, err, errors.CHANGESET_TARGET_NOT_FOUND)
}

func TestTranslateChangeset_AllValid(t *testing.T) {
	tr := NewTranslator(decode(t))
	cs := &Changeset{Ops: []Operation{
		{Op: OpReplace, Path: "/fruits/config/title", HasValue: true, Value: json.RawMessage(`"X"`)},
		{Op: OpReplace, Path: "/$manifest/title", HasValue: true, Value: json.RawMessage(`"Y"`)},
	}}
	out, err := tr.TranslateChangeset(cs)
	if err != nil {
		t.Fatalf("TranslateChangeset: %v", err)
	}
	if out.Ops[0].Path != "/root/children/0/children/0/config/content/config/title" {
		t.Fatalf("op0 path = %q", out.Ops[0].Path)
	}
	if out.Ops[1].Path != "/manifest/title" {
		t.Fatalf("op1 path = %q", out.Ops[1].Path)
	}
}

func TestNewTranslator_NilDoc(t *testing.T) {
	tr := NewTranslator(nil)
	// $-scopes still resolve (they are document-member constants).
	got, err := tr.Translate("/$manifest/title", 0)
	if err != nil || got != "/manifest/title" {
		t.Fatalf("scope on nil doc: got %q err %v", got, err)
	}
	// An item id resolves to not-found against an empty index.
	_, err = tr.Translate("/anything", 0)
	hasCode(t, err, errors.CHANGESET_TARGET_NOT_FOUND)
}
