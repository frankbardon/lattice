package mcp

import (
	"context"
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/frankbardon/lattice/service"
)

// The two grammar tools registered here — list_schemas and get_schema — expose
// the dashboard GRAMMAR catalog so an MCP host knows what it may build, not just
// what is editable on an existing node. list_schemas enumerates the available
// item-type names plus the dashboard envelope; get_schema returns one type's
// JSON Schema (config fields, required keys, $ref form) so the model can validly
// ADD a new node of a type or author a new dashboard. Both call ONLY the
// ./service facade and surface its *errors.CodedError verbatim as MCP tool
// errors.

func init() {
	registrars = append(registrars, registerListSchemas, registerGetSchema)
}

// listSchemasInput is the input for list_schemas: it takes no arguments. AddTool
// still requires an object-typed input schema, so this is an empty struct.
type listSchemasInput struct{}

// listSchemasOutput is the structured result of list_schemas: the catalog of
// type tokens get_schema accepts.
type listSchemasOutput struct {
	// Types are the grammar type tokens: each item-type name available under the
	// schema catalog's items directory plus the reserved "dashboard" envelope
	// token. Every entry is a valid get_schema input.
	Types []string `json:"types" jsonschema:"the available grammar type tokens (item-type names plus the dashboard envelope), each accepted by get_schema"`
}

// registerListSchemas registers the list_schemas tool: it returns the grammar
// catalog via service.ListSchemas — the item-type names plus the dashboard
// envelope token — read from the same schema filesystem the resolver validates
// against. A catalog read failure surfaces the facade's SCHEMA_IO
// *errors.CodedError verbatim as a tool error.
func registerListSchemas(s *sdkmcp.Server, svc *service.Service) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "list_schemas",
		Description: "List the dashboard grammar catalog: every available item-type name plus the \"dashboard\" envelope token. Each token is a valid get_schema input — use this to discover what node types you may build.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ listSchemasInput) (*sdkmcp.CallToolResult, listSchemasOutput, error) {
		types, err := svc.ListSchemas()
		if err != nil {
			// Surface the facade's *errors.CodedError verbatim as a tool error.
			return nil, listSchemasOutput{}, err
		}
		return nil, listSchemasOutput{Types: types}, nil
	})
}

// getSchemaInput is the input for get_schema: the grammar type token whose JSON
// Schema to fetch.
type getSchemaInput struct {
	// Type is the grammar type token (an item-type name from list_schemas, or
	// "dashboard" for the envelope) whose JSON Schema to return.
	Type string `json:"type" jsonschema:"the grammar type token to fetch a schema for: an item-type name from list_schemas, or \"dashboard\" for the envelope"`
}

// getSchemaOutput is the structured result of get_schema: the requested type and
// its JSON Schema.
type getSchemaOutput struct {
	// Type echoes the requested grammar type token.
	Type string `json:"type" jsonschema:"the requested grammar type token"`

	// Schema is the type's JSON Schema as a decoded JSON value (the bytes
	// service.Schema returns, unmarshaled so the result is structured rather than
	// a string). It is typed `any` so the reflective output-schema leaves it
	// unconstrained — its shape is the JSON Schema meta-schema, not this tool's
	// contract.
	Schema any `json:"schema" jsonschema:"the type's JSON Schema (config fields, required keys, $ref form) as JSON"`
}

// registerGetSchema registers the get_schema tool: it returns one grammar type's
// JSON Schema via service.Schema, read verbatim from the schema filesystem the
// resolver validates against. An unknown type surfaces the facade's
// SCHEMA_NOT_FOUND *errors.CodedError verbatim as a tool error.
func registerGetSchema(s *sdkmcp.Server, svc *service.Service) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "get_schema",
		Description: "Fetch the JSON Schema for one grammar type (an item-type name from list_schemas, or \"dashboard\" for the envelope). Returns the schema's config fields, required keys, and $ref form so you can validly build or patch a node of that type.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, input getSchemaInput) (*sdkmcp.CallToolResult, getSchemaOutput, error) {
		raw, err := svc.Schema(input.Type)
		if err != nil {
			// Unknown type surfaces SCHEMA_NOT_FOUND verbatim as a tool error.
			return nil, getSchemaOutput{}, err
		}

		var schema any
		if err := json.Unmarshal(raw, &schema); err != nil {
			return nil, getSchemaOutput{}, err
		}
		return nil, getSchemaOutput{Type: input.Type, Schema: schema}, nil
	})
}
