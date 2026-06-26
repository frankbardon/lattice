// Package mcp is the SDK-free core of lattice's Model Context Protocol surface:
// the transport-agnostic home for tool definitions and their typed contracts.
//
// THE INSULATION. This package deliberately does NOT import the MCP SDK
// (github.com/modelcontextprotocol/go-sdk). It depends only on the public
// service facade, the schema-reflection library (github.com/google/jsonschema-go,
// a module separate from the protocol SDK), and the module-root errors package.
// A consumer can therefore pull in lattice's tool catalog — names, descriptions,
// JSON Schemas, and invocable handlers — without ever coupling to a particular
// protocol-SDK version. The SDK coupling lives in exactly one adapter package
// (mcp/gosdk, added downstream) which mounts these descriptors onto a real
// server. The firewall is enforced by a test (firewall_test.go); breaking it
// is a build-blocking regression, not a style nit.
//
// THE SEAM. Tools are described by ToolDescriptor values and produced by the
// Tools(cfg) catalog constructor. cfg carries the runtime configuration (the
// build version, at least) that version-reporting tools close over — this is how
// the historical serverVersion process-global is retired. Each tool's typed
// handler is wrapped (via NewTool) into an erased Invoke so a transport adapter
// can dispatch any tool uniformly off raw JSON without knowing its In/Out types.
package mcp

import (
	"context"
	"encoding/json"

	"github.com/frankbardon/lattice/service"
)

// Config is the runtime configuration the catalog constructor bakes into the
// tools it builds. It is intentionally minimal: it carries only what a tool must
// close over at construction time (the build version today), replacing the
// process-global the legacy MCP layer used to thread.
type Config struct {
	// Version is the lattice build version reported by version-aware tools
	// (e.g. the manifest). It is baked into the relevant descriptors when
	// Tools constructs the catalog.
	Version string
}

// ToolDescriptor is the SDK-free description of a single MCP tool: its identity,
// its reflected input/output JSON Schemas, and a type-erased Invoke that runs the
// tool against the service facade. A transport adapter turns each descriptor into
// a concrete server registration; nothing here names the protocol SDK.
type ToolDescriptor struct {
	// Name is the tool's stable MCP name (snake_case, e.g. "get_outline").
	Name string

	// Description is the one-paragraph human/LLM-facing summary shown by hosts.
	Description string

	// InputSchema is the JSON Schema reflected from the tool's typed input
	// struct (see NewTool). It is the raw, already-marshaled schema document.
	InputSchema json.RawMessage

	// OutputSchema is the JSON Schema reflected from the tool's typed output
	// struct. Recursive output fields are typed `any` in their Go struct so the
	// reflector does not panic on a type cycle (see reflectSchema).
	OutputSchema json.RawMessage

	// Invoke runs the tool: it unmarshals args into the tool's input type, calls
	// the typed handler against svc, and returns the output as `any`. It surfaces
	// the facade's *errors.CodedError verbatim — handlers never flatten errors to
	// strings, and Invoke does not wrap them (save for a malformed-args envelope).
	Invoke func(ctx context.Context, svc *service.Service, args json.RawMessage) (any, error)
}

// Tools returns the tool catalog for the given runtime configuration. It is the
// single seam every tool registers into: E2 stories append their descriptors
// here, each built with NewTool. For now the catalog is empty — but never nil —
// so a transport adapter can range over it unconditionally.
func Tools(cfg Config) []ToolDescriptor {
	// Non-nil empty slice: a zero-tool catalog is a valid, rangeable result, and
	// callers must not have to nil-check before iterating.
	descriptors := make([]ToolDescriptor, 0)

	// Navigation/read tools (E2-S1). Names and descriptions match the legacy
	// internal/mcp registrations so the downstream catalog text holds parity.
	descriptors = append(descriptors,
		NewTool("get_outline", getOutlineDescription, getOutline),
		NewTool("get_node", getNodeDescription, getNode),
	)

	// Grammar + truth tools (E2-S2). list_schemas/get_schema expose the dashboard
	// grammar catalog; validate_patch is the never-persists dry-run. Names and
	// descriptions match the legacy internal/mcp registrations for catalog parity.
	descriptors = append(descriptors,
		NewTool("list_schemas", listSchemasDescription, listSchemas),
		NewTool("get_schema", getSchemaDescription, getSchema),
		NewTool("validate_patch", validatePatchDescription, validatePatch),
	)

	// Skill-pack tools (E2-S3). list_skills/get_skill expose the embedded skill
	// corpus (mcp/skills); they read pure embedded data and ignore the service
	// facade. Names and descriptions match the legacy internal/mcp registrations
	// for catalog parity.
	descriptors = append(descriptors,
		NewTool("list_skills", listSkillsDescription, listSkills),
		NewTool("get_skill", getSkillDescription, getSkill),
	)

	_ = cfg

	return descriptors
}
