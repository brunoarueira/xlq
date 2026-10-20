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
// the sheet's own highest row and column index present) - the natural
// extension of "a whole row" and "a whole column" already being dense
// 1D arrays, and it's what gives the whole-workbook object (keyed by
// sheet name) a well-defined value to put under each key.
//
// The "records" builtin (ADR-0011, filter.Builtin{Name: "records"}
// since ADR-0022 generalized it) adds two more public result shapes: a
// []map[string]any (the records array itself) and a map[string]any
// (one record, once indexed into). Internally,
// during evaluation, these are actually []record/record (ADR-0021,
// carrying header/column-letter metadata applyField's own fallback
// needs) - toJSON flattens either down to the public map shape before
// Eval returns, so nothing outside this package ever sees record at
// all.
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
// column), a record or []record (ADR-0011/ADR-0021, before toJSON
// flattens either to its public map shape), a scalar, or nil - per
// ADR-0008's evaluation table. Sheets, workbooks, and records are only
// converted to their final JSON shape once, in Eval, since a
// Field/Index/Slice still needs to see the real internal type to
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
	case filter.Slice:
		base, err := eval(e.Base, cur)
		if err != nil {
			return nil, err
		}
		return applySlice(base, e.Start, e.End)
	case filter.Pipe:
		left, err := eval(e.Left, cur)
		if err != nil {
			return nil, err
		}
		return eval(e.Right, left)
	case filter.Builtin:
		return applyBuiltin(e.Name, e.Args, cur)
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
			return wholeColumn(v, name)
		case cellReference.MatchString(name):
			return cellScalar(v, name)
		case rangeReference.MatchString(name):
			return sheetRange(v, name)
		default:
			return nil, fmt.Errorf("eval: %q is not a valid column letter, cell reference, or range on sheet %q", name, v.Name)
		}
	case record:
		// A single record, produced by filter.Builtin{Name: "records"}
		// (ADR-0011/ADR-0022): an
		// exact header-name match always wins (sheetHeaders already
		// guarantees every header name is globally unique, so this can
		// never be "the wrong column's" value); failing that, a valid,
		// in-range column letter falls back to that column's value by
		// position (ADR-0021). Either way, a miss is null, jq's own
		// convention for a missing object key - not an error, unlike
		// every other "not indexable further" case below.
		if val, ok := v.byName[name]; ok {
			return val, nil
		}
		// len(name) > 3 is rejected before columnIndex ever computes
		// it, the same guard parseCellReference already applies and
		// documents: columnIndex's running total can silently overflow
		// an int for an unbounded letter sequence, well before
		// maxExcelCol would otherwise reject it.
		if columnLetters.MatchString(name) && len(name) <= 3 {
			if idx := columnIndex(name); idx >= 0 && idx < maxExcelCol && idx < len(v.headers) {
				return v.byName[v.headers[idx]], nil
			}
		}
		return nil, nil
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
		return wholeRow(v, n)
	case []record:
		// Indexing into an array produced by filter.Builtin{Name:
		// "records"} (ADR-0011/ADR-0022).
		return indexArray(v, n), nil
	case []any:
		// Indexing into an already-produced row/column array
		// (wholeColumn/wholeRow's result), e.g. ".Sheet1.A[0]" - the
		// other half of ADR-0008's deferred "indexing an already-
		// produced array" note, closed by ADR-0020 alongside slicing.
		return indexArray(v, n), nil
	default:
		return nil, fmt.Errorf("eval: cannot index %s with [%d] - not indexable further in v1", describeType(base), n)
	}
}

// indexArray implements jq-style 0-based indexing, generic over any
// array element type produced by this package ([]record from records
// - ADR-0011/ADR-0021, []any from wholeColumn/wholeRow - generalized
// by ADR-0020): a negative index counts from the end (jq's own
// convention: .[-1] is the last element), not merely "no further than
// -1" - matching jq fully, not just its 0-based-from-the-front half.
// Still out of range (too high, or too negative even after counting
// back from the end) is null, not an error.
func indexArray[T any](v []T, n int) any {
	if n < 0 {
		n += len(v)
	}
	if n < 0 || n >= len(v) {
		return nil
	}
	return v[n]
}

