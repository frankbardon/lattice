package resolver

// This file implements the E3-S1 CONFIGURABLE-SURFACE pass: the mechanism by
// which an item-type schema declares which of its config fields are
// runtime-configurable, so a configurator (E5) can auto-generate an editor and
// the config-override system (E4) knows which fields it may override.
//
// An item-type schema may carry a `configurable` keyword — a schema-level
// keyword (a sibling of `expectedResult`, not per-instance config) captured by
// google/jsonschema-go as an unknown keyword in Schema.Extra. It maps each
// runtime-configurable config FIELD to a descriptor:
//
//	"configurable": {
//	  "<field>": {
//	    "type": "<string|number|integer|boolean|enum|array>",
//	    "label": "<human label>",
//	    "constraints": { ... },        // optional, opaque
//	    "rendering": "<widget-name>"   // optional preferred widget item-type
//	  }
//	}
//
// For every resolved instance the resolver validates the declaration of its item
// type, fail-fast, reporting CONFIGURABLE_SURFACE_INVALID when:
//
//   - a field name is not a real config property of the item type,
//   - a field's `type` is not one of the variable type set, or
//   - a `rendering` hint names a widget item-type the catalog does not know
//     (validated against widgetFamilies, the widget catalog).
//
// The validated surface is attached to the ResolvedInstance (see tree.go's
// Surface field) so downstream epics read it without re-parsing the schema.
//
// This is the MECHANISM only; concrete surfaces for real item types land in
// E3-S2/S3. The pass lives in its own file (per file-ownership rules) and is
// invoked by a single call from resolver.go's resolveBytes, after the instance
// walk (it reads each node's resolved type identity).

import (
	"sort"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/schema"
	"github.com/frankbardon/lattice/internal/variables"
)

// pathSeparator is the delimiter for a NESTED configurable-field path (E2-S1): a
// `configurable` key carrying a "." declares an explicit sub-path into a nested
// config object (e.g. "grid.gap"), as opposed to a bare top-level field name.
const pathSeparator = "."

// propLookup answers, for a `configurable`-declared field key, whether that key
// addresses a real, settable property of the surface owner — the single honesty
// check that keeps a surface from ever offering an editor for a field the owner
// cannot accept. It is the seam that lets the SHARED buildSurface serve both the
// item-type surface (whose owner is a JSON Schema that can be walked for nested
// paths, E2-S1) and the document-scope surfaces (whose owner is a flat,
// top-level-only field set).
type propLookup interface {
	// has reports whether field (a `configurable` key — a bare name, or a
	// dotted nested path for an item-type schema) addresses a real property of
	// the surface owner.
	has(field string) bool
}

// flatProps is a top-level-only property set. A dotted (nested) key never
// matches — document scopes cover top-level fields only — so a flatProps owner
// can never declare a nested surface entry.
type flatProps map[string]struct{}

func (p flatProps) has(field string) bool {
	if strings.Contains(field, pathSeparator) {
		return false
	}
	_, ok := p[field]
	return ok
}

// schemaProps validates a `configurable` key against an item-type schema's
// `properties`, supporting NESTED dotted paths (E2-S1): a bare key must be a
// top-level property; a dotted key is walked segment by segment through the
// nested `properties` of each intermediate object property. A path is legal only
// when every segment exists, so a surface can never declare a sub-path the item
// type's schema does not actually carry.
type schemaProps struct {
	schema *jsonschema.Schema
}

func (p schemaProps) has(field string) bool {
	if p.schema == nil {
		return false
	}
	cur := p.schema
	for _, seg := range strings.Split(field, pathSeparator) {
		if cur.Properties == nil {
			return false
		}
		next, ok := cur.Properties[seg]
		if !ok || next == nil {
			return false
		}
		cur = next
	}
	return true
}

