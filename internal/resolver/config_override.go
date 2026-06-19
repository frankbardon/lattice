package resolver

import (
	"strconv"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/schema"
	"github.com/frankbardon/lattice/internal/variables"
)

// This file implements the E4-S2 CONFIG-OVERRIDE pass: the mechanism by which a
// node+field runtime override ("<node-id>.<field>", carried by the unified
// OverrideSet from E4-S1) actually MUTATES a resolved instance's config.
//
// It runs LAST in the resolver pipeline, after the instance walk (so config is
// interpolated and schema-validated) and after the configurable-surface pass (so
// every node carries its validated surface). Each override is applied like so:
//
//   - resolve the target instance by its node id (a tree walk over ResolvedInstance.ID),
//   - the addressed field MUST be exposed by that item type's configurable surface
//     (E3) — a field absent from the surface, OR a dotted sub-path into a nested
//     object (surfaces cover TOP-LEVEL config fields only), fails fast with
//     CONFIG_OVERRIDE_FIELD_UNKNOWN,
//   - the value MUST satisfy the surface field's declared type and the item type's
//     config-schema constraints for that field, else CONFIG_OVERRIDE_VALUE_INVALID,
//   - the addressed config field is set to the override value, OVERWRITING any
//     interpolated value already there (PRECEDENCE: a config override wins over an
//     interpolated value for the same field).
//
// The mutation is EPHEMERAL: only the in-memory resolved tree is changed; the
// document on disk is never written.

// applyConfigOverrides applies every node+field config override in overs to the
// resolved tree rooted at root, fail-fast on the first violation. The graph g
// supplies the target item type's config schema for the constraint check. Each
// override is resolved, validated against the target's surface, and applied
// post-interpolation so it takes precedence over the interpolated value.
func applyConfigOverrides(g *schema.ResolvedGraph, root *ResolvedInstance, overs []variables.NodeFieldOverride) error {
	for _, ov := range overs {
		if err := applyConfigOverride(g, root, ov); err != nil {
			return err
		}
	}
	return nil
}

// applyConfigOverride applies a single node+field override to the tree. It
// resolves the target node by id, validates the field against the node's surface
// and the value against the field's type/constraints, and sets the field.
func applyConfigOverride(g *schema.ResolvedGraph, root *ResolvedInstance, ov variables.NodeFieldOverride) error {
	nodeID := ov.Target.Name
	field := ov.Target.Field

	inst, path := findByID(root, nodeID, "root")
	if inst == nil {
		// The addressed node does not exist in the resolved tree. Treat an unknown
		// node id like an unknown target field: there is no surface that exposes it.
		return errors.NewCodedErrorWithDetails(errors.CONFIG_OVERRIDE_FIELD_UNKNOWN,
			"config override addresses a node id that is not in the resolved tree",
			map[string]any{"node": nodeID, "field": field})
	}

	// The field must be a TOP-LEVEL field exposed by the item type's configurable
	// surface (E3). Surfaces cover top-level config fields only, so a dotted
	// sub-path (e.g. "grid.gap") never matches a surface field and is rejected as
	// unknown — keeping config overrides honest about what the surface declares.
	sf, ok := surfaceField(inst.Surface, field)
	if !ok {
		return errors.NewCodedErrorWithDetails(errors.CONFIG_OVERRIDE_FIELD_UNKNOWN,
			"config override field is not in the target item type's configurable surface",
			map[string]any{"path": path, "type": inst.Type.Name, "node": nodeID, "field": field})
	}

	// Validate the value against the surface field's declared type (and enum
	// options, if any). A value of the wrong type fails fast as an override-value
	// violation rather than the generic VAR_TYPE the variable path would report.
	coerced, err := variables.CoerceValue(ov.Value, sf.Type, surfaceEnumOptions(sf), path, field)
	if err != nil {
		return errors.WrapCodedErrorWithDetails(err, errors.CONFIG_OVERRIDE_VALUE_INVALID,
			"config override value does not satisfy the surface field's declared type",
			map[string]any{"path": path, "type": inst.Type.Name, "node": nodeID, "field": field})
	}

	// PRECEDENCE: set the field on a COPY of the config, overwriting any
	// interpolated value already there, then validate the whole candidate config
	// against the item type's schema so the surface field's declared CONSTRAINTS
	// (min/max, item shape, …) are enforced. Only commit the mutation if the
	// candidate validates — a constraint violation must not leave the tree in a
	// half-applied state.
	candidate := cloneConfig(inst.Config)
	candidate[field] = coerced

	rt := g.Types[inst.Type.ID]
	if rt == nil {
		return errors.NewCodedErrorWithDetails(errors.RESOLVE_INTERNAL,
			"config override target has no resolved item type",
			map[string]any{"path": path, "node": nodeID, "field": field})
	}
	if err := validateOverriddenConfig(rt, candidate, path); err != nil {
		return errors.WrapCodedErrorWithDetails(err, errors.CONFIG_OVERRIDE_VALUE_INVALID,
			"config override value violates the item type's config constraints",
			map[string]any{"path": path, "type": inst.Type.Name, "node": nodeID, "field": field})
	}

	inst.Config = candidate
	return nil
}

// validateOverriddenConfig validates a candidate config (post-override) against
// the item type's resolved schema, enforcing the surface field's declared
// constraints. It reuses the same schema compile + validate the Pass-2 config
// check uses; the caller re-codes any failure as CONFIG_OVERRIDE_VALUE_INVALID.
func validateOverriddenConfig(rt *schema.ResolvedType, config map[string]any, path string) error {
	resolved, err := rt.Schema.Resolve(nil)
	if err != nil {
		return errors.WrapCodedErrorWithDetails(err, errors.RESOLVE_INTERNAL,
			"failed compiling item-type schema for override validation",
			map[string]any{"path": path, "type": rt.ID})
	}
	if err := resolved.Validate(config); err != nil {
		return err
	}
	return nil
}

// findByID returns the first instance in the tree (pre-order) whose ID equals id,
// along with its resolved-tree path, or (nil, "") if none matches. A node with an
// empty id never matches (an override must address an identified node).
func findByID(inst *ResolvedInstance, id, path string) (*ResolvedInstance, string) {
	if inst == nil {
		return nil, ""
	}
	if id != "" && inst.ID == id {
		return inst, path
	}
	for i, child := range inst.Children {
		childPath := path + ".children[" + strconv.Itoa(i) + "]"
		if found, fp := findByID(child, id, childPath); found != nil {
			return found, fp
		}
	}
	return nil, ""
}

// surfaceField returns the configurable-surface entry for field and whether the
// surface exposes it.
func surfaceField(surface []ConfigurableField, field string) (ConfigurableField, bool) {
	for _, f := range surface {
		if f.Field == field {
			return f, true
		}
	}
	return ConfigurableField{}, false
}

// surfaceEnumOptions extracts the enum option set for an enum-typed surface field
// from its opaque constraints (a JSON Schema-ish `enum` array). It returns nil for
// non-enum fields or when no options are declared, in which case the value is
// type-checked but not membership-checked.
func surfaceEnumOptions(f ConfigurableField) []any {
	if f.Type != variables.VarTypeEnum || f.Constraints == nil {
		return nil
	}
	opts, _ := f.Constraints["enum"].([]any)
	return opts
}

// cloneConfig returns a shallow copy of a config map (never nil), so a candidate
// mutation can be validated before it is committed without disturbing the
// already-resolved config.
func cloneConfig(cfg map[string]any) map[string]any {
	out := make(map[string]any, len(cfg)+1)
	for k, v := range cfg {
		out[k] = v
	}
	return out
}
