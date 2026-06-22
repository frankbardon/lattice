package schema

import "github.com/google/jsonschema-go/jsonschema"

// ResolvedGraph is the in-memory, fully linked schema graph produced by the
// loader. It is the hand-off artifact consumed by E1-S4 (two-pass validation +
// resolved-tree emission). It is intentionally a thin, read-only view: the
// loader has already located every referenced item-type schema and pinned its
// versioned $id, but it does NOT validate instance config against those schemas
// (that is E1-S4's job).
//
// The shape is deliberately minimal so E1-S4 can walk the instance tree and,
// for each node, look up its item-type schema by the node's resolved $ref.
type ResolvedGraph struct {
	// Document is the parsed dashboard document (manifest + root instance tree).
	Document *Document

	// DashboardSchema is the parsed top-level dashboard schema
	// (dashboard.schema.json). E1-S4 validates Document against this in its
	// structural pass.
	DashboardSchema *jsonschema.Schema

	// Types maps a resolved item-type identifier (the canonical $id string, e.g.
	// "https://lattice.dev/schemas/items/table/1.0.0") to the resolved type
	// schema. Every instance $ref in the tree appears as a key here.
	Types map[string]*ResolvedType

	// Refs maps each instance node's pointer to the canonical type identifier
	// that its $ref resolved to. E1-S4 uses this to associate a tree node with
	// its type schema without re-running resolution. Inline ($defs) and relative
	// refs are normalized to a stable key here too.
	Refs map[*Instance]string
}

// ResolvedType is a single resolved item-type schema in the graph.
type ResolvedType struct {
	// ID is the canonical identifier the schema is keyed by. For catalog
	// (absolute-URL) and relative refs this is the schema's $id; for inline
	// $defs fragments it is the fragment URI (e.g. "#/$defs/foo").
	ID string

	// Name is the item-type name parsed from a versioned $id path
	// (e.g. "table" from ".../items/table/1.0.0"). Empty for inline fragments
	// that carry no versioned $id.
	Name string

	// Version is the semver pinned in the $id path (e.g. "1.0.0"). Empty for
	// inline fragments that carry no versioned $id.
	Version string

	// Schema is the parsed JSON Schema for the item type. E1-S4 compiles and
	// validates instance config against this.
	Schema *jsonschema.Schema

	// Source records where the schema was loaded from, for diagnostics
	// (a catalog file path, the dashboard document path, or a URL).
	Source string
}

// Document is the decoded dashboard document: a manifest plus a recursive root
// instance tree. Only the fields the resolver needs are typed; per-instance
// config/placement are preserved as raw decoded values for E1-S4.
type Document struct {
	// Defs holds the document's inline $defs, used to resolve "#/$defs/..."
	// instance refs. May be nil.
	Defs map[string]*jsonschema.Schema `json:"$defs,omitempty"`

	Manifest map[string]any `json:"manifest"`
	Root     *Instance      `json:"root"`
}

// Instance is one node in the recursive item-instance tree.
type Instance struct {
	// Ref is the raw item-type reference as written in the document. The loader
	// resolves it; the canonical resolved identifier is recorded in
	// ResolvedGraph.Refs.
	Ref string `json:"$ref"`

	// ID is the optional instance-local identifier.
	ID string `json:"id,omitempty"`

	// Config is opaque per-instance configuration, validated by E1-S4 against the
	// resolved item-type schema.
	Config map[string]any `json:"config,omitempty"`

	// Placement is optional layout hints, opaque at this stage.
	Placement map[string]any `json:"placement,omitempty"`

	// Metadata is the optional per-element metadata map (element-metadata E1):
	// a freeform map of non-empty keys to SCALAR values. Structurally permitted
	// on every instance (the envelope is shared), but the resolver enforces both
	// eligibility (which node kinds may carry it) and the scalar-value rule. Nil
	// when the instance declared no metadata.
	Metadata map[string]any `json:"metadata,omitempty"`

	// Children are nested instances. Structurally allowed on any node; the
	// container-only-children rule is enforced by E1-S4.
	Children []*Instance `json:"children,omitempty"`
}
