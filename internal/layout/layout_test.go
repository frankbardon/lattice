package layout

import (
	stderrors "errors"
	"math"
	"strconv"
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// asCoded unwraps the chain to the first *CodedError.
func asCoded(err error, target **errors.CodedError) bool {
	return stderrors.As(err, target)
}

// itoaTest is a tiny int-to-string helper for building expected nested paths.
func itoaTest(i int) string { return strconv.Itoa(i) }

// floatEq compares two fractional track sizes within a small epsilon, since
// normalization divides weights and exact equality is brittle.
func floatEq(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func tracksEq(got, want []float64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if !floatEq(got[i], want[i]) {
			return false
		}
	}
	return true
}

// TestNormalizeTracks covers weight normalization: weights become fractions
// summing to 1, absent/empty lists become a single full track, and degenerate
// totals fall back to equal fractions.
func TestNormalizeTracks(t *testing.T) {
	tests := []struct {
		name    string
		weights []float64
		want    []float64
	}{
		{"nil yields single full track", nil, []float64{1}},
		{"empty yields single full track", []float64{}, []float64{1}},
		{"single weight normalizes to 1", []float64{5}, []float64{1}},
		{"equal weights split evenly", []float64{1, 1}, []float64{0.5, 0.5}},
		{"unequal weights normalize", []float64{1, 3}, []float64{0.25, 0.75}},
		{"three uneven weights", []float64{2, 1, 1}, []float64{0.5, 0.25, 0.25}},
		{"fractional weights normalize", []float64{0.5, 0.5}, []float64{0.5, 0.5}},
		{"zero total falls back to equal", []float64{0, 0}, []float64{0.5, 0.5}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeTracks(tc.weights)
			if !tracksEq(got, tc.want) {
				t.Fatalf("normalizeTracks(%v) = %v, want %v", tc.weights, got, tc.want)
			}
			// Non-degenerate inputs must sum to 1.
			var sum float64
			for _, f := range got {
				sum += f
			}
			if !floatEq(sum, 1) {
				t.Errorf("track fractions sum = %v, want 1", sum)
			}
		})
	}
}

// fixedPath is a childPath function for tests that returns a constant path.
func fixedPath(string) func(int) string {
	return func(i int) string { return "root.children[0]" }
}

