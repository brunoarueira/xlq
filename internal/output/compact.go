// Package output post-processes an eval.Eval result for display,
// independent of the dense result shapes ADR-0008 defines - those stay
// exactly as evaluated; this only reshapes a copy for printing.
package output

// Compact drops rows and columns with no values from v, recursing into
// a whole-workbook result (map[string]any of per-sheet grids). v's
// shape otherwise matches what eval.Eval returns:
//
//   - map[string]any (a workbook): each value is compacted in place.
//   - [][]any (a sheet grid): a row is dropped if every cell in it is
//     nil; a column is dropped if every cell in every row is nil.
//     Both checks run against the original grid, so which columns
//     survive doesn't depend on which rows happened to survive first.
//   - []any (a single row or column): nil entries are dropped; there's
//     no second axis left to call a "column" once the shape is 1-D.
//   - anything else (a scalar or nil): returned unchanged.
func Compact(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, sheet := range val {
			out[k] = Compact(sheet)
		}
		return out
	case [][]any:
		return compactGrid(val)
	case []any:
		return compactSlice(val)
	default:
		return v
	}
}

func compactGrid(grid [][]any) [][]any {
	rows := len(grid)
	cols := 0
	for _, row := range grid {
		if len(row) > cols {
			cols = len(row)
		}
	}

	keepRow := make([]bool, rows)
	keepCol := make([]bool, cols)
	for i, row := range grid {
		for j, cell := range row {
			if cell != nil {
				keepRow[i] = true
				keepCol[j] = true
			}
		}
	}

	out := make([][]any, 0, rows)
	for i, row := range grid {
		if !keepRow[i] {
			continue
		}
		newRow := make([]any, 0, cols)
		for j, cell := range row {
			if keepCol[j] {
				newRow = append(newRow, cell)
			}
		}
		out = append(out, newRow)
	}
	return out
}

func compactSlice(s []any) []any {
	out := make([]any, 0, len(s))
	for _, v := range s {
		if v != nil {
			out = append(out, v)
		}
	}
	return out
}
