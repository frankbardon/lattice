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

// enumDoc builds a minimal dashboard with a single enum-family widget instance
// whose config is supplied verbatim (so the options set can be exercised). The
// bound variable is declared at document scope as an enum with a fixed option
// set and a default drawn from it. varType lets a case declare a NON-enum
// variable to exercise the type-mismatch path; for a non-enum type the options
// declaration is omitted (the var model forbids options on non-enum vars) and a
// type-appropriate default is used.
func enumDoc(widgetType, varName, varType, config string) string {
	varDecl := fmt.Sprintf(`{"name": %q, "type": %q, "options": ["us", "eu", "apac"], "default": "us"}`, varName, varType)
	if varType != "enum" {
		varDecl = fmt.Sprintf(`{"name": %q, "type": %q, "default": %s}`, varName, varType, defaultFor(varType))
	}
	return fmt.Sprintf(`{
  "manifest": {"formatVersion": "1.0.0", "id": "wdoc", "title": "Widget Doc"},
  "variables": [%s],
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/%s/1.0.0",
        "config": %s
      }
    ]
  }
}`, varDecl, widgetType, config)
}

// TestResolveEnumWidgets drives the E1-S3 enum family (select, radio-group,
// segmented) through the real pipeline: each widget binds a compatible enum
// variable (resolves, carrying its options set), a mismatched non-enum variable
// (WIDGET_TYPE_MISMATCH), and an undefined variable (VAR_UNDEFINED). Optional
// sort/options config is exercised on the compatible binds. Table-driven so
// every enum widget is covered uniformly.
func TestResolveEnumWidgets(t *testing.T) {
	const opts = `[{"value": "us", "label": "United States"}, {"value": "eu", "label": "Europe"}, {"value": "apac"}]`
	tests := []struct {
		name     string
		widget   string
		varName  string
		varType  string
		config   string
		wantCode errors.Code // "" = resolves successfully
		wantKV   [2]string   // expected Details key/value; "" key to skip
	}{
		// Compatible enum binds, exercising the options set and sort ordering.
		{
			name:    "select binds an enum variable with options",
			widget:  "select",
			varName: "region",
			varType: "enum",
			config:  fmt.Sprintf(`{"variable": "region", "options": %s}`, opts),
		},
		{
			name:    "radio-group binds an enum variable with label sort",
			widget:  "radio-group",
			varName: "region",
			varType: "enum",
			config:  fmt.Sprintf(`{"variable": "region", "options": %s, "sort": "label"}`, opts),
		},
		{
			name:    "segmented binds an enum variable with value sort",
			widget:  "segmented",
			varName: "region",
			varType: "enum",
			config:  fmt.Sprintf(`{"variable": "region", "options": %s, "sort": "value"}`, opts),
		},
		// Type mismatch: enum widgets reject non-enum variables.
		{
			name:     "select bound to a string variable mismatches",
			widget:   "select",
			varName:  "label",
			varType:  "string",
			config:   `{"variable": "label", "options": [{"value": "a"}]}`,
			wantCode: errors.WIDGET_TYPE_MISMATCH,
			wantKV:   [2]string{"varType", "string"},
		},
		{
			name:     "segmented bound to a boolean variable mismatches",
			widget:   "segmented",
			varName:  "active",
			varType:  "boolean",
			config:   `{"variable": "active", "options": [{"value": "a"}]}`,
			wantCode: errors.WIDGET_TYPE_MISMATCH,
			wantKV:   [2]string{"widget", "segmented"},
		},
		// Undefined variable: reuses VAR_UNDEFINED.
		{
			name:     "radio-group bound to an undefined variable",
			widget:   "radio-group",
			varName:  "region",
			varType:  "enum",
			config:   `{"variable": "missing", "options": [{"value": "a"}]}`,
			wantCode: errors.VAR_UNDEFINED,
			wantKV:   [2]string{"variable", "missing"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := newRepoResolver(t)
			doc := enumDoc(tc.widget, tc.varName, tc.varType, tc.config)
			tree, err := res.resolveBytes([]byte(doc), tc.name, nil)

			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("resolveBytes: unexpected error: %v", err)
				}
				widget := tree.Root.Children[0]
				if got := widget.Config["variable"]; got != tc.varName {
					t.Errorf("widget variable = %v, want %q", got, tc.varName)
				}
				gotOpts, ok := widget.Config["options"].([]any)
				if !ok || len(gotOpts) != 3 {
					t.Errorf("widget options = %v, want 3 options", widget.Config["options"])
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
			if tc.wantKV[0] != "" {
				if got, _ := ce.Details[tc.wantKV[0]].(string); got != tc.wantKV[1] {
					t.Errorf("error %s = %q, want %q", tc.wantKV[0], got, tc.wantKV[1])
				}
			}
		})
	}
}

