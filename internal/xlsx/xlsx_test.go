package xlsx

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/brunoarueira/xlq/internal/model"
)

// build writes fn's changes to a fresh excelize file and returns the path
// to the saved .xlsx.
func build(t *testing.T, fn func(f *excelize.File)) string {
	t.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	fn(f)

	path := filepath.Join(t.TempDir(), "book.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}

func cellAt(t *testing.T, wb model.Workbook, sheet string, rowIndex, colIndex int) (model.Cell, bool) {
	t.Helper()
	for _, s := range wb.Sheets {
		if s.Name != sheet {
			continue
		}
		for _, r := range s.Rows {
			if r.Index != rowIndex {
				continue
			}
			for _, c := range r.Cells {
				if c.Column == colIndex {
					return c, true
				}
			}
		}
	}
	return model.Cell{}, false
}

func TestReadMissingFile(t *testing.T) {
	if _, err := Read(filepath.Join(t.TempDir(), "missing.xlsx")); err == nil {
		t.Fatal("Read() with a missing file: want error, got nil")
	}
}

func TestReadSheetNamesInOrder(t *testing.T) {
	path := build(t, func(f *excelize.File) {
		if _, err := f.NewSheet("Data"); err != nil {
			t.Fatalf("NewSheet: %v", err)
		}
	})

	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(wb.Sheets) != 2 {
		t.Fatalf("len(wb.Sheets) = %d, want 2", len(wb.Sheets))
	}
	if wb.Sheets[0].Name != "Sheet1" || wb.Sheets[1].Name != "Data" {
		t.Errorf("sheet names = [%q, %q], want [\"Sheet1\", \"Data\"]", wb.Sheets[0].Name, wb.Sheets[1].Name)
	}
}

func TestReadStringCell(t *testing.T) {
	path := build(t, func(f *excelize.File) {
		must(t, f.SetCellValue("Sheet1", "A1", "hello"))
	})
	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	c, ok := cellAt(t, wb, "Sheet1", 0, 0)
	if !ok {
		t.Fatal("cell A1 not found")
	}
	if v, ok := c.StringValue(); !ok || v != "hello" {
		t.Errorf("StringValue() = (%q, %v), want (\"hello\", true)", v, ok)
	}
}

func TestReadNumberCell(t *testing.T) {
	path := build(t, func(f *excelize.File) {
		must(t, f.SetCellValue("Sheet1", "A1", 42.5))
	})
	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	c, ok := cellAt(t, wb, "Sheet1", 0, 0)
	if !ok {
		t.Fatal("cell A1 not found")
	}
	if v, ok := c.NumberValue(); !ok || v != 42.5 {
		t.Errorf("NumberValue() = (%v, %v), want (42.5, true)", v, ok)
	}
}

func TestReadBoolCell(t *testing.T) {
	path := build(t, func(f *excelize.File) {
		must(t, f.SetCellValue("Sheet1", "A1", true))
		must(t, f.SetCellValue("Sheet1", "B1", false))
	})
	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	a1, ok := cellAt(t, wb, "Sheet1", 0, 0)
	if !ok {
		t.Fatal("cell A1 not found")
	}
	if v, ok := a1.BoolValue(); !ok || !v {
		t.Errorf("A1 BoolValue() = (%v, %v), want (true, true)", v, ok)
	}
	b1, ok := cellAt(t, wb, "Sheet1", 0, 1)
	if !ok {
		t.Fatal("cell B1 not found")
	}
	if v, ok := b1.BoolValue(); !ok || v {
		t.Errorf("B1 BoolValue() = (%v, %v), want (false, true)", v, ok)
	}
}

func TestReadDateCell(t *testing.T) {
	want := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	path := build(t, func(f *excelize.File) {
		styleID, err := f.NewStyle(&excelize.Style{NumFmt: 14})
		if err != nil {
			t.Fatalf("NewStyle: %v", err)
		}
		must(t, f.SetCellValue("Sheet1", "A1", want))
		must(t, f.SetCellStyle("Sheet1", "A1", "A1", styleID))
	})
	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	c, ok := cellAt(t, wb, "Sheet1", 0, 0)
	if !ok {
		t.Fatal("cell A1 not found")
	}
	v, ok := c.DateValue()
	if !ok {
		t.Fatalf("DateValue() ok = false, want true (Kind = %v)", c.Kind)
	}
	if !v.Equal(want) {
		t.Errorf("DateValue() = %v, want %v", v, want)
	}
}

func TestReadISODateCell(t *testing.T) {
	// The rare ISO-8601-native date cell type (OOXML t="d") isn't
	// produced by excelize's write API at all (Excel itself essentially
	// never writes it either - dates are normally a styled number), so
	// this patches it directly into the saved XML.
	path := build(t, func(f *excelize.File) {
		must(t, f.SetCellValue("Sheet1", "A1", "placeholder"))
	})
	patchXML(t, path, "xl/worksheets/sheet1.xml",
		`<c r="A1" t="s"><v>0</v></c>`,
		`<c r="A1" t="d"><v>2026-09-25T00:00:00</v></c>`,
	)

	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	c, ok := cellAt(t, wb, "Sheet1", 0, 0)
	if !ok {
		t.Fatal("cell A1 not found")
	}
	want := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	v, ok := c.DateValue()
	if !ok {
		t.Fatalf("DateValue() ok = false, want true (Kind = %v)", c.Kind)
	}
	if !v.Equal(want) {
		t.Errorf("DateValue() = %v, want %v", v, want)
	}
}

func TestReadLiveFormulaDateResult(t *testing.T) {
	// A date-styled formula with no cached result: CalcCellValue must be
	// read with RawCellValue so the computed result is the serial number
	// ("46290"), not display text ("09-25-26") that can't be parsed back
	// into a date.
	want := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	path := build(t, func(f *excelize.File) {
		styleID, err := f.NewStyle(&excelize.Style{NumFmt: 14})
		if err != nil {
			t.Fatalf("NewStyle: %v", err)
		}
		must(t, f.SetCellValue("Sheet1", "A1", want))
		must(t, f.SetCellFormula("Sheet1", "B1", "A1"))
		must(t, f.SetCellStyle("Sheet1", "B1", "B1", styleID))
	})
	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	c, ok := cellAt(t, wb, "Sheet1", 0, 1)
	if !ok {
		t.Fatal("cell B1 not found")
	}
	if !c.IsFormula() {
		t.Error("IsFormula() = false, want true")
	}
	v, ok := c.DateValue()
	if !ok {
		t.Fatalf("DateValue() ok = false, want true (Kind = %v)", c.Kind)
	}
	if !v.Equal(want) {
		t.Errorf("DateValue() = %v, want %v", v, want)
	}
}

func TestReadFormulaCell(t *testing.T) {
	path := build(t, func(f *excelize.File) {
		must(t, f.SetCellValue("Sheet1", "A1", 2.0))
		must(t, f.SetCellValue("Sheet1", "B1", 3.0))
		// excelize (and Excel itself) store the formula without a leading
		// "="; that's purely a UI convention.
		must(t, f.SetCellFormula("Sheet1", "C1", "A1+B1"))
	})
	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	c, ok := cellAt(t, wb, "Sheet1", 0, 2)
	if !ok {
		t.Fatal("cell C1 not found")
	}
	if !c.IsFormula() {
		t.Error("IsFormula() = false, want true")
	}
	if c.Formula != "=A1+B1" {
		t.Errorf("Formula = %q, want \"=A1+B1\"", c.Formula)
	}
	if v, ok := c.NumberValue(); !ok || v != 5 {
		t.Errorf("NumberValue() = (%v, %v), want (5, true)", v, ok)
	}
}

func TestReadFormulaEvaluatingBlank(t *testing.T) {
	path := build(t, func(f *excelize.File) {
		must(t, f.SetCellFormula("Sheet1", "A1", `""`))
	})
	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	c, ok := cellAt(t, wb, "Sheet1", 0, 0)
	if !ok {
		t.Fatal("cell A1 not found")
	}
	if !c.IsFormula() {
		t.Error("IsFormula() = false, want true")
	}
	if c.Kind != model.Empty {
		t.Errorf("Kind = %v, want %v", c.Kind, model.Empty)
	}
}

func TestReadSparseSheet(t *testing.T) {
	// Only A1 and C3 are set; the sheet should skip row/column gaps
	// rather than materializing a dense grid.
	path := build(t, func(f *excelize.File) {
		must(t, f.SetCellValue("Sheet1", "A1", "corner"))
		must(t, f.SetCellValue("Sheet1", "C3", 9.0))
	})
	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(wb.Sheets) != 1 {
		t.Fatalf("len(wb.Sheets) = %d, want 1", len(wb.Sheets))
	}
	sheet := wb.Sheets[0]
	if len(sheet.Rows) != 2 {
		t.Fatalf("len(sheet.Rows) = %d, want 2 (rows 0 and 2 only)", len(sheet.Rows))
	}
	if sheet.Rows[0].Index != 0 || sheet.Rows[1].Index != 2 {
		t.Errorf("row indices = [%d, %d], want [0, 2]", sheet.Rows[0].Index, sheet.Rows[1].Index)
	}
	rows, cols := sheet.Dimensions()
	if rows != 3 || cols != 3 {
		t.Errorf("Dimensions() = (%d, %d), want (3, 3)", rows, cols)
	}
}

// patchXML replaces the first occurrence of want with replacement inside
// the named entry of the zip (.xlsx) file at path, rewriting the file in
// place. It's used to construct fixtures excelize's own write API can't
// produce, such as a formula cell with a pre-existing cached result.
func patchXML(t *testing.T, path, entry, want, replacement string) {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("zip.OpenReader: %v", err)
	}
	defer func() { _ = r.Close() }()

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	found := false
	for _, zf := range r.File {
		rc, err := zf.Open()
		if err != nil {
			t.Fatalf("open %s: %v", zf.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", zf.Name, err)
		}
		if zf.Name == entry {
			if !strings.Contains(string(data), want) {
				t.Fatalf("did not find %q in %s", want, entry)
			}
			data = []byte(strings.Replace(string(data), want, replacement, 1))
			found = true
		}
		fw, err := w.Create(zf.Name)
		if err != nil {
			t.Fatalf("create %s: %v", zf.Name, err)
		}
		if _, err := fw.Write(data); err != nil {
			t.Fatalf("write %s: %v", zf.Name, err)
		}
	}
	if !found {
		t.Fatalf("entry %q not found in %s", entry, path)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip writer Close: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestReadFormattedNumberCell(t *testing.T) {
	// A currency/thousands-separator style must not stop the value from
	// being read as a Number: the display text "$1,234.50" isn't itself
	// parseable, so the raw underlying value has to be used.
	path := build(t, func(f *excelize.File) {
		styleID, err := f.NewStyle(&excelize.Style{CustomNumFmt: strPtr(`"$"#,##0.00`)})
		if err != nil {
			t.Fatalf("NewStyle: %v", err)
		}
		must(t, f.SetCellValue("Sheet1", "A1", 1234.5))
		must(t, f.SetCellStyle("Sheet1", "A1", "A1", styleID))
	})
	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	c, ok := cellAt(t, wb, "Sheet1", 0, 0)
	if !ok {
		t.Fatal("cell A1 not found")
	}
	if v, ok := c.NumberValue(); !ok || v != 1234.5 {
		t.Errorf("NumberValue() = (%v, %v), want (1234.5, true) - Kind = %v", v, ok, c.Kind)
	}
}

func TestReadHiddenFormatCell(t *testing.T) {
	// The ";;;" custom format renders any value as blank text in every
	// spreadsheet application; the cell must still be read as present,
	// using its raw value. A cell in this state must be followed by
	// another populated cell in its row: excelize's row iterator itself
	// trims a *trailing* format-hidden cell before this package ever
	// sees it (see the package doc's known limitation), so B1 here is
	// what keeps A1 from being trimmed away.
	path := build(t, func(f *excelize.File) {
		styleID, err := f.NewStyle(&excelize.Style{CustomNumFmt: strPtr(";;;")})
		if err != nil {
			t.Fatalf("NewStyle: %v", err)
		}
		must(t, f.SetCellValue("Sheet1", "A1", 99.0))
		must(t, f.SetCellStyle("Sheet1", "A1", "A1", styleID))
		must(t, f.SetCellValue("Sheet1", "B1", "visible"))
	})
	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	c, ok := cellAt(t, wb, "Sheet1", 0, 0)
	if !ok {
		t.Fatal("cell A1 not found (format-hidden value was dropped)")
	}
	if v, ok := c.NumberValue(); !ok || v != 99 {
		t.Errorf("NumberValue() = (%v, %v), want (99, true) - Kind = %v", v, ok, c.Kind)
	}
}

func TestReadErrorProducingFormula(t *testing.T) {
	path := build(t, func(f *excelize.File) {
		must(t, f.SetCellFormula("Sheet1", "A1", "1/0"))
	})
	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	c, ok := cellAt(t, wb, "Sheet1", 0, 0)
	if !ok {
		t.Fatal("cell A1 not found")
	}
	if !c.IsFormula() {
		t.Error("IsFormula() = false, want true")
	}
	if v, ok := c.StringValue(); !ok || v != "#DIV/0!" {
		t.Errorf("StringValue() = (%q, %v), want (\"#DIV/0!\", true) - Kind = %v", v, ok, c.Kind)
	}
}

func TestReadCachedFormulaStringResult(t *testing.T) {
	// excelize's own write API can't attach a cached result to a formula
	// (SetCellValue after SetCellFormula clears the formula instead), so
	// this patches the saved XML directly to reproduce what a real
	// spreadsheet application writes: <f> and a cached <v> together.
	// This is the case that matters most, since it's what every
	// Excel/LibreOffice/Google-Sheets-saved formula cell looks like.
	path := build(t, func(f *excelize.File) {
		must(t, f.SetCellFormula("Sheet1", "A1", `"123"`))
	})
	patchXML(t, path, "xl/worksheets/sheet1.xml",
		`<c r="A1" t="str"><f>&#34;123&#34;</f></c>`,
		`<c r="A1" t="str"><f>&#34;123&#34;</f><v>123</v></c>`,
	)

	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	c, ok := cellAt(t, wb, "Sheet1", 0, 0)
	if !ok {
		t.Fatal("cell A1 not found")
	}
	if !c.IsFormula() {
		t.Error("IsFormula() = false, want true")
	}
	// The cached result is text ("123"), even though it looks numeric -
	// with a real cache present, the cell's actual type must win over
	// any text-based number guess.
	if v, ok := c.StringValue(); !ok || v != "123" {
		t.Errorf("StringValue() = (%q, %v), want (\"123\", true) - Kind = %v", v, ok, c.Kind)
	}
}

func TestSheetNames(t *testing.T) {
	path := build(t, func(f *excelize.File) {
		if _, err := f.NewSheet("Data"); err != nil {
			t.Fatalf("NewSheet: %v", err)
		}
	})
	names, err := SheetNames(path)
	if err != nil {
		t.Fatalf("SheetNames: %v", err)
	}
	want := []string{"Sheet1", "Data"}
	if len(names) != len(want) || names[0] != want[0] || names[1] != want[1] {
		t.Errorf("SheetNames() = %v, want %v", names, want)
	}
}

func TestSheetNamesMissingFile(t *testing.T) {
	if _, err := SheetNames(filepath.Join(t.TempDir(), "missing.xlsx")); err == nil {
		t.Fatal("SheetNames() with a missing file: want error, got nil")
	}
}

func strPtr(s string) *string { return &s }

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
