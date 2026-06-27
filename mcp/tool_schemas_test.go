package mcp

// Typed-handler + descriptor tests for the two grammar tools (E2-S2), ported from
// the legacy internal/mcp/tools_schemas_test.go but driven WITHOUT a server. They
// confirm: list_schemas enumerates known item types plus the dashboard envelope,
// get_schema returns valid JSON for a known type (with its $id), the reserved
// "dashboard" token returns the envelope schema, and an unknown type surfaces the
// facade's SCHEMA_NOT_FOUND coded error verbatim.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// TestListSchemasRegistered asserts list_schemas is present in the Tools() catalog
// with a reflection-generated input+output schema and the legacy description.
func TestListSchemasRegistered(t *testing.T) {
	d := findDescriptor(t, "lattice_list_schemas")
	if d.Description != listSchemasDescription {
		t.Errorf("description mismatch:\n got %q\nwant %q", d.Description, listSchemasDescription)
	}
	if len(d.InputSchema) == 0 {
		t.Errorf("list_schemas has empty InputSchema (expected reflection-generated)")
	}
	if len(d.OutputSchema) == 0 {
		t.Errorf("list_schemas has empty OutputSchema (expected reflection-generated)")
	}
}

// TestListSchemas asserts list_schemas returns the grammar catalog: known item
// types (block, table, and the markdown/image/heading content types) plus the
// dashboard envelope token. It drives the descriptor's erased Invoke so the wire
// shape is exercised end to end.
func TestListSchemas(t *testing.T) {
	svc := newTestService(t)
	d := findDescriptor(t, "lattice_list_schemas")

	raw, err := d.Invoke(context.Background(), svc, nil)
	if err != nil {
		t.Fatalf("Invoke list_schemas: %v", err)
	}

	var out struct {
		Types []string `json:"types"`
	}
	remarshal(t, raw, &out)

	have := make(map[string]bool, len(out.Types))
	for _, ty := range out.Types {
		have[ty] = true
	}
	for _, want := range []string{"block", "table", "markdown", "image", "heading", "dashboard"} {
		if !have[want] {
			t.Errorf("catalog missing %q; got %v", want, out.Types)
		}
	}
}

// TestGetSchemaRegistered asserts get_schema is present in the Tools() catalog with
// a reflection-generated schema (the `any` schema field did not panic the
// reflector) and the legacy description.
func TestGetSchemaRegistered(t *testing.T) {
	d := findDescriptor(t, "lattice_get_schema")
	if d.Description != getSchemaDescription {
		t.Errorf("description mismatch:\n got %q\nwant %q", d.Description, getSchemaDescription)
	}
	if len(d.InputSchema) == 0 {
		t.Errorf("get_schema has empty InputSchema (expected reflection-generated)")
	}
	if len(d.OutputSchema) == 0 {
		t.Errorf("get_schema has empty OutputSchema (expected reflection-generated)")
	}
}

// TestGetSchemaKnownType asserts get_schema returns a valid JSON Schema for a
// known item type, with the type echoed and the schema carrying its $id.
func TestGetSchemaKnownType(t *testing.T) {
	svc := newTestService(t)
	d := findDescriptor(t, "lattice_get_schema")

	raw, err := d.Invoke(context.Background(), svc, json.RawMessage(`{"type":"block"}`))
	if err != nil {
		t.Fatalf("Invoke get_schema: %v", err)
	}

	var out struct {
		Type   string          `json:"type"`
		Schema json.RawMessage `json:"schema"`
	}
	remarshal(t, raw, &out)

	if out.Type != "block" {
		t.Errorf("type = %q, want %q", out.Type, "block")
	}
	var schema struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(out.Schema, &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	if schema.ID == "" {
		t.Errorf("schema has no $id; got %s", out.Schema)
	}
}

// TestGetSchemaDashboardEnvelope asserts the reserved "dashboard" token returns
// the envelope schema as valid JSON.
func TestGetSchemaDashboardEnvelope(t *testing.T) {
	svc := newTestService(t)

	out, err := getSchema(context.Background(), svc, getSchemaInput{Type: "dashboard"})
	if err != nil {
		t.Fatalf("getSchema(dashboard): %v", err)
	}
	b, err := json.Marshal(out.Schema)
	if err != nil {
		t.Fatalf("marshal dashboard schema: %v", err)
	}
	if !json.Valid(b) {
		t.Fatalf("dashboard schema is not valid JSON: %s", b)
	}
}

// TestGetSchemaUnknownTypeIsCodedError asserts an unknown type surfaces the
// facade's SCHEMA_NOT_FOUND coded error verbatim (not flattened to a string).
func TestGetSchemaUnknownTypeIsCodedError(t *testing.T) {
	svc := newTestService(t)

	_, err := getSchema(context.Background(), svc, getSchemaInput{Type: "no-such-type"})
	if err == nil {
		t.Fatalf("expected an error for unknown type, got success")
	}
	if !errors.HasCode(err, errors.SCHEMA_NOT_FOUND) {
		t.Errorf("error = %v, want it to carry SCHEMA_NOT_FOUND", err)
	}
}
