package resolver

import (
	"github.com/frankbardon/lattice/internal/layout"
	"github.com/frankbardon/lattice/internal/variables"
)

// This file defines the RESOLVED TREE: the durable, JSON-serializable contract
// that E1-S4 emits and that three downstream epics consume unchanged —
//   - E2 (visual sketch)  walks the tree to render structure,
//   - E3 (variables)      attaches variable scopes/bindings to nodes,
//   - E4 (connections)    binds data sources to nodes.
//
// Keep this shape STABLE. Additive, backward-compatible fields are fine;
// renaming or removing fields breaks downstream consumers. Every exported field
// carries a JSON tag so the serialized form is the real contract (not the Go
// field names).

// ResolvedTree is the top-level output of resolution: the document manifest plus
// the recursively resolved root instance. It is produced only after BOTH
// validation passes succeed, so consumers may assume every node is structurally
// valid and type-checked.
type ResolvedTree struct {
	// Manifest is the document's verbatim manifest object (formatVersion, id,
	// title, and any optional metadata). It is passed through unchanged from the
	// source document.
	Manifest map[string]any `json:"manifest"`

	// Root is the resolved root instance. It is never nil in a successful result.
	Root *ResolvedInstance `json:"root"`

	// Connections are the document-scoped data connections, in declaration order,
	// each validated against its connection-type schema (E4-S1). Connections are
	// declared and validated only, never dialed (no live fetch). Empty/omitted
	// when the document declares no connections.
	Connections []*ResolvedConnection `json:"connections,omitempty"`
}

// ResolvedInstance is one node of the resolved tree: a single item instance with
// its type reference fully resolved to a canonical, versioned type identity.
//
// The distinction from schema.Instance (the raw parsed node) is deliberate:
// schema.Instance mirrors the on-disk JSON, whereas ResolvedInstance is the
// post-resolution view that records the *resolved* type identity and guarantees
// the instance has passed config validation and the container-only-children
// rule. Downstream epics build on ResolvedInstance, not the raw node.
type ResolvedInstance struct {
	// ID is the optional instance-local identifier copied from the document.
	// Empty when the instance declared no id.
	ID string `json:"id,omitempty"`

	// Type is the resolved item type this instance is an instance of: its
	// canonical identifier plus the parsed name/version. This is the stable hook
	// downstream code uses to dispatch on item type (e.g. container vs table).
	Type ResolvedTypeRef `json:"type"`

	// Container reports whether this node's resolved type is a container, i.e.
	// the only type permitted to have children. Surfaced explicitly so consumers
	// (notably E2's sketch) do not have to re-derive it from the type name.
	Container bool `json:"container"`

	// Config is the instance's verbatim, schema-validated configuration object.
	// Its structure is defined by the item type's schema; it is opaque here.
	// Nil when the instance declared no config.
	Config map[string]any `json:"config,omitempty"`

	// Placement is the instance's verbatim placement hints, passed through
	// unchanged. Opaque at this stage; E2-S1 formalizes track placement.
	// Nil when the instance declared no placement.
	Placement map[string]any `json:"placement,omitempty"`

	// Layout is the normalized, renderer-agnostic grid layout for a container
	// node (E2-S1): fractional track sizes plus each child's validated, 1-indexed
	// placement. Non-nil only for container nodes; nil for leaf item types.
	Layout *layout.Block `json:"layout,omitempty"`

	// Children are the resolved child instances, in document order. Always empty
	// (never nil-vs-non-nil significant) for non-container types, since the
	// container-only-children rule is enforced before the tree is assembled.
	Children []*ResolvedInstance `json:"children,omitempty"`

	// VarEnv is the variable environment VISIBLE at this node (E3-S1): every
	// variable name in scope mapped to its shadowing-winner declaration plus the
	// path of the node that declared it. Computed by a dedicated pass after the
	// tree is assembled (see variables.go). Omitted when no variables are in
	// scope. Each entry's DeclaredAt exposes var->node visibility so downstream
	// consumers and a future dependency tracker can scope re-resolution.
	VarEnv variables.Environment `json:"varEnv,omitempty"`
}

// ResolvedTypeRef is the resolved identity of an item type as referenced by an
// instance. It records both the canonical identifier the instance's $ref
// resolved to and the parsed name/version, so consumers never have to re-parse
// the identifier string.
type ResolvedTypeRef struct {
	// Ref is the raw $ref string exactly as written in the source document
	// (e.g. "https://lattice.dev/schemas/items/table/1.0.0" or "#/$defs/badge").
	// Preserved for diagnostics and round-tripping.
	Ref string `json:"ref"`

	// ID is the canonical identifier the ref resolved to. For catalog and
	// relative refs this is the schema's $id; for inline $defs fragments it is
	// the fragment URI. This is the key into the schema graph's type table.
	ID string `json:"id"`

	// Name is the item-type name parsed from a versioned $id (e.g. "table").
	// Empty for inline fragments that carry no versioned $id.
	Name string `json:"name,omitempty"`

	// Version is the semver pinned in the $id path (e.g. "1.0.0"). Empty for
	// inline fragments that carry no versioned $id.
	Version string `json:"version,omitempty"`
}
