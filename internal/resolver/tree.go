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

	// DefaultTheme is the document-scope DEFAULT THEME (E2-S2): the document's base
	// presentation choices, drawn from the shared theme vocabulary and passed
	// through verbatim. Its tokens have already been constrained to the vocabulary
	// by the structural pass, so consumers may treat every present token as valid.
	// It is the DEFAULT LAYER ONLY — per-block theme overrides (E2-S3) are emitted
	// on their own block nodes and are NOT merged into it here; a downstream
	// consumer composes the cascade. Nil/omitted when the document declares no
	// default theme.
	DefaultTheme map[string]any `json:"defaultTheme,omitempty"`
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

	// Flow is the normalized, renderer-agnostic FLOW layout for a `form` node
	// (E2-S1): the column count the form flows its widget children into plus each
	// child's computed (column, row) cell. It is a representation parallel to
	// Layout/Block, not a grid — form widgets are packed into compact label+control
	// cells and do not consume a main-grid placement. Non-nil only for form nodes;
	// nil for plain containers and leaf item types.
	Flow *layout.Flow `json:"flow,omitempty"`

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

	// Binding is this item's resolved data binding (E4-S2): the document-scoped
	// connection it draws from plus its own variable-filled query. Non-nil only
	// for items that declared a connectionId; nil for unbound items. Computed by a
	// dedicated pass after the tree is assembled and connections are resolved (see
	// binding.go).
	Binding *ResolvedBinding `json:"binding,omitempty"`

	// Generated is the auto-generated editor form a `configurator` produces from
	// its target's configurable surface (E5-S2): one widget per surface field, laid
	// out via the form flow layout, each widget bound to a `<target-id>.<field>`
	// config override so viewer interaction mutates the target ephemerally. Non-nil
	// only for configurator nodes whose target declares a configurable surface; nil
	// for every other item type (and for a configurator targeting a surface-less
	// item). See configurator.go.
	Generated *GeneratedForm `json:"generated,omitempty"`

	// Surface is this item's CONFIGURABLE SURFACE (E3-S1): the validated set of
	// config fields the item type declares as runtime-configurable, each with its
	// value type, label, optional constraints, and optional preferred widget
	// rendering. It is derived from the item type's schema-level `configurable`
	// keyword (not per-instance config) and validated by a dedicated pass after
	// the tree is assembled (see surface.go). Surfacing it on the resolved
	// instance lets E4 (config overrides) and E5 (configurator auto-generation)
	// read the surface without re-parsing the item-type schema. The slice is in
	// declared field order; omitted when the item type declares no surface.
	Surface []ConfigurableField `json:"surface,omitempty"`
}

// ConfigurableField is one entry of an item type's configurable surface (E3-S1):
// it maps a single config field of the item type to the metadata a configurator
// needs to render an editor for it. The field name is validated against the item
// type's own config schema, the type against the variable type set, and the
// optional rendering hint against the widget catalog, so every ConfigurableField
// on a ResolvedInstance is known-valid.
type ConfigurableField struct {
	// Field is the name of the item type's config property this entry exposes.
	// Guaranteed to be a real config property of the item type (validated against
	// the item-type schema's properties).
	Field string `json:"field"`

	// Type is the field's value type, one of the variable type set (string,
	// number, integer, boolean, enum, array). It tells a configurator which editor
	// primitive the field needs.
	Type variables.VarType `json:"type"`

	// Label is the human-readable label a configurator renders for the field.
	Label string `json:"label"`

	// Constraints is the optional, opaque constraint object declared for the field
	// (e.g. min/max/options). It is passed through verbatim; its interpretation is
	// left to the configurator. Nil when the field declared no constraints.
	Constraints map[string]any `json:"constraints,omitempty"`

	// Rendering is the optional preferred widget item-type name a configurator
	// should use to render the field's editor (e.g. "slider" for a number field).
	// When present it is guaranteed to name a registered widget family. Empty when
	// the field declared no rendering preference.
	Rendering string `json:"rendering,omitempty"`
}

// GeneratedForm is the editor a `configurator` auto-generates from its target's
// configurable surface (E5-S2). It carries the resolved target's id, one
// GeneratedWidget per surface field (in surface order), and the form flow layout
// the widgets are arranged into (reusing E2-S1's flow representation). The whole
// form is derived purely from the target's surface — there is no per-field
// authoring on the configurator — so a configurator's editor always faithfully
// mirrors what its target declares as runtime-configurable.
type GeneratedForm struct {
	// Target is the stable instance id of the item this form edits. It is the
	// node-id half of every generated widget's `<target-id>.<field>` config-override
	// address; surfacing it once keeps the form self-describing.
	Target string `json:"target"`

	// Widgets are the generated controls, one per configurable surface field of the
	// target, in surface (sorted field) order. Empty when the target declares no
	// configurable surface — a configurator may target a surface-less item, which
	// yields an empty (but present) form.
	Widgets []GeneratedWidget `json:"widgets"`

	// Flow is the normalized flow layout the widgets are arranged into (E2-S1),
	// computed via the same layout.NormalizeFlow path a `form` node uses, so a
	// renderer lays the generated controls out exactly like an authored form. The
	// cell count equals len(Widgets).
	Flow *layout.Flow `json:"flow,omitempty"`
}

