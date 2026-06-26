package mcp

import (
	"context"

	"github.com/frankbardon/lattice/mcp/skills"
	"github.com/frankbardon/lattice/service"
)

// The get_manifest tool defined here is the "CALL FIRST" bootstrap: in a single
// call it orients a consuming LLM in the lattice surface — server identity + build
// version, the tool catalog, the item-type grammar (via the ./service facade), the
// connection types a dashboard can wire, the slim skills index (from the embedded
// corpus, so it stays in sync), and the read/simulate-vs-write capability split. It
// is the recommended first call: a host reads it once, then follows up with the
// targeted tools (get_schema, get_skill, …) it names.
//
// Unlike the legacy internal/mcp registration, the tool catalog this manifest
// advertises is DERIVED from the live descriptors Tools(cfg) builds (see
// newManifestDescriptor) rather than a hand-kept slice, so tool/manifest drift is
// impossible — and the build version comes from cfg, not a process-global. It calls
// ONLY the ./service facade (for the item-type catalog) and the pure embedded
// skills package; everything else is static or derived. A catalog read failure
// surfaces the facade's *errors.CodedError verbatim as a tool error.

// serverName is the MCP implementation name the manifest advertises. It mirrors the
// name the transport adapter advertises in the initialize handshake, so a host sees
// one identity. (The legacy layer kept this in server.go; the SDK-free core owns it
// here because version-and-identity reporting is a tool concern, not a transport one.)
const serverName = "lattice"

// patchWriteEndpoint is the out-of-band HTTP route that commits a validated
// changeset (the only persist path). MCP never calls it — a human applies the
// patch — but the manifest names it so the model knows where writes land.
const patchWriteEndpoint = "POST /api/patch"

// manifestToolEntry is one entry in the manifest's tool catalog: a registered MCP
// tool's name and one-line description. Unlike the legacy hand-kept slice, the
// manifest's catalog is DERIVED from the live descriptors Tools(cfg) returns (see
// newManifestDescriptor), so it can never drift from the real tool surface.
type manifestToolEntry struct {
	// Name is the tool's MCP name (the CallTool name a host invokes).
	Name string `json:"name" jsonschema:"the tool's MCP name"`
	// Description is a one-line summary of what the tool does.
	Description string `json:"description" jsonschema:"a one-line summary of the tool"`
}

// manifestConnectionType is one entry in the manifest's connection-type catalog: a
// data-source kind a dashboard node can wire. The set is known and small, so it is
// a static slice rather than derived.
type manifestConnectionType struct {
	// Name is the connection type's token (e.g. "http", "static").
	Name string `json:"name" jsonschema:"the connection type token"`
	// Description is a one-line summary of the connection type.
	Description string `json:"description" jsonschema:"a one-line summary of the connection type"`
}

// manifestSkillEntry is one entry in the manifest's slim skills index: a skill's
// name + description only (not its full frontmatter), so a host learns what guidance
// exists and can follow up with get_skill. It is derived from skills.List, so adding
// a skill file updates the index automatically.
type manifestSkillEntry struct {
	// Name is the skill's name (the get_skill argument).
	Name string `json:"name" jsonschema:"the skill's name (the get_skill argument)"`
	// Description is the skill's one-line frontmatter description.
	Description string `json:"description" jsonschema:"the skill's one-line description"`
}

// manifestCapabilities is the manifest's capability split: what the MCP surface can
// do directly (read + simulate, never persisting) versus how a write is actually
// committed (the out-of-band HTTP patch endpoint a human drives).
type manifestCapabilities struct {
	// Read is true: every read tool (get_outline, get_node, list_schemas,
	// get_schema) serves stored state directly.
	Read bool `json:"read" jsonschema:"the MCP surface can read stored dashboards"`
	// Simulate is true: validate_patch dry-runs a changeset (apply→validate) and
	// returns the result without persisting anything.
	Simulate bool `json:"simulate" jsonschema:"the MCP surface can simulate (dry-run) a patch without persisting"`
	// Persist is false: MCP NEVER persists. A write is committed only by POSTing
	// the validated changeset to WriteEndpoint, which a human drives.
	Persist bool `json:"persist" jsonschema:"the MCP surface never persists writes (always false)"`
	// WriteEndpoint names the out-of-band HTTP route that actually commits a write
	// (the only persist path), so the model knows where a human applies a patch.
	WriteEndpoint string `json:"writeEndpoint" jsonschema:"the HTTP route that commits a write (the only persist path; MCP never calls it)"`
}

// getManifestInput is the input for get_manifest: it takes no arguments. NewTool
// still reflects an object-typed input schema, so this is an empty struct.
type getManifestInput struct{}

