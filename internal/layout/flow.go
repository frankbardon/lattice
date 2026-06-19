package layout

// This file adds the FLOW layout mode (E2-S1): a compact, stacked
// label+control arrangement used by the `form` container-like item type. It is a
// representation PARALLEL to Block, not an extension of it — a form packs its
// widget children into label+control rows so each widget occupies a compact cell
// rather than consuming a weighted main-grid placement.
//
// Flow is deliberately CSS-agnostic, exactly like Block: it carries no styling,
// units, or keywords. It describes only how many columns the form flows its
// children into and, for each child, which (column, row) cell it lands in when
// the children are filled in document order, left-to-right then top-to-bottom.
// A CSS renderer and a native renderer are equally able to consume it.
//
// The `form` schema is structured so a future `mode: grid` discriminator (E2-S2)
// can reuse Block; this file owns the flow mode only.

import (
	"github.com/frankbardon/lattice/errors"
)

// defaultFlowColumns is the implied column count when a form omits its layout
// column count: a single column of stacked label+control rows.
const defaultFlowColumns = 1

// maxFlowColumns bounds the column count to a sane upper limit. A form is a
// compact control surface, not a full grid; an absurd column count almost
// certainly indicates an authoring mistake and is rejected fail-fast rather than
// silently honored.
const maxFlowColumns = 12

// Flow is the normalized, renderer-agnostic layout description of a `form`
// container in FLOW mode. It is attached to a form node in the resolved tree
// (ResolvedInstance.Flow) and is the stable contract downstream renderers
// consume, alongside (but distinct from) Block.
//
// Flow is intentionally unitless and CSS-free: it records the column count the
// form flows into and, per child, the cell that child occupies. Children are
// arranged in document order, row-major (fill a row left-to-right, then wrap to
// the next row), so a renderer can lay out label+control pairs without any
// authored placement on the children. Widgets in a form therefore do NOT
// require or consume a main-grid placement.
type Flow struct {
	// Mode is the layout mode discriminator. It is always "flow" for a Flow; the
	// field is carried explicitly so the resolved contract is self-describing and
	// a future weighted-grid mode (E2-S2) is distinguishable in the same field.
	Mode string `json:"mode"`

	// Columns is the number of columns the form flows its children into (>= 1).
	// A single column stacks every child vertically.
	Columns int `json:"columns"`

	// Cells are the computed cells of the form's direct children, in child
	// (document) order. The slice length equals the number of children.
	Cells []FlowCell `json:"cells"`
}

// FlowCell is one child's computed position within a form's flow. Both
// coordinates are 1-indexed and derived purely from the child's document order
// and the form's column count — flow placement is computed, never authored, so
// form widgets never carry a main-grid placement.
type FlowCell struct {
	// Column is the 1-indexed column this child lands in (1 <= Column <= Columns).
	Column int `json:"column"`

	// Row is the 1-indexed row this child lands in. Rows grow as children wrap
	// past the column count.
	Row int `json:"row"`
}

// FlowConfig is the parsed (not yet validated) flow-layout declaration from a
// form's config. Columns records the authored column count, with HasColumns
// distinguishing an absent count (defaults to defaultFlowColumns) from an
// explicit value that still must be validated.
type FlowConfig struct {
	Columns    int
	HasColumns bool
}

// NormalizeFlow turns a parsed flow config and a child count into a Flow. The
// column count is defaulted (absent -> 1) and validated (must be in
// [1, maxFlowColumns]); each child is then assigned a row-major cell. path is the
// form instance's path, embedded in any validation error so the author can locate
// the offending form.
func NormalizeFlow(cfg FlowConfig, childCount int, path string) (*Flow, error) {
	cols := defaultFlowColumns
	if cfg.HasColumns {
		cols = cfg.Columns
	}

	if cols < 1 {
		return nil, flowColumnsError(path, cols,
			"form flow column count must be a positive integer")
	}
	if cols > maxFlowColumns {
		return nil, flowColumnsError(path, cols,
			"form flow column count exceeds the maximum")
	}

	f := &Flow{
		Mode:    FlowMode,
		Columns: cols,
		Cells:   make([]FlowCell, 0, childCount),
	}
	for i := 0; i < childCount; i++ {
		f.Cells = append(f.Cells, FlowCell{
			Column: i%cols + 1,
			Row:    i/cols + 1,
		})
	}
	return f, nil
}

// FlowMode is the value of Flow.Mode and the `form` schema's flow-mode
// discriminator. Exported so the resolver and tests can refer to it without
// hardcoding the string.
const FlowMode = "flow"

// flowColumnsError builds a LAYOUT_FORM_COLUMNS_INVALID CodedError naming the
// offending form path and the rejected column count.
func flowColumnsError(path string, value int, msg string) error {
	return errors.NewCodedErrorWithDetails(errors.LAYOUT_FORM_COLUMNS_INVALID, msg,
		map[string]any{
			"path":  path,
			"field": "columns",
			"value": value,
		})
}
