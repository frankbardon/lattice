package resolver

// This file holds the FORM pass: after a form node is assembled, resolveForm
// validates that every child is a variable widget, reads the form's
// (schema-validated) layout config, and — depending on the `layout.mode`
// discriminator — normalizes it into one of two PARALLEL representations
// attached to the node:
//
//   - flow mode (E2-S1): a layout.Flow (compact label+control rows, optionally
//     split into N columns). Widgets carry no main-grid placement.
//   - grid mode (E2-S2): a layout.Block, reusing the EXACT same weighted-grid
//     path as `container` (parseGridFrom + parsePlacement + layout.Normalize), so
//     placement bounds reuse LAYOUT_PLACEMENT_*.
//
// Flow and grid are two MODES of the one `form` type, not two types. It is the
// resolver hook for the `form` item type, mirroring resolveLayout for
// `container`; the normalization/validation logic lives in internal/layout.
//
// A form is the answer to "widgets shouldn't take up entire grid blocks": its
// children are variable widgets only. To keep that surface coherent, a form
// rejects any non-widget child fail-fast with LAYOUT_FORM_CHILD_INVALID,
// regardless of mode.

import (
	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/layout"
)

// formLayoutKey is the form config key carrying the layout declaration
// ({mode, columns, ...}). The form item-type schema validates its shape; this
// pass reads it to normalize the flow or grid.
const formLayoutKey = "layout"

// formModeKey is the layout sub-key carrying the mode discriminator.
const formModeKey = "mode"

// gridMode is the `layout.mode` value selecting the weighted-grid form
// arrangement. It mirrors layout.FlowMode ("flow"); kept local since the grid
// Block contract is shared with container and is not self-describing by mode.
const gridMode = "grid"

// resolveForm validates a form's children and normalizes its layout. It rejects
// non-widget children (LAYOUT_FORM_CHILD_INVALID), then dispatches on the
// form's `layout.mode` discriminator: flow mode attaches a layout.Flow, grid
// mode attaches a layout.Block via the same weighted-grid path container uses.
// Caller guarantees node is a form.
func (r *Resolver) resolveForm(node *ResolvedInstance, path string) error {
	// Form children must be variable widgets: a form is a control surface, not a
	// general grouping container. Reject anything else fail-fast so both flow and
	// grid modes stay a coherent set of widget cells.
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

	mode, err := parseFormMode(node.Config, path)
	if err != nil {
		return err
	}
	if mode == gridMode {
		return r.resolveFormGrid(node, path)
	}
	return r.resolveFormFlow(node, path)
}

// resolveFormFlow normalizes a flow-mode form: it parses the flow config and
// attaches the resulting layout.Flow to node (E2-S1 behaviour, unchanged).
func (r *Resolver) resolveFormFlow(node *ResolvedInstance, path string) error {
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

// resolveFormGrid normalizes a grid-mode form (E2-S2). It reuses the EXACT same
// weighted-grid path as container: the grid (columns/rows/gap weights) is read
// from the form's layout config and each child's explicit placement is parsed
// and validated against the grid bounds by layout.Normalize, which emits the
// shared LAYOUT_PLACEMENT_* errors naming the offending child. The result is a
// layout.Block attached to node.Layout, parallel to flow mode's node.Flow.
func (r *Resolver) resolveFormGrid(node *ResolvedInstance, path string) error {
	grid, err := parseFormGrid(node.Config, path)
	if err != nil {
		return err
	}

	placements := make([]layout.RawPlacement, len(node.Children))
	for i, child := range node.Children {
		rp, perr := parsePlacement(child.Placement, childPath(path, i))
		if perr != nil {
			return perr
		}
		placements[i] = rp
	}

	block, err := layout.Normalize(grid, placements, func(i int) string {
		return childPath(path, i)
	})
	if err != nil {
		return err
	}
	node.Layout = block
	return nil
}

// parseFormMode reads the `layout.mode` discriminator from a form's
// schema-validated config. An absent layout object or absent mode defaults to
// flow. The config has passed item-type schema validation upstream, so the mode
// is a known enum value; parseFormMode still guards types defensively and
// surfaces a coded error rather than panicking on a contract violation.
func parseFormMode(config map[string]any, path string) (string, error) {
	if config == nil {
		return layout.FlowMode, nil
	}
	raw, ok := config[formLayoutKey]
	if !ok {
		return layout.FlowMode, nil
	}
	lm, ok := raw.(map[string]any)
	if !ok {
		return "", flowConfigTypeError(path, formLayoutKey, raw)
	}
	mraw, ok := lm[formModeKey]
	if !ok || mraw == nil {
		return layout.FlowMode, nil
	}
	mode, ok := mraw.(string)
	if !ok {
		return "", flowConfigTypeError(path, formModeKey, mraw)
	}
	if mode != layout.FlowMode && mode != gridMode {
		// The schema enum bounds mode to {flow, grid}; an unknown value here is a
		// contract mismatch, surfaced fail-fast and in-domain.
		return "", flowConfigTypeError(path, formModeKey, mraw)
	}
	return mode, nil
}

// parseFormGrid extracts the grid-mode form's weighted grid from its layout
// config. The grid lives under config.layout (alongside mode), in the same
// columns/rows/gap shape container uses under config.grid, so it delegates to
// the shared parseGridFrom. An absent layout object yields the zero grid (a
// single implicit track per axis).
func parseFormGrid(config map[string]any, path string) (layout.Grid, error) {
	var g layout.Grid
	if config == nil {
		return g, nil
	}
	raw, ok := config[formLayoutKey]
	if !ok {
		return g, nil
	}
	lm, ok := raw.(map[string]any)
	if !ok {
		return g, flowConfigTypeError(path, formLayoutKey, raw)
	}
	return parseGridFrom(lm, path)
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