// configurableKey is the reserved item-type schema keyword that declares an item
// type's configurable surface. Like expectedResultKey it is a top-level keyword
// on the item-type schema (not an instance-config property), captured by
// google/jsonschema-go as an unknown keyword in Schema.Extra.
const configurableKey = "configurable"

// surfaceTypeKey, surfaceLabelKey, surfaceConstraintsKey, and surfaceRenderingKey
// are the descriptor keys of a single configurable-field entry.
const (
	surfaceTypeKey        = "type"
	surfaceLabelKey       = "label"
	surfaceConstraintsKey = "constraints"
	surfaceRenderingKey   = "rendering"
)

// resolveSurfaces walks the assembled resolved tree and, for every node whose
// item type declares a `configurable` surface, validates the declaration and
// attaches the resolved surface to the node. Non-declaring nodes are left
// untouched. It is fail-fast: the first malformed surface stops the walk.
func resolveSurfaces(g *schema.ResolvedGraph, root *ResolvedInstance) error {
	return checkSurface(g, root, "root")
}

// checkSurface validates one node's configurable surface (if its item type
// declares one) and recurses into children.
func checkSurface(g *schema.ResolvedGraph, inst *ResolvedInstance, path string) error {
	surface, err := resolveSurface(g, inst, path)
	if err != nil {
		return err
	}
	inst.Surface = surface
	for i, child := range inst.Children {
		childPath := path + ".children[" + strconv.Itoa(i) + "]"
		if err := checkSurface(g, child, childPath); err != nil {
			return err
		}
	}
	return nil
}

// resolveSurface builds and validates the configurable surface for one node from
// its item type's `configurable` keyword. It returns the validated surface (in
// declared field order) or the first CONFIGURABLE_SURFACE_INVALID error. A type
// that declares no surface returns (nil, nil).
//
// Validation rules:
//   - every declared field must be a real config property of the item type
//     (present in the item-type schema's properties),
//   - each field's `type` must be one of the variable type set, and
//   - any `rendering` hint must name a registered widget family.
func resolveSurface(g *schema.ResolvedGraph, inst *ResolvedInstance, path string) ([]ConfigurableField, error) {
	decl, ok := configurableFor(g, inst.Type.ID)
	if !ok {
		return nil, nil
	}

	return buildSurface(decl, sortedFields(decl), itemProps(g, inst.Type.ID), inst.Type.Name, path)
}

