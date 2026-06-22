package resolver

// This file implements the WIDGET BINDING pass (E1): the shared contract every
// variable widget follows. A widget is a leaf item type that SETS a single
// variable — it carries a `variable` config key naming the one variable it
// drives, and the resolver enforces widget↔variable TYPE COMPATIBILITY: a widget
// may only bind a variable whose declared type its FAMILY permits.
//
// The compatibility rule is the reusable core, and it is now KEYWORD-DRIVEN
// (E2-S1): a node is a widget iff its item type declares `latticeBehavior.role ==
// "widget"`, the permitted variable-type set is the type's `binds` list, the
// cross-field range check is gated by `rangeCheck`, and the non-empty options
// check by `requireOptions`. The schema is the single source of truth — every
// family (string, number, boolean, enum, array) is a pair of schema files
// carrying the keyword, with NO per-family or name-keyed resolver registry. The
// pass itself is generic over the behavior accessors in internal/schema.
//
// The pass runs AFTER the instance walk because it reads each node's scoped
// variable environment (ResolvedInstance.VarEnv, computed during the walk) to
// resolve the bound variable and check its declared type. It is fail-fast: the
// first offending widget stops the walk and is returned as a CodedError naming
// the instance path. A widget bound to an undefined variable reuses the existing
// VAR_UNDEFINED code; a type mismatch is WIDGET_TYPE_MISMATCH.
//
// The pass lives in its own file (per file-ownership rules) and is invoked by a
// single call from resolver.go's resolveBytes.

import (
	"strconv"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/schema"
	"github.com/frankbardon/lattice/internal/variables"
)

// widgetVariableKey is the reserved config key naming the single variable a
// widget sets. Every widget family declares it in its item-type schema; the pass
// reads it generically.
const widgetVariableKey = "variable"

// widgetType resolves a node's item-type behavior from the graph's type table.
// It returns nil when the graph is absent or the node's type is not catalogued —
// callers treat a nil result as "not a widget".
func widgetType(g *schema.ResolvedGraph, inst *ResolvedInstance) *schema.ResolvedType {
	if g == nil {
		return nil
	}
	return g.Types[inst.Type.ID]
}

// isVariableWidget reports whether a resolved node's item type declares the
// widget role (`latticeBehavior.role == "widget"`). It is the schema-keyword
// replacement for the old name-membership test against the former family map.
func isVariableWidget(g *schema.ResolvedGraph, inst *ResolvedInstance) bool {
	rt := widgetType(g, inst)
	return rt != nil && rt.Role() == schema.RoleWidget
}

// permitsVarType reports whether a widget type's declared `binds` set admits a
// bound variable's declared type. The `binds` members use the same string
// vocabulary as variables.VarType (string/number/integer/boolean/enum/array), so
// the comparison is a direct match — superseding the old per-name permitted-type
// map.
func permitsVarType(rt *schema.ResolvedType, vt variables.VarType) bool {
	for _, b := range rt.Binds() {
		if variables.VarType(b) == vt {
			return true
		}
	}
	return false
}

// resolveWidgets walks the assembled resolved tree and validates every variable
// widget's binding: the named variable must be visible in the widget's scope
// (else VAR_UNDEFINED) and its declared type must be one the widget's `binds`
// set permits (else WIDGET_TYPE_MISMATCH). Non-widget nodes are left untouched.
// It is fail-fast: the first offending widget stops the walk. The graph supplies
// each node's item-type behavior (role + binds + rangeCheck/requireOptions).
func resolveWidgets(g *schema.ResolvedGraph, root *ResolvedInstance) error {
	return checkWidget(g, root, "root")
}

// checkWidget validates one node's widget binding (if it is a widget) and
// recurses into children.
func checkWidget(g *schema.ResolvedGraph, inst *ResolvedInstance, path string) error {
	if err := validateWidgetBinding(g, inst, path); err != nil {
		return err
	}
	for i, child := range inst.Children {
		childPath := path + ".children[" + strconv.Itoa(i) + "]"
		if err := checkWidget(g, child, childPath); err != nil {
			return err
		}
	}
	return nil
}

