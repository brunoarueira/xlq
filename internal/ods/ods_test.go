package ods

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/brunoarueira/xlq/internal/model"
)

// wrapSpreadsheet wraps body (the contents of <office:spreadsheet>) into
// a minimal, complete content.xml document.
func wrapSpreadsheet(body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<office:document-content
    xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0"
    xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0"
    xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0">
  <office:body>
    <office:spreadsheet>` + body + `</office:spreadsheet>
  </office:body>
</office:document-content>`
}

// buildODS writes a minimal .ods file (a mimetype entry plus the given
// content.xml) to a temp path and returns it.
func buildODS(t *testing.T, contentXML string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "book.ods")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	mw, err := zw.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatalf("CreateHeader: %v", err)
	}
	if _, err := mw.Write([]byte("application/vnd.oasis.opendocument.spreadsheet")); err != nil {
		t.Fatalf("write mimetype: %v", err)
	}
	cw, err := zw.Create("content.xml")
	if err != nil {
		t.Fatalf("Create content.xml: %v", err)
	}
	if _, err := cw.Write([]byte(contentXML)); err != nil {
		t.Fatalf("write content.xml: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close: %v", err)
	}
	return path
}

func cellAt(t *testing.T, wb model.Workbook, sheetIndex, rowIndex, colIndex int) (model.Cell, bool) {
	t.Helper()
	for _, r := range wb.Sheets[sheetIndex].Rows {
		if r.Index != rowIndex {
			continue
		}
		for _, c := range r.Cells {
			if c.Column == colIndex {
				return c, true
			}
		}
	}
	return model.Cell{}, false
}

func TestReadMissingFile(t *testing.T) {
	if _, err := Read(filepath.Join(t.TempDir(), "missing.ods")); err == nil {
		t.Fatal("Read() with a missing file: want error, got nil")
	}
}

func TestReadNotAZip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.ods")
	if err := os.WriteFile(path, []byte("not a zip"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Read(path); err == nil {
		t.Fatal("Read() on a non-zip file: want error, got nil")
	}
}

func TestReadZipWithoutContentXML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.ods")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	zw := zip.NewWriter(f)
	if _, err := zw.Create("mimetype"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close: %v", err)
	}
	_ = f.Close()

	if _, err := Read(path); err == nil {
		t.Fatal("Read() on a zip with no content.xml: want error, got nil")
	}
}

func TestReadBasicCellTypes(t *testing.T) {
	path := buildODS(t, wrapSpreadsheet(`
      <table:table table:name="Sheet1">
        <table:table-row>
          <table:table-cell office:value-type="string"><text:p>hello</text:p></table:table-cell>
          <table:table-cell office:value-type="float" office:value="42.5"/>
          <table:table-cell office:value-type="boolean" office:boolean-value="true"/>
          <table:table-cell office:value-type="date" office:date-value="2026-09-25"/>
        </table:table-row>
      </table:table>`))

	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(wb.Sheets) != 1 {
		t.Fatalf("len(Sheets) = %d, want 1", len(wb.Sheets))
	}
	if wb.Sheets[0].Name != "Sheet1" {
		t.Errorf("sheet name = %q, want \"Sheet1\"", wb.Sheets[0].Name)
	}

	c, ok := cellAt(t, wb, 0, 0, 0)
	if !ok {
		t.Fatal("cell (0,0) not found")
	}
	if v, ok := c.StringValue(); !ok || v != "hello" {
		t.Errorf("StringValue() = (%q, %v), want (\"hello\", true)", v, ok)
	}

	c, ok = cellAt(t, wb, 0, 0, 1)
	if !ok {
		t.Fatal("cell (0,1) not found")
	}
	if v, ok := c.NumberValue(); !ok || v != 42.5 {
		t.Errorf("NumberValue() = (%v, %v), want (42.5, true)", v, ok)
	}

	c, ok = cellAt(t, wb, 0, 0, 2)
	if !ok {
		t.Fatal("cell (0,2) not found")
	}
	if v, ok := c.BoolValue(); !ok || !v {
		t.Errorf("BoolValue() = (%v, %v), want (true, true)", v, ok)
	}

	c, ok = cellAt(t, wb, 0, 0, 3)
	if !ok {
		t.Fatal("cell (0,3) not found")
	}
	want := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	if v, ok := c.DateValue(); !ok || !v.Equal(want) {
		t.Errorf("DateValue() = (%v, %v), want (%v, true)", v, ok, want)
	}
}

func TestReadPercentageAndCurrencyAreNumbers(t *testing.T) {
	path := buildODS(t, wrapSpreadsheet(`
      <table:table table:name="Sheet1">
        <table:table-row>
          <table:table-cell office:value-type="percentage" office:value="0.5"/>
          <table:table-cell office:value-type="currency" office:value="19.99"/>
        </table:table-row>
      </table:table>`))

	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	c, _ := cellAt(t, wb, 0, 0, 0)
	if v, ok := c.NumberValue(); !ok || v != 0.5 {
		t.Errorf("percentage NumberValue() = (%v, %v), want (0.5, true)", v, ok)
	}
	c, _ = cellAt(t, wb, 0, 0, 1)
	if v, ok := c.NumberValue(); !ok || v != 19.99 {
		t.Errorf("currency NumberValue() = (%v, %v), want (19.99, true)", v, ok)
	}
}

func TestReadTimeDurationKeptAsString(t *testing.T) {
	path := buildODS(t, wrapSpreadsheet(`
      <table:table table:name="Sheet1">
        <table:table-row>
          <table:table-cell office:value-type="time" office:time-value="PT2H30M"/>
        </table:table-row>
      </table:table>`))

	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	c, ok := cellAt(t, wb, 0, 0, 0)
	if !ok {
		t.Fatal("cell (0,0) not found")
	}
	if v, ok := c.StringValue(); !ok || v != "PT2H30M" {
		t.Errorf("StringValue() = (%q, %v), want (\"PT2H30M\", true) - Kind = %v", v, ok, c.Kind)
	}
}

func TestReadUnrecognizedValueTypeFallsBackToText(t *testing.T) {
	path := buildODS(t, wrapSpreadsheet(`
      <table:table table:name="Sheet1">
        <table:table-row>
          <table:table-cell office:value-type="some-future-type"><text:p>fallback</text:p></table:table-cell>
        </table:table-row>
      </table:table>`))

	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	c, ok := cellAt(t, wb, 0, 0, 0)
	if !ok {
		t.Fatal("cell (0,0) not found")
	}
	if v, ok := c.StringValue(); !ok || v != "fallback" {
		t.Errorf("StringValue() = (%q, %v), want (\"fallback\", true)", v, ok)
	}
}

func TestReadFormulaWithCachedValue(t *testing.T) {
	path := buildODS(t, wrapSpreadsheet(`
      <table:table table:name="Sheet1">
        <table:table-row>
          <table:table-cell table:formula="of:=[.A1]+1" office:value-type="float" office:value="5"/>
        </table:table-row>
      </table:table>`))

	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	c, ok := cellAt(t, wb, 0, 0, 0)
	if !ok {
		t.Fatal("cell (0,0) not found")
	}
	if !c.IsFormula() {
		t.Error("IsFormula() = false, want true")
	}
	if v, ok := c.NumberValue(); !ok || v != 5 {
		t.Errorf("NumberValue() = (%v, %v), want (5, true)", v, ok)
	}
}

func TestReadFormulaEvaluatingBlank(t *testing.T) {
	path := buildODS(t, wrapSpreadsheet(`
      <table:table table:name="Sheet1">
        <table:table-row>
          <table:table-cell table:formula="of:=&quot;&quot;"/>
        </table:table-row>
      </table:table>`))

	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	c, ok := cellAt(t, wb, 0, 0, 0)
	if !ok {
		t.Fatal("cell (0,0) not found")
	}
	if !c.IsFormula() {
		t.Error("IsFormula() = false, want true")
	}
	if c.Kind != model.Empty {
		t.Errorf("Kind = %v, want %v", c.Kind, model.Empty)
	}
}

func TestReadRepeatedBlankCellSkipsGap(t *testing.T) {
	path := buildODS(t, wrapSpreadsheet(`
      <table:table table:name="Sheet1">
        <table:table-row>
          <table:table-cell office:value-type="string"><text:p>a</text:p></table:table-cell>
          <table:table-cell table:number-columns-repeated="5"/>
          <table:table-cell office:value-type="string"><text:p>b</text:p></table:table-cell>
        </table:table-row>
      </table:table>`))

	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(wb.Sheets[0].Rows[0].Cells) != 2 {
		t.Fatalf("len(Cells) = %d, want 2 (the 5 blank repeats must not be materialized)", len(wb.Sheets[0].Rows[0].Cells))
	}
	c, ok := cellAt(t, wb, 0, 0, 6)
	if !ok {
		t.Fatal("cell (0,6) not found - blank repeat count did not advance the column correctly")
	}
	if v, _ := c.StringValue(); v != "b" {
		t.Errorf("StringValue() = %q, want \"b\"", v)
	}
}

func TestReadRepeatedRealCellIsExpanded(t *testing.T) {
	path := buildODS(t, wrapSpreadsheet(`
      <table:table table:name="Sheet1">
        <table:table-row>
          <table:table-cell office:value-type="float" office:value="7" table:number-columns-repeated="3"/>
        </table:table-row>
      </table:table>`))

	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(wb.Sheets[0].Rows[0].Cells) != 3 {
		t.Fatalf("len(Cells) = %d, want 3", len(wb.Sheets[0].Rows[0].Cells))
	}
	for col := 0; col < 3; col++ {
		c, ok := cellAt(t, wb, 0, 0, col)
		if !ok {
			t.Fatalf("cell (0,%d) not found", col)
		}
		if v, ok := c.NumberValue(); !ok || v != 7 {
			t.Errorf("cell (0,%d) NumberValue() = (%v, %v), want (7, true)", col, v, ok)
		}
	}
}

func TestReadHugeBlankRowRepeatIsSkippedInstantly(t *testing.T) {
	// A single trailing filler row like this is exactly how real ODS
	// files pad a sheet to a spreadsheet application's default extent
	// (over a million rows); it must never be materialized.
	path := buildODS(t, wrapSpreadsheet(`
      <table:table table:name="Sheet1">
        <table:table-row>
          <table:table-cell office:value-type="string"><text:p>only real row</text:p></table:table-cell>
        </table:table-row>
        <table:table-row table:number-rows-repeated="1048575">
          <table:table-cell/>
        </table:table-row>
      </table:table>`))

	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(wb.Sheets[0].Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1 (the huge blank-repeat row must be skipped)", len(wb.Sheets[0].Rows))
	}
}

func TestReadExceedsMaterializedBudget(t *testing.T) {
	// Real content (not blank) claiming a repeat count past the budget
	// must error immediately, not attempt the allocation.
	path := buildODS(t, wrapSpreadsheet(`
      <table:table table:name="Sheet1">
        <table:table-row>
          <table:table-cell office:value-type="float" office:value="1" table:number-columns-repeated="20000000"/>
        </table:table-row>
      </table:table>`))

	if _, err := Read(path); err == nil {
		t.Fatal("Read() over the materialized-cell budget: want error, got nil")
	}
}

func TestReadInvalidRepeatCount(t *testing.T) {
	path := buildODS(t, wrapSpreadsheet(`
      <table:table table:name="Sheet1">
        <table:table-row>
          <table:table-cell office:value-type="string" table:number-columns-repeated="not-a-number"><text:p>x</text:p></table:table-cell>
        </table:table-row>
      </table:table>`))

	if _, err := Read(path); err == nil {
		t.Fatal("Read() with a malformed repeat count: want error, got nil")
	}
}

func TestReadMultipleSheetsInOrder(t *testing.T) {
	path := buildODS(t, wrapSpreadsheet(`
      <table:table table:name="First">
        <table:table-row><table:table-cell office:value-type="string"><text:p>a</text:p></table:table-cell></table:table-row>
      </table:table>
      <table:table table:name="Second">
        <table:table-row><table:table-cell office:value-type="string"><text:p>b</text:p></table:table-cell></table:table-row>
      </table:table>`))

	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(wb.Sheets) != 2 {
		t.Fatalf("len(Sheets) = %d, want 2", len(wb.Sheets))
	}
	if wb.Sheets[0].Name != "First" || wb.Sheets[1].Name != "Second" {
		t.Errorf("sheet names = [%q, %q], want [\"First\", \"Second\"]", wb.Sheets[0].Name, wb.Sheets[1].Name)
	}
}

func TestReadInvalidNumericValue(t *testing.T) {
	path := buildODS(t, wrapSpreadsheet(`
      <table:table table:name="Sheet1">
        <table:table-row>
          <table:table-cell office:value-type="float" office:value="not-a-float"/>
        </table:table-row>
      </table:table>`))

	if _, err := Read(path); err == nil {
		t.Fatal("Read() with an unparseable numeric value: want error, got nil")
	}
}
