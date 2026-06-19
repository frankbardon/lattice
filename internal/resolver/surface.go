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

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/schema"
	"github.com/frankbardon/lattice/internal/variables"
)

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

	props := configProperties(g, inst.Type.ID)

	// Iterate fields in a stable (sorted) order so the resolved surface and any
	// error are deterministic regardless of map iteration order.
	fields := make([]string, 0, len(decl))
	for field := range decl {
		fields = append(fields, field)
	}
	sort.Strings(fields)

	out := make([]ConfigurableField, 0, len(decl))
	for _, field := range fields {
		entry, ok := decl[field].(map[string]any)
		if !ok {
			return nil, errors.NewCodedErrorWithDetails(errors.CONFIGURABLE_SURFACE_INVALID,
				"configurable surface entry is not an object",
				map[string]any{"path": path, "type": inst.Type.Name, "field": field})
		}

		// The field must be a real config property of the item type. An unknown
		// field would let a configurator offer an editor for a property the item
		// type cannot accept.
		if _, isProp := props[field]; !isProp {
			return nil, errors.NewCodedErrorWithDetails(errors.CONFIGURABLE_SURFACE_INVALID,
				"configurable surface names a field the item type does not declare",
				map[string]any{"path": path, "type": inst.Type.Name, "field": field})
		}

		// The declared value type must be one of the variable type set.
		typeStr, _ := entry[surfaceTypeKey].(string)
		vt := variables.VarType(typeStr)
		if !variables.IsValidType(vt) {
			return nil, errors.NewCodedErrorWithDetails(errors.CONFIGURABLE_SURFACE_INVALID,
				"configurable surface field has an unknown value type",
				map[string]any{"path": path, "type": inst.Type.Name, "field": field, "fieldType": typeStr})
		}

		// An optional rendering hint must name a widget the catalog knows.
		rendering, _ := entry[surfaceRenderingKey].(string)
		if rendering != "" {
			if _, isWidget := widgetFamilies[rendering]; !isWidget {
				return nil, errors.NewCodedErrorWithDetails(errors.CONFIGURABLE_SURFACE_INVALID,
					"configurable surface rendering hint names an unknown widget item-type",
					map[string]any{"path": path, "type": inst.Type.Name, "field": field, surfaceRenderingKey: rendering})
			}
		}

		label, _ := entry[surfaceLabelKey].(string)
		constraints, _ := entry[surfaceConstraintsKey].(map[string]any)

		out = append(out, ConfigurableField{
			Field:       field,
			Type:        vt,
			Label:       label,
			Constraints: constraints,
			Rendering:   rendering,
		})
	}
	return out, nil
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

// configProperties returns the set of config property names the resolved item
// type declares (its JSON Schema `properties` keys). It is the authority for the
// "unknown field" check. A type with no declared properties yields an empty map.
func configProperties(g *schema.ResolvedGraph, typeID string) map[string]struct{} {
	props := map[string]struct{}{}
	rt := g.Types[typeID]
	if rt == nil || rt.Schema == nil {
		return props
	}
	for name := range rt.Schema.Properties {
		props[name] = struct{}{}
	}
	return props
}
