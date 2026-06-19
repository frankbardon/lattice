package resolver

import (
	"fmt"
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// widgetDoc builds a minimal dashboard document: a container root declaring one
// document-scope variable of varType, holding a single widget instance of
// widgetType bound to that variable. It exercises the widget↔variable
// type-compatibility contract end to end through the real resolver pipeline.
func widgetDoc(widgetType, varName, varType, boundVar string) string {
	return fmt.Sprintf(`{
  "manifest": {"formatVersion": "1.0.0", "id": "wdoc", "title": "Widget Doc"},
  "variables": [{"name": %q, "type": %q, "default": %s}],
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/%s/1.0.0",
        "config": {"variable": %q}
      }
    ]
  }
}`, varName, varType, defaultFor(varType), widgetType, boundVar)
}

// defaultFor returns a JSON literal default appropriate for varType, so the
// variable declaration is well-formed regardless of which type a case declares.
func defaultFor(varType string) string {
	switch varType {
	case "string":
		return `"hello"`
	case "number", "integer":
		return `42`
	case "boolean":
		return `true`
	case "array":
		return `[]`
	default:
		return `null`
	}
}

// TestResolveWidgetBinding drives the string-family widgets (text-input,
// textarea) through the widget binding pass: a compatible bind resolves, a
// type-mismatched bind fails WIDGET_TYPE_MISMATCH, and an undefined-variable bind
// fails (reusing) VAR_UNDEFINED. Table-driven over both widgets so the string
// family is covered uniformly.
func TestResolveWidgetBinding(t *testing.T) {
	tests := []struct {
		name     string
		widget   string
		varName  string
		varType  string
		boundVar string
		wantCode errors.Code // "" = resolves successfully
		wantKV   [2]string   // expected Details key/value; "" key to skip
	}{
		{
			name:     "text-input bound to a string variable resolves",
			widget:   "text-input",
			varName:  "region",
			varType:  "string",
			boundVar: "region",
		},
		{
			name:     "textarea bound to a string variable resolves",
			widget:   "textarea",
			varName:  "notes",
			varType:  "string",
			boundVar: "notes",
		},
		{
			name:     "text-input bound to a number variable mismatches",
			widget:   "text-input",
			varName:  "count",
			varType:  "number",
			boundVar: "count",
			wantCode: errors.WIDGET_TYPE_MISMATCH,
			wantKV:   [2]string{"varType", "number"},
		},
		{
			name:     "textarea bound to a boolean variable mismatches",
			widget:   "textarea",
			varName:  "active",
			varType:  "boolean",
			boundVar: "active",
			wantCode: errors.WIDGET_TYPE_MISMATCH,
			wantKV:   [2]string{"widget", "textarea"},
		},
		{
			name:     "text-input bound to an undefined variable",
			widget:   "text-input",
			varName:  "region",
			varType:  "string",
			boundVar: "missing",
			wantCode: errors.VAR_UNDEFINED,
			wantKV:   [2]string{"variable", "missing"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := newRepoResolver(t)
			doc := widgetDoc(tc.widget, tc.varName, tc.varType, tc.boundVar)
			tree, err := res.resolveBytes([]byte(doc), tc.name, nil)

			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("resolveBytes: unexpected error: %v", err)
				}
				widget := tree.Root.Children[0]
				if got := widget.Config["variable"]; got != tc.boundVar {
					t.Errorf("widget variable = %v, want %q", got, tc.boundVar)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error %s, got nil", tc.wantCode)
			}
			if !errors.HasCode(err, tc.wantCode) {
				t.Fatalf("error = %v, want code %s", err, tc.wantCode)
			}
			var ce *errors.CodedError
			if !asCoded(err, &ce) {
				t.Fatalf("error is not a CodedError: %v", err)
			}
			if got, _ := ce.Details["path"].(string); got != "root.children[0]" {
				t.Errorf("error path = %q, want %q", got, "root.children[0]")
			}
			if tc.wantKV[0] != "" {
				if got, _ := ce.Details[tc.wantKV[0]].(string); got != tc.wantKV[1] {
					t.Errorf("error %s = %q, want %q", tc.wantKV[0], got, tc.wantKV[1])
				}
			}
		})
	}
}
