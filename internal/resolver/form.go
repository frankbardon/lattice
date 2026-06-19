package resolver

// This file holds the E2-S1 FORM pass: after a form node is assembled,
// resolveForm validates that every child is a variable widget, reads the form's
// (schema-validated) flow-layout config, and normalizes it into a parallel
// layout.Flow attached to the node. It is the resolver hook E2-S1 adds for the
// `form` item type, mirroring resolveLayout for `container`; the
// normalization/validation of the flow itself lives in internal/layout.
//
// A form is the answer to "widgets shouldn't take up entire grid blocks": its
// children are packed into compact label+control cells and do NOT carry or
// consume a main-grid placement. To keep that surface coherent, a form rejects
// any non-widget child fail-fast with LAYOUT_FORM_CHILD_INVALID.
//
// The schema is structured so a future `mode: grid` discriminator (E2-S2) slots
// in alongside flow without disturbing this pass.

import (
	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/layout"
)

// formLayoutKey is the form config key carrying the flow-layout declaration
// ({mode, columns}). The form item-type schema validates its shape; this pass
// reads it to normalize the flow.
const formLayoutKey = "layout"

// resolveForm validates a form's children and normalizes its flow layout. It
// rejects non-widget children (LAYOUT_FORM_CHILD_INVALID), parses the flow
// config from the form's schema-validated config, and attaches the resulting
// layout.Flow to node. Caller guarantees node is a form.
func (r *Resolver) resolveForm(node *ResolvedInstance, path string) error {
	// Form children must be variable widgets: a form is a control surface, not a
	// general grouping container. Reject anything else fail-fast so the flow stays
	// a coherent set of label+control rows.
	for i, child := range node.Children {
		if !isWidgetType(child.Type.Name) {
			return errors.NewCodedErrorWithDetails(errors.LAYOUT_FORM_CHILD_INVALID,
				"a form may only contain variable-widget children",
				map[string]any{
					"path": childPath(path, i),
					"type": child.Type.Name,
				})
		}
	}

	cfg, err := parseFlowConfig(node.Config, path)
	if err != nil {
		return err
	}

	flow, err := layout.NormalizeFlow(cfg, len(node.Children), path)
	if err != nil {
		return err
	}
	node.Flow = flow
	return nil
}

// isWidgetType reports whether an item-type name is a registered variable widget
// family (the only children a form permits). It reuses the widget registry that
// is the single source of truth for "what is a widget" (see widget.go).
func isWidgetType(name string) bool {
	_, ok := widgetFamilies[name]
	return ok
}

// parseFlowConfig extracts the flow-layout declaration from a form's config into
// a layout.FlowConfig. An absent layout object (or absent column count) yields the
// zero FlowConfig, which NormalizeFlow defaults to a single column. The config has
// already passed item-type schema validation in Pass 2, so shapes are trusted;
// parseFlowConfig still guards types defensively and surfaces a coded error rather
// than panicking on a contract violation.
func parseFlowConfig(config map[string]any, path string) (layout.FlowConfig, error) {
	var fc layout.FlowConfig
	if config == nil {
		return fc, nil
	}
	raw, ok := config[formLayoutKey]
	if !ok {
		return fc, nil
	}
	lm, ok := raw.(map[string]any)
	if !ok {
		return fc, flowConfigTypeError(path, formLayoutKey, raw)
	}

	colsRaw, ok := lm["columns"]
	if !ok || colsRaw == nil {
		return fc, nil
	}
	f, ok := toFloat(colsRaw)
	if !ok || f != float64(int(f)) {
		return fc, flowConfigTypeError(path, "columns", colsRaw)
	}
	fc.Columns = int(f)
	fc.HasColumns = true
	return fc, nil
}

// flowConfigTypeError reports a form layout field with an unexpected JSON type.
// The layout is schema-validated upstream, so this indicates a contract
// mismatch; it is surfaced as LAYOUT_FORM_COLUMNS_INVALID to stay fail-fast and
// in-domain.
func flowConfigTypeError(path, field string, value any) error {
	return errors.NewCodedErrorWithDetails(errors.LAYOUT_FORM_COLUMNS_INVALID,
		"form layout field has an unexpected type",
		map[string]any{"path": path, "field": field, "value": value})
}