// buildSurface validates a `configurable`-shaped declaration (field -> descriptor)
// against a known set of legal property names and returns the resolved surface in
// the given field order. It is the SHARED validator behind both the item-type
// surface pass (resolveSurface) and the document-scope surfaces (E4-S2,
// resolveScopeSurface): both express their knobs with the identical descriptor
// shape, so they reuse one validation path rather than diverging. typeName is the
// surface owner reported in errors (the item-type name, or the reserved scope
// keyword for a document scope).
//
// A `configurable` key may be a bare top-level field name OR, for an item-type
// schema, a NESTED dotted path (e.g. "grid.gap") that declares an explicit,
// bounded sub-path into a nested config object (E2-S1) — never recursive
// whole-object editability, only the paths the schema actually enumerates. A
// nested entry carries its parsed segments in ConfigurableField.Path so a
// guardrail can look it up by path; a top-level entry leaves Path nil.
//
// Validation rules (identical to the item-type surface):
//   - every declared field must address a real property of the surface owner
//     (via props.has — its config properties for an item type, walked
//     segment-by-segment for a nested path, or its scope's top-level settable
//     fields for a document scope), so a surface can never offer an editor for a
//     field the owner cannot accept;
//   - each field's `type` must be one of the variable type set;
//   - any `rendering` hint must name a registered widget family.
func buildSurface(decl map[string]any, fields []string, props propLookup, typeName, path string) ([]ConfigurableField, error) {
	out := make([]ConfigurableField, 0, len(decl))
	for _, field := range fields {
		entry, ok := decl[field].(map[string]any)
		if !ok {
			return nil, errors.NewCodedErrorWithDetails(errors.CONFIGURABLE_SURFACE_INVALID,
				"configurable surface entry is not an object",
				map[string]any{"path": path, "type": typeName, "field": field})
		}

		// The field must address a real property of the surface owner — a top-level
		// property, or, for a dotted key, a property reachable by walking the item
		// type's nested schema. An unknown field (or unreachable sub-path) would let
		// a configurator offer an editor for a property the owner cannot accept.
		if !props.has(field) {
			return nil, errors.NewCodedErrorWithDetails(errors.CONFIGURABLE_SURFACE_INVALID,
				"configurable surface names a field the item type does not declare",
				map[string]any{"path": path, "type": typeName, "field": field})
		}

		// The declared value type must be one of the variable type set.
		typeStr, _ := entry[surfaceTypeKey].(string)
		vt := variables.VarType(typeStr)
		if !variables.IsValidType(vt) {
			return nil, errors.NewCodedErrorWithDetails(errors.CONFIGURABLE_SURFACE_INVALID,
				"configurable surface field has an unknown value type",
				map[string]any{"path": path, "type": typeName, "field": field, "fieldType": typeStr})
		}

		// An optional rendering hint must name a widget the catalog knows.
		rendering, _ := entry[surfaceRenderingKey].(string)
		if rendering != "" {
			if _, isWidget := widgetFamilies[rendering]; !isWidget {
				return nil, errors.NewCodedErrorWithDetails(errors.CONFIGURABLE_SURFACE_INVALID,
					"configurable surface rendering hint names an unknown widget item-type",
					map[string]any{"path": path, "type": typeName, "field": field, surfaceRenderingKey: rendering})
			}
		}

		label, _ := entry[surfaceLabelKey].(string)
		constraints, _ := entry[surfaceConstraintsKey].(map[string]any)

		out = append(out, ConfigurableField{
			Field:       field,
			Path:        nestedPath(field),
			Type:        vt,
			Label:       label,
			Constraints: constraints,
			Rendering:   rendering,
		})
	}
	return out, nil
}

// sortedFields returns the keys of a `configurable`-shaped declaration in a
// stable (sorted) order so a resolved surface and any error are deterministic
// regardless of map iteration order.
func sortedFields(decl map[string]any) []string {
	fields := make([]string, 0, len(decl))
	for field := range decl {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

// configurableFor returns the verbatim `configurable` mapping declared by the
// resolved item type identified by typeID, captured in the item-type schema's
// Extra keywords. The second return is false when the type declares no surface.
func configurableFor(g *schema.ResolvedGraph, typeID string) (map[string]any, bool) {
	rt := g.Types[typeID]
	if rt == nil || rt.Schema == nil || rt.Schema.Extra == nil {
		return nil, false
	}
	raw, ok := rt.Schema.Extra[configurableKey]
	if !ok {
		return nil, false
	}
	decl, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	return decl, true
}

// itemProps returns the property lookup for a resolved item type: a schemaProps
// over the item-type schema, so the surface's "unknown field" check accepts both
// a top-level property and a NESTED dotted path (E2-S1) reachable by walking the
// schema's nested `properties`. A type with no resolved schema rejects every
// field (an empty surface owner).
func itemProps(g *schema.ResolvedGraph, typeID string) propLookup {
	rt := g.Types[typeID]
	if rt == nil {
		return schemaProps{}
	}
	return schemaProps{schema: rt.Schema}
}

// nestedPath parses a `configurable` field key into its dotted segments for a
// NESTED entry (E2-S1), returning nil for a bare top-level key (whose whole
// address is the key itself, so Path stays omitted). It is the inverse of
// joining the segments with "." that produces the entry's Field.
func nestedPath(field string) []string {
	if !strings.Contains(field, pathSeparator) {
		return nil
	}
	return strings.Split(field, pathSeparator)
}
