package eval

import (
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
	sheet1 := model.Sheet{
		Name: "Sheet1",
		Rows: []model.Row{
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
		},
	}
	data := model.Sheet{
		Name: "Data",
		Rows: []model.Row{
			{Index: 0, Cells: []model.Cell{
				model.NewStringCell("from data sheet").At(0),
			}},
		},
	}
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
		{"index on a column array", ".Sheet1.B[0]"},
		{"an assignment filter requires --in-place, not Eval", `.Sheet1.A1 = "x"`},
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
			{
				Name: "Sheet1",
				Rows: []model.Row{
					{Index: 1048575, Cells: []model.Cell{
						model.NewNumberCell(1).At(16383),
					}},
				},
			},
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
		return model.Sheet{
			Name: name,
			Rows: []model.Row{
				{Index: 1999, Cells: []model.Cell{
					model.NewNumberCell(1).At(1999),
				}},
			},
		}
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

func TestEvalRecordsIndexAndFieldAccess(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  any
	}{
		{"field by header name", `.Sheet1 | records | .[0].hello`, true},
		{"field by fallback column-letter header", `.Sheet1 | records | .[1].C`, testDate.Format(time.RFC3339)},
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
	wb := model.Workbook{Sheets: []model.Sheet{{
		Name: "Sheet1",
		Rows: []model.Row{
			{Index: 0, Cells: []model.Cell{model.NewStringCell("Name").At(0)}},
			{Index: 1, Cells: []model.Cell{model.NewStringCell("Alice").At(0)}},
			// Index 2 deliberately absent: an entirely blank row.
			{Index: 3, Cells: []model.Cell{model.NewStringCell("Bob").At(0)}},
		},
	}}}
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
	wb := model.Workbook{Sheets: []model.Sheet{{
		Name: "Sheet1",
		Rows: []model.Row{
			{Index: 0, Cells: []model.Cell{model.NewStringCell("Name").At(0)}},
			{Index: 1, Cells: []model.Cell{model.NewEmptyCell().At(0)}},
			{Index: 2, Cells: []model.Cell{model.NewStringCell("Alice").At(0)}},
		},
	}}}
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
	wb := model.Workbook{Sheets: []model.Sheet{{
		Name: "Sheet1",
		Rows: []model.Row{
			{Index: 0, Cells: []model.Cell{model.NewNumberCell(42).At(0)}},
			{Index: 1, Cells: []model.Cell{model.NewStringCell("value under 42").At(0)}},
		},
	}}}
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
	wb := model.Workbook{Sheets: []model.Sheet{{
		Name: "Sheet1",
		Rows: []model.Row{
			{Index: 1, Cells: []model.Cell{
				model.NewStringCell("x").At(0),
				model.NewStringCell("y").At(1),
			}},
		},
	}}}
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
	wb := model.Workbook{Sheets: []model.Sheet{{
		Name: "Sheet1",
		Rows: []model.Row{
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
		},
	}}}
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
	wb := model.Workbook{Sheets: []model.Sheet{{
		Name: "Sheet1",
		Rows: []model.Row{
			{Index: 0, Cells: []model.Cell{
				model.NewNumberCell(42).At(0),
				model.NewStringCell("42").At(1),
			}},
		},
	}}}
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
			{
				Name: "Sheet1",
				Rows: []model.Row{
					{Index: 1048575, Cells: []model.Cell{
						model.NewNumberCell(1).At(16383),
					}},
				},
			},
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
	wb := model.Workbook{Sheets: []model.Sheet{{
		Name: "Sheet1",
		Rows: []model.Row{
			{Index: 0, Cells: []model.Cell{model.NewStringCell("Name").At(0)}},
			// Column A only: B and C are blank in every row of the
			// range, so nothing but the anchor-cell logic keeps them
			// present at all.
			{Index: 1, Cells: []model.Cell{model.NewStringCell("Alice").At(0)}},
			// Real content far outside the "A1:C3" range below, so it
			// can't accidentally anchor that range's own bottom-right
			// corner - only the anchor-cell logic can.
			{Index: 50, Cells: []model.Cell{model.NewStringCell("elsewhere").At(50)}},
		},
	}}}

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
	wb := model.Workbook{Sheets: []model.Sheet{{
		Name: "TOP POSTS",
		Rows: []model.Row{
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
		},
	}}}

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
	wb := model.Workbook{Sheets: []model.Sheet{{
		Name: "Sheet1",
		Rows: []model.Row{
			{Index: 0, Cells: []model.Cell{model.NewStringCell("x").At(0)}},
		},
	}}}
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
// sheetRecords - allocates a slice sized from Dimensions() with no
// maxDenseCells check at all, so ".A" on such a range would have tried
// to allocate a billion-element slice outright. The row bound in
// parseCellReference must reject this before sheetRange ever builds
// that Dimensions() value, not after.
func TestEvalRangeRowBeyondExcelLimitRejectedBeforeColumnAllocation(t *testing.T) {
	wb := model.Workbook{Sheets: []model.Sheet{{
		Name: "Sheet1",
		Rows: []model.Row{
			{Index: 0, Cells: []model.Cell{model.NewStringCell("x").At(0)}},
		},
	}}}
	expr, err := filter.Parse(`.Sheet1["A1:A1000000000"].A`)
	if err != nil {
		t.Fatalf("filter.Parse: %v", err)
	}
	if _, err := Eval(expr, wb); err == nil {
		t.Error(`Eval('.Sheet1["A1:A1000000000"].A'): want error, got nil`)
	}
}
