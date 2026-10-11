package eval

import (
	"reflect"
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
		{"field by numeric-stringified header name", `.Sheet1 | records | .[0]["42"]`, nil},
		{"field by fallback column-letter header", `.Sheet1 | records | .[1].C`, testDate.Format(time.RFC3339)},
		{"missing field on a record is null, not an error", `.Sheet1 | records | .[0].NoSuchHeader`, nil},
		{"out-of-range index is null, not an error", `.Sheet1 | records | .[99]`, nil},
		{"negative index is null", `.Sheet1 | records | .[-1]`, nil},
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
