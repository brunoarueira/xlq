package eval

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/brunoarueira/xlq/internal/filter"
	"github.com/brunoarueira/xlq/internal/model"
)

// testWorkbook builds a small fixture workbook, independent of any file
// format:
//
//	Sheet1:      A1="hello"  B1=42                (row 1)
//	             A2=true     B2=Empty (explicit)   (row 2)
//	             C3=2026-09-25                     (row 3)
//	Data:        A1="from data sheet"
//
// C1 is never set at all (a genuinely absent cell, as opposed to B2's
// explicit-but-empty Kind), to exercise both null-producing paths.
var testDate = time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)

func testWorkbook() model.Workbook {
	sheet1 := model.NewSheet("Sheet1", []model.Row{
		{Index: 0, Cells: []model.Cell{
			model.NewStringCell("hello").At(0),
			model.NewNumberCell(42).At(1),
		}},
		{Index: 1, Cells: []model.Cell{
			model.NewBoolCell(true).At(0),
			model.NewEmptyCell().At(1),
		}},
		{Index: 2, Cells: []model.Cell{
			model.NewDateCell(testDate).At(2),
		}},
	})
	data := model.NewSheet("Data", []model.Row{
		{Index: 0, Cells: []model.Cell{
			model.NewStringCell("from data sheet").At(0),
		}},
	})
	return model.Workbook{Sheets: []model.Sheet{sheet1, data}}
}

func evalString(t *testing.T, input string) (any, error) {
	t.Helper()
	expr, err := filter.Parse(input)
	if err != nil {
		t.Fatalf("filter.Parse(%q): %v", input, err)
	}
	return Eval(expr, testWorkbook())
}