// applySlice implements filter.Slice (ADR-0020): jq-style positional
// slicing, 0-based and end-exclusive. start/end are nil for an
// omitted bound (".[1:]", ".[:10]", ".[:]").
//
// model.Sheet and model.Workbook are deliberately excluded, each with
// its own clear error rather than a third, conflicting way to express
// a row range: ".Sheet1[5]" already means "spreadsheet row 5"
// (1-based, wholeRow), so ".Sheet1[5:10]" meaning "0-based array
// positions 5 through 9" right next to it would be a foot-gun on the
// exact same receiver type - and a row range is already expressible,
// unambiguously, via ADR-0012's own ".Sheet1[\"A5:J10\"]".
func applySlice(base any, start, end *int) (any, error) {
	switch v := base.(type) {
	case nil:
		return nil, nil
	case model.Workbook:
		return nil, fmt.Errorf("eval: cannot slice a workbook with [START:END]; select a sheet first, then slice an array it produces, e.g. .Sheet1.A[1:10] or .Sheet1 | records | .[0:10]")
	case model.Sheet:
		return nil, fmt.Errorf("eval: cannot slice a sheet with [START:END]; use an A1-style range reference instead, e.g. [\"A1:C10\"]")
	case []record:
		s, e := resolveSliceBounds(start, end, len(v))
		return v[s:e], nil
	case []any:
		s, e := resolveSliceBounds(start, end, len(v))
		return v[s:e], nil
	default:
		return nil, fmt.Errorf("eval: cannot slice %s with [START:END] - not sliceable in v1", describeType(base))
	}
}

// resolveSliceBounds turns start/end (nil for an omitted bound) into
// concrete, in-range [s, e) indices into a length-length array:
// negative bounds count from the end, and an out-of-range bound
// clamps rather than erroring - unlike indexArray's "out of range is
// null", slicing has no single obvious "absent slice" value to fall
// back on, and clamping matches jq's own actual behavior (".[0:1000]"
// on a 3-element array is simply ".[0:3]"). If, after resolving, end
// is still before start, the result is an empty slice, not an error -
// also matching jq (".[2:0]" is "[]").
func resolveSliceBounds(start, end *int, length int) (int, int) {
	s := clampBound(start, 0, length)
	e := clampBound(end, length, length)
	if e < s {
		e = s
	}
	return s, e
}

// clampBound resolves one optional slice bound: def if bound is nil,
// otherwise bound's value counted from the end if negative, then
// clamped to [0, length].
func clampBound(bound *int, def, length int) int {
	if bound == nil {
		return def
	}
	n := *bound
	if n < 0 {
		n += length
	}
	if n < 0 {
		n = 0
	}
	if n > length {
		n = length
	}
	return n
}