// getManifestOutput is the structured result of get_manifest: the full bootstrap
// payload. Every field is a flat (non-recursive) struct or slice of one, so the
// reflective output-schema generator handles it directly — no `any` escape hatch is
// needed here.
type getManifestOutput struct {
	// Server is the MCP implementation name advertised in the handshake.
	Server string `json:"server" jsonschema:"the MCP server name"`
	// Version is the lattice build version this catalog was constructed with (from
	// Config.Version), not a process-global.
	Version string `json:"version" jsonschema:"the lattice build version"`
	// Tools is the catalog of registered MCP tools with one-line descriptions,
	// derived from the live descriptors Tools(cfg) built.
	Tools []manifestToolEntry `json:"tools" jsonschema:"the registered MCP tools, each with a one-line description"`
	// ItemTypes are the dashboard grammar's item-type tokens (plus the dashboard
	// envelope) — what a host may build — read from the schema catalog via the
	// service facade. Each is a valid get_schema input.
	ItemTypes []string `json:"itemTypes" jsonschema:"the dashboard grammar item-type tokens (plus the dashboard envelope), each a valid get_schema input"`
	// ConnectionTypes are the data-source kinds a dashboard node can wire.
	ConnectionTypes []manifestConnectionType `json:"connectionTypes" jsonschema:"the connection types a dashboard node can wire"`
	// Skills is the slim skills index (name + description) from the embedded corpus;
	// follow up with get_skill for a skill's body.
	Skills []manifestSkillEntry `json:"skills" jsonschema:"the slim skills index (name + description); fetch a body with get_skill"`
	// Capabilities is the read/simulate-vs-persist split: MCP reads and simulates
	// but never persists; a write is committed out of band via the HTTP endpoint.
	Capabilities manifestCapabilities `json:"capabilities" jsonschema:"the capability split: MCP reads and simulates but never persists; writes commit via the HTTP endpoint"`
}

// manifestConnectionTypes is the known catalog of connection types a dashboard node
// can wire. The set is small and stable, so it is a static slice; extend it here
// when a new connection kind ships.
var manifestConnectionTypes = []manifestConnectionType{
	{"http", "Fetch data from an HTTP(S) endpoint at resolve time."},
	{"static", "Inline literal data embedded in the document — no external fetch."},
}

// getManifestDescription is the get_manifest tool description, kept identical to the
// legacy registration so downstream catalog text holds parity.
const getManifestDescription = "CALL FIRST. Returns the lattice bootstrap manifest in one call: server name + version, the full tool catalog, the item types you can build (via the schema catalog), connection types, a slim skills index (fetch bodies with get_skill), and the capability split — MCP reads and simulates but never persists; writes commit out of band via " + patchWriteEndpoint + "."

// newManifestDescriptor builds the get_manifest descriptor for a given runtime
// configuration. It is the seam that retires the legacy hand-kept manifestToolCatalog
// slice: instead of restating the tool surface, the manifest's tool list is DERIVED
// from the descriptors Tools(cfg) already built (catalog), snapshotted into the
// handler's closure so a call needs no live catalog reference.
//
// catalog is the other tools Tools(cfg) constructed BEFORE get_manifest is appended;
// get_manifest is itself a catalog tool, so its own {name, description} is added
// explicitly here (matching the legacy self-including catalog). The resulting Tools
// list is therefore exactly the tools Tools(cfg) returns — the anti-drift guarantee.
//
// The handler closes over the snapshot and cfg.Version (the version no longer comes
// from a process-global). The closure-over-catalog shape fits NewTool's plain-func
// handler signature directly, so no manual ToolDescriptor construction is needed.
func newManifestDescriptor(catalog []ToolDescriptor, cfg Config) ToolDescriptor {
	// Snapshot the live catalog's {name, description} so the manifest's tool list is
	// derived, not hand-kept. Appended in catalog order; get_manifest's own entry
	// goes last, so this matches the final Tools(cfg) ordering exactly.
	entries := make([]manifestToolEntry, 0, len(catalog)+1)
	for _, d := range catalog {
		entries = append(entries, manifestToolEntry{Name: d.Name, Description: d.Description})
	}
	entries = append(entries, manifestToolEntry{Name: "get_manifest", Description: getManifestDescription})

	// Bake the build version from cfg into the closure, retiring the legacy
	// serverVersion process-global.
	version := cfg.Version

	handler := func(_ context.Context, svc *service.Service, _ getManifestInput) (getManifestOutput, error) {
		itemTypes, err := svc.ListSchemas()
		if err != nil {
			// The item-type catalog read is the only failure path; surface the
			// facade's SCHEMA_IO *errors.CodedError verbatim as a tool error.
			return getManifestOutput{}, err
		}

		// Slim the embedded skills corpus to a name + description index, so a host
		// learns what guidance exists without the full frontmatter. skills.List is
		// already sorted by name.
		index := skills.List()
		skillEntries := make([]manifestSkillEntry, 0, len(index))
		for _, meta := range index {
			skillEntries = append(skillEntries, manifestSkillEntry{
				Name:        meta.Name,
				Description: meta.Description,
			})
		}

		return getManifestOutput{
			Server:          serverName,
			Version:         version,
			Tools:           entries,
			ItemTypes:       itemTypes,
			ConnectionTypes: manifestConnectionTypes,
			Skills:          skillEntries,
			Capabilities: manifestCapabilities{
				Read:          true,
				Simulate:      true,
				Persist:       false,
				WriteEndpoint: patchWriteEndpoint,
			},
		}, nil
	}

	return NewTool("get_manifest", getManifestDescription, handler)
}