func TestEvalValid(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  any
	}{
		{"cell string", ".Sheet1.A1", "hello"},
		{"cell number", ".Sheet1.B1", 42.0},
		{"cell bool", ".Sheet1.A2", true},
		{"cell date as RFC3339", ".Sheet1.C3", testDate.Format(time.RFC3339)},
		{"case-insensitive sheet name", ".sheet1.A1", "hello"},
		{"case-insensitive cell reference", ".Sheet1.a1", "hello"},
		{"explicit Empty cell is null", ".Sheet1.B2", nil},
		{"genuinely absent cell within dimensions is null", ".Sheet1.C1", nil},
		{"absent cell beyond dimensions is null", ".Sheet1.Z99", nil},
		{"missing sheet is null", ".NoSuchSheet", nil},
		{"field on null propagates null", ".NoSuchSheet.A1", nil},
		{"index on null propagates null", ".NoSuchSheet[1]", nil},
		{"cross-sheet access", ".Data.A1", "from data sheet"},
		{
			"whole column, dense with nulls for absent cells",
			".Sheet1.B",
			[]any{42.0, nil, nil},
		},
		{
			"whole column beyond data is all null",
			".Sheet1.AA",
			[]any{nil, nil, nil},
		},
		{
			"whole row, dense with nulls for absent cells",
			".Sheet1[1]",
			[]any{"hello", 42.0, nil},
		},
		{
			"row beyond sheet's data is all null, still sized to columns",
			".Sheet1[999]",
			[]any{nil, nil, nil},
		},
		{
			"bare sheet is a dense 2D grid",
			".Sheet1",
			[][]any{
				{"hello", 42.0, nil},
				{true, nil, nil},
				{nil, nil, testDate.Format(time.RFC3339)},
			},
		},
		{"pipe is a no-op when followed by identity", ".Sheet1.A1 | .", "hello"},
		{"pipe rebinds identity to the left result", ".Sheet1 | .A1", "hello"},
		{"index into a column array (ADR-0020)", ".Sheet1.B[0]", 42.0},
		{"negative index into a column array counts from the end", ".Sheet1.B[-3]", 42.0},
		{"out-of-range index into a column array is null", ".Sheet1.B[99]", nil},
		{"index into a row array", ".Sheet1[1][1]", 42.0},
		{
			"slice a column array",
			".Sheet1.B[0:2]",
			[]any{42.0, nil},
		},
		{
			"slice with an omitted start",
			".Sheet1.B[:2]",
			[]any{42.0, nil},
		},
		{
			"slice with an omitted end",
			".Sheet1.B[1:]",
			[]any{nil, nil},
		},
		{
			"slice with both bounds omitted is the whole array",
			".Sheet1.B[:]",
			[]any{42.0, nil, nil},
		},
		{
			"slice with negative bounds counts from the end",
			".Sheet1.B[-2:-1]",
			[]any{nil},
		},
		{
			"slice clamps an out-of-range end rather than erroring",
			".Sheet1.B[0:1000]",
			[]any{42.0, nil, nil},
		},
		{
			"slice with end before start is an empty array, not an error",
			".Sheet1.B[2:0]",
			[]any{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := evalString(t, tc.input)
			if err != nil {
				t.Fatalf("Eval(%q) returned error: %v", tc.input, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Eval(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}

func TestEvalIdentityReturnsWholeWorkbook(t *testing.T) {
	got, err := evalString(t, ".")
	if err != nil {
		t.Fatalf("Eval(\".\") returned error: %v", err)
	}
	want := map[string]any{
		"Sheet1": [][]any{
			{"hello", 42.0, nil},
			{true, nil, nil},
			{nil, nil, testDate.Format(time.RFC3339)},
		},
		"Data": [][]any{
			{"from data sheet"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Eval(\".\") = %#v, want %#v", got, want)
	}
}

// TestEvalEmptySheetShapesAreEmptyArraysNotNull guards against the
// dense accumulators (sheetGrid, wholeColumn, wholeRow) reverting to a
// nil slice for a sheet with no rows at all, which would marshal to
// JSON null instead of the empty-array shape these operators have
// always produced.
func TestEvalEmptySheetShapesAreEmptyArraysNotNull(t *testing.T) {
	wb := model.Workbook{Sheets: []model.Sheet{model.NewSheet("Empty", nil)}}

	cases := []struct {
		name  string
		input string
		want  any
	}{
		{"bare empty sheet is an empty grid", ".Empty", [][]any{}},
		{"whole column of an empty sheet is an empty array", ".Empty.A", []any{}},
		{"whole row of an empty sheet is an empty array", ".Empty[1]", []any{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := filter.Parse(tc.input)
			if err != nil {
				t.Fatalf("filter.Parse(%q): %v", tc.input, err)
			}
			got, err := Eval(expr, wb)
			if err != nil {
				t.Fatalf("Eval(%q) returned error: %v", tc.input, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Eval(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}

// TestEvalGridOfCellLessRowsIsEmptyArraysNotNull guards a narrower case
// than TestEvalEmptySheetShapesAreEmptyArraysNotNull: a sheet whose
// rows are all *present but cell-less* (e.g. internal/csv's own
// handling of a blank line, which yields Row{Cells: []model.Cell{}},
// not an absent row). sheetGrid's per-row widening used to key off
// len(row) == cols alone, which treated a nil placeholder row as
// already correctly sized whenever cols was itself 0 (true here, since
// no cell anywhere ever grows it past 0) and left it nil - serializing
// as null instead of the same empty-array shape every other present
// row gets.
func TestEvalGridOfCellLessRowsIsEmptyArraysNotNull(t *testing.T) {
	wb := model.Workbook{Sheets: []model.Sheet{model.NewSheet("Blanks", []model.Row{
		{Index: 0, Cells: []model.Cell{}},
		{Index: 1, Cells: []model.Cell{}},
	})}}

	got, err := Eval(mustParse(t, ".Blanks"), wb)
	if err != nil {
		t.Fatalf("Eval(\".Blanks\") returned error: %v", err)
	}
	want := [][]any{{}, {}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Eval(\".Blanks\") = %#v, want %#v", got, want)
	}
}

func mustParse(t *testing.T, input string) filter.Expr {
	t.Helper()
	expr, err := filter.Parse(input)
	if err != nil {
		t.Fatalf("filter.Parse(%q): %v", input, err)
	}
	return expr
}

func TestEvalInvalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"row 0 is invalid", ".Sheet1[0]"},
		{"negative row is invalid", ".Sheet1[-1]"},
		{"cell reference with row 0 is invalid", ".Sheet1.A0"},
		{"indexing a workbook by row number", ".[1]"},
		{"name that isn't a column letter or cell reference", ".Sheet1.Sheet_1"},
		{"field on a scalar", ".Sheet1.A1.foo"},
		{"index on a scalar", ".Sheet1.A1[0]"},
		{"field on a row array", ".Sheet1[1].A1"},
		{"an assignment filter requires --in-place, not Eval", `.Sheet1.A1 = "x"`},
		{"slicing a sheet directly", `.Sheet1[1:3]`},
		{"slicing a workbook", `.[1:3]`},
		{"slicing a scalar", ".Sheet1.A1[0:1]"},
		{"records rejects arguments at eval time (ADR-0022)", `.Sheet1 | records("x")`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := evalString(t, tc.input)
			if err == nil {
				t.Errorf("Eval(%q): want error, got nil", tc.input)
			}
		})
	}
}

// TestEvalAssignRejectedWithClearError confirms an assignment filter
// fails with a specific, actionable message (forgot --in-place) rather
// than falling through to eval's generic "unknown expression type"
// case, which would be confusing for what's actually a plain user
// mistake, not an internal inconsistency.
func TestEvalAssignRejectedWithClearError(t *testing.T) {
	_, err := evalString(t, `.Sheet1.A1 = "x"`)
	if err == nil {
		t.Fatal("Eval(assignment): want error, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "--in-place") {
		t.Errorf("Eval(assignment) error = %q, want it to mention --in-place", got)
	}
}

// TestEvalHugeDimensionsRejected guards against materializing a dense
// grid sized by a sheet's Dimensions() when that would require an
// unreasonable amount of memory: a single cell at Excel's actual maximum
// address (XFD1048576) gives a sheet real dimensions of about 17 billion
// cells. Building the fixture itself only ever allocates one Row/Cell,
// so this test can't OOM even if the guard it's checking for were
// missing - Eval must reject it with an error, not attempt the
// allocation.
func TestEvalHugeDimensionsRejected(t *testing.T) {
	huge := model.Workbook{
		Sheets: []model.Sheet{
			model.NewSheet("Sheet1", []model.Row{
				{Index: 1048575, Cells: []model.Cell{
					model.NewNumberCell(1).At(16383),
				}},
			}),
		},
	}

	for _, input := range []string{".Sheet1", "."} {
		t.Run(input, func(t *testing.T) {
			expr, err := filter.Parse(input)
			if err != nil {
				t.Fatalf("filter.Parse(%q): %v", input, err)
			}
			if _, err := Eval(expr, huge); err == nil {
				t.Errorf("Eval(%q) on a sheet with real dimensions ~17 billion cells: want error, got nil", input)
			}
		})
	}
}

// TestEvalCumulativeWorkbookBudgetRejected guards against the
// whole-workbook path materializing every sheet's grid independently: no
// single sheet here is anywhere near maxDenseCells on its own (each is
// 2,000 x 2,000 = 4,000,000 cells), but three of them together (12M)
// exceed the 10M budget. Only ".", which builds every sheet's grid at
// once, can see this - a single sheet alone is always fine.
func TestEvalCumulativeWorkbookBudgetRejected(t *testing.T) {
	sheet := func(name string) model.Sheet {
		return model.NewSheet(name, []model.Row{
			{Index: 1999, Cells: []model.Cell{
				model.NewNumberCell(1).At(1999),
			}},
		})
	}
	wb := model.Workbook{Sheets: []model.Sheet{
		sheet("Sheet1"), sheet("Sheet2"), sheet("Sheet3"),
	}}

	if _, err := Eval(filter.Identity{}, wb); err == nil {
		t.Error("Eval(\".\") on three sheets whose grids sum to 12M cells: want error, got nil")
	}

	// Each sheet alone is still well within budget.
	for _, name := range []string{"Sheet1", "Sheet2", "Sheet3"} {
		expr, err := filter.Parse("." + name)
		if err != nil {
			t.Fatalf("filter.Parse: %v", err)
		}
		if _, err := Eval(expr, wb); err != nil {
			t.Errorf("Eval(%q) on a single 4M-cell sheet: want no error, got %v", "."+name, err)
		}
	}
}

// TestEvalRecords exercises the "records" builtin (ADR-0011) against
// testWorkbook()'s Sheet1: row 1 (A1="hello", B1=42) is the header row,
// row 2 (A2=true, B2=Empty) and row 3 (C3=date) are data rows. Column C
// has no header cell at all, so it falls back to its own letter, "C".
func TestEvalRecords(t *testing.T) {
	want := []map[string]any{
		{"hello": true, "42": nil, "C": nil},
		{"hello": nil, "42": nil, "C": testDate.Format(time.RFC3339)},
	}
	got, err := evalString(t, ".Sheet1 | records")
	if err != nil {
		t.Fatalf("Eval(%q) returned error: %v", ".Sheet1 | records", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Eval(%q) = %#v, want %#v", ".Sheet1 | records", got, want)
	}
}

// tonumberWorkbook is a small, dedicated fixture for the "tonumber"
// builtin (#37): one of every input shape tonumber needs to handle.
func tonumberWorkbook() model.Workbook {
	return model.Workbook{Sheets: []model.Sheet{model.NewSheet("Sheet1", []model.Row{
		{Index: 0, Cells: []model.Cell{
			model.NewStringCell("3048").At(0),     // A1: plain numeric text
			model.NewStringCell(" 42 ").At(1),     // B1: numeric text with whitespace
			model.NewStringCell("-10.5").At(2),    // C1: negative, decimal
			model.NewNumberCell(99).At(3),         // D1: already a number
			model.NewStringCell("abc").At(4),      // E1: unconvertible text
			model.NewStringCell("").At(5),         // F1: empty string
			model.NewStringCell("NaN").At(6),      // G1: rejected despite strconv.ParseFloat accepting it
			model.NewStringCell("Infinity").At(7), // H1: same
			model.NewBoolCell(true).At(8),         // I1: bool, not string/number
			model.NewStringCell("0x1p2").At(9),    // J1: Go/C99 hex float - also rejected, not treated as 4
			model.NewStringCell("1.5e10").At(10),  // K1: valid scientific notation
		}},
	})}}
}

func TestEvalToNumber(t *testing.T) {
	wb := tonumberWorkbook()
	cases := []struct {
		name  string
		input string
		want  any
	}{
		{"plain numeric text", ".Sheet1.A1 | tonumber", 3048.0},
		{"whitespace is trimmed, a deliberate deviation from jq", ".Sheet1.B1 | tonumber", 42.0},
		{"negative decimal", ".Sheet1.C1 | tonumber", -10.5},
		{"already a number is a pass-through", ".Sheet1.D1 | tonumber", 99.0},
		{"empty parens is the same as the bare form", ".Sheet1.A1 | tonumber()", 3048.0},
		{"scientific notation", ".Sheet1.K1 | tonumber", 1.5e10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := filter.Parse(tc.input)
			if err != nil {
				t.Fatalf("filter.Parse(%q): %v", tc.input, err)
			}
			got, err := Eval(expr, wb)
			if err != nil {
				t.Fatalf("Eval(%q) returned error: %v", tc.input, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Eval(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}

func TestEvalToNumberInvalid(t *testing.T) {
	wb := tonumberWorkbook()
	cases := []struct {
		name  string
		input string
	}{
		{"unconvertible text", ".Sheet1.E1 | tonumber"},
		{"empty string", ".Sheet1.F1 | tonumber"},
		{"\"NaN\" is rejected despite strconv.ParseFloat accepting it", ".Sheet1.G1 | tonumber"},
		{"\"Infinity\" is rejected despite strconv.ParseFloat accepting it", ".Sheet1.H1 | tonumber"},
		{"a bool errors, matching jq - no null-style pass-through", ".Sheet1.I1 | tonumber"},
		{"a Go/C99 hex float is rejected, not silently parsed as 4", ".Sheet1.J1 | tonumber"},
		{"null errors, matching jq - tonumber has no null-propagates exception", ".Sheet1.ZZ1 | tonumber"},
		{"tonumber takes no arguments", `.Sheet1.A1 | tonumber("x")`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := filter.Parse(tc.input)
			if err != nil {
				t.Fatalf("filter.Parse(%q): %v", tc.input, err)
			}
			if _, err := Eval(expr, wb); err == nil {
				t.Errorf("Eval(%q): want error, got nil", tc.input)
			}
		})
	}
}

// todateWorkbook is a small, dedicated fixture for the "todate"
// builtin (#42): one of every input shape todate needs to handle.
func todateWorkbook() model.Workbook {
	return model.Workbook{Sheets: []model.Sheet{model.NewSheet("Sheet1", []model.Row{
		{Index: 0, Cells: []model.Cell{
			model.NewStringCell("7/28/2026").At(0),            // A1: US-order, matches "M/D/YYYY"
			model.NewStringCell("28/7/2026").At(1),            // B1: EU-order, matches "D/M/YYYY"
			model.NewStringCell("2026-07-28").At(2),           // C1: ISO-ish, matches "YYYY-MM-DD"
			model.NewStringCell("7/28/26").At(3),              // D1: 2-digit year, pivots to 2026
			model.NewStringCell("7/28/95").At(4),              // E1: 2-digit year, pivots to 1995
			model.NewStringCell("1282026").At(5),              // F1: no separators, needs backtracking for "MDDYYYY"
			model.NewStringCell("2/30/2026").At(6),            // G1: invalid date (no Feb 30)
			model.NewStringCell("7/28/2026/5").At(7),          // H1: would bind D twice against "M/D/YYYY/D"
			model.NewNumberCell(123).At(8),                    // I1: not a string at all
			model.NewBoolCell(true).At(9),                     // J1: not a string at all
			model.NewStringCell("7/28/68").At(10),             // K1: YY pivot boundary, pivots to 2068
			model.NewStringCell("7/28/69").At(11),             // L1: YY pivot boundary, pivots to 1969
			model.NewStringCell("28 of 7, March 2026").At(12), // M1: "March" is literal text; the real month value (7) comes from the M token
			model.NewStringCell(" 7/28/2026 ").At(13),         // N1: stray whitespace, trimmed before parsing
			model.NewStringCell("28 (MAY be 7) 2026").At(14),  // O1: all-caps "MAY" is literal; the real month value (7) comes from the M token
			model.NewStringCell("28-MAY-2026").At(15),         // P1: Oracle-style "DD-MON-YYYY" with no separate numeric month at all
		}},
	})}}
}

func TestEvalToDate(t *testing.T) {
	wb := todateWorkbook()
	cases := []struct {
		name  string
		input string
		want  any
	}{
		{"US order", `.Sheet1.A1 | todate("M/D/YYYY")`, "2026-07-28T00:00:00Z"},
		{"EU order - same date, different token order", `.Sheet1.B1 | todate("D/M/YYYY")`, "2026-07-28T00:00:00Z"},
		{"ISO-ish order", `.Sheet1.C1 | todate("YYYY-MM-DD")`, "2026-07-28T00:00:00Z"},
		{"2-digit year pivots to 2000s", `.Sheet1.D1 | todate("M/D/YY")`, "2026-07-28T00:00:00Z"},
		{"2-digit year pivots to 1900s", `.Sheet1.E1 | todate("M/D/YY")`, "1995-07-28T00:00:00Z"},
		{"no separators, requires backtracking", `.Sheet1.F1 | todate("MDDYYYY")`, "2026-01-28T00:00:00Z"},
		{"YY pivot boundary: 68 pivots to 2068", `.Sheet1.K1 | todate("M/D/YY")`, "2068-07-28T00:00:00Z"},
		{"YY pivot boundary: 69 pivots to 1969", `.Sheet1.L1 | todate("M/D/YY")`, "1969-07-28T00:00:00Z"},
		{
			"a word starting with a token letter (\"March\") is literal, not misread as a 1-digit month",
			`.Sheet1.M1 | todate("D of M, March YYYY")`,
			"2026-07-28T00:00:00Z",
		},
		{
			"leading/trailing whitespace is trimmed, matching tonumber's own deviation from jq",
			`.Sheet1.N1 | todate("M/D/YYYY")`,
			"2026-07-28T00:00:00Z",
		},
		{
			"an all-caps word starting with a token letter (\"MAY\") is also literal, not misread as a month",
			`.Sheet1.O1 | todate("D (MAY be M) YYYY")`,
			"2026-07-28T00:00:00Z",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := filter.Parse(tc.input)
			if err != nil {
				t.Fatalf("filter.Parse(%q): %v", tc.input, err)
			}
			got, err := Eval(expr, wb)
			if err != nil {
				t.Fatalf("Eval(%q) returned error: %v", tc.input, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Eval(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}

// TestEvalToDateLiteralNotReinterpreted guards ADR-0023's own central
// fix: "January" used as an arbitrary literal separator (no month
// token in the format at all) must never be reinterpreted as a
// month-name directive - it should behave exactly like any other
// literal separator, matching only the identical literal text.
func TestEvalToDateLiteralNotReinterpreted(t *testing.T) {
	wb := model.Workbook{Sheets: []model.Sheet{model.NewSheet("Sheet1", []model.Row{
		{Index: 0, Cells: []model.Cell{model.NewStringCell("28 January 7 2026").At(0)}},
	})}}
	got, err := Eval(mustParse(t, `.Sheet1.A1 | todate("D January M YYYY")`), wb)
	if err != nil {
		t.Fatalf("Eval returned error: %v", err)
	}
	want := "2026-07-28T00:00:00Z"
	if got != want {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

// TestEvalToDateMonthNameAloneIsNotAToken guards the other side of the
// same fix TestEvalToDateLiteralNotReinterpreted covers: a word that
// starts with a token letter ("March") is correctly treated as
// literal text, not a month name - so a format relying on it alone to
// supply the month value, with no separate numeric month token, still
// correctly errors (missing month token) rather than silently
// guessing month=3 from the word.
func TestEvalToDateMonthNameAloneIsNotAToken(t *testing.T) {
	wb := model.Workbook{Sheets: []model.Sheet{model.NewSheet("Sheet1", []model.Row{
		{Index: 0, Cells: []model.Cell{model.NewStringCell("28 March 2026").At(0)}},
	})}}
	if _, err := Eval(mustParse(t, `.Sheet1.A1 | todate("D March YYYY")`), wb); err == nil {
		t.Error("Eval: want error (format has no month token), got nil")
	}
}

// comparisonWorkbook is a small, dedicated fixture for comparison
// operators (ADR-0024): one of every input shape a comparison needs to
// handle.
func comparisonWorkbook() model.Workbook {
	return model.Workbook{Sheets: []model.Sheet{model.NewSheet("Sheet1", []model.Row{
		{Index: 0, Cells: []model.Cell{
			model.NewNumberCell(42).At(0),   // A1: number
			model.NewNumberCell(42).At(1),   // B1: same number
			model.NewNumberCell(100).At(2),  // C1: a bigger number
			model.NewStringCell("42").At(3), // D1: the string "42" - not equal to the number 42
			model.NewStringCell("abc").At(4),
			model.NewStringCell("abd").At(5),
			model.NewBoolCell(true).At(6),
			model.NewBoolCell(false).At(7),
		}},
	})}}
}

func TestEvalComparison(t *testing.T) {
	wb := comparisonWorkbook()
	cases := []struct {
		name  string
		input string
		want  any
	}{
		{"equal numbers", ".Sheet1.A1 == .Sheet1.B1", true},
		{"unequal numbers", ".Sheet1.A1 == .Sheet1.C1", false},
		{"not-equal numbers", ".Sheet1.A1 != .Sheet1.C1", true},
		{"cross-type equality is simply unequal, not an error", ".Sheet1.A1 == .Sheet1.D1", false},
		{"null == null", ".Sheet1.ZZ1 == .Sheet1.ZZ2", true},
		{"null == a number is false, not an error", ".Sheet1.ZZ1 == .Sheet1.A1", false},
		{"literal number comparison", ".Sheet1.A1 == 42", true},
		{"literal string comparison", ".Sheet1.E1 == \"abc\"", true},
		{"literal bool comparison", ".Sheet1.G1 == true", true},
		{"literal null comparison", ".Sheet1.ZZ1 == null", true},
		{"numeric ordering: less than", ".Sheet1.A1 < .Sheet1.C1", true},
		{"numeric ordering: greater than", ".Sheet1.C1 > .Sheet1.A1", true},
		{"numeric ordering: less-or-equal, equal case", ".Sheet1.A1 <= .Sheet1.B1", true},
		{"numeric ordering: greater-or-equal, equal case", ".Sheet1.A1 >= .Sheet1.B1", true},
		{"numeric ordering against a literal", ".Sheet1.A1 > 10", true},
		{"string ordering (lexicographic)", ".Sheet1.E1 < .Sheet1.F1", true},
		{"null ordering is always false, never an error, on the left", ".Sheet1.ZZ1 > .Sheet1.A1", false},
		{"null ordering is always false, never an error, on the right", ".Sheet1.A1 > .Sheet1.ZZ1", false},
		{"null ordering against null is also false", ".Sheet1.ZZ1 < .Sheet1.ZZ2", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := filter.Parse(tc.input)
			if err != nil {
				t.Fatalf("filter.Parse(%q): %v", tc.input, err)
			}
			got, err := Eval(expr, wb)
			if err != nil {
				t.Fatalf("Eval(%q) returned error: %v", tc.input, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Eval(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}

func TestEvalComparisonInvalid(t *testing.T) {
	wb := comparisonWorkbook()
	cases := []struct {
		name  string
		input string
	}{
		{"cross-type ordering is an error, not a guessed ranking", ".Sheet1.E1 > .Sheet1.A1"},
		{"bool ordering is an error - no established ordering for bools", ".Sheet1.G1 > .Sheet1.H1"},
		{"comparing a whole sheet errors instead of a wrong DeepEqual answer", ".Sheet1 == .Sheet1"},
		{"ordering a sheet against null errors too, not a silent false", ".NoSuchSheet > .Sheet1"},
		{"comparing a whole workbook errors the same way", ". == ."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := filter.Parse(tc.input)
			if err != nil {
				t.Fatalf("filter.Parse(%q): %v", tc.input, err)
			}
			if _, err := Eval(expr, wb); err == nil {
				t.Errorf("Eval(%q): want error, got nil", tc.input)
			}
		})
	}
}

// TestEvalComparisonBogusOpWithNilOperand guards a hand-built
// filter.Comparison with an operator the parser could never actually
// produce (Eval is exported, so this is reachable without going
// through Parse at all) - specifically when one operand is nil, which
// used to short-circuit to a silent "false" before the operator was
// ever validated (caught in review).
func TestEvalComparisonBogusOpWithNilOperand(t *testing.T) {
	expr := filter.Comparison{Op: "bogus", Left: filter.Literal{Value: 5.0}, Right: filter.Literal{Value: nil}}
	if _, err := Eval(expr, model.Workbook{}); err == nil {
		t.Error("Eval: want error for an unrecognized operator, got nil")
	}
}

// mapSelectWorkbook is a small, dedicated fixture for map/select
// (ADR-0025): a header row (Name, Likes) and three data rows, with
// Likes already a real number - so map/select's own mechanics are
// exercised directly, without needing tonumber along the way too.
func mapSelectWorkbook() model.Workbook {
	return model.Workbook{Sheets: []model.Sheet{model.NewSheet("Sheet1", []model.Row{
		{Index: 0, Cells: []model.Cell{
			model.NewStringCell("Name").At(0),
			model.NewStringCell("Likes").At(1),
		}},
		{Index: 1, Cells: []model.Cell{
			model.NewStringCell("alice").At(0),
			model.NewNumberCell(150).At(1),
		}},
		{Index: 2, Cells: []model.Cell{
			model.NewStringCell("bob").At(0),
			model.NewNumberCell(50).At(1),
		}},
		{Index: 3, Cells: []model.Cell{
			model.NewStringCell("carol").At(0),
			model.NewNumberCell(300).At(1),
		}},
	})}}
}

func TestEvalMap(t *testing.T) {
	wb := mapSelectWorkbook()
	cases := []struct {
		name  string
		input string
		want  any
	}{
		{
			"map extracts a field across every record",
			".Sheet1 | records | map(.Likes)",
			[]any{150.0, 50.0, 300.0},
		},
		{
			"map transforms with a comparison",
			".Sheet1 | records | map(.Likes > 100)",
			[]any{true, false, true},
		},
		{
			"map over a plain []any (a column range)",
			".Sheet1.B[1:] | map(. > 100)",
			[]any{true, false, true},
		},
		{
			"map(.) is the identity map - exercises the toJSON record-recovery fix",
			".Sheet1 | records | map(.)",
			// []map[string]any, not []any{map[string]any{...}}: toJSON
			// promotes a []any whose every element is a flattened
			// record back to []map[string]any, so --csv/--table still
			// recognize it as a records table (caught in review -
			// without this, map(.) rendered as one opaque JSON-text
			// cell per row instead of a real table).
			[]map[string]any{
				{"Name": "alice", "Likes": 150.0},
				{"Name": "bob", "Likes": 50.0},
				{"Name": "carol", "Likes": 300.0},
			},
		},
		{
			"map composes with a further pipeline step",
			".Sheet1 | records | map(.Name) | .[0]",
			"alice",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := filter.Parse(tc.input)
			if err != nil {
				t.Fatalf("filter.Parse(%q): %v", tc.input, err)
			}
			got, err := Eval(expr, wb)
			if err != nil {
				t.Fatalf("Eval(%q) returned error: %v", tc.input, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Eval(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}

// TestEvalMapNilDescribesAsNull guards describeType's nil case (caught
// in review): without it, a nil input's error reads "got a <nil>"
// instead of "got null".
func TestEvalMapNilDescribesAsNull(t *testing.T) {
	expr, err := filter.Parse(".NoSuchSheet | map(.)")
	if err != nil {
		t.Fatalf("filter.Parse: %v", err)
	}
	_, err = Eval(expr, mapSelectWorkbook())
	if err == nil {
		t.Fatal("Eval: want error, got nil")
	}
	want := "eval: map requires an array, got null"
	if err.Error() != want {
		t.Errorf("Eval error = %q, want %q", err.Error(), want)
	}
}

func TestEvalMapInvalid(t *testing.T) {
	wb := mapSelectWorkbook()
	cases := []struct {
		name  string
		input string
	}{
		{"map requires an array, not a scalar", ".Sheet1.A1 | map(.)"},
		{"map requires an array, not a single record", ".Sheet1 | records | .[0] | map(.)"},
		{"map on null errors, matching jq, rather than propagating null", ".NoSuchSheet | map(.)"},
		{"map(select(...)) errors per-element - select needs an array, not one record", ".Sheet1 | records | map(select(.Likes > 100))"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := filter.Parse(tc.input)
			if err != nil {
				t.Fatalf("filter.Parse(%q): %v", tc.input, err)
			}
			if _, err := Eval(expr, wb); err == nil {
				t.Errorf("Eval(%q): want error, got nil", tc.input)
			}
		})
	}
}

func TestEvalSelect(t *testing.T) {
	wb := mapSelectWorkbook()
	cases := []struct {
		name  string
		input string
		want  any
	}{
		{
			"select filters records, keeping them unchanged",
			".Sheet1 | records | select(.Likes > 100)",
			// []record stays []record (selectOver preserves cur's own
			// slice type), which toJSON then flattens the same way it
			// already does for records on its own: []map[string]any,
			// not []any - unlike map, whose result is always []any.
			[]map[string]any{
				{"Name": "alice", "Likes": 150.0},
				{"Name": "carol", "Likes": 300.0},
			},
		},
		{
			"select matching nothing is [], not null",
			".Sheet1 | records | select(.Likes > 100000)",
			[]map[string]any{},
		},
		{
			"select over a plain []any preserves that type",
			".Sheet1.B[1:] | select(. > 100)",
			[]any{150.0, 300.0},
		},
		{
			"select composes with a further pipeline step",
			".Sheet1 | records | select(.Likes > 100) | map(.Name)",
			[]any{"alice", "carol"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := filter.Parse(tc.input)
			if err != nil {
				t.Fatalf("filter.Parse(%q): %v", tc.input, err)
			}
			got, err := Eval(expr, wb)
			if err != nil {
				t.Fatalf("Eval(%q) returned error: %v", tc.input, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Eval(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}

func TestEvalSelectInvalid(t *testing.T) {
	wb := mapSelectWorkbook()
	cases := []struct {
		name  string
		input string
	}{
		{"select requires an array, not a scalar", ".Sheet1.A1 | select(. == \"Name\")"},
		{"select's filter must be a boolean, not a jq-style truthy value", ".Sheet1 | records | select(.Likes)"},
		{"select on null errors, matching jq, rather than propagating null", ".NoSuchSheet | select(.)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := filter.Parse(tc.input)
			if err != nil {
				t.Fatalf("filter.Parse(%q): %v", tc.input, err)
			}
			if _, err := Eval(expr, wb); err == nil {
				t.Errorf("Eval(%q): want error, got nil", tc.input)
			}
		})
	}
}

// aggregatesWorkbook is a small, dedicated fixture for sum/count/avg
// (ADR-0026): a header row (Name, Likes) and three data rows, with
// bob's Likes cell left entirely blank - sum/avg's own sparse-column
// handling is this fixture's whole point.
func aggregatesWorkbook() model.Workbook {
	return model.Workbook{Sheets: []model.Sheet{model.NewSheet("Sheet1", []model.Row{
		{Index: 0, Cells: []model.Cell{
			model.NewStringCell("Name").At(0),
			model.NewStringCell("Likes").At(1),
		}},
		{Index: 1, Cells: []model.Cell{
			model.NewStringCell("alice").At(0),
			model.NewNumberCell(150).At(1),
		}},
		{Index: 2, Cells: []model.Cell{
			model.NewStringCell("bob").At(0),
			// Likes left blank.
		}},
		{Index: 3, Cells: []model.Cell{
			model.NewStringCell("carol").At(0),
			model.NewNumberCell(300).At(1),
		}},
	})}}
}

func TestEvalAggregates(t *testing.T) {
	wb := aggregatesWorkbook()
	cases := []struct {
		name  string
		input string
		want  any
	}{
		{"count on records, including the blank row", ".Sheet1 | records | count", 3.0},
		{"count on a mapped array, including the null", ".Sheet1 | records | map(.Likes) | count", 3.0},
		{"sum treats the blank as 0, not an error", ".Sheet1 | records | map(.Likes) | sum", 450.0},
		{"avg excludes the blank from the divisor, not just the total", ".Sheet1 | records | map(.Likes) | avg", 225.0},
		{"sum of an empty select result is null, not 0", ".Sheet1 | records | select(.Likes > 100000) | map(.Likes) | sum", nil},
		{"avg of an empty select result is null", ".Sheet1 | records | select(.Likes > 100000) | map(.Likes) | avg", nil},
		{"count of an empty select result is 0", ".Sheet1 | records | select(.Likes > 100000) | count", 0.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := filter.Parse(tc.input)
			if err != nil {
				t.Fatalf("filter.Parse(%q): %v", tc.input, err)
			}
			got, err := Eval(expr, wb)
			if err != nil {
				t.Fatalf("Eval(%q) returned error: %v", tc.input, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Eval(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}

// TestEvalSumAllNull guards ADR-0026's own cubic-caught fix: an
// all-null, non-empty array must sum to null, not 0 - distinct from
// (but just as important as) the already-covered literally-empty case.
func TestEvalSumAllNull(t *testing.T) {
	// Every row's Likes cell is entirely absent (not an empty string -
	// a genuinely blank cell), so map(.Likes) is [null, null] - non-
	// empty, but with no real number in it anywhere. Name stays
	// present so records() doesn't skip these rows as entirely blank
	// (ADR-0011).
	wb := model.Workbook{Sheets: []model.Sheet{model.NewSheet("Sheet1", []model.Row{
		{Index: 0, Cells: []model.Cell{model.NewStringCell("Name").At(0), model.NewStringCell("Likes").At(1)}},
		{Index: 1, Cells: []model.Cell{model.NewStringCell("alice").At(0)}},
		{Index: 2, Cells: []model.Cell{model.NewStringCell("bob").At(0)}},
	})}}
	cases := []struct {
		name  string
		input string
		want  any
	}{
		{"sum of an all-null array is null, not 0", ".Sheet1 | records | map(.Likes) | sum", nil},
		{"avg of an all-null array is null", ".Sheet1 | records | map(.Likes) | avg", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := filter.Parse(tc.input)
			if err != nil {
				t.Fatalf("filter.Parse(%q): %v", tc.input, err)
			}
			got, err := Eval(expr, wb)
			if err != nil {
				t.Fatalf("Eval(%q) returned error: %v", tc.input, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Eval(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}

func TestEvalAggregatesInvalid(t *testing.T) {
	wb := aggregatesWorkbook()
	cases := []struct {
		name  string
		input string
	}{
		{"count requires an array, not a scalar", ".Sheet1.A1 | count"},
		{"sum requires []any, not []record - project a field first", ".Sheet1 | records | sum"},
		{"avg requires []any, not []record - project a field first", ".Sheet1 | records | avg"},
		{"sum errors on a non-number, non-null element", ".Sheet1 | records | map(.Name) | sum"},
		{"avg errors on a non-number, non-null element", ".Sheet1 | records | map(.Name) | avg"},
		{"count on null errors, matching map/select's own precedent", ".NoSuchSheet | count"},
		{"sum on null errors", ".NoSuchSheet | sum"},
		{"avg on null errors", ".NoSuchSheet | avg"},
		{"sum takes no arguments", ".Sheet1.B | sum(5)"},
		{"count takes no arguments", ".Sheet1.B | count(5)"},
		{"avg takes no arguments", ".Sheet1.B | avg(5)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := filter.Parse(tc.input)
			if err != nil {
				t.Fatalf("filter.Parse(%q): %v", tc.input, err)
			}
			if _, err := Eval(expr, wb); err == nil {
				t.Errorf("Eval(%q): want error, got nil", tc.input)
			}
		})
	}
}

func TestEvalGroupBy(t *testing.T) {
	wb := model.Workbook{Sheets: []model.Sheet{model.NewSheet("Sheet1", []model.Row{
		{Index: 0, Cells: []model.Cell{
			model.NewStringCell("Name").At(0),
			model.NewStringCell("Category").At(1),
			model.NewStringCell("Likes").At(2),
		}},
		{Index: 1, Cells: []model.Cell{
			model.NewStringCell("alice").At(0),
			model.NewStringCell("fruit").At(1),
			model.NewNumberCell(150).At(2),
		}},
		{Index: 2, Cells: []model.Cell{
			model.NewStringCell("bob").At(0),
			model.NewStringCell("veg").At(1),
			model.NewNumberCell(50).At(2),
		}},
		{Index: 3, Cells: []model.Cell{
			model.NewStringCell("carol").At(0),
			model.NewStringCell("fruit").At(1),
			model.NewNumberCell(300).At(2),
		}},
		{Index: 4, Cells: []model.Cell{
			model.NewStringCell("dave").At(0),
			model.NewStringCell("veg").At(1),
			model.NewNumberCell(20).At(2),
		}},
		{Index: 5, Cells: []model.Cell{
			model.NewStringCell("eve").At(0),
			// Category left blank.
			model.NewNumberCell(10).At(2),
		}},
	})}}
	cases := []struct {
		name  string
		input string
		want  any
	}{
		{
			"groups come out in first-encountered order, not sorted",
			".Sheet1 | records | group_by(.Category) | map(.[0].Name)",
			[]any{"alice", "bob", "eve"},
		},
		{
			"each group preserves input order within it",
			".Sheet1 | records | group_by(.Category) | map(map(.Name))",
			[]any{[]any{"alice", "carol"}, []any{"bob", "dave"}, []any{"eve"}},
		},
		{
			"group_by composes with count to size each group",
			".Sheet1 | records | group_by(.Category) | map(count)",
			[]any{2.0, 2.0, 1.0},
		},
		{
			"group_by composes with map+sum to total each group",
			".Sheet1 | records | group_by(.Category) | map(map(.Likes) | sum)",
			[]any{450.0, 70.0, 10.0},
		},
		{
			"a null Category groups on its own, not an error",
			".Sheet1 | records | group_by(.Category) | map(.[0].Name) | .[2]",
			"eve",
		},
		{
			"group_by on a plain []any groups by identity",
			".Sheet1.A[1:] | group_by(.)",
			[]any{[]any{"alice"}, []any{"bob"}, []any{"carol"}, []any{"dave"}, []any{"eve"}},
		},
		{
			"group_by on an empty select result is an empty array",
			".Sheet1 | records | select(.Name == \"nonexistent\") | group_by(.Category)",
			[]any{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := filter.Parse(tc.input)
			if err != nil {
				t.Fatalf("filter.Parse(%q): %v", tc.input, err)
			}
			got, err := Eval(expr, wb)
			if err != nil {
				t.Fatalf("Eval(%q) returned error: %v", tc.input, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Eval(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}

func TestEvalGroupByInvalid(t *testing.T) {
	wb := mapSelectWorkbook()
	cases := []struct {
		name  string
		input string
	}{
		{"group_by requires an array, not a scalar", ".Sheet1.A1 | group_by(.)"},
		{"group_by on null errors, matching map/select's own precedent", ".NoSuchSheet | group_by(.)"},
		{"a record key is rejected - no single field to group by", ".Sheet1 | records | group_by(.)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := filter.Parse(tc.input)
			if err != nil {
				t.Fatalf("filter.Parse(%q): %v", tc.input, err)
			}
			if _, err := Eval(expr, wb); err == nil {
				t.Errorf("Eval(%q): want error, got nil", tc.input)
			}
		})
	}
}

// TestEvalGroupByNaN guards ADR-0027's own cubic-caught fix: a NaN key
// must error, not silently produce a nil (and therefore data-losing)
// group - a hand-built filter.Comparison can reach this without going
// through tonumber at all (which would otherwise refuse "NaN" as text).
func TestEvalGroupByNaN(t *testing.T) {
	wb := model.Workbook{Sheets: []model.Sheet{model.NewSheet("Sheet1", []model.Row{
		{Index: 0, Cells: []model.Cell{model.NewNumberCell(1).At(0)}},
		{Index: 1, Cells: []model.Cell{model.NewNumberCell(2).At(0)}},
	})}}
	// group_by(0.0 / 0.0)-equivalent: no arithmetic exists in the
	// grammar to produce NaN, so build the FilterArg directly.
	nan := filter.Literal{Value: math.NaN()}
	expr := filter.FilterArg{
		Name: "group_by",
		Arg:  nan,
	}
	base, err := filter.Parse(".Sheet1.A")
	if err != nil {
		t.Fatalf("filter.Parse: %v", err)
	}
	full := filter.Pipe{Left: base, Right: expr}
	if _, err := Eval(full, wb); err == nil {
		t.Error("Eval: want error for a NaN group key, got nil")
	}
}

func TestEvalToDateInvalid(t *testing.T) {
	wb := todateWorkbook()
	cases := []struct {
		name  string
		input string
	}{
		{"invalid date (no Feb 30)", `.Sheet1.G1 | todate("M/D/YYYY")`},
		{"duplicate token binds the same component twice", `.Sheet1.H1 | todate("M/D/YYYY/D")`},
		{"input doesn't match format", `.Sheet1.A1 | todate("YYYY-MM-DD")`},
		{"a number is not a string to parse", `.Sheet1.I1 | todate("M/D/YYYY")`},
		{"a bool is not a string to parse", `.Sheet1.J1 | todate("M/D/YYYY")`},
		{"null errors, no null-propagates exception", `.Sheet1.ZZ1 | todate("M/D/YYYY")`},
		{"missing format argument", `.Sheet1.A1 | todate`},
		{"too many arguments", `.Sheet1.A1 | todate("M/D/YYYY", "extra")`},
		{"format argument must be a string", `.Sheet1.A1 | todate(5)`},
		{"format missing a year token", `.Sheet1.A1 | todate("M/D")`},
		{"an all-caps month name alone (\"MAY\") supplies no month token either", `.Sheet1.P1 | todate("D-MAY-YYYY")`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := filter.Parse(tc.input)
			if err != nil {
				t.Fatalf("filter.Parse(%q): %v", tc.input, err)
			}
			if _, err := Eval(expr, wb); err == nil {
				t.Errorf("Eval(%q): want error, got nil", tc.input)
			}
		})
	}
}

// TestEvalRecordsEmptyParens guards ADR-0022's own claim that
// "records()" and bare "records" are the exact same Builtin, not two
// different code paths that happen to currently agree.
func TestEvalRecordsEmptyParens(t *testing.T) {
	bare, err := evalString(t, ".Sheet1 | records")
	if err != nil {
		t.Fatalf("Eval(\".Sheet1 | records\") returned error: %v", err)
	}
	parens, err := evalString(t, ".Sheet1 | records()")
	if err != nil {
		t.Fatalf("Eval(\".Sheet1 | records()\") returned error: %v", err)
	}
	if !reflect.DeepEqual(bare, parens) {
		t.Errorf("records() = %#v, want the same as bare records: %#v", parens, bare)
	}
}

func TestEvalRecordsIndexAndFieldAccess(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  any
	}{
		{"field by header name", `.Sheet1 | records | .[0].hello`, true},
		{"field by fallback column-letter header", `.Sheet1 | records | .[1].C`, testDate.Format(time.RFC3339)},
		{
			"column letter access falls back to position when the header is a real, different name (ADR-0021)",
			`.Sheet1 | records | .[0].A`,
			true, // column A's own value; its header is "hello", not "A"
		},
		{
			"column letter beyond the record's own width is null, not an error",
			`.Sheet1 | records | .[0].D`, // testWorkbook's Sheet1 has 3 columns (A-C); D is in-range for Excel but beyond this record's own width
			nil,
		},
		{
			"an overlong column letter is rejected before it could overflow, not treated as a field name match either",
			`.Sheet1 | records | .[0].ZZZZZZZZZZZZZZZZZZZZZZZZ`,
			nil,
		},
		{"missing field on a record is null, not an error", `.Sheet1 | records | .[0].NoSuchHeader`, nil},
		{"out-of-range index is null, not an error", `.Sheet1 | records | .[99]`, nil},
		{
			"negative index counts from the end, jq-style",
			`.Sheet1 | records | .[-1]`,
			map[string]any{"hello": nil, "42": nil, "C": testDate.Format(time.RFC3339)},
		},
		{"negative index too far back is null, not an error", `.Sheet1 | records | .[-99]`, nil},
		{"records on a missing sheet propagates null", `.NoSuchSheet | records`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := evalString(t, tc.input)
			if err != nil {
				t.Fatalf("Eval(%q) returned error: %v", tc.input, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Eval(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}

// TestEvalRecordsSlicing exercises ADR-0020's own motivating case:
// slicing a records array, e.g. "give me the top N rows".
func TestEvalRecordsSlicing(t *testing.T) {
	record0 := map[string]any{"hello": true, "42": nil, "C": nil}
	record1 := map[string]any{"hello": nil, "42": nil, "C": testDate.Format(time.RFC3339)}

	cases := []struct {
		name  string
		input string
		want  any
	}{
		{"first record via a slice", `.Sheet1 | records | .[0:1]`, []map[string]any{record0}},
		{"whole array via an open slice", `.Sheet1 | records | .[0:]`, []map[string]any{record0, record1}},
		{"out-of-range end clamps instead of erroring", `.Sheet1 | records | .[0:100]`, []map[string]any{record0, record1}},
		{"negative start counts from the end", `.Sheet1 | records | .[-1:]`, []map[string]any{record1}},
		{"end before start is an empty array, not an error", `.Sheet1 | records | .[1:0]`, []map[string]any{}},
		{"a slice still composes with further field access", `.Sheet1 | records | .[0:1][0].hello`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := evalString(t, tc.input)
			if err != nil {
				t.Fatalf("Eval(%q) returned error: %v", tc.input, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Eval(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}

func TestEvalRecordsInvalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"records on the whole workbook", ". | records"},
		{"records on an already-produced row array", ".Sheet1[1] | records"},
		{"records on an already-produced column array", ".Sheet1.A | records"},
		{"records on a scalar", ".Sheet1.A1 | records"},
		{"index on a single record", `.Sheet1 | records | .[0][0]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := evalString(t, tc.input); err == nil {
				t.Errorf("Eval(%q): want error, got nil", tc.input)
			}
		})
	}
}

// TestEvalRecordsSkipsEntirelyBlankDataRows confirms a data row with no
// cells at all contributes no record (model.Sheet's sparse
// representation never materializes it), rather than an all-null one -
// there are 2 data rows in the fixture (indexes 1 and 3), with index 2
// entirely absent.
func TestEvalRecordsSkipsEntirelyBlankDataRows(t *testing.T) {
	wb := model.Workbook{Sheets: []model.Sheet{model.NewSheet("Sheet1", []model.Row{
		{Index: 0, Cells: []model.Cell{model.NewStringCell("Name").At(0)}},
		{Index: 1, Cells: []model.Cell{model.NewStringCell("Alice").At(0)}},
		// Index 2 deliberately absent: an entirely blank row.
		{Index: 3, Cells: []model.Cell{model.NewStringCell("Bob").At(0)}},
	})}}
	expr, err := filter.Parse(".Sheet1 | records")
	if err != nil {
		t.Fatalf("filter.Parse: %v", err)
	}
	got, err := Eval(expr, wb)
	if err != nil {
		t.Fatalf("Eval returned error: %v", err)
	}
	want := []map[string]any{{"Name": "Alice"}, {"Name": "Bob"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Eval(%q) = %#v, want %#v", ".Sheet1 | records", got, want)
	}
}

// TestEvalRecordsSkipsRowPresentButAllCellsEmpty confirms the same
// skip applies to a row that *does* have a model.Row (so it isn't
// caught by the "absent from Sheet.Rows" case above) but whose only
// cell is explicitly model.Empty, e.g. a formula that evaluated to
// blank - still just noise, not a record worth emitting.
func TestEvalRecordsSkipsRowPresentButAllCellsEmpty(t *testing.T) {
	wb := model.Workbook{Sheets: []model.Sheet{model.NewSheet("Sheet1", []model.Row{
		{Index: 0, Cells: []model.Cell{model.NewStringCell("Name").At(0)}},
		{Index: 1, Cells: []model.Cell{model.NewEmptyCell().At(0)}},
		{Index: 2, Cells: []model.Cell{model.NewStringCell("Alice").At(0)}},
	})}}
	expr, err := filter.Parse(".Sheet1 | records")
	if err != nil {
		t.Fatalf("filter.Parse: %v", err)
	}
	got, err := Eval(expr, wb)
	if err != nil {
		t.Fatalf("Eval returned error: %v", err)
	}
	want := []map[string]any{{"Name": "Alice"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Eval(%q) = %#v, want %#v", ".Sheet1 | records", got, want)
	}
}

// TestEvalRecordsNumericHeaderLookup confirms a numeric header cell's
// stringified name can actually be looked up, with a genuinely
// non-null value - unlike testWorkbook()'s own fixture, where the
// column under the numeric "42" header happens to be empty in every
// data row, which can't distinguish a correct lookup from a missing
// key defaulting to null.
func TestEvalRecordsNumericHeaderLookup(t *testing.T) {
	wb := model.Workbook{Sheets: []model.Sheet{model.NewSheet("Sheet1", []model.Row{
		{Index: 0, Cells: []model.Cell{model.NewNumberCell(42).At(0)}},
		{Index: 1, Cells: []model.Cell{model.NewStringCell("value under 42").At(0)}},
	})}}
	expr, err := filter.Parse(`.Sheet1 | records | .[0]["42"]`)
	if err != nil {
		t.Fatalf("filter.Parse: %v", err)
	}
	got, err := Eval(expr, wb)
	if err != nil {
		t.Fatalf("Eval returned error: %v", err)
	}
	if want := "value under 42"; got != want {
		t.Errorf(`Eval(".Sheet1 | records | .[0][\"42\"]") = %#v, want %#v`, got, want)
	}
}

// TestEvalRecordsMissingHeaderRowFallsBackToLetters confirms a sheet
// whose row 1 doesn't exist at all (not just blank-but-present) still
// produces records, keyed by column letter for every column.
func TestEvalRecordsMissingHeaderRowFallsBackToLetters(t *testing.T) {
	wb := model.Workbook{Sheets: []model.Sheet{model.NewSheet("Sheet1", []model.Row{
		{Index: 1, Cells: []model.Cell{
			model.NewStringCell("x").At(0),
			model.NewStringCell("y").At(1),
		}},
	})}}
	expr, err := filter.Parse(".Sheet1 | records")
	if err != nil {
		t.Fatalf("filter.Parse: %v", err)
	}
	got, err := Eval(expr, wb)
	if err != nil {
		t.Fatalf("Eval returned error: %v", err)
	}
	want := []map[string]any{{"A": "x", "B": "y"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Eval(%q) = %#v, want %#v", ".Sheet1 | records", got, want)
	}
}

// TestEvalRecordsDuplicateHeaderRejected guards against the exact
// failure mode ADR-0011 calls out: two columns sharing header text -
// as a real export's two side-by-side tables can - would silently
// collide in a plain Go map, keeping only the last-written value.
func TestEvalRecordsDuplicateHeaderRejected(t *testing.T) {
	wb := model.Workbook{Sheets: []model.Sheet{model.NewSheet("Sheet1", []model.Row{
		{Index: 0, Cells: []model.Cell{
			model.NewStringCell("Post URL").At(0),
			model.NewStringCell("Engagements").At(1),
			model.NewStringCell("Post URL").At(2),
		}},
		{Index: 1, Cells: []model.Cell{
			model.NewStringCell("a").At(0),
			model.NewNumberCell(1).At(1),
			model.NewStringCell("b").At(2),
		}},
	})}}
	expr, err := filter.Parse(".Sheet1 | records")
	if err != nil {
		t.Fatalf("filter.Parse: %v", err)
	}
	if _, err := Eval(expr, wb); err == nil {
		t.Error("Eval on a sheet with duplicate headers: want error, got nil")
	}
}

// TestEvalRecordsNumericAndTextHeaderCollide confirms a numeric header
// cell and a literal-text header cell that render the same way are
// treated as one collision, not two distinct headers - the duplicate
// check runs on the final rendered key, not the pre-render cellValue().
func TestEvalRecordsNumericAndTextHeaderCollide(t *testing.T) {
	wb := model.Workbook{Sheets: []model.Sheet{model.NewSheet("Sheet1", []model.Row{
		{Index: 0, Cells: []model.Cell{
			model.NewNumberCell(42).At(0),
			model.NewStringCell("42").At(1),
		}},
	})}}
	expr, err := filter.Parse(".Sheet1 | records")
	if err != nil {
		t.Fatalf("filter.Parse: %v", err)
	}
	if _, err := Eval(expr, wb); err == nil {
		t.Error("Eval with a numeric 42 header and a literal \"42\" header: want error, got nil")
	}
}

// TestEvalRecordsHugeDimensionsRejected mirrors
// TestEvalHugeDimensionsRejected: records operates on model.Sheet
// directly, bypassing sheetGrid's own maxDenseCells check entirely, so
// it needs its own guard against the same unreasonable allocation.
func TestEvalRecordsHugeDimensionsRejected(t *testing.T) {
	huge := model.Workbook{
		Sheets: []model.Sheet{
			model.NewSheet("Sheet1", []model.Row{
				{Index: 1048575, Cells: []model.Cell{
					model.NewNumberCell(1).At(16383),
				}},
			}),
		},
	}
	expr, err := filter.Parse(".Sheet1 | records")
	if err != nil {
		t.Fatalf("filter.Parse: %v", err)
	}
	if _, err := Eval(expr, huge); err == nil {
		t.Error("Eval(\".Sheet1 | records\") on a sheet with real dimensions ~17 billion cells: want error, got nil")
	}
}

// TestEvalRange exercises the range-reference pattern (ADR-0012)
// against testWorkbook()'s Sheet1, confirming a bare range converts to
// a dense grid re-indexed to the range's own top-left corner.
func TestEvalRange(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  any
	}{
		{
			"bare range is a dense grid relative to the range",
			".Sheet1[\"A1:B2\"]",
			[][]any{
				{"hello", 42.0},
				{true, nil},
			},
		},
		{
			"reversed corners give the same range",
			".Sheet1[\"B2:A1\"]",
			[][]any{
				{"hello", 42.0},
				{true, nil},
			},
		},
		{
			"case-insensitive range reference",
			".Sheet1[\"a1:b2\"]",
			[][]any{
				{"hello", 42.0},
				{true, nil},
			},
		},
		{
			"cell access relative to the range, not the sheet",
			`.Sheet1["B1:C3"].A1`,
			42.0,
		},
		{
			"row access relative to the range",
			`.Sheet1["B1:C3"][1]`,
			[]any{42.0, nil},
		},
		{
			"single-cell range is still a 1x1 grid, not a bare scalar",
			`.Sheet1["A1:A1"]`,
			[][]any{{"hello"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := evalString(t, tc.input)
			if err != nil {
				t.Fatalf("Eval(%q) returned error: %v", tc.input, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Eval(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}

func TestEvalRangeInvalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"row 0 in the start corner", `.Sheet1["A0:C9"]`},
		{"row 0 in the end corner", `.Sheet1["A1:C0"]`},
		{"not a valid range, column, or cell reference", `.Sheet1["A1:C9:E1"]`},
		{"row beyond Excel's real maximum", `.Sheet1["A1:A1048577"]`},
		{"column beyond Excel's real maximum", `.Sheet1["A1:XFE1"]`},
		{"column letter sequence too long to safely compute", `.Sheet1["A1:AAAAAAAAAAAAAAAAAAAA1"]`},
		{"plain cell reference beyond Excel's real maximum row", `.Sheet1.A1048577`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := evalString(t, tc.input); err == nil {
				t.Errorf("Eval(%q): want error, got nil", tc.input)
			}
		})
	}
}

// TestEvalRangePreservesDeclaredShapeOverSparseContent guards against
// the exact gap a cubic-dev-ai review caught in ADR-0012 itself: a
// range's declared bottom-right corner must be present in the result
// even when nothing real occupies it, or a sparse range silently
// shrinks to whatever's populated - here, a single real cell at the
// range's top-left, with real content well outside the range's bottom-
// right corner so nothing else keeps that corner alive by accident.
func TestEvalRangePreservesDeclaredShapeOverSparseContent(t *testing.T) {
	wb := model.Workbook{Sheets: []model.Sheet{model.NewSheet("Sheet1", []model.Row{
		{Index: 0, Cells: []model.Cell{model.NewStringCell("Name").At(0)}},
		// Column A only: B and C are blank in every row of the
		// range, so nothing but the anchor-cell logic keeps them
		// present at all.
		{Index: 1, Cells: []model.Cell{model.NewStringCell("Alice").At(0)}},
		// Real content far outside the "A1:C3" range below, so it
		// can't accidentally anchor that range's own bottom-right
		// corner - only the anchor-cell logic can.
		{Index: 50, Cells: []model.Cell{model.NewStringCell("elsewhere").At(50)}},
	})}}

	expr, err := filter.Parse(`.Sheet1["A1:C3"]`)
	if err != nil {
		t.Fatalf("filter.Parse: %v", err)
	}
	grid, err := Eval(expr, wb)
	if err != nil {
		t.Fatalf("Eval returned error: %v", err)
	}
	wantGrid := [][]any{
		{"Name", nil, nil},
		{"Alice", nil, nil},
		{nil, nil, nil},
	}
	if !reflect.DeepEqual(grid, wantGrid) {
		t.Errorf(`Eval(".Sheet1[\"A1:C3\"]") = %#v, want %#v`, grid, wantGrid)
	}

	recExpr, err := filter.Parse(`.Sheet1["A1:C3"] | records`)
	if err != nil {
		t.Fatalf("filter.Parse: %v", err)
	}
	records, err := Eval(recExpr, wb)
	if err != nil {
		t.Fatalf("Eval returned error: %v", err)
	}
	// The one data row (relative index 1) only has column A populated,
	// but the record still carries B's and C's fallback-letter keys
	// with null values - proving they were preserved as real headers,
	// not silently dropped because they're always blank in this range.
	want := []map[string]any{
		{"Name": "Alice", "B": nil, "C": nil},
	}
	if !reflect.DeepEqual(records, want) {
		t.Errorf(`Eval(".Sheet1[\"A1:C3\"] | records") = %#v, want %#v`, records, want)
	}
}

// TestEvalRangeComposesWithRecordsForMultiTableSheet is the direct
// motivating case from ADR-0012 and ADR-0011 alike: a sheet shaped
// like the real LinkedIn "TOP POSTS" export, two tables side by side
// sharing header text, with a title row (not the real header) at row
// 1. records on the whole sheet can't be isolated to one table and
// collides on the duplicate headers; a range that isolates one
// table's rows and columns first resolves it.
func TestEvalRangeComposesWithRecordsForMultiTableSheet(t *testing.T) {
	wb := model.Workbook{Sheets: []model.Sheet{model.NewSheet("TOP POSTS", []model.Row{
		{Index: 0, Cells: []model.Cell{model.NewStringCell("Maximum of 50 posts").At(0)}},
		// Index 1 deliberately absent: a blank separator row.
		{Index: 2, Cells: []model.Cell{
			model.NewStringCell("Post URL").At(0),
			model.NewStringCell("Date").At(1),
			model.NewStringCell("Engagements").At(2),
			model.NewStringCell("Post URL").At(4),
			model.NewStringCell("Date").At(5),
			model.NewStringCell("Impressions").At(6),
		}},
		{Index: 3, Cells: []model.Cell{
			model.NewStringCell("url1").At(0),
			model.NewStringCell("d1").At(1),
			model.NewNumberCell(10).At(2),
			model.NewStringCell("url2").At(4),
			model.NewStringCell("d2").At(5),
			model.NewNumberCell(20).At(6),
		}},
	})}}

	// records on the whole sheet uses row 0 (the title), not row 2 -
	// no error, but useless: every column but A falls back to a
	// letter, matching ADR-0011's own corrected Consequences example.
	wholeExpr, err := filter.Parse(".[\"TOP POSTS\"] | records")
	if err != nil {
		t.Fatalf("filter.Parse: %v", err)
	}
	if _, err := Eval(wholeExpr, wb); err != nil {
		t.Errorf(`Eval(".[\"TOP POSTS\"] | records") on the whole sheet: want no error (row 0 is the title, not a duplicate header), got %v`, err)
	}

	// A range spanning BOTH tables' columns still collides once row 2
	// becomes its own row 0 - narrowing rows alone isn't enough.
	bothTablesExpr, err := filter.Parse(`.["TOP POSTS"]["A3:G10"] | records`)
	if err != nil {
		t.Fatalf("filter.Parse: %v", err)
	}
	if _, err := Eval(bothTablesExpr, wb); err == nil {
		t.Error(`Eval('.["TOP POSTS"]["A3:G10"] | records') spanning both tables' columns: want a duplicate-header error, got nil`)
	}

	// Narrowing to just the first table's rows AND columns resolves
	// it cleanly.
	oneTableExpr, err := filter.Parse(`.["TOP POSTS"]["A3:C10"] | records`)
	if err != nil {
		t.Fatalf("filter.Parse: %v", err)
	}
	got, err := Eval(oneTableExpr, wb)
	if err != nil {
		t.Fatalf(`Eval('.["TOP POSTS"]["A3:C10"] | records') returned error: %v`, err)
	}
	want := []map[string]any{
		{"Post URL": "url1", "Date": "d1", "Engagements": 10.0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf(`Eval('.["TOP POSTS"]["A3:C10"] | records') = %#v, want %#v`, got, want)
	}
}

// TestEvalRangeHugeDeclaredSizeRejected confirms a range's declared
// bounds, not just its real content, are what maxDenseCells checks -
// a single real cell at the range's top-left with a declared bottom-
// right corner at Excel's actual maximum address is still rejected.
func TestEvalRangeHugeDeclaredSizeRejected(t *testing.T) {
	wb := model.Workbook{Sheets: []model.Sheet{model.NewSheet("Sheet1", []model.Row{
		{Index: 0, Cells: []model.Cell{model.NewStringCell("x").At(0)}},
	})}}
	expr, err := filter.Parse(`.Sheet1["A1:XFD1048576"]`)
	if err != nil {
		t.Fatalf("filter.Parse: %v", err)
	}
	if _, err := Eval(expr, wb); err == nil {
		t.Error("Eval on a range declared ~17 billion cells wide with one real cell: want error, got nil")
	}
}

// TestEvalRangeRowBeyondExcelLimitRejectedBeforeColumnAllocation guards
// against the exact gap a cubic-dev-ai review caught: a range's row
// endpoint has no upper bound of its own beyond "non-zero", so nothing
// stopped a filter string alone (no malicious file needed) from
// declaring a billion-row range. wholeColumn - unlike sheetGrid/
// sheetRecords - has no maxDenseCells check at all, so ".A" on such a
// range would have tried to grow its result to a billion elements
// outright. The row bound in parseCellReference must reject this
// before sheetRange ever builds a range that wide, not after.
func TestEvalRangeRowBeyondExcelLimitRejectedBeforeColumnAllocation(t *testing.T) {
	wb := model.Workbook{Sheets: []model.Sheet{model.NewSheet("Sheet1", []model.Row{
		{Index: 0, Cells: []model.Cell{model.NewStringCell("x").At(0)}},
	})}}
	expr, err := filter.Parse(`.Sheet1["A1:A1000000000"].A`)
	if err != nil {
		t.Fatalf("filter.Parse: %v", err)
	}
	if _, err := Eval(expr, wb); err == nil {
		t.Error(`Eval('.Sheet1["A1:A1000000000"].A'): want error, got nil`)
	}
}
