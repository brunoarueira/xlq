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
//
// filter.Records (the "records" builtin, ADR-0011) adds two more
// result shapes: a []map[string]any (the records array itself) and a
// map[string]any (one record, once indexed into). Both are already
// JSON-marshalable as-is, so toJSON doesn't need a case for either.
//
// A range reference (ADR-0012, e.g. ".Sheet1[\"A3:C9\"]") adds no new
// shape at all: it's a third pattern applyField recognizes on a Sheet,
// alongside a column letter and a cell reference, and it evaluates to
// another model.Sheet (re-indexed to the range's own top-left corner).
// Every function in this package that already handles a Sheet - dense-
// grid conversion, cell/row/column access, records - handles a range
// result identically, with no awareness that a range exists at all.
package eval

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/brunoarueira/xlq/internal/filter"
	"github.com/brunoarueira/xlq/internal/model"
)

// Eval evaluates expr against wb and returns a JSON-marshalable result.
// expr must be read-only: an Assign (ADR-0014's "path = value" in-place
// edit) is rejected here rather than falling through to eval's default
// "unknown expression type" case, since it's a clear, specific user
// error (forgetting --in-place), not an internal inconsistency.
func Eval(expr filter.Expr, wb model.Workbook) (any, error) {
	if _, ok := expr.(filter.Assign); ok {
		return nil, fmt.Errorf("eval: an assignment filter (\"path = value\") only works with --in-place")
	}
	v, err := eval(expr, wb)
	if err != nil {
		return nil, err
	}
	return toJSON(v)
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
	case filter.Records:
		return applyRecords(cur)
	default:
		return nil, fmt.Errorf("eval: unknown expression type %T", expr)
	}
}

var (
	columnLetters = regexp.MustCompile(`^[A-Za-z]+$`)
	cellReference = regexp.MustCompile(`^([A-Za-z]+)([0-9]+)$`)
	// rangeReference matches two cell references joined by a colon,
	// e.g. "A3:C9" (ADR-0012). Each half is itself a cellReference.
	rangeReference = regexp.MustCompile(`^([A-Za-z]+[0-9]+):([A-Za-z]+[0-9]+)$`)
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
			return cellScalar(v, name)
		case rangeReference.MatchString(name):
			return sheetRange(v, name)
		default:
			return nil, fmt.Errorf("eval: %q is not a valid column letter, cell reference, or range on sheet %q", name, v.Name)
		}
	case map[string]any:
		// A single record, produced by filter.Records (ADR-0011): a
		// missing key is null, jq's own convention for a missing
		// object key - not an error, unlike every other "not
		// indexable further" case below.
		return v[name], nil
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
	case []map[string]any:
		// Indexing into an array produced by filter.Records (ADR-0011):
		// 0-based, jq-style, per ADR-0008's own deferred note that
		// indexing an already-produced array - as opposed to a
		// spreadsheet row number - should be. A negative index counts
		// from the end (jq's own convention: .[-1] is the last
		// element), not merely "no further than -1" - matching jq
		// fully, not just its 0-based-from-the-front half. Still out
		// of range (too high, or too negative even after counting
		// back from the end) is null, not an error.
		if n < 0 {
			n += len(v)
		}
		if n < 0 || n >= len(v) {
			return nil, nil
		}
		return v[n], nil
	default:
		return nil, fmt.Errorf("eval: cannot index %s with [%d] - not indexable further in v1", describeType(base), n)
	}
}

// applyRecords implements the "records" builtin (ADR-0011): an array of
// record objects, one per data row, keyed by header name.
func applyRecords(cur any) (any, error) {
	switch v := cur.(type) {
	case nil:
		return nil, nil
	case model.Sheet:
		return sheetRecords(v)
	default:
		return nil, fmt.Errorf("eval: records: cannot apply to %s - records only applies to a sheet", describeType(cur))
	}
}

