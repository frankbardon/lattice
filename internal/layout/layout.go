// Package layout interprets the CSS-agnostic container grid model into a
// stable, renderer-agnostic layout block.
//
// The grid model is deliberately free of CSS keywords and absolute units. A
// container declares its tracks as relative-weight lists (unitless numbers) and
// a relative gap; children are placed with explicit 1-indexed coordinates
// ({colStart, colSpan, rowStart, rowSpan}). Resolution (E2-S1) normalizes this
// into a Block: weights become fractional track sizes that sum to 1, and each
// child's placement is validated against the grid bounds and copied verbatim.
//
// A Block is purely structural. It carries no styling, no theming, and no CSS
// units, so a CSS renderer (E2-S3) and a native renderer are equally able to
// consume it. Subgrids fall out naturally: a container nested inside another
// container gets its own independent Block.
package layout

import (
	"github.com/frankbardon/lattice/errors"
)

// defaultTrackWeight is the implied weight of a track when a grid omits its
// columns/rows list (or declares an empty one): a single full-size track.
const defaultTrackWeight = 1.0

// Block is the normalized, renderer-agnostic layout description of a single
// container. It is attached to a container node in the resolved tree and is the
// stable contract downstream renderers consume.
//
// All sizes are relative and unitless: track fractions sum to 1.0 across each
// axis, and Gap is a relative unit (the author's gap weight, passed through).
// There are intentionally no CSS keywords or absolute units anywhere.
type Block struct {
	// Columns are the normalized column track sizes, in document order. Each is
	// a fraction in (0,1]; the slice sums to 1.0 (modulo float rounding). A grid
	// with no declared columns yields a single 1.0 track.
	Columns []float64 `json:"columns"`

	// Rows are the normalized row track sizes, in document order, with the same
	// semantics as Columns.
	Rows []float64 `json:"rows"`

	// Gap is the relative gap between tracks, passed through from the grid as a
	// unitless relative unit (default 0). It is not normalized: it is a weight
	// expressed in the same relative space as the track weights.
	Gap float64 `json:"gap"`

	// Placements are the validated placements of this container's direct
	// children, in child (document) order. The slice length equals the number of
	// children; a child that declared no placement gets a default placement
	// spanning the first cell (see normalizePlacement).
	Placements []Placement `json:"placements"`
}

// Placement is one child's validated, 1-indexed position within the parent
// container's grid. All fields are guaranteed in-bounds and positive once the
// Block is built: 1 <= ColStart, ColStart+ColSpan-1 <= len(Columns) (and
// likewise for rows).
type Placement struct {
	ColStart int `json:"colStart"`
	ColSpan  int `json:"colSpan"`
	RowStart int `json:"rowStart"`
	RowSpan  int `json:"rowSpan"`
}

// Grid is the parsed (not yet normalized) grid declaration from a container's
// config. Weights are relative and unitless. Nil/empty track lists mean a
// single implicit track.
type Grid struct {
	Columns []float64
	Rows    []float64
	Gap     float64
}

// RawPlacement is the parsed (not yet validated) placement declaration from a
// child instance. The bool fields record whether each coordinate was present in
// the source, so missing coordinates can default while explicit zero/negative
// values still fail validation.
type RawPlacement struct {
	ColStart, ColSpan, RowStart, RowSpan             int
	HasColStart, HasColSpan, HasRowStart, HasRowSpan bool
}

// Normalize turns a parsed grid and its children's raw placements into a Block.
//
// Track weights are normalized to fractions summing to 1.0 per axis. Each
// child's placement is defaulted (missing start -> 1, missing span -> 1) and
// then validated against the grid bounds; the first violation returns a
// fail-fast CodedError naming the offending child via childPath(i).
//
// childPath maps a child index to its instance path (e.g. "root.children[2]")
// so errors point at the exact offending instance.
func Normalize(grid Grid, placements []RawPlacement, childPath func(i int) string) (*Block, error) {
	cols := normalizeTracks(grid.Columns)
	rows := normalizeTracks(grid.Rows)

	b := &Block{
		Columns:    cols,
		Rows:       rows,
		Gap:        grid.Gap,
		Placements: make([]Placement, 0, len(placements)),
	}

	for i, rp := range placements {
		p, err := normalizePlacement(rp, len(cols), len(rows), childPath(i))
		if err != nil {
			return nil, err
		}
		b.Placements = append(b.Placements, p)
	}

	return b, nil
}