// applyBuiltin implements filter.Builtin (ADR-0022): dispatches by
// name to each builtin's own implementation. Argument count/type
// validation happens here, per builtin, not in the parser - the same
// way applyIndex validates a row number's own range at eval time, not
// parse time.
func applyBuiltin(name string, args []any, cur any) (any, error) {
	switch name {
	case "records":
		if len(args) != 0 {
			return nil, fmt.Errorf("eval: records takes no arguments, got %d", len(args))
		}
		return applyRecords(cur)
	case "tonumber":
		if len(args) != 0 {
			return nil, fmt.Errorf("eval: tonumber takes no arguments, got %d", len(args))
		}
		return applyToNumber(cur)
	default:
		// Unreachable today: internal/filter's own builtinNames set
		// already rejects any other name at parse time. Kept as a
		// clear internal-consistency error, not a silent fallthrough,
		// in case the two ever drift.
		return nil, fmt.Errorf("eval: unknown builtin %q", name)
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

// decimalNumber matches a plain decimal number - optional sign, digits,
// an optional fraction, an optional exponent - and nothing else.
// strconv.ParseFloat alone is too permissive for applyToNumber's own
// purposes: it also accepts Go/C99 hex floats ("0x1p2") and
// "NaN"/"Inf"/"Infinity" (any case), none of which is a number a
// spreadsheet cell's text would ever legitimately mean. Checking the
// text against this grammar first, before ParseFloat ever sees it,
// rules all of that out up front rather than trying to special-case
// every one of ParseFloat's own extra accepted forms after the fact.
//
// No ReDoS concern: Go's regexp package is RE2-based, which guarantees
// linear-time matching with no catastrophic backtracking regardless of
// pattern or input, and this pattern has no nested or overlapping
// quantifiers to begin with - three sequential, unambiguous groups.
// The input is also bounded in practice to one cell's text (Excel's
// own per-cell limit is 32,767 characters), not an attacker-controlled
// size.
var decimalNumber = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// applyToNumber implements the "tonumber" builtin (#37): LinkedIn (and
// many other exports) store numbers as text, so a cell's own value is
// often a string that every calculation needs converted first.
// "Never guess a cell's type from its text" (ADR-0010) is about
// *implicit* inference while reading a file; this is the opposite - an
// explicit, deliberate request to convert, which this package already
// decided (ADR-0022) should error rather than produce null when it
// can't.
//
// Matches jq's own tonumber in the cases that matter most - verified
// against real jq (1.8.2) rather than assumed: a number passes through
// unchanged, and a string that doesn't parse as a number errors, as
// does any other type (including null and bool - jq itself errors on
// both: "null (null) cannot be parsed as a number",
// "boolean (true) cannot be parsed as a number" - tonumber doesn't get
// the "null propagates" exception this package's own structural
// Field/Index/Slice operators have, since converting a value is not
// the same kind of operation as navigating into one).
//
// Deliberately diverges from jq on two points, both because the
// motivating case is messy real-world spreadsheet exports, not
// hand-written jq programs: leading/trailing whitespace is trimmed
// before parsing (verified jq itself does not: `" 3048 " | tonumber`
// errors there), since stray whitespace from a copy-pasted export is
// exactly the kind of incidental noise this conversion exists to look
// past; and "NaN"/"Inf"/"Infinity" text (which Go's strconv.ParseFloat
// accepts but a JSON/jq number never is, and decimalNumber above
// rejects) is explicitly not treated as a number, since a spreadsheet
// column is never legitimately expected to contain those as one.
func applyToNumber(cur any) (any, error) {
	switch v := cur.(type) {
	case float64:
		return v, nil
	case nil:
		return nil, fmt.Errorf("eval: tonumber: null cannot be parsed as a number")
	case string:
		trimmed := strings.TrimSpace(v)
		if !decimalNumber.MatchString(trimmed) {
			return nil, fmt.Errorf("eval: tonumber: %q cannot be parsed as a number", v)
		}
		n, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			// decimalNumber already guarantees valid decimal syntax;
			// only reachable for a value too large for a float64
			// (ParseFloat itself reports that as an error, "value out
			// of range", rather than silently returning +/-Inf).
			return nil, fmt.Errorf("eval: tonumber: %q cannot be parsed as a number", v)
		}
		return n, nil
	default:
		return nil, fmt.Errorf("eval: tonumber: %s cannot be parsed as a number", describeType(cur))
	}
}

// record is one row produced by sheetRecords (ADR-0011/ADR-0021).
// byName is the exact map[string]any every existing consumer already
// expects - toJSON flattens record (and []record) down to it, so
// -o json/-o csv/-o table never see this type at all, only its byName
// map. headers is the same column-index -> name slice sheetHeaders
// already builds, carried alongside (not merged into byName) so
// applyField's column-letter fallback can resolve a position without
// duplicating data under two keys in the map that actually gets
// serialized.
type record struct {
	byName  map[string]any
	headers []string // shared across every record from one sheetRecords call
}

// sheetRecords converts sheet into an array of record objects, one per
// data row (every row after row 1), keyed by header name, per
// ADR-0011. A row entirely absent from sheet.Rows (every cell blank)
// contributes no record at all - there's no Row to iterate for it -
// and neither does a row that is present but whose every cell is
// still blank (e.g. a formula that evaluated to blank, model.Empty):
// both produce an all-null record, which is exactly the noise a
// sheet's sparse representation already omits everywhere else.
//
// Every record needs every column's header key, nil-filled where that
// row has no cell, which means the full column count must be known
// before any record can be finalized - a later row can still introduce
// a wider column than every row seen so far. So this makes one pass
// over sheet.Rows, collecting header info (from row 0, if present),
// checking the maxDenseCells budget incrementally (on prospective,
// not-yet-allocated bounds - see sheetGrid's identical reasoning), and
// buffering every data row; building the actual record objects, keyed
// by the now-fully-known headers, happens afterward over that
// in-memory buffer, not a second pass over sheet.Rows itself.
func sheetRecords(sheet model.Sheet) ([]record, error) {
	headerCells := make(map[int]string) // column index -> header row's own value
	headerSeen := false
	var dataRows []model.Row
	var cols int

	for r, err := range sheet.All() {
		if err != nil {
			return nil, err
		}
		rowCount := r.Index + 1
		rowCols := cols
		for _, c := range r.Cells {
			if c.Column+1 > rowCols {
				rowCols = c.Column + 1
			}
		}
		if cells := int64(rowCount) * int64(rowCols); cells > maxDenseCells {
			return nil, fmt.Errorf("eval: sheet %q is at least %d x %d (%d cells), over the %d-cell limit for records; index a specific cell, row, or column instead", sheet.Name, rowCount, rowCols, cells, maxDenseCells)
		}
		cols = rowCols

		if !headerSeen && r.Index == 0 {
			headerSeen = true
			for _, c := range r.Cells {
				if name, ok := headerString(cellValue(c)); ok {
					headerCells[c.Column] = name
				}
			}
			continue // the header row itself, not a data row
		}
		dataRows = append(dataRows, r)
	}

	headers, err := sheetHeaders(headerCells, cols)
	if err != nil {
		return nil, err
	}

	records := make([]record, 0, len(dataRows))
	for _, r := range dataRows {
		byName := make(map[string]any, cols)
		for _, h := range headers {
			byName[h] = nil
		}
		hasValue := false
		for _, c := range r.Cells {
			v := cellValue(c)
			byName[headers[c.Column]] = v
			if v != nil {
				hasValue = true
			}
		}
		if !hasValue {
			continue
		}
		records = append(records, record{byName: byName, headers: headers})
	}
	return records, nil
}

// sheetHeaders returns one header name per column (0..cols-1), per
// ADR-0011: headerCells' own value for a column the header row set
// (sheetRecords' row-0 pass) - or that column's own A1 letter if the
// header row left it blank, never set it at all, or doesn't exist in
// the first place (an empty headerCells covers all three). Two
// columns resolving to the same rendered name is an error, naming
// both.
func sheetHeaders(headerCells map[int]string, cols int) ([]string, error) {
	headers := make([]string, cols)
	for j := range headers {
		if name, ok := headerCells[j]; ok {
			headers[j] = name
		} else {
			headers[j] = columnLetter(j)
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
// sheet's last row. It grows the result as rows stream by - sized to
// the highest row index actually seen - rather than pre-sizing from a
// separate Sheet.Dimensions() scan (removed, docs/adr/0018): a single
// pass over the whole sheet is still required (the column's own last
// row isn't known until iteration ends), but this way it's exactly one
// pass, not two.
func wholeColumn(sheet model.Sheet, letters string) ([]any, error) {
	col := columnIndex(letters)
	result := make([]any, 0)
	for r, err := range sheet.All() {
		if err != nil {
			return nil, err
		}
		for len(result) <= r.Index {
			result = append(result, nil)
		}
		for _, c := range r.Cells {
			if c.Column == col {
				result[r.Index] = cellValue(c)
			}
		}
	}
	return result, nil
}

// wholeRow returns row n (1-based) as a dense array of scalars, one per
// column from column 1 through the sheet's last column.
//
// Unlike every other operation in this file, this one genuinely can't
// early-exit: it's sized to the *sheet's* overall column count, not
// row n's own width (an existing, deliberately-unchanged behavior -
// see docs/adr/0018-streaming-xlsx-reader.md), so every row must still
// be scanned to find the true column bound even once row n itself has
// been seen. Memory still improves (nothing but the current row's
// cells is ever resident at once), but time complexity stays a full
// scan.
func wholeRow(sheet model.Sheet, n int) ([]any, error) {
	rowIndex := n - 1
	result := make([]any, 0)
	for r, err := range sheet.All() {
		if err != nil {
			return nil, err
		}
		maxCol := 0
		for _, c := range r.Cells {
			if c.Column+1 > maxCol {
				maxCol = c.Column + 1
			}
		}
		for len(result) < maxCol {
			result = append(result, nil)
		}
		if r.Index == rowIndex {
			for _, c := range r.Cells {
				result[c.Column] = cellValue(c)
			}
		}
	}
	return result, nil
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

	var outRows []model.Row
	for row, err := range sheet.All() {
		if err != nil {
			return model.Sheet{}, err
		}
		// sheet.Rows is ascending by Index: once a row past the
		// range's bottom edge has been seen, nothing further in the
		// sheet can still fall inside it.
		if row.Index > r2 {
			break
		}
		if row.Index < r1 {
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
			outRows = append(outRows, model.Row{Index: row.Index - r1, Cells: cells})
		}
	}

	// outRows (and each row's Cells) were only ever appended in the
	// ascending order sheet.Rows/row.Cells already guarantee, and the
	// anchor position is this range's maximum possible row/column, so
	// it can only ever need appending at the very end to stay sorted.
	anchorRow, anchorCol := r2-r1, c2-c1
	if n := len(outRows); n == 0 || outRows[n-1].Index != anchorRow {
		outRows = append(outRows, model.Row{Index: anchorRow, Cells: []model.Cell{model.NewEmptyCell().At(anchorCol)}})
	} else if last := &outRows[n-1]; len(last.Cells) == 0 || last.Cells[len(last.Cells)-1].Column != anchorCol {
		last.Cells = append(last.Cells, model.NewEmptyCell().At(anchorCol))
	}
	return model.NewSheet(fmt.Sprintf("%s!%s", sheet.Name, ref), outRows), nil
}

// cellScalar returns the scalar value of the cell at the A1 reference
// ref, or nil if the cell is absent.
func cellScalar(sheet model.Sheet, ref string) (any, error) {
	row, col, err := parseCellReference(ref)
	if err != nil {
		return nil, err
	}
	for r, err := range sheet.All() {
		if err != nil {
			return nil, err
		}
		if r.Index == row {
			for _, c := range r.Cells {
				if c.Column == col {
					return cellValue(c), nil
				}
			}
			return nil, nil
		}
		// sheet.Rows is ascending by Index, so once a row beyond the
		// target has been seen, the target is absent and the rest of
		// the sheet - potentially the vast majority of it - never
		// needs to be read.
		if r.Index > row {
			break
		}
	}
	return nil, nil
}

// maxExcelRow and maxExcelCol are Excel's own real limits (the
// maximum valid cell address is XFD1048576). A real file can never
// declare a cell beyond them, which is what ADR-0008 relies on when
// it calls wholeColumn/wholeRow - each grows its result to the highest
// row/column index actually seen, with no maxDenseCells check of their
// own - "never this dangerous": Excel's own limits already bound a
// full row or column to a few tens of megabytes at most. A range
// reference (ADR-0012) can state a row or column number straight from
// filter text, with nothing tying it to any real file's content, so
// without this check here a range like ".Sheet1[\"A1:A1000000000\"]"
// could make sheetRange report a billion rows, and ".A" on it would
// then try to grow a slice that size with nothing to stop it.
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
		// sheetGrid already enforces maxDenseCells within one sheet;
		// this tracks the same budget cumulatively *across* sheets
		// (PR #24), since many individually-fine sheets can still add
		// up to too much for a single whole-workbook result. Each
		// sheet's own grid is still built in full before its
		// contribution can be added to the running total - its true
		// (rows, cols) aren't known any earlier than that, now that
		// Sheet.Dimensions() is gone (docs/adr/0018) - bounded by
		// sheetGrid's own per-sheet budget check, so this can only
		// ever over-build by at most one sheet's worth before
		// rejecting.
		obj := make(map[string]any, len(val.Sheets))
		var total int64
		for _, s := range val.Sheets {
			grid, err := sheetGrid(s)
			if err != nil {
				return nil, err
			}
			var cols int
			if len(grid) > 0 {
				cols = len(grid[0])
			}
			total += int64(len(grid)) * int64(cols)
			if total > maxDenseCells {
				return nil, fmt.Errorf("eval: the workbook's sheets together would need over %d cells for a dense whole-workbook result (%q alone is %d x %d); index a specific sheet, cell, row, or column instead", maxDenseCells, s.Name, len(grid), cols)
			}
			obj[s.Name] = grid
		}
		return obj, nil
	case model.Sheet:
		return sheetGrid(val)
	case record:
		// Flattens to the exact map[string]any ADR-0011 already
		// defined - the headers slice (ADR-0021's own addition, for
		// applyField's column-letter fallback) is purely an
		// evaluation-time detail, never part of the JSON/CSV/table
		// shape.
		return val.byName, nil
	case []record:
		out := make([]map[string]any, len(val))
		for i, r := range val {
			out[i] = r.byName
		}
		return out, nil
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

// sheetGrid returns sheet as a dense 2D array, [row][col]. Rows and
// the overall column count both grow as sheet.Rows streams by -
// Sheet.Dimensions() is gone (docs/adr/0018) - with the maxDenseCells
// budget checked against the running (rows so far) x (cols so far)
// product on every row: since both factors only ever grow, a running
// product already over budget can only still be over budget once the
// sheet's true dimensions are known, so this rejects a huge sheet as
// soon as that's detectable rather than after scanning all of it -
// the same accept/reject outcome as the old two-pass check, reached
// sooner. Every row's own slice is widened to the sheet's final
// column count in a second pass once that's known - over the
// already-materialized grid, not another pass over sheet.Rows itself.
func sheetGrid(sheet model.Sheet) ([][]any, error) {
	grid := make([][]any, 0)
	var cols int
	for r, err := range sheet.All() {
		if err != nil {
			return nil, err
		}
		// Compute this row's prospective contribution to the running
		// (rows so far) x (cols so far) bound, and check budget on
		// those prospective values, *before* growing grid to
		// accommodate r.Index - a single sparse row far out (e.g. one
		// cell at Excel's real maximum address) must be rejected
		// without ever allocating a slice sized to get there.
		rowCount := r.Index + 1
		rowCols := cols
		for _, c := range r.Cells {
			if c.Column+1 > rowCols {
				rowCols = c.Column + 1
			}
		}
		if cells := int64(rowCount) * int64(rowCols); cells > maxDenseCells {
			return nil, fmt.Errorf("eval: sheet %q is at least %d x %d (%d cells), over the %d-cell limit for a dense result; index a specific cell, row, or column instead", sheet.Name, rowCount, rowCols, cells, maxDenseCells)
		}
		cols = rowCols

		for len(grid) <= r.Index {
			grid = append(grid, nil)
		}
		row := grid[r.Index]
		for _, c := range r.Cells {
			for len(row) <= c.Column {
				row = append(row, nil)
			}
			row[c.Column] = cellValue(c)
		}
		grid[r.Index] = row
	}
	for i, row := range grid {
		if row != nil && len(row) == cols {
			continue
		}
		widened := make([]any, cols)
		copy(widened, row)
		grid[i] = widened
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
	case []record:
		return "a records array"
	case record:
		return "a record"
	default:
		return fmt.Sprintf("a %T", v)
	}
}