// GeneratedWidget is one control of a configurator's generated form (E5-S2): the
// editor for a single configurable field of the target. It records which widget
// item-type renders it (the surface field's preferred rendering, else the
// canonical widget for the field's value type), the field's value type and label,
// the field's opaque constraints (passed through verbatim so the renderer can
// honor option sets, min/max, etc.), and — crucially — the override binding the
// renderer posts when the viewer interacts: target id + field. A change to this
// widget produces a `<Target>.<Field>` config override (E4-S2) that re-resolves
// the target ephemerally.
type GeneratedWidget struct {
	// Widget is the widget item-type name that renders this control (e.g.
	// "text-input", "select"). It is the surface field's preferred `rendering` when
	// present, else the canonical widget for the field's value type. Guaranteed to
	// name a registered widget family.
	Widget string `json:"widget"`

	// Target is the id of the item this widget configures — the node-id half of the
	// config-override address. Mirrors the enclosing form's Target on every widget so
	// each control is independently self-describing for the renderer.
	Target string `json:"target"`

	// Field is the target's config field this widget edits — the field half of the
	// `<target-id>.<field>` config-override address. It is a real top-level config
	// property of the target item type (it came from the target's validated surface).
	Field string `json:"field"`

	// Type is the field's value type (one of the variable type set). It tells a
	// renderer which input primitive the widget needs and how to coerce the posted
	// value.
	Type variables.VarType `json:"type"`

	// Label is the human-readable label rendered for the control, taken from the
	// surface field's label (falling back to the field name when the surface
	// declared none).
	Label string `json:"label"`

	// Constraints is the surface field's opaque constraint object (enum options,
	// min/max, item shape, …), passed through verbatim so the renderer can present
	// the control faithfully. Nil when the field declared no constraints.
	Constraints map[string]any `json:"constraints,omitempty"`
}

// ResolvedBinding is an item's direct data-flow binding (E4-S2): a reference to a
// document-scoped connection (already validated to exist) plus the item's own
// query whose parameters have been filled from the variables in scope at the
// item (the E3-S2 interpolation pass runs on the item config before binding, so
// Query carries concrete, typed values rather than $var/${} references). The
// binding is declared and resolved only; no live fetch happens this effort.
type ResolvedBinding struct {
	// ConnectionID is the id of the document-scoped connection this item binds to.
	// Guaranteed to match a ResolvedTree.Connections entry after resolution.
	ConnectionID string `json:"connectionId"`

	// Query is the item's variable-filled query object, passed through after
	// interpolation. Its structure is opaque to the resolver (the connection type
	// interprets it). Nil when the item declared a connection but no query.
	Query map[string]any `json:"query,omitempty"`

	// Contract is this item's result-shape contract (E4-S3): the item type, the
	// connection it draws from, and the JSON Schema fragment describing the result
	// rows/fields the item expects. Non-nil for every bound item whose item type
	// declares an expectedResult; the resolver has already verified the fragment
	// is well-formed (and, for static connections, that the inline data conforms).
	// Nil only when the item type declares no expectedResult.
	Contract *ResolvedContract `json:"contract,omitempty"`
}

// ResolvedContract is the result-shape contract between a bound item and its
// connection (E4-S3): the declared, well-formed JSON Schema fragment describing
// the rows/fields the item expects from its connection, recorded alongside the
// item type and connection id it ties together. It is model-only — the resolver
// validates the declared shape, not live fetched data. The one place a real data
// check happens is a static connection, whose inline rows the resolver validates
// against ExpectedResult before resolution succeeds; this struct still records
// only the declared contract, never the fetched data.
type ResolvedContract struct {
	// ItemType is the resolved item-type name that declared the contract (e.g.
	// "table"). Records WHICH item type's expectedResult shapes this binding.
	ItemType string `json:"itemType"`

	// ConnectionID is the id of the connection the contract applies to. Mirrors
	// the enclosing binding's ConnectionID; surfaced here so the contract is
	// self-describing.
	ConnectionID string `json:"connectionId"`

	// ExpectedResult is the item type's declared result-shape JSON Schema
	// fragment, passed through verbatim. The resolver has compiled it to confirm
	// it is well-formed; consumers may treat it as a valid draft 2020-12 schema.
	ExpectedResult map[string]any `json:"expectedResult"`
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