// normalizeTracks converts a relative-weight list into fractional track sizes
// summing to 1.0. An empty list yields a single full-size track. Non-positive
// or absent weights are guarded by the schema (minimum > 0) and by the caller;
// here a zero total degrades to equal fractions rather than dividing by zero.
func normalizeTracks(weights []float64) []float64 {
	if len(weights) == 0 {
		return []float64{defaultTrackWeight}
	}
	var total float64
	for _, w := range weights {
		total += w
	}
	out := make([]float64, len(weights))
	if total <= 0 {
		// Degenerate input (should be blocked by the schema's minimum); fall
		// back to equal fractions so the output is still a valid track list.
		eq := 1.0 / float64(len(weights))
		for i := range out {
			out[i] = eq
		}
		return out
	}
	for i, w := range weights {
		out[i] = w / total
	}
	return out
}

// normalizePlacement defaults missing coordinates, validates positivity and
// grid bounds, and returns the resolved Placement. cols/rows are the track
// counts of the parent grid. path is the offending child's instance path,
// embedded in any error.
func normalizePlacement(rp RawPlacement, cols, rows int, path string) (Placement, error) {
	p := Placement{
		ColStart: 1,
		ColSpan:  1,
		RowStart: 1,
		RowSpan:  1,
	}
	if rp.HasColStart {
		p.ColStart = rp.ColStart
	}
	if rp.HasColSpan {
		p.ColSpan = rp.ColSpan
	}
	if rp.HasRowStart {
		p.RowStart = rp.RowStart
	}
	if rp.HasRowSpan {
		p.RowSpan = rp.RowSpan
	}

	// Spans must be positive.
	if p.ColSpan < 1 {
		return Placement{}, placementError(path, "colSpan", p.ColSpan,
			"placement span must be a positive integer")
	}
	if p.RowSpan < 1 {
		return Placement{}, placementError(path, "rowSpan", p.RowSpan,
			"placement span must be a positive integer")
	}

	// Starts must be 1-indexed and within the grid.
	if p.ColStart < 1 {
		return Placement{}, placementError(path, "colStart", p.ColStart,
			"placement start must be a positive (1-indexed) integer")
	}
	if p.RowStart < 1 {
		return Placement{}, placementError(path, "rowStart", p.RowStart,
			"placement start must be a positive (1-indexed) integer")
	}

	// The placed cell range must fit inside the grid bounds.
	if p.ColStart > cols || p.ColStart+p.ColSpan-1 > cols {
		return Placement{}, boundsError(path, "column", p.ColStart, p.ColSpan, cols)
	}
	if p.RowStart > rows || p.RowStart+p.RowSpan-1 > rows {
		return Placement{}, boundsError(path, "row", p.RowStart, p.RowSpan, rows)
	}

	return p, nil
}

// placementError builds a LAYOUT_PLACEMENT_INVALID CodedError naming the
// offending instance path and field.
func placementError(path, field string, value int, msg string) error {
	return errors.NewCodedErrorWithDetails(errors.LAYOUT_PLACEMENT_INVALID, msg,
		map[string]any{
			"path":  path,
			"field": field,
			"value": value,
		})
}

// boundsError builds a LAYOUT_PLACEMENT_OUT_OF_BOUNDS CodedError describing the
// axis, the placed range, and the grid extent.
func boundsError(path, axis string, start, span, tracks int) error {
	return errors.NewCodedErrorWithDetails(errors.LAYOUT_PLACEMENT_OUT_OF_BOUNDS,
		"placement extends beyond the container grid bounds",
		map[string]any{
			"path":   path,
			"axis":   axis,
			"start":  start,
			"span":   span,
			"tracks": tracks,
		})
}
