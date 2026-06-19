package layout

import (
	"testing"

	"github.com/frankbardon/lattice/errors"
)

// TestNormalizeFlowColumns covers flow column-count handling: an absent count
// defaults to a single column, an explicit in-range count is honored, and
// out-of-range counts fail fast with LAYOUT_FORM_COLUMNS_INVALID naming the form
// path.
func TestNormalizeFlowColumns(t *testing.T) {
	tests := []struct {
		name     string
		cfg      FlowConfig
		want     int
		wantCode errors.Code // "" => expect success
	}{
		{"absent count defaults to single column", FlowConfig{}, 1, ""},
		{"explicit single column", FlowConfig{Columns: 1, HasColumns: true}, 1, ""},
		{"explicit multi column", FlowConfig{Columns: 3, HasColumns: true}, 3, ""},
		{"max columns honored", FlowConfig{Columns: maxFlowColumns, HasColumns: true}, maxFlowColumns, ""},
		{"zero columns rejected", FlowConfig{Columns: 0, HasColumns: true}, 0, errors.LAYOUT_FORM_COLUMNS_INVALID},
		{"negative columns rejected", FlowConfig{Columns: -2, HasColumns: true}, 0, errors.LAYOUT_FORM_COLUMNS_INVALID},
		{"above-max columns rejected", FlowConfig{Columns: maxFlowColumns + 1, HasColumns: true}, 0, errors.LAYOUT_FORM_COLUMNS_INVALID},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			flow, err := NormalizeFlow(tc.cfg, 0, "root.children[0]")
			if tc.wantCode != "" {
				if err == nil {
					t.Fatalf("expected error %s, got nil (flow=%+v)", tc.wantCode, flow)
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
			if flow.Mode != FlowMode {
				t.Errorf("flow mode = %q, want %q", flow.Mode, FlowMode)
			}
			if flow.Columns != tc.want {
				t.Errorf("flow columns = %d, want %d", flow.Columns, tc.want)
			}
		})
	}
}

// TestNormalizeFlowCellSplitting verifies that children are assigned row-major
// cells: they fill a row left-to-right across the column count, then wrap to the
// next row. Covers single-column stacking and a multi-column split with a
// partial final row.
func TestNormalizeFlowCellSplitting(t *testing.T) {
	tests := []struct {
		name       string
		columns    int
		childCount int
		want       []FlowCell
	}{
		{
			name:       "single column stacks vertically",
			columns:    1,
			childCount: 3,
			want: []FlowCell{
				{Column: 1, Row: 1},
				{Column: 1, Row: 2},
				{Column: 1, Row: 3},
			},
		},
		{
			name:       "two columns wrap row-major",
			columns:    2,
			childCount: 5,
			want: []FlowCell{
				{Column: 1, Row: 1},
				{Column: 2, Row: 1},
				{Column: 1, Row: 2},
				{Column: 2, Row: 2},
				{Column: 1, Row: 3}, // partial final row
			},
		},
		{
			name:       "three columns exact fill",
			columns:    3,
			childCount: 3,
			want: []FlowCell{
				{Column: 1, Row: 1},
				{Column: 2, Row: 1},
				{Column: 3, Row: 1},
			},
		},
		{
			name:       "no children yields empty cells",
			columns:    2,
			childCount: 0,
			want:       []FlowCell{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			flow, err := NormalizeFlow(FlowConfig{Columns: tc.columns, HasColumns: true}, tc.childCount, "root.children[0]")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(flow.Cells) != len(tc.want) {
				t.Fatalf("cell count = %d, want %d (cells=%+v)", len(flow.Cells), len(tc.want), flow.Cells)
			}
			for i := range tc.want {
				if flow.Cells[i] != tc.want[i] {
					t.Errorf("cell[%d] = %+v, want %+v", i, flow.Cells[i], tc.want[i])
				}
			}
		})
	}
}
