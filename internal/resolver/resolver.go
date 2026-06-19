// Package resolver performs two-pass validation of a dashboard document and
// emits the resolved tree (see tree.go for the emitted contract).
//
// The two passes are:
//
//	Pass 1 (structural): the whole document validates against the dashboard
//	  JSON Schema. Failure -> RESOLVE_DOCUMENT_INVALID.
//	Pass 2 (per-instance): for each instance, the resolver layers its scoped
//	  variable environment (E3-S1), interpolates variable references in its config
//	  (E3-S2: $var typed bindings + ${} string templates), then validates the
//	  INTERPOLATED config against its resolved item-type schema and enforces the
//	  container-only-children rule. Failure -> VAR_UNDEFINED (missing reference) /
//	  RESOLVE_CONFIG_INVALID / RESOLVE_CHILDREN_NOT_ALLOWED, naming the offending
//	  instance path (e.g. "root.children[2]").
//
// Interpolation runs BEFORE config validation on purpose: a raw {"$var":…} node
// would not satisfy an item-type schema expecting the concrete value, so the
// scoped environment must be available during the instance walk (it is computed
// per node there and attached as VarEnv), not after tree assembly.
//
// Resolution is FAIL-FAST: the first error stops the walk and is returned as a
// CodedError; errors are never aggregated.
package resolver

import (
	"encoding/json"
	"strconv"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spf13/afero"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/schema"
	"github.com/frankbardon/lattice/internal/variables"
)

// containerTypeName is the weighted-grid container item-type name. It and the
// form item-type are the structurally-special types permitted to carry children
// (see schemas/items/container and schemas/items/form).
const containerTypeName = "container"

// formTypeName is the form item-type name (E2-S1): a container-like type that
// packs variable-widget children into a compact flow layout (label+control rows,
// optionally split into N columns) rather than the weighted-grid Block. A form
// may carry children but is NOT a grid container — its children resolve into a
// parallel layout.Flow attached to the node, and they do not consume main-grid
// placements.
const formTypeName = "form"

// Resolver validates dashboard documents and emits resolved trees. It wraps the
// schema loader (which links every instance $ref to its item-type schema) and
// adds the two validation passes plus tree assembly.
type Resolver struct {
	fs           afero.Fs
	loader       *schema.Loader
	dashSch      *jsonschema.Schema
	dashResolved *jsonschema.Resolved // lazily compiled in Pass 1
}

// New constructs a Resolver that loads schemas from the given catalog
// directories on fs and validates documents against dashboardSchema.
//
// dashboardSchema is the parsed top-level dashboard schema; it is both carried
// into the graph and compiled for the structural pass. catalogDirs are scanned
// (recursively) for *.schema.json item-type definitions. relRoots, if any, are
// extra directories against which relative instance $refs are resolved as files.
func New(fs afero.Fs, dashboardSchema *jsonschema.Schema, catalogDirs []string, relRoots ...string) (*Resolver, error) {
	cat, err := schema.NewCatalog(fs, catalogDirs...)
	if err != nil {
		return nil, err
	}
	loader := schema.NewLoader(fs, cat, dashboardSchema, schema.WithRelativeRoots(relRoots...))
	return &Resolver{
		fs:      fs,
		loader:  loader,
		dashSch: dashboardSchema,
	}, nil
}

// Resolve loads, validates (two passes), and assembles the resolved tree for the
// dashboard document at docPath. It returns the first error as a CodedError.
func (r *Resolver) Resolve(docPath string) (*ResolvedTree, error) {
	return r.ResolveWithValues(docPath, nil)
}

// ResolveWithValues is Resolve with E4-S1 runtime overrides: a UNIFIED,
// addressable override set whose entries are keyed by ADDRESS. A bare name
// ("region") addresses a settable variable (the E3-S4 path); a "<node-id>.<field>"
// address targets a node config field (carried for E4-S2). Variable-addressed
// values are applied as override > default to settable variables of those names;
// computed variables remain computed. A nil/empty set is identical to Resolve, so
// the resolved-tree contract is unchanged; the only difference is which value a
// settable variable carries. A variable override for an undeclared name is a
// no-op (no scope declares it); a bad override value fails fast with the same
// VAR_* codes a bad default would; a malformed address fails fast with
// VAR_OVERRIDE_INVALID.
func (r *Resolver) ResolveWithValues(docPath string, overrides variables.OverrideSet) (*ResolvedTree, error) {
	data, err := afero.ReadFile(r.fs, docPath)
	if err != nil {
		return nil, errors.WrapCodedError(err, errors.RESOLVE_IO, "failed reading dashboard document "+docPath)
	}
	return r.resolveBytes(data, docPath, overrides)
}