// validateWidgetBinding enforces the widget↔variable type-compatibility contract
// for a single node. It is a no-op for nodes whose item type does not declare the
// widget role. For a widget it resolves the bound variable name from config,
// looks it up in the node's scoped environment, and checks the declared type
// against the type's `binds` set; the `rangeCheck`/`requireOptions` behavior
// flags then gate the optional cross-field checks.
//
// The `variable` key's presence and string shape are already guaranteed by the
// widget item-type schema (required, minLength 1) validated in Pass 2, so this
// pass only handles the semantic checks: visibility and type compatibility.
func validateWidgetBinding(g *schema.ResolvedGraph, inst *ResolvedInstance, path string) error {
	rt := widgetType(g, inst)
	if rt == nil || rt.Role() != schema.RoleWidget {
		return nil
	}

	name, _ := inst.Config[widgetVariableKey].(string)
	if name == "" {
		// The item-type schema requires a non-empty `variable`; reaching here would
		// mean Pass 2 was bypassed. Guard rather than panic.
		return errors.NewCodedErrorWithDetails(errors.RESOLVE_INTERNAL,
			"widget is missing its required variable binding",
			map[string]any{"path": path, "widget": inst.Type.Name})
	}

	v, ok := inst.VarEnv.Lookup(name)
	if !ok {
		return errors.NewCodedErrorWithDetails(errors.VAR_UNDEFINED,
			"widget binds a variable that is not declared in its scope",
			map[string]any{"path": path, "variable": name, "widget": inst.Type.Name})
	}

	if !permitsVarType(rt, v.Type) {
		return errors.NewCodedErrorWithDetails(errors.WIDGET_TYPE_MISMATCH,
			"widget is not compatible with the bound variable's declared type",
			map[string]any{
				"path":     path,
				"variable": name,
				"widget":   inst.Type.Name,
				"varType":  string(v.Type),
			})
	}

	if rt.RangeCheck() {
		if err := validateWidgetRange(inst, path); err != nil {
			return err
		}
	}

	if rt.RequireOptions() {
		if err := validateWidgetOptions(inst, path); err != nil {
			return err
		}
	}
	return nil
}

// validateWidgetOptions enforces the option-constrained array widgets' bounded
// option-set rule: a multiselect/checkbox-group must declare a non-empty
// `options` set for a viewer to choose from. The schema deliberately leaves
// `options` optional so this rule is the single coded-error surface (consistent
// with E1-S2's range check), distinguishing the option-constrained array widgets
// from the freeform `tag-input`. An absent or empty set is reported as
// RESOLVE_CONFIG_INVALID, naming the offending field. The `{value,label}` shape
// of each option, when present, is already enforced by the item-type schema.
func validateWidgetOptions(inst *ResolvedInstance, path string) error {
	opts, _ := inst.Config["options"].([]any)
	if len(opts) == 0 {
		return errors.NewCodedErrorWithDetails(errors.RESOLVE_CONFIG_INVALID,
			"option-constrained array widget requires a non-empty options set",
			map[string]any{
				"path":   path,
				"widget": inst.Type.Name,
				"field":  "options",
			})
	}
	return nil
}

// validateWidgetRange enforces the cross-field range invariants on a number
// widget's optional min/max/step config: min may not exceed max, and step must
// be positive. Each value's individual JSON type — and the positive-step bound
// (exclusiveMinimum 0) — is already guaranteed by the widget item-type schema in
// Pass 2, so a violation reaching here is the inverted-range case JSON Schema
// cannot express. The step guard is retained defensively. Violations reuse the
// config-validation code RESOLVE_CONFIG_INVALID, naming the offending field.
func validateWidgetRange(inst *ResolvedInstance, path string) error {
	min, hasMin := numberConfig(inst.Config, "min")
	max, hasMax := numberConfig(inst.Config, "max")
	step, hasStep := numberConfig(inst.Config, "step")

	if hasMin && hasMax && min > max {
		return errors.NewCodedErrorWithDetails(errors.RESOLVE_CONFIG_INVALID,
			"number widget range is inverted: min must not exceed max",
			map[string]any{
				"path":   path,
				"widget": inst.Type.Name,
				"field":  "min",
				"min":    min,
				"max":    max,
			})
	}
	if hasStep && step <= 0 {
		return errors.NewCodedErrorWithDetails(errors.RESOLVE_CONFIG_INVALID,
			"number widget step must be a positive number",
			map[string]any{
				"path":   path,
				"widget": inst.Type.Name,
				"field":  "step",
				"step":   step,
			})
	}
	return nil
}

// numberConfig reads a numeric config value by key. Decoded JSON numbers arrive
// as float64; the value's type is already schema-validated, so a non-number is
// treated as absent. The bool reports whether a numeric value was present.
func numberConfig(config map[string]any, key string) (float64, bool) {
	if config == nil {
		return 0, false
	}
	n, ok := config[key].(float64)
	return n, ok
}