// sheetRecords converts sheet into an array of record objects, one per
// data row (every row after row 1), keyed by header name, per
// ADR-0011. A row entirely absent from sheet.Rows (every cell blank)
// contributes no record at all - there's no Row to iterate for it -
// and neither does a row that is present but whose every cell is
// still blank (e.g. a formula that evaluated to blank, model.Empty):
// both produce an all-null record, which is exactly the noise a
// sheet's sparse representation already omits everywhere else.
func sheetRecords(sheet model.Sheet) ([]map[string]any, error) {
	rows, cols := sheet.Dimensions()
	cells := int64(rows) * int64(cols)
	if cells > maxDenseCells {
		return nil, fmt.Errorf("eval: sheet %q is %d x %d (%d cells), over the %d-cell limit for records; index a specific cell, row, or column instead", sheet.Name, rows, cols, cells, maxDenseCells)
	}

	headers, err := sheetHeaders(sheet, cols)
	if err != nil {
		return nil, err
	}

	records := make([]map[string]any, 0, len(sheet.Rows))
	for _, r := range sheet.Rows {
		if r.Index == 0 {
			continue // the header row itself, not a data row
		}
		record := make(map[string]any, cols)
		for _, h := range headers {
			record[h] = nil
		}
		hasValue := false
		for _, c := range r.Cells {
			v := cellValue(c)
			record[headers[c.Column]] = v
			if v != nil {
				hasValue = true
			}
		}
		if !hasValue {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

// sheetHeaders returns one header name per column (0..cols-1), per
// ADR-0011: a non-blank header cell's own cellValue(), stringified -
// or that column's own A1 letter if the header cell is blank or
// absent, including when the header row itself doesn't exist at all
// (sheet.Rows has no Row with Index 0). Two columns resolving to the
// same rendered name is an error, naming both.
func sheetHeaders(sheet model.Sheet, cols int) ([]string, error) {
	headers := make([]string, cols)
	for j := range headers {
		headers[j] = columnLetter(j)
	}
	// sheet.Rows is ordered ascending by Index, so the header row, if
	// present, is always sheet.Rows[0].
	if len(sheet.Rows) > 0 && sheet.Rows[0].Index == 0 {
		for _, c := range sheet.Rows[0].Cells {
			if name, ok := headerString(cellValue(c)); ok {
				headers[c.Column] = name
			}
		}
	}

	seen := make(map[string]int, cols) // rendered name -> first column index
	for j, name := range headers {
		if first, ok := seen[name]; ok {
			return nil, fmt.Errorf("eval: records: duplicate header %q at columns %s and %s", name, columnLetter(first), columnLetter(j))
		}
		seen[name] = j
	}
	return headers, nil
}

// headerString renders a cellValue() result as a record's map key: a
// string as-is, anything else (a number, a bool) via its own JSON
// text - the same text that value would already render as in JSON
// output, so a numeric 42 and a literal "42" are one rendered name,
// not two. Reports false for a blank header (a nil cellValue(), i.e.
// an absent or Empty cell).
func headerString(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	if s, ok := v.(string); ok {
		return s, true
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// columnLetter converts a 0-based column index to spreadsheet column
// letters (0 -> "A", 25 -> "Z", 26 -> "AA", ...), the reverse of
// columnIndex.
func columnLetter(col int) string {
	col++
	var letters []byte
	for col > 0 {
		col--
		letters = append([]byte{byte('A' + col%26)}, letters...)
		col /= 26
	}
	return string(letters)
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

// sheetRange implements the range pattern of applyField's Sheet
// dispatch (ADR-0012): two cell references joined by a colon, e.g.
// "A3:C9". The two corners don't need to be given top-left-to-
// bottom-right - whichever has the smaller row/column anchors the
// top-left, matching Excel's own order-independent range notation.
//
// The result is a new model.Sheet, re-indexed so the range's own
// top-left corner becomes row 0, column 0: every other operator
// (dense-grid conversion, further cell/row/column access, records)
// already works on a model.Sheet, so none of them need any new code
// to handle a range.
//
// The range's own declared bottom-right corner is always given a
// presence in the result, even if nothing real occupies it, via an
// explicit model.Empty cell if the slicing below didn't already
// produce one there. Without it, Dimensions() - which measures the
// highest row/column actually present - would silently shrink a
// sparse range to whatever's populated, dropping blank trailing rows
// and columns the range explicitly asked for (and, downstream, the
// header count records derives from them).
func sheetRange(sheet model.Sheet, ref string) (model.Sheet, error) {
	m := rangeReference.FindStringSubmatch(ref)
	r1, c1, err := parseCellReference(m[1])
	if err != nil {
		return model.Sheet{}, err
	}
	r2, c2, err := parseCellReference(m[2])
	if err != nil {
		return model.Sheet{}, err
	}
	if r1 > r2 {
		r1, r2 = r2, r1
	}
	if c1 > c2 {
		c1, c2 = c2, c1
	}

	out := model.Sheet{Name: fmt.Sprintf("%s!%s", sheet.Name, ref)}
	for _, row := range sheet.Rows {
		if row.Index < r1 || row.Index > r2 {
			continue
		}
		var cells []model.Cell
		for _, c := range row.Cells {
			if c.Column < c1 || c.Column > c2 {
				continue
			}
			cells = append(cells, c.At(c.Column-c1))
		}
		if len(cells) > 0 {
			out.Rows = append(out.Rows, model.Row{Index: row.Index - r1, Cells: cells})
		}
	}

	// Rows (and each row's Cells) were only ever appended in the
	// ascending order sheet.Rows/row.Cells already guarantee, and the
	// anchor position is this range's maximum possible row/column, so
	// it can only ever need appending at the very end to stay sorted.
	anchorRow, anchorCol := r2-r1, c2-c1
	if n := len(out.Rows); n == 0 || out.Rows[n-1].Index != anchorRow {
		out.Rows = append(out.Rows, model.Row{Index: anchorRow, Cells: []model.Cell{model.NewEmptyCell().At(anchorCol)}})
	} else if last := &out.Rows[n-1]; len(last.Cells) == 0 || last.Cells[len(last.Cells)-1].Column != anchorCol {
		last.Cells = append(last.Cells, model.NewEmptyCell().At(anchorCol))
	}
	return out, nil
}

// cellScalar returns the scalar value of the cell at the A1 reference
// ref, or nil if the cell is absent.
func cellScalar(sheet model.Sheet, ref string) (any, error) {
	row, col, err := parseCellReference(ref)
	if err != nil {
		return nil, err
	}
	for _, r := range sheet.Rows {
		if r.Index == row {
			for _, c := range r.Cells {
				if c.Column == col {
					return cellValue(c), nil
				}
			}
			return nil, nil
		}
	}
	return nil, nil
}

// maxExcelRow and maxExcelCol are Excel's own real limits (the
// maximum valid cell address is XFD1048576). A real file can never
// declare a cell beyond them, which is what ADR-0008 relies on when
// it calls wholeColumn/wholeRow - each allocates a slice sized from
// model.Sheet.Dimensions(), with no maxDenseCells check of their own -
// "never this dangerous": Excel's own limits already bound a full row
// or column to a few tens of megabytes at most. A range reference
// (ADR-0012) can state a row or column number straight from filter
// text, with nothing tying it to any real file's content, so without
// this check here a range like ".Sheet1[\"A1:A1000000000\"]" could
// make Dimensions() report a billion rows, and ".A" on it would then
// try to allocate a slice that size with nothing to stop it.
const (
	maxExcelRow = 1_048_576
	maxExcelCol = 16_384 // column XFD
)

// parseCellReference splits an A1 reference already matched by
// cellReference into its 0-based (row, col). A row of 0 or less (e.g.
// "A0") is syntactically a valid cellReference match but not a real A1
// address - spreadsheet rows are 1-based, matching applyIndex's own
// n < 1 rejection - and an out-of-range row number (overflowing int)
// is likewise rejected rather than silently discarded. A column
// longer than 3 letters is rejected before columnIndex ever computes
// it - not just because it's beyond maxExcelCol either way, but
// because columnIndex's running total can silently overflow an int
// well before that point for an unbounded letter sequence.
func parseCellReference(ref string) (row, col int, err error) {
	m := cellReference.FindStringSubmatch(ref)
	if len(m[1]) > 3 {
		return 0, 0, fmt.Errorf("eval: column in %q exceeds Excel's maximum column, XFD", ref)
	}
	col = columnIndex(m[1])
	if col >= maxExcelCol {
		return 0, 0, fmt.Errorf("eval: column in %q exceeds Excel's maximum column, XFD", ref)
	}
	n, convErr := strconv.Atoi(m[2])
	if convErr != nil {
		return 0, 0, fmt.Errorf("eval: row number in %q is out of range", ref)
	}
	if n < 1 {
		return 0, 0, fmt.Errorf("eval: row number in %q must be >= 1 (spreadsheet rows are 1-based)", ref)
	}
	if n > maxExcelRow {
		return 0, 0, fmt.Errorf("eval: row number in %q exceeds Excel's maximum row, %d", ref, maxExcelRow)
	}
	return n - 1, col, nil
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
func toJSON(v any) (any, error) {
	switch val := v.(type) {
	case model.Workbook:
		obj := make(map[string]any, len(val.Sheets))
		var total int64
		for _, s := range val.Sheets {
			rows, cols := s.Dimensions()
			total += int64(rows) * int64(cols)
			if total > maxDenseCells {
				return nil, fmt.Errorf("eval: the workbook's sheets together would need over %d cells for a dense whole-workbook result (%q alone is %d x %d); index a specific sheet, cell, row, or column instead", maxDenseCells, s.Name, rows, cols)
			}
			grid, err := sheetGrid(s)
			if err != nil {
				return nil, err
			}
			obj[s.Name] = grid
		}
		return obj, nil
	case model.Sheet:
		return sheetGrid(val)
	default:
		return v, nil
	}
}

// maxDenseCells caps how many cells the dense 2D grids
// (Sheet.Dimensions() rows * cols) this package will materialize add up
// to across a single result - one sheet's grid, or every sheet's grid
// combined for the whole workbook. A sheet whose only cell sits at
// Excel's actual maximum address (XFD1048576) has real dimensions of
// about 17 billion cells; without this guard, evaluating that sheet -
// or the whole workbook, via "." - would try to allocate on the order
// of hundreds of gigabytes, and a workbook of many smaller sheets could
// exhaust the same budget cumulatively even if no single sheet does. A
// whole row or whole column alone is never this dangerous, since
// Excel's own row/column limits (1,048,576 and 16,384) already bound
// either one to a few tens of megabytes at most.
const maxDenseCells = 10_000_000

// sheetGrid returns sheet as a dense 2D array, [row][col], sized to
// Sheet.Dimensions().
func sheetGrid(sheet model.Sheet) ([][]any, error) {
	rows, cols := sheet.Dimensions()
	cells := int64(rows) * int64(cols)
	if cells > maxDenseCells {
		return nil, fmt.Errorf("eval: sheet %q is %d x %d (%d cells), over the %d-cell limit for a dense result; index a specific cell, row, or column instead", sheet.Name, rows, cols, cells, maxDenseCells)
	}
	grid := make([][]any, rows)
	for i := range grid {
		grid[i] = make([]any, cols)
	}
	for _, r := range sheet.Rows {
		for _, c := range r.Cells {
			grid[r.Index][c.Column] = cellValue(c)
		}
	}
	return grid, nil
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
	case model.Workbook:
		return "a workbook"
	case []map[string]any:
		return "a records array"
	case map[string]any:
		return "a record"
	default:
		return fmt.Sprintf("a %T", v)
	}
}