// resolveBytes runs the full pipeline against raw document bytes. Split out so
// tests can drive resolution without touching the filesystem for the document.
func (r *Resolver) resolveBytes(data []byte, source string, overrides variables.OverrideSet) (*ResolvedTree, error) {
	// E4-S1/S2: classify the unified override set by address. The variable subset
	// (bare names) feeds the scope walk exactly as the E3-S4 Overrides map did; the
	// node+field subset ("<node-id>.<field>") is applied post-resolution by the
	// config-override pass (E4-S2) once each node carries its interpolated config
	// and validated surface. A malformed address fails fast.
	varOverrides, err := overrides.VariableOverrides()
	if err != nil {
		return nil, err
	}
	nodeFieldOverrides, err := overrides.NodeFieldOverrides()
	if err != nil {
		return nil, err
	}

	// Pass 1: structural validation of the whole document against the dashboard
	// schema. We validate the raw decoded JSON value (not the typed Document)
	// so the schema sees the document exactly as authored.
	if err := r.validateDocument(data, source); err != nil {
		return nil, err
	}

	// Link every instance $ref to its item-type schema. This reuses the E1-S3
	// loader and surfaces ref/version errors fail-fast as SCHEMA_* CodedErrors.
	g, err := r.loader.LoadBytes(data, source)
	if err != nil {
		return nil, err
	}

	// E3-S1/S2: build the tree-scoped variable model from the raw document, then
	// thread it through the instance walk. The per-node environment must exist
	// DURING Pass 2 because config interpolation ($var / ${...}) runs before that
	// node's config is validated — an interpolated config carries concrete values
	// that satisfy the item-type schema, whereas a raw {"$var":…} node would not.
	docEnv, rawRoot, err := buildVariableModel(data, varOverrides)
	if err != nil {
		return nil, err
	}

	// Pass 2 + assembly: per node, layer variables, interpolate config, validate
	// the interpolated config, enforce the container-only-children rule, and build
	// the resolved node, all in one walk.
	root, err := r.resolveInstance(g, g.Document.Root, "root", docEnv, rawRoot, varOverrides)
	if err != nil {
		return nil, err
	}

	// Connection pass (E4-S1): decode document-scoped connections, validate each
	// against its connection-type schema, and reject duplicate ids. Fail-fast,
	// same machinery as the item-type passes. See connections.go.
	conns, err := r.resolveConnections(g, data, source)
	if err != nil {
		return nil, err
	}

	// Binding pass (E4-S2) + result-shape contract (E4-S3): attach each item's
	// direct data binding (connectionId + variable-filled query), validate that
	// every referenced connection exists, and validate/attach the item↔connection
	// result-shape contract (well-formed expectedResult; static inline data
	// conforms). Runs after both the instance walk (item configs are interpolated)
	// and the connection pass (the set of valid connections is known). The graph
	// supplies the item-type schemas carrying each expectedResult. See binding.go
	// and contract.go.
	if err := resolveBindings(g, root, conns); err != nil {
		return nil, err
	}

	// Widget pass (E1-S1): enforce the widget↔variable type-compatibility contract
	// for every variable widget (text-input/textarea/…). Runs after the instance
	// walk because it reads each node's scoped variable environment to resolve the
	// bound variable and check its declared type. Fail-fast, same machinery as the
	// other passes. See widget.go.
	if err := resolveWidgets(root); err != nil {
		return nil, err
	}

	// Configurable-surface pass (E3-S1): for every node whose item type declares a
	// `configurable` surface, validate the declaration (real config field, known
	// value type, rendering hint naming a catalogued widget) and attach the
	// validated surface to the node so E4 (config overrides) and E5 (configurator
	// auto-gen) can read it. Runs after the instance walk because it reads each
	// node's resolved type identity. Fail-fast, same machinery as the other
	// passes. See surface.go.
	if err := resolveSurfaces(g, root); err != nil {
		return nil, err
	}

	// Config-override pass (E4-S2): apply each node+field override
	// ("<node-id>.<field>") to the resolved tree. Runs LAST — after the instance
	// walk (config is interpolated and schema-validated) and the surface pass (each
	// node carries its validated configurable surface) — so an override targets a
	// real surface field, is validated against that field's declared type and the
	// item type's config constraints, and OVERWRITES the interpolated value
	// (precedence). The mutation is ephemeral: only this in-memory tree changes, the
	// document on disk is never written. Fail-fast, same machinery as the other
	// passes. See config_override.go.
	if err := applyConfigOverrides(g, root, nodeFieldOverrides); err != nil {
		return nil, err
	}

	tree := &ResolvedTree{
		Manifest:    g.Document.Manifest,
		Root:        root,
		Connections: conns,
	}

	return tree, nil
}

// validateDocument runs Pass 1: the whole document validates against the
// dashboard JSON Schema. The dashboard schema is compiled (resolved) once and
// reused across calls.
func (r *Resolver) validateDocument(data []byte, source string) error {
	if r.dashSch == nil {
		return errors.NewCodedError(errors.RESOLVE_INTERNAL,
			"no dashboard schema configured for structural validation")
	}
	if r.dashResolved == nil {
		resolved, err := r.dashSch.Resolve(nil)
		if err != nil {
			return errors.WrapCodedError(err, errors.RESOLVE_INTERNAL,
				"failed compiling dashboard schema for validation")
		}
		r.dashResolved = resolved
	}

	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return errors.WrapCodedErrorWithDetails(err, errors.RESOLVE_DOCUMENT_INVALID,
			"dashboard document is not valid JSON", map[string]any{"source": source})
	}
	if err := r.dashResolved.Validate(doc); err != nil {
		return errors.WrapCodedErrorWithDetails(err, errors.RESOLVE_DOCUMENT_INVALID,
			"dashboard document failed schema validation",
			map[string]any{"source": source})
	}
	return nil
}

