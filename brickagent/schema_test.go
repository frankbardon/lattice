package brickagent

import "testing"

func TestValidateTemplate(t *testing.T) {
	cases := []struct {
		name string
		in   string
		ok   bool
	}{
		{"valid", paramTemplate, true},
		{"minimal", `{"pulse_request": {}, "prism_spec": {"mark": "line"}}`, true},
		{"notObject", `[1,2,3]`, false},
		{"notJSON", `nope`, false},
		{"missingPulse", `{"prism_spec": {"mark": "bar"}}`, false},
		{"missingPrism", `{"pulse_request": {}}`, false},
		{"prismNoMark", `{"pulse_request": {}, "prism_spec": {}}`, false},
		{"extraKey", `{"pulse_request": {}, "prism_spec": {"mark": "bar"}, "x": 1}`, false},
		{"pulseNotObject", `{"pulse_request": 5, "prism_spec": {"mark": "bar"}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTemplate([]byte(tc.in))
			if tc.ok && err != nil {
				t.Fatalf("validateTemplate(%s) = %v, want nil", tc.in, err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("validateTemplate(%s) = nil, want error", tc.in)
			}
		})
	}
}

func TestExtractTemplate(t *testing.T) {
	obj := `{"pulse_request": {}, "prism_spec": {"mark": "bar"}}`
	cases := []struct {
		name string
		in   string
		ok   bool
	}{
		{"bare", obj, true},
		{"fenced", "```json\n" + obj + "\n```", true},
		{"fencedNoLang", "```\n" + obj + "\n```", true},
		{"prose", "Sure! " + obj + " done", true},
		{"bracesInString", `{"prism_spec": {"mark": "bar"}, "pulse_request": {"note": "a } brace"}}`, true},
		{"noObject", "I cannot do that", false},
		{"unterminated", `{"pulse_request": {`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractTemplate(tc.in)
			if tc.ok {
				if err != nil {
					t.Fatalf("extractTemplate(%q) = %v, want nil", tc.in, err)
				}
				if err := validateTemplate(got); err != nil {
					t.Fatalf("extracted %q not valid: %v", got, err)
				}
			} else if err == nil {
				t.Fatalf("extractTemplate(%q) = nil error, want error", tc.in)
			}
		})
	}
}

func TestIsParameterized(t *testing.T) {
	if !isParameterized([]byte(`{"x": "${region}"}`)) {
		t.Fatal("expected parameterized")
	}
	if isParameterized([]byte(`{"x": "concrete"}`)) {
		t.Fatal("expected not parameterized")
	}
}

func TestCanonicalizePreservesPlaceholders(t *testing.T) {
	out, err := canonicalize([]byte(paramTemplate))
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	if !isParameterized(out) {
		t.Fatalf("canonical form lost placeholders: %s", out)
	}
	if err := validateTemplate(out); err != nil {
		t.Fatalf("canonical form invalid: %v", err)
	}
}
