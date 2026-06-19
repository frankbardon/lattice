package resolver

// This file implements the WIDGET BINDING pass (E1): the shared contract every
// variable widget follows. A widget is a leaf item type that SETS a single
// variable — it carries a `variable` config key naming the one variable it
// drives, and the resolver enforces widget↔variable TYPE COMPATIBILITY: a widget
// may only bind a variable whose declared type its FAMILY permits.
//
// The compatibility rule is the reusable core. Each widget family registers its
// item-type name(s) plus the set of variable types it accepts in widgetFamilies;
// every subsequent family (number, boolean, enum, …) is one entry here and a pair
// of schema files — no per-family resolver logic. The pass itself is generic over
// the registry.
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
	"github.com/frankbardon/lattice/internal/variables"
)

// widgetVariableKey is the reserved config key naming the single variable a
// widget sets. Every widget family declares it in its item-type schema; the pass
// reads it generically.
const widgetVariableKey = "variable"

// widgetFamilies maps a widget item-type name to the set of variable types its
// family permits. A node whose resolved item-type name appears here is treated as
// a variable widget and type-checked against the bound variable's declared type.
//
// This is the single registration point: E1-S2/S3/S4 add their family by adding
// an entry (e.g. "number-input"/"slider" → {VarTypeNumber, VarTypeInteger}). The
// pass logic does not change.
var widgetFamilies = map[string]map[variables.VarType]bool{
	// String family (E1-S1): single-line and multi-line free-text controls bind a
	// string variable.
	"text-input": {variables.VarTypeString: true},
	"textarea":   {variables.VarTypeString: true},

	// Number family (E1-S2): free-entry field, draggable slider, and increment
	// stepper bind a number or integer variable. Each carries optional
	// min/max/step range config (validated below).
	"number-field": {variables.VarTypeNumber: true, variables.VarTypeInteger: true},
	"slider":       {variables.VarTypeNumber: true, variables.VarTypeInteger: true},
	"stepper":      {variables.VarTypeNumber: true, variables.VarTypeInteger: true},

	// Boolean family (E1-S2): on/off switch and checkbox bind a boolean variable.
	"toggle":   {variables.VarTypeBoolean: true},
	"checkbox": {variables.VarTypeBoolean: true},

	// Enum family (E1-S3): single-choice dropdown, radio-button group, and
	// segmented control bind an enum variable, exposing the option set plus
	// ordering config. `select` is the canonical replacement for the retired
	// `dropdown` item.
	"select":      {variables.VarTypeEnum: true},
	"radio-group": {variables.VarTypeEnum: true},
	"segmented":   {variables.VarTypeEnum: true},

	// Array family (E1-S4): multi-choice menu, checkbox group, and freeform tag
	// entry bind an array variable. `multiselect` and `checkbox-group` are
	// option-constrained (they require a bounded option set, checked below);
	// `tag-input` is freeform and declares no options.
	"multiselect":    {variables.VarTypeArray: true},
	"checkbox-group": {variables.VarTypeArray: true},
	"tag-input":      {variables.VarTypeArray: true},
}

// numberWidgets is the subset of widget families that accept the optional
// min/max/step range config. Membership gates the semantic range check
// (validateWidgetRange) — JSON Schema already enforces each value's type and a
// positive step, but the cross-field min > max relationship is checked here.
var numberWidgets = map[string]bool{
	"number-field": true,
	"slider":       true,
	"stepper":      true,
}

// optionConstrainedArrayWidgets is the subset of the array family that requires a
// bounded option set. Membership gates the options-presence check
// (validateWidgetOptions): a multiselect/checkbox-group with no declared options
// has nothing for a viewer to choose from and fails RESOLVE_CONFIG_INVALID. The
// freeform `tag-input` is deliberately absent — it resolves without options.
var optionConstrainedArrayWidgets = map[string]bool{
	"multiselect":    true,
	"checkbox-group": true,
}

// resolveWidgets walks the assembled resolved tree and validates every variable
// widget's binding: the named variable must be visible in the widget's scope
// (else VAR_UNDEFINED) and its declared type must be one the widget's family
// permits (else WIDGET_TYPE_MISMATCH). Non-widget nodes are left untouched. It is
// fail-fast: the first offending widget stops the walk.
func resolveWidgets(root *ResolvedInstance) error {
	return checkWidget(root, "root")
}

// checkWidget validates one node's widget binding (if it is a widget) and
// recurses into children.
func checkWidget(inst *ResolvedInstance, path string) error {
	if err := validateWidgetBinding(inst, path); err != nil {
		return err
	}
	for i, child := range inst.Children {
		childPath := path + ".children[" + strconv.Itoa(i) + "]"
		if err := checkWidget(child, childPath); err != nil {
			return err
		}
	}
	return nil
}

// validateWidgetBinding enforces the widget↔variable type-compatibility contract
// for a single node. It is a no-op for nodes whose item type is not a registered
// widget family. For a widget it resolves the bound variable name from config,
// looks it up in the node's scoped environment, and checks the declared type
// against the family's permitted set.
//
// The `variable` key's presence and string shape are already guaranteed by the
// widget item-type schema (required, minLength 1) validated in Pass 2, so this
// pass only handles the semantic checks: visibility and type compatibility.
func validateWidgetBinding(inst *ResolvedInstance, path string) error {
	permitted, isWidget := widgetFamilies[inst.Type.Name]
	if !isWidget {
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

	if !permitted[v.Type] {
		return errors.NewCodedErrorWithDetails(errors.WIDGET_TYPE_MISMATCH,
			"widget is not compatible with the bound variable's declared type",
			map[string]any{
				"path":     path,
				"variable": name,
				"widget":   inst.Type.Name,
				"varType":  string(v.Type),
			})
	}

	if numberWidgets[inst.Type.Name] {
		if err := validateWidgetRange(inst, path); err != nil {
			return err
		}
	}

	if optionConstrainedArrayWidgets[inst.Type.Name] {
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
