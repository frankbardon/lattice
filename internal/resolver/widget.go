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
	return nil
}