// numberBooleanDoc builds a minimal dashboard with a single widget instance whose
// config is supplied verbatim (so number-family range config min/max/step can be
// exercised). The bound variable is declared at document scope with the given
// type and a type-appropriate default.
func numberBooleanDoc(widgetType, varName, varType, config string) string {
	return fmt.Sprintf(`{
  "manifest": {"formatVersion": "1.0.0", "id": "wdoc", "title": "Widget Doc"},
  "variables": [{"name": %q, "type": %q, "default": %s}],
  "root": {
    "$ref": "https://lattice.dev/schemas/items/container/1.0.0",
    "children": [
      {
        "$ref": "https://lattice.dev/schemas/items/%s/1.0.0",
        "config": %s
      }
    ]
  }
}`, varName, varType, defaultFor(varType), widgetType, config)
}

// TestResolveNumberBooleanWidgets drives the E1-S2 number (number-field, slider,
// stepper) and boolean (toggle, checkbox) families through the real pipeline:
// each widget binds a compatible variable (resolves), a mismatched variable
// (WIDGET_TYPE_MISMATCH), and — for number widgets — invalid range config
// (RESOLVE_CONFIG_INVALID for both an inverted min>max range and a non-positive
// step). Table-driven so every new widget is covered uniformly.
func TestResolveNumberBooleanWidgets(t *testing.T) {
	tests := []struct {
		name     string
		widget   string
		varName  string
		varType  string
		config   string
		wantCode errors.Code // "" = resolves successfully
		wantKV   [2]string   // expected Details key/value; "" key to skip
	}{
		// Number family: compatible binds (number and integer both satisfy).
		{
			name:    "number-field binds a number variable",
			widget:  "number-field",
			varName: "ratio",
			varType: "number",
			config:  `{"variable": "ratio", "min": 0, "max": 10, "step": 0.5}`,
		},
		{
			name:    "slider binds an integer variable with a bounded track",
			widget:  "slider",
			varName: "level",
			varType: "integer",
			config:  `{"variable": "level", "min": 1, "max": 100}`,
		},
		{
			name:    "stepper binds a number variable",
			widget:  "stepper",
			varName: "qty",
			varType: "number",
			config:  `{"variable": "qty", "step": 2}`,
		},
		// Number family: type mismatch.
		{
			name:     "number-field bound to a string variable mismatches",
			widget:   "number-field",
			varName:  "label",
			varType:  "string",
			config:   `{"variable": "label"}`,
			wantCode: errors.WIDGET_TYPE_MISMATCH,
			wantKV:   [2]string{"varType", "string"},
		},
		{
			name:     "slider bound to a boolean variable mismatches",
			widget:   "slider",
			varName:  "active",
			varType:  "boolean",
			config:   `{"variable": "active"}`,
			wantCode: errors.WIDGET_TYPE_MISMATCH,
			wantKV:   [2]string{"widget", "slider"},
		},
		// Number family: invalid range config.
		{
			name:     "number-field with inverted range fails",
			widget:   "number-field",
			varName:  "ratio",
			varType:  "number",
			config:   `{"variable": "ratio", "min": 10, "max": 1}`,
			wantCode: errors.RESOLVE_CONFIG_INVALID,
			wantKV:   [2]string{"field", "min"},
		},
		{
			name:     "stepper with non-positive step fails",
			widget:   "stepper",
			varName:  "qty",
			varType:  "number",
			config:   `{"variable": "qty", "step": 0}`,
			wantCode: errors.RESOLVE_CONFIG_INVALID,
		},
		// Boolean family: compatible binds.
		{
			name:    "toggle binds a boolean variable",
			widget:  "toggle",
			varName: "enabled",
			varType: "boolean",
			config:  `{"variable": "enabled"}`,
		},
		{
			name:    "checkbox binds a boolean variable",
			widget:  "checkbox",
			varName: "agreed",
			varType: "boolean",
			config:  `{"variable": "agreed"}`,
		},
		// Boolean family: type mismatch.
		{
			name:     "toggle bound to a number variable mismatches",
			widget:   "toggle",
			varName:  "count",
			varType:  "number",
			config:   `{"variable": "count"}`,
			wantCode: errors.WIDGET_TYPE_MISMATCH,
			wantKV:   [2]string{"varType", "number"},
		},
		{
			name:     "checkbox bound to a string variable mismatches",
			widget:   "checkbox",
			varName:  "region",
			varType:  "string",
			config:   `{"variable": "region"}`,
			wantCode: errors.WIDGET_TYPE_MISMATCH,
			wantKV:   [2]string{"widget", "checkbox"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := newRepoResolver(t)
			doc := numberBooleanDoc(tc.widget, tc.varName, tc.varType, tc.config)
			tree, err := res.resolveBytes([]byte(doc), tc.name, nil)

			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("resolveBytes: unexpected error: %v", err)
				}
				widget := tree.Root.Children[0]
				if got := widget.Config["variable"]; got != tc.varName {
					t.Errorf("widget variable = %v, want %q", got, tc.varName)
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
			if tc.wantKV[0] != "" {
				if got, _ := ce.Details[tc.wantKV[0]].(string); got != tc.wantKV[1] {
					t.Errorf("error %s = %q, want %q", tc.wantKV[0], got, tc.wantKV[1])
				}
			}
		})
	}
}
