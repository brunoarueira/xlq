// Package eval evaluates a filter.Expr against a model.Workbook,
// producing a JSON-marshalable result: a map[string]any, a []any, a
// scalar (string, float64, bool), or nil. See
// docs/adr/0008-filter-grammar-v1.md for the evaluation-semantics table
// this package implements.
//
// One shape isn't pinned down by that ADR, which only defines what
// indexing into a Workbook or a Sheet produces: what a *bare* Sheet
// value (".Sheet1", with no further Field/Index) serializes to on its
// own. This package treats it as a dense 2D array ([row][col], sized to
// Sheet.Dimensions()) - the natural extension of "a whole row" and "a
// whole column" already being dense 1D arrays, and it's what gives the
// whole-workbook object (keyed by sheet name) a well-defined value to
// put under each key.
package eval

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/brunoarueira/xlq/internal/filter"
	"github.com/brunoarueira/xlq/internal/model"
)

// Eval evaluates expr against wb and returns a JSON-marshalable result.
func Eval(expr filter.Expr, wb model.Workbook) (any, error) {
	v, err := eval(expr, wb)
	if err != nil {
		return nil, err
	}
	return toJSON(v), nil
}

// eval walks expr, threading a "current value" that may be a
// model.Workbook, a model.Sheet, a []any (an already-resolved row or
// column), a scalar, or nil - per ADR-0008's evaluation table. Sheets
// and workbooks are only converted to their final JSON shape once, in
// Eval, since a Field/Index still needs to see the real model type to
// dispatch correctly.
func eval(expr filter.Expr, cur any) (any, error) {
	switch e := expr.(type) {
	case filter.Identity:
		return cur, nil
	case filter.Field:
		base, err := eval(e.Base, cur)
		if err != nil {
			return nil, err
		}
		return applyField(base, e.Name)
	case filter.Index:
		base, err := eval(e.Base, cur)
		if err != nil {
			return nil, err
		}
		return applyIndex(base, e.N)
	case filter.Pipe:
		left, err := eval(e.Left, cur)
		if err != nil {
			return nil, err
		}
		return eval(e.Right, left)
	default:
		return nil, fmt.Errorf("eval: unknown expression type %T", expr)
	}
}

var (
	columnLetters = regexp.MustCompile(`^[A-Za-z]+$`)
	cellReference = regexp.MustCompile(`^([A-Za-z]+)([0-9]+)$`)
)

// applyField implements the Field{Name} column of ADR-0008's evaluation
// table.
func applyField(base any, name string) (any, error) {
	switch v := base.(type) {
	case nil:
		return nil, nil
	case model.Workbook:
		sheet, ok := findSheet(v, name)
		if !ok {
			return nil, nil
		}
		return sheet, nil
	case model.Sheet:
		switch {
		case columnLetters.MatchString(name):
			return wholeColumn(v, name), nil
		case cellReference.MatchString(name):
			return cellScalar(v, name), nil
		default:
			return nil, fmt.Errorf("eval: %q is not a valid column letter or cell reference on sheet %q", name, v.Name)
		}
	default:
		return nil, fmt.Errorf("eval: cannot access field %q on %s - not indexable further in v1", name, describeType(base))
	}
}

// applyIndex implements the Index{N} column of ADR-0008's evaluation
// table.
func applyIndex(base any, n int) (any, error) {
	switch v := base.(type) {
	case nil:
		return nil, nil
	case model.Workbook:
		return nil, fmt.Errorf("eval: cannot index a workbook by row number %d; index a sheet instead", n)
	case model.Sheet:
		if n < 1 {
			return nil, fmt.Errorf("eval: row number must be >= 1 (spreadsheet rows are 1-based), got %d", n)
		}
		return wholeRow(v, n), nil
	default:
		return nil, fmt.Errorf("eval: cannot index %s with [%d] - not indexable further in v1", describeType(base), n)
	}
}

// findSheet looks up a sheet by name, case-insensitively: an exact-case
// match wins if one exists, otherwise the first case-insensitive match
// in workbook order (see ADR-0008 on why this can't introduce genuine
// ambiguity against a well-formed file).
func findSheet(wb model.Workbook, name string) (model.Sheet, bool) {
	for _, s := range wb.Sheets {
		if s.Name == name {
			return s, true
		}
	}
	for _, s := range wb.Sheets {
		if strings.EqualFold(s.Name, name) {
			return s, true
		}
	}
	return model.Sheet{}, false
}

