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
		for _, r := range rowsOf(t, s) {
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

// rowsOf drains sheet's rows into a slice, failing the test on any
// error.
func rowsOf(t *testing.T, sheet model.Sheet) []model.Row {
	t.Helper()
	rows, err := model.CollectRows(sheet)
	if err != nil {
		t.Fatalf("CollectRows: %v", err)
	}
	return rows
}

// dimensions returns sheet's bounding box (one past the highest row
// and column index present), replicating what model.Sheet.Dimensions
// used to do directly - removed along with Sheet.Rows becoming lazy
// (docs/adr/0018-streaming-xlsx-reader.md).
func dimensions(t *testing.T, sheet model.Sheet) (rows, cols int) {
	t.Helper()
	for _, r := range rowsOf(t, sheet) {
		if r.Index+1 > rows {
			rows = r.Index + 1
		}
		for _, c := range r.Cells {
			if c.Column+1 > cols {
				cols = c.Column + 1
			}
		}
	}
	return rows, cols
}

func TestReadMissingFile(t *testing.T) {
	if _, err := Read(filepath.Join(t.TempDir(), "missing.xlsx"), 0); err == nil {
		t.Fatal("Read() with a missing file: want error, got nil")
	}
}

// buildHighlyCompressible writes rows cells, each the same string
// value, to a fresh workbook - small on disk (the zip container
// compresses such repetitive XML markup extremely well - excelize's
// own shared-string table also means the string's own content is
// stored once regardless of row count, so the decompressed size comes
// from accumulated per-cell XML structure, not the string value
// itself) but large once excelize decompresses it: the shape of risk a
// real small-on-disk zip bomb poses (#51,
// docs/adr/0031-memory-ceiling-for-untrusted-files.md), without
// needing an actual malicious file to demonstrate the mechanism.
func buildHighlyCompressible(t *testing.T, rows int) string {
	t.Helper()
	path := build(t, func(f *excelize.File) {
		for row := 1; row <= rows; row++ {
			cell, err := excelize.CoordinatesToCellName(1, row)
			if err != nil {
				t.Fatalf("CoordinatesToCellName: %v", err)
			}
			if err := f.SetCellValue("Sheet1", cell, "A"); err != nil {
				t.Fatalf("SetCellValue: %v", err)
			}
		}
	})
	if info, err := os.Stat(path); err != nil || info.Size() > 512<<10 {
		t.Fatalf("fixture isn't small on disk as expected: %+v, %v", info, err)
	}
	return path
}

// TestReadRejectsOversizedDecompressedContent guards the actual reason
// maxSize is passed to excelize.Options rather than just checked
// against the file's own on-disk size: a pre-flight size check alone
// (internal/input's checkFileSize) can't catch this fixture at all,
// since it's small on disk - only excelize's own UnzipSizeLimit,
// applied once it starts decompressing the workbook, catches it.
func TestReadRejectsOversizedDecompressedContent(t *testing.T) {
	path := buildHighlyCompressible(t, 60000) // well under 512 KiB on disk, over 1 MiB decompressed

	if _, err := Read(path, 1<<20); err == nil {
		t.Fatal("Read with maxSize=1MiB against a small-on-disk, highly compressible file: want error, got nil")
	} else if !strings.Contains(err.Error(), "unzip size exceeds") {
		t.Errorf("error = %q, want it to mention excelize's own unzip size limit", err)
	}
}

// TestReadMaxSizeZeroDisablesDecompressionLimit confirms the same
// fixture that TestReadRejectsOversizedDecompressedContent rejects at
// a 1MiB ceiling reads successfully when maxSize is 0 - excelize's own
// generous built-in defaults (16 GiB/16 MiB) apply instead of some
// implicit floor sneaking in regardless of the "0 disables it"
// convention.
func TestReadMaxSizeZeroDisablesDecompressionLimit(t *testing.T) {
	path := buildHighlyCompressible(t, 60000)

	wb, err := Read(path, 0)
	if err != nil {
		t.Fatalf("Read with maxSize=0: %v", err)
	}
	rows, err := model.CollectRows(wb.Sheets[0])
	if err != nil {
		t.Fatalf("CollectRows: %v", err)
	}
	if len(rows) != 60000 {
		t.Errorf("len(rows) = %d, want 60000", len(rows))
	}
}

func TestReadSheetNamesInOrder(t *testing.T) {
	path := build(t, func(f *excelize.File) {
		if _, err := f.NewSheet("Data"); err != nil {
			t.Fatalf("NewSheet: %v", err)
		}
	})

	wb, err := Read(path, 0)
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
	wb, err := Read(path, 0)
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
	wb, err := Read(path, 0)
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
	wb, err := Read(path, 0)
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
	wb, err := Read(path, 0)
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

	wb, err := Read(path, 0)
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
	wb, err := Read(path, 0)
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

func TestReadLiveFormulaBoolResult(t *testing.T) {
	// A boolean formula with no cached result: CalcCellValue under
	// RawCellValue still renders a bool as "TRUE"/"FALSE" text (not
	// "1"/"0"), which is what cellFromComputedText's literal check
	// expects - this locks that rendering in.
	path := build(t, func(f *excelize.File) {
		must(t, f.SetCellFormula("Sheet1", "A1", "1=1"))
	})
	wb, err := Read(path, 0)
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
	if v, ok := c.BoolValue(); !ok || !v {
		t.Errorf("BoolValue() = (%v, %v), want (true, true) - Kind = %v", v, ok, c.Kind)
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
	wb, err := Read(path, 0)
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
	wb, err := Read(path, 0)
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
	wb, err := Read(path, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(wb.Sheets) != 1 {
		t.Fatalf("len(wb.Sheets) = %d, want 1", len(wb.Sheets))
	}
	sheetRows := rowsOf(t, wb.Sheets[0])
	if len(sheetRows) != 2 {
		t.Fatalf("len(sheetRows) = %d, want 2 (rows 0 and 2 only)", len(sheetRows))
	}
	if sheetRows[0].Index != 0 || sheetRows[1].Index != 2 {
		t.Errorf("row indices = [%d, %d], want [0, 2]", sheetRows[0].Index, sheetRows[1].Index)
	}
	rows, cols := dimensions(t, wb.Sheets[0])
	if rows != 3 || cols != 3 {
		t.Errorf("dimensions() = (%d, %d), want (3, 3)", rows, cols)
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
	wb, err := Read(path, 0)
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
	wb, err := Read(path, 0)
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
	wb, err := Read(path, 0)
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

// TestReadMutuallyReferencingAnchorArrayFormulas guards #49/ADR-0029: two
// array formulas whose ANCHORARRAY calls reference each other used to
// recurse unboundedly inside excelize's CalcCellValue, ending in a Go
// fatal error (stack overflow, not a panic - recover() can't catch
// it), per GHSA-wp2g-vpjj-g53r. Verified directly before this test was
// added: on the excelize version this project depended on before
// ADR-0029's pin, this exact workbook crashed the whole test binary,
// not just failed a test.
func TestReadMutuallyReferencingAnchorArrayFormulas(t *testing.T) {
	path := build(t, func(f *excelize.File) {
		must(t, f.SetCellValue("Sheet1", "A1", "safe"))

		if _, err := f.NewSheet("Sheet2"); err != nil {
			t.Fatalf("NewSheet: %v", err)
		}
		arrayType := excelize.STCellFormulaTypeArray
		refA, refB := "A1:A1", "B1:B1"
		must(t, f.SetCellFormula("Sheet2", "A1", "_xlfn.ANCHORARRAY($B$1)", excelize.FormulaOpts{Type: &arrayType, Ref: &refA}))
		must(t, f.SetCellFormula("Sheet2", "B1", "_xlfn.ANCHORARRAY($A$1)", excelize.FormulaOpts{Type: &arrayType, Ref: &refB}))
	})
	wb, err := Read(path, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	// A sheet never ranged over is never read at all (ADR-0018) - a
	// query that only touches Sheet1 is unaffected by Sheet2's poison
	// regardless of this fix, confirming that narrower-than-#49's-own-
	// claim blast radius too.
	sheet1 := rowsOf(t, wb.Sheets[0])
	if len(sheet1) != 1 || len(sheet1[0].Cells) != 1 {
		t.Fatalf("Sheet1 rows = %#v, want one row with one cell", sheet1)
	}
	if v, ok := sheet1[0].Cells[0].StringValue(); !ok || v != "safe" {
		t.Errorf("Sheet1.A1 = (%q, %v), want (\"safe\", true)", v, ok)
	}

	// Actually ranging over the poisoned sheet is the real regression
	// guard: this used to never return at all. Asserting Sheet2 was
	// found and its row actually holds both formula cells, not just
	// that the loop completed - a missing Sheet2 would let this pass
	// without ever exercising the regression path at all.
	var sheet2 *model.Sheet
	for i := range wb.Sheets {
		if wb.Sheets[i].Name == "Sheet2" {
			sheet2 = &wb.Sheets[i]
			break
		}
	}
	if sheet2 == nil {
		t.Fatal("Sheet2 not found in wb.Sheets")
	}
	rows := rowsOf(t, *sheet2)
	if len(rows) != 1 || len(rows[0].Cells) != 2 {
		t.Fatalf("Sheet2 rows = %#v, want one row with both formula cells (A1, B1)", rows)
	}
	for _, c := range rows[0].Cells {
		if !c.IsFormula() {
			t.Errorf("Sheet2 cell at column %d: IsFormula() = false, want true", c.Column)
		}
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

	wb, err := Read(path, 0)
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
	names, err := SheetNames(path, 0)
	if err != nil {
		t.Fatalf("SheetNames: %v", err)
	}
	want := []string{"Sheet1", "Data"}
	if len(names) != len(want) || names[0] != want[0] || names[1] != want[1] {
		t.Errorf("SheetNames() = %v, want %v", names, want)
	}
}

func TestSheetNamesMissingFile(t *testing.T) {
	if _, err := SheetNames(filepath.Join(t.TempDir(), "missing.xlsx"), 0); err == nil {
		t.Fatal("SheetNames() with a missing file: want error, got nil")
	}
}

// TestSheetNamesRejectsOversizedDecompressedContent guards SheetNames
// against the same decompression-based risk Read's own
// TestReadRejectsOversizedDecompressedContent does - SheetNames skips
// parsing any cell data, but excelize.OpenFile alone still risks it
// (#51, docs/adr/0031-memory-ceiling-for-untrusted-files.md).
func TestSheetNamesRejectsOversizedDecompressedContent(t *testing.T) {
	path := buildHighlyCompressible(t, 60000)

	if _, err := SheetNames(path, 1<<20); err == nil {
		t.Fatal("SheetNames with maxSize=1MiB against a ~5MB-decompressed file: want error, got nil")
	} else if !strings.Contains(err.Error(), "unzip size exceeds") {
		t.Errorf("error = %q, want it to mention excelize's own unzip size limit", err)
	}
}

// TestExcelizeOptionsCapsXMLLimitAtMaxSize guards the one subtlety in
// excelizeOptions worth a dedicated test: excelize.OpenFile itself
// rejects UnzipXMLSizeLimit > UnzipSizeLimit outright, so a maxSize
// smaller than unzipXMLSizeLimit's own fixed default must cap the XML
// limit down to maxSize too, not pass the fixed default through
// unconditionally.
func TestExcelizeOptionsCapsXMLLimitAtMaxSize(t *testing.T) {
	small := int64(1 << 10) // far below unzipXMLSizeLimit's own 16 MiB default
	opts := excelizeOptions(small)
	if opts.UnzipSizeLimit != small {
		t.Errorf("UnzipSizeLimit = %d, want %d", opts.UnzipSizeLimit, small)
	}
	if opts.UnzipXMLSizeLimit != small {
		t.Errorf("UnzipXMLSizeLimit = %d, want %d (capped at maxSize, not the fixed 16MiB default)", opts.UnzipXMLSizeLimit, small)
	}

	large := int64(1 << 30) // well above unzipXMLSizeLimit's own 16 MiB default
	opts2 := excelizeOptions(large)
	if opts2.UnzipXMLSizeLimit != unzipXMLSizeLimit {
		t.Errorf("UnzipXMLSizeLimit = %d, want the fixed %d default (maxSize is large enough not to cap it)", opts2.UnzipXMLSizeLimit, unzipXMLSizeLimit)
	}

	if disabled := excelizeOptions(0); disabled != (excelize.Options{}) {
		t.Errorf("excelizeOptions(0) = %+v, want the zero Options (disabled)", disabled)
	}
}

func strPtr(s string) *string { return &s }

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
