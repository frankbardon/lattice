package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/service"
)

// NewTool builds a ToolDescriptor from a typed handler, reflecting the In and Out
// struct types into JSON Schemas and wrapping the handler into the erased Invoke.
// This is the mechanical seam E2 tool stories use: define a typed
// func(ctx, *service.Service, In) (Out, error), then register
// NewTool(name, desc, handler) into Tools.
//
// Invoke unmarshals raw args into In, calls the handler, and returns Out as `any`.
// A handler's *errors.CodedError is surfaced verbatim (never wrapped or flattened)
// so the facade's coded-error vocabulary reaches the host intact; only a malformed
// args payload is turned into an MCP_INVALID envelope before the handler runs.
//
// Recursive Out fields MUST be typed `any` in their Go struct — the reflector
// panics (via reflectSchema) on a type cycle, the same constraint the SDK's
// reflector imposes (see reflectSchema and tool_outline.go).
func NewTool[In, Out any](name, desc string, h func(context.Context, *service.Service, In) (Out, error)) ToolDescriptor {
	return ToolDescriptor{
		Name:         name,
		Description:  desc,
		InputSchema:  reflectSchema[In](),
		OutputSchema: reflectSchema[Out](),
		Invoke: func(ctx context.Context, svc *service.Service, args json.RawMessage) (any, error) {
			var in In
			// An empty payload leaves In zero-valued (a tool with no required
			// inputs is callable with no args); a present-but-malformed payload is
			// the caller's error and surfaces as MCP_INVALID.
			if len(args) > 0 {
				if err := json.Unmarshal(args, &in); err != nil {
					return nil, errors.WrapCodedError(err, errors.MCP_INVALID, fmt.Sprintf("invalid arguments for tool %q", name))
				}
			}
			out, err := h(ctx, svc, in)
			if err != nil {
				// Verbatim: the handler already returns a *errors.CodedError.
				return nil, err
			}
			return out, nil
		},
	}
}

// reflectSchema reflects a Go struct type into its JSON Schema document via
// jsonschema-go and returns the marshaled schema. It panics on any reflection or
// marshaling failure: both indicate a programming error baked into a tool's
// In/Out type at construction time, not a runtime condition.
//
// The reflector returns an error on a type cycle (a Go type recursive through
// itself). The convention — matching the MCP SDK's reflector — is to type such
// recursive/nested fields as `any`, which jsonschema-go treats as an unconstrained
// schema, breaking the cycle. A panic here means a tool author left a recursive
// field typed concretely; the fix is to retype it `any`.
func reflectSchema[T any]() json.RawMessage {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		// A type cycle or otherwise unrepresentable field type — fix the Go type
		// (type recursive fields as `any`), do not recover at runtime.
		panic(fmt.Sprintf("mcp: reflecting schema for %T: %v", *new(T), err))
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("mcp: marshaling reflected schema for %T: %v", *new(T), err))
	}
	return raw
}