// TestNormalizePlacementBounds covers placement validation: defaults, in-bounds
// success, out-of-bounds failure, and non-positive spans/starts. Each case
// asserts the resulting Placement or the expected LAYOUT_* code and path.
func TestNormalizePlacementBounds(t *testing.T) {
	grid := Grid{Columns: []float64{1, 1, 1}, Rows: []float64{1, 1}} // 3x2

	tests := []struct {
		name     string
		rp       RawPlacement
		want     Placement
		wantCode errors.Code // "" => expect success
	}{
		{
			name: "defaults to first cell",
			rp:   RawPlacement{},
			want: Placement{ColStart: 1, ColSpan: 1, RowStart: 1, RowSpan: 1},
		},
		{
			name: "explicit in-bounds",
			rp:   RawPlacement{ColStart: 2, HasColStart: true, ColSpan: 2, HasColSpan: true, RowStart: 1, HasRowStart: true, RowSpan: 2, HasRowSpan: true},
			want: Placement{ColStart: 2, ColSpan: 2, RowStart: 1, RowSpan: 2},
		},
		{
			name: "spans to exact grid edge",
			rp:   RawPlacement{ColStart: 1, HasColStart: true, ColSpan: 3, HasColSpan: true, RowStart: 1, HasRowStart: true, RowSpan: 2, HasRowSpan: true},
			want: Placement{ColStart: 1, ColSpan: 3, RowStart: 1, RowSpan: 2},
		},
		{
			name:     "colStart beyond grid",
			rp:       RawPlacement{ColStart: 4, HasColStart: true},
			wantCode: errors.LAYOUT_PLACEMENT_OUT_OF_BOUNDS,
		},
		{
			name:     "colSpan overflows grid",
			rp:       RawPlacement{ColStart: 3, HasColStart: true, ColSpan: 2, HasColSpan: true},
			wantCode: errors.LAYOUT_PLACEMENT_OUT_OF_BOUNDS,
		},
		{
			name:     "rowStart beyond grid",
			rp:       RawPlacement{RowStart: 3, HasRowStart: true},
			wantCode: errors.LAYOUT_PLACEMENT_OUT_OF_BOUNDS,
		},
		{
			name:     "rowSpan overflows grid",
			rp:       RawPlacement{RowStart: 2, HasRowStart: true, RowSpan: 2, HasRowSpan: true},
			wantCode: errors.LAYOUT_PLACEMENT_OUT_OF_BOUNDS,
		},
		{
			name:     "zero colSpan is non-positive",
			rp:       RawPlacement{ColSpan: 0, HasColSpan: true},
			wantCode: errors.LAYOUT_PLACEMENT_INVALID,
		},
		{
			name:     "negative rowSpan is non-positive",
			rp:       RawPlacement{RowSpan: -1, HasRowSpan: true},
			wantCode: errors.LAYOUT_PLACEMENT_INVALID,
		},
		{
			name:     "zero colStart is non-positive",
			rp:       RawPlacement{ColStart: 0, HasColStart: true},
			wantCode: errors.LAYOUT_PLACEMENT_INVALID,
		},
		{
			name:     "negative rowStart is non-positive",
			rp:       RawPlacement{RowStart: -2, HasRowStart: true},
			wantCode: errors.LAYOUT_PLACEMENT_INVALID,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			block, err := Normalize(grid, []RawPlacement{tc.rp}, fixedPath(""))
			if tc.wantCode != "" {
				if err == nil {
					t.Fatalf("expected error %s, got nil (block=%+v)", tc.wantCode, block)
				}
				if !errors.HasCode(err, tc.wantCode) {
					t.Fatalf("error = %v, want code %s", err, tc.wantCode)
				}
				var ce *errors.CodedError
				if !asCoded(err, &ce) {
					t.Fatalf("error is not a CodedError: %v", err)
				}
				if got, _ := ce.Details["path"].(string); got != "root.children[0]" {
					t.Errorf("error path = %q, want %q", got, "root.children[0]")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(block.Placements) != 1 || block.Placements[0] != tc.want {
				t.Fatalf("placement = %+v, want %+v", block.Placements, tc.want)
			}
		})
	}
}

// TestNormalizeImplicitGrid verifies that a container with no declared tracks
// gets a 1x1 grid and that the implicit single cell still bounds placements.
func TestNormalizeImplicitGrid(t *testing.T) {
	block, err := Normalize(Grid{}, []RawPlacement{{}}, fixedPath(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !tracksEq(block.Columns, []float64{1}) || !tracksEq(block.Rows, []float64{1}) {
		t.Fatalf("implicit grid = cols %v rows %v, want [1]/[1]", block.Columns, block.Rows)
	}

	// A second child placed in column 2 of a 1x1 grid must fail.
	_, err = Normalize(Grid{}, []RawPlacement{
		{},
		{ColStart: 2, HasColStart: true},
	}, func(i int) string { return "root.children[1]" })
	if !errors.HasCode(err, errors.LAYOUT_PLACEMENT_OUT_OF_BOUNDS) {
		t.Fatalf("error = %v, want LAYOUT_PLACEMENT_OUT_OF_BOUNDS", err)
	}
}

// TestNormalizeErrorPathIndexing confirms the childPath callback is invoked with
// the offending child's index, so the error names the right instance — the basis
// for nested/subgrid containers each reporting their own child paths.
func TestNormalizeErrorPathIndexing(t *testing.T) {
	grid := Grid{Columns: []float64{1, 1}, Rows: []float64{1}}
	placements := []RawPlacement{
		{ColStart: 1, HasColStart: true}, // ok
		{ColStart: 5, HasColStart: true}, // out of bounds -> index 1
	}
	_, err := Normalize(grid, placements, func(i int) string {
		return "root.children[2].children[" + itoaTest(i) + "]"
	})
	var ce *errors.CodedError
	if !asCoded(err, &ce) {
		t.Fatalf("expected CodedError, got %v", err)
	}
	if got, _ := ce.Details["path"].(string); got != "root.children[2].children[1]" {
		t.Errorf("error path = %q, want nested subgrid path", got)
	}
}