// wholeColumn returns the column identified by letters (case-insensitive)
// as a dense array of scalars, one per row from row 1 through the
// sheet's last row.
func wholeColumn(sheet model.Sheet, letters string) []any {
	col := columnIndex(letters)
	rows, _ := sheet.Dimensions()
	result := make([]any, rows)
	for _, r := range sheet.Rows {
		for _, c := range r.Cells {
			if c.Column == col {
				result[r.Index] = cellValue(c)
			}
		}
	}
	return result
}

// wholeRow returns row n (1-based) as a dense array of scalars, one per
// column from column 1 through the sheet's last column.
func wholeRow(sheet model.Sheet, n int) []any {
	rowIndex := n - 1
	_, cols := sheet.Dimensions()
	result := make([]any, cols)
	for _, r := range sheet.Rows {
		if r.Index == rowIndex {
			for _, c := range r.Cells {
				result[c.Column] = cellValue(c)
			}
			break
		}
	}
	return result
}

// cellScalar returns the scalar value of the cell at the A1 reference
// ref, or nil if the cell is absent.
func cellScalar(sheet model.Sheet, ref string) any {
	row, col := parseCellReference(ref)
	for _, r := range sheet.Rows {
		if r.Index == row {
			for _, c := range r.Cells {
				if c.Column == col {
					return cellValue(c)
				}
			}
			return nil
		}
	}
	return nil
}

// parseCellReference splits an A1 reference already matched by
// cellReference into its 0-based (row, col).
func parseCellReference(ref string) (row, col int) {
	m := cellReference.FindStringSubmatch(ref)
	col = columnIndex(m[1])
	n, _ := strconv.Atoi(m[2]) // guaranteed valid by cellReference
	return n - 1, col
}

// columnIndex converts case-insensitive spreadsheet column letters
// (A, B, ..., Z, AA, AB, ...) to a 0-based column index.
func columnIndex(letters string) int {
	n := 0
	for i := 0; i < len(letters); i++ {
		c := letters[i]
		switch {
		case c >= 'A' && c <= 'Z':
			n = n*26 + int(c-'A') + 1
		case c >= 'a' && c <= 'z':
			n = n*26 + int(c-'a') + 1
		}
	}
	return n - 1
}

// cellValue converts a model.Cell to its JSON-native scalar value, per
// ADR-0008: a formula cell's raw expression is never exposed, only its
// computed value.
func cellValue(c model.Cell) any {
	switch c.Kind {
	case model.String:
		v, _ := c.StringValue()
		return v
	case model.Number:
		v, _ := c.NumberValue()
		return v
	case model.Bool:
		v, _ := c.BoolValue()
		return v
	case model.Date:
		v, _ := c.DateValue()
		return v.Format(time.RFC3339)
	default: // model.Empty
		return nil
	}
}

// toJSON converts a final Workbook or Sheet result to its JSON shape;
// anything else (a row/column array, a scalar, nil) is already there.
func toJSON(v any) any {
	switch val := v.(type) {
	case model.Workbook:
		obj := make(map[string]any, len(val.Sheets))
		for _, s := range val.Sheets {
			obj[s.Name] = sheetGrid(s)
		}
		return obj
	case model.Sheet:
		return sheetGrid(val)
	default:
		return v
	}
}

// sheetGrid returns sheet as a dense 2D array, [row][col], sized to
// Sheet.Dimensions().
func sheetGrid(sheet model.Sheet) [][]any {
	rows, cols := sheet.Dimensions()
	grid := make([][]any, rows)
	for i := range grid {
		grid[i] = make([]any, cols)
	}
	for _, r := range sheet.Rows {
		for _, c := range r.Cells {
			grid[r.Index][c.Column] = cellValue(c)
		}
	}
	return grid
}

func describeType(v any) string {
	switch v.(type) {
	case []any:
		return "an array"
	case string:
		return "a string"
	case float64:
		return "a number"
	case bool:
		return "a boolean"
	default:
		return fmt.Sprintf("a %T", v)
	}
}
