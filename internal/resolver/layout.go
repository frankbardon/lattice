package resolver

// This file holds the E2-S1 layout pass: after a container node is assembled,
// resolveLayout reads its (schema-validated) grid config and its children's
// placement objects, normalizes them into a renderer-agnostic layout.Block, and
// attaches the block to the node. It is the only resolver hook E2-S1 adds; the
// normalization/validation logic itself lives in internal/layout.

import (
	"strconv"

	"github.com/frankbardon/lattice/errors"
	"github.com/frankbardon/lattice/internal/layout"
)

// resolveLayout normalizes a container's grid and its children's placements into
// a layout.Block and attaches it to node. It is a no-op for non-containers. The
// grid config has already passed item-type schema validation in Pass 2, so the
// shapes here are trusted; placement bounds/positivity are validated by
// layout.Normalize, which returns fail-fast LAYOUT_* CodedErrors naming the
// offending child instance path.
func (r *Resolver) resolveLayout(node *ResolvedInstance, path string) error {
	if !node.Container {
		return nil
	}

	grid, err := parseGrid(node.Config, path)
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

// childPath formats the instance path of the i-th child of the node at parent.
// It mirrors the path scheme used elsewhere in the resolver ("...children[i]").
func childPath(parent string, i int) string {
	return parent + ".children[" + strconv.Itoa(i) + "]"
}

// parseGrid extracts the relative-weight grid from a container's config. An
// absent grid (or absent track lists) yields a single implicit track per axis,
// matching layout.Normalize's defaults. The grid config has already been
// schema-validated, so numbers are well-typed; parseGrid still guards types
// defensively and surfaces a LAYOUT_GRID_INVALID-free internal error rather
// than panicking on a contract violation.
func parseGrid(config map[string]any, path string) (layout.Grid, error) {
	var g layout.Grid
	if config == nil {
		return g, nil
	}
	raw, ok := config["grid"]
	if !ok {
		return g, nil
	}
	gm, ok := raw.(map[string]any)
	if !ok {
		return g, gridTypeError(path, "grid", raw)
	}

	cols, err := parseWeights(gm["columns"], path, "columns")
	if err != nil {
		return g, err
	}
	rows, err := parseWeights(gm["rows"], path, "rows")
	if err != nil {
		return g, err
	}
	gap, err := parseNumber(gm["gap"], path, "gap")
	if err != nil {
		return g, err
	}

	g.Columns = cols
	g.Rows = rows
	g.Gap = gap
	return g, nil
}

// parseWeights converts a JSON array of relative weights into a []float64. A nil
// value means the track list was absent.
func parseWeights(v any, path, field string) ([]float64, error) {
	if v == nil {
		return nil, nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, gridTypeError(path, field, v)
	}
	out := make([]float64, 0, len(arr))
	for _, e := range arr {
		f, ok := toFloat(e)
		if !ok {
			return nil, gridTypeError(path, field, e)
		}
		out = append(out, f)
	}
	return out, nil
}

// parseNumber converts a single JSON number into a float64, treating a nil value
// as the zero default.
func parseNumber(v any, path, field string) (float64, error) {
	if v == nil {
		return 0, nil
	}
	f, ok := toFloat(v)
	if !ok {
		return 0, gridTypeError(path, field, v)
	}
	return f, nil
}

// parsePlacement extracts an explicit {colStart,colSpan,rowStart,rowSpan} block
// from an instance's placement object into a layout.RawPlacement, recording
// which coordinates were present. An absent placement yields the zero
// RawPlacement (all defaults applied downstream).
func parsePlacement(placement map[string]any, path string) (layout.RawPlacement, error) {
	var rp layout.RawPlacement
	if placement == nil {
		return rp, nil
	}

	var err error
	rp.ColStart, rp.HasColStart, err = parseCoord(placement, "colStart", path)
	if err != nil {
		return rp, err
	}
	rp.ColSpan, rp.HasColSpan, err = parseCoord(placement, "colSpan", path)
	if err != nil {
		return rp, err
	}
	rp.RowStart, rp.HasRowStart, err = parseCoord(placement, "rowStart", path)
	if err != nil {
		return rp, err
	}
	rp.RowSpan, rp.HasRowSpan, err = parseCoord(placement, "rowSpan", path)
	if err != nil {
		return rp, err
	}
	return rp, nil
}

// parseCoord reads one integer placement coordinate. It returns (value, present,
// error). A non-integer value is a LAYOUT_PLACEMENT_INVALID error; positivity
// and bounds are checked later by layout.Normalize.
func parseCoord(placement map[string]any, field, path string) (int, bool, error) {
	v, ok := placement[field]
	if !ok {
		return 0, false, nil
	}
	f, ok := toFloat(v)
	if !ok || f != float64(int(f)) {
		return 0, false, errors.NewCodedErrorWithDetails(errors.LAYOUT_PLACEMENT_INVALID,
			"placement coordinate must be an integer",
			map[string]any{"path": path, "field": field, "value": v})
	}
	return int(f), true, nil
}

// toFloat coerces a decoded JSON number (float64) or any integer-typed value
// into a float64. JSON decoding yields float64, but the helper also accepts ints
// so it is robust to alternative decoders.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

// gridTypeError reports a grid field with an unexpected JSON type. The grid is
// schema-validated upstream, so this indicates a contract mismatch; it is
// surfaced as a LAYOUT_PLACEMENT_INVALID-adjacent coded error to stay fail-fast.
func gridTypeError(path, field string, value any) error {
	return errors.NewCodedErrorWithDetails(errors.LAYOUT_PLACEMENT_INVALID,
		"container grid field has an unexpected type",
		map[string]any{"path": path, "field": field, "value": value})
}