// resolveInstance runs Pass 2 for a single node and recurses. path is the
// human-readable JSON-ish path to this node (e.g. "root.children[2]"), reported
// in errors so an author can locate the offending instance.
func (r *Resolver) resolveInstance(g *schema.ResolvedGraph, inst *schema.Instance, path string, parentEnv variables.Environment, raw *rawInstance, overrides variables.Overrides) (*ResolvedInstance, error) {
	typeID := g.Refs[inst]
	rt := g.Types[typeID]
	if rt == nil {
		// Should not happen: the loader records a type for every node. Guard
		// rather than panic so a graph/resolver mismatch is a coded error.
		return nil, errors.NewCodedErrorWithDetails(errors.RESOLVE_INTERNAL,
			"instance has no resolved item type", map[string]any{"path": path, "ref": inst.Ref})
	}

	isContainer := rt.Name == containerTypeName
	isForm := rt.Name == formTypeName

	// Children-allowed rule: children are permitted only on the structurally
	// special types — the weighted-grid container and the flow-packing form. A
	// child on any other type fails fast.
	if len(inst.Children) > 0 && !isContainer && !isForm {
		return nil, errors.NewCodedErrorWithDetails(errors.RESOLVE_CHILDREN_NOT_ALLOWED,
			"children are only permitted on container item types",
			map[string]any{
				"path": path,
				"type": rt.Name,
				"ref":  inst.Ref,
			})
	}

	// E3-S1: layer this node's own variable declarations onto the inherited
	// environment (inner shadows outer), recording provenance for var->node
	// visibility. Fail-fast on the first invalid declaration.
	var decls []variables.Declaration
	if raw != nil {
		decls = raw.Variables
	}
	env, err := parentEnv.ExtendWithOverrides(decls, path, overrides)
	if err != nil {
		return nil, err
	}

	// E3-S2: interpolate variable references in this instance's config BEFORE it
	// is validated, so Pass 2 sees concrete, typed values rather than raw
	// {"$var":…} / ${…} references. A missing reference fails fast (VAR_UNDEFINED).
	cfg := inst.Config
	if cfg != nil {
		interpolated, err := variables.Interpolate(cfg, env, path)
		if err != nil {
			return nil, err
		}
		// Interpolate preserves the top-level object shape of a config.
		cfg, _ = interpolated.(map[string]any)
	}

	// Pass 2: validate this instance's interpolated config against its item-type
	// schema.
	if err := r.validateConfig(rt, cfg, path); err != nil {
		return nil, err
	}

	node := &ResolvedInstance{
		ID: inst.ID,
		Type: ResolvedTypeRef{
			Ref:     inst.Ref,
			ID:      rt.ID,
			Name:    rt.Name,
			Version: rt.Version,
		},
		Container: isContainer,
		Config:    cfg,
		Placement: inst.Placement,
		VarEnv:    env,
	}

	for i, child := range inst.Children {
		childPath := path + ".children[" + strconv.Itoa(i) + "]"
		var rawChild *rawInstance
		if raw != nil && i < len(raw.Children) {
			rawChild = raw.Children[i]
		}
		resolvedChild, err := r.resolveInstance(g, child, childPath, env, rawChild, overrides)
		if err != nil {
			return nil, err
		}
		node.Children = append(node.Children, resolvedChild)
	}

	// E2-S1: normalize this container's grid + child placements into a layout
	// block (no-op for non-containers). See layout.go.
	if err := r.resolveLayout(node, path); err != nil {
		return nil, err
	}

	// E2-S1: a form packs its widget children into a parallel flow layout
	// (label+control rows, optionally split into N columns) instead of the
	// weighted grid. No-op for non-forms. See form.go.
	if isForm {
		if err := r.resolveForm(node, path); err != nil {
			return nil, err
		}
	}

	return node, nil
}

// validateConfig validates one instance's config against its resolved item-type
// schema. An absent config validates as an empty object so that required-field
// constraints in the item-type schema still apply.
func (r *Resolver) validateConfig(rt *schema.ResolvedType, config map[string]any, path string) error {
	resolved, err := rt.Schema.Resolve(nil)
	if err != nil {
		return errors.WrapCodedErrorWithDetails(err, errors.RESOLVE_INTERNAL,
			"failed compiling item-type schema for validation",
			map[string]any{"path": path, "type": rt.ID})
	}

	var cfg any = config
	if config == nil {
		cfg = map[string]any{}
	}
	if err := resolved.Validate(cfg); err != nil {
		return errors.WrapCodedErrorWithDetails(err, errors.RESOLVE_CONFIG_INVALID,
			"instance config failed item-type schema validation",
			map[string]any{
				"path": path,
				"type": rt.ID,
			})
	}
	return nil
}
