// Package resolver performs two-pass validation of a dashboard document and
// emits the resolved tree (see tree.go for the emitted contract).
//
// The two passes are:
//
//	Pass 1 (structural): the whole document validates against the dashboard
//	  JSON Schema. Failure -> RESOLVE_DOCUMENT_INVALID.
//	Pass 2 (per-instance): each instance's config validates against its resolved
//	  item-type schema, and the container-only-children rule is enforced. Failure
//	  -> RESOLVE_CONFIG_INVALID / RESOLVE_CHILDREN_NOT_ALLOWED, naming the
//	  offending instance path (e.g. "root.children[2]").
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
)

// containerTypeName is the item-type name whose instances may carry children.
// It is the only structurally-special item type (see schemas/items/container).
const containerTypeName = "container"

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
	data, err := afero.ReadFile(r.fs, docPath)
	if err != nil {
		return nil, errors.WrapCodedError(err, errors.RESOLVE_IO, "failed reading dashboard document "+docPath)
	}
	return r.resolveBytes(data, docPath)
}

// resolveBytes runs the full pipeline against raw document bytes. Split out so
// tests can drive resolution without touching the filesystem for the document.
func (r *Resolver) resolveBytes(data []byte, source string) (*ResolvedTree, error) {
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

	// Pass 2 + assembly: validate each instance's config, enforce the
	// container-only-children rule, and build the resolved node, all in one walk.
	root, err := r.resolveInstance(g, g.Document.Root, "root")
	if err != nil {
		return nil, err
	}

	return &ResolvedTree{
		Manifest: g.Document.Manifest,
		Root:     root,
	}, nil
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
func (r *Resolver) resolveInstance(g *schema.ResolvedGraph, inst *schema.Instance, path string) (*ResolvedInstance, error) {
	typeID := g.Refs[inst]
	rt := g.Types[typeID]
	if rt == nil {
		// Should not happen: the loader records a type for every node. Guard
		// rather than panic so a graph/resolver mismatch is a coded error.
		return nil, errors.NewCodedErrorWithDetails(errors.RESOLVE_INTERNAL,
			"instance has no resolved item type", map[string]any{"path": path, "ref": inst.Ref})
	}

	isContainer := rt.Name == containerTypeName

	// Container-only-children rule: children on a non-container type fail fast.
	if len(inst.Children) > 0 && !isContainer {
		return nil, errors.NewCodedErrorWithDetails(errors.RESOLVE_CHILDREN_NOT_ALLOWED,
			"children are only permitted on container item types",
			map[string]any{
				"path": path,
				"type": rt.Name,
				"ref":  inst.Ref,
			})
	}

	// Pass 2: validate this instance's config against its item-type schema.
	if err := r.validateConfig(rt, inst, path); err != nil {
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
		Config:    inst.Config,
		Placement: inst.Placement,
	}

	for i, child := range inst.Children {
		childPath := path + ".children[" + strconv.Itoa(i) + "]"
		resolvedChild, err := r.resolveInstance(g, child, childPath)
		if err != nil {
			return nil, err
		}
		node.Children = append(node.Children, resolvedChild)
	}

	return node, nil
}

// validateConfig validates one instance's config against its resolved item-type
// schema. An absent config validates as an empty object so that required-field
// constraints in the item-type schema still apply.
func (r *Resolver) validateConfig(rt *schema.ResolvedType, inst *schema.Instance, path string) error {
	resolved, err := rt.Schema.Resolve(nil)
	if err != nil {
		return errors.WrapCodedErrorWithDetails(err, errors.RESOLVE_INTERNAL,
			"failed compiling item-type schema for validation",
			map[string]any{"path": path, "type": rt.ID})
	}

	var cfg any = inst.Config
	if inst.Config == nil {
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
