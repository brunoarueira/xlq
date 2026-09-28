package csv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/brunoarueira/xlq/internal/model"
)

// writeFixture writes content to a temp file with name and returns its
// path.
func writeFixture(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func cellAt(t *testing.T, wb model.Workbook, rowIndex, colIndex int) (model.Cell, bool) {
	t.Helper()
	for _, r := range wb.Sheets[0].Rows {
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
	if _, err := Read(filepath.Join(t.TempDir(), "missing.csv")); err == nil {
		t.Fatal("Read() with a missing file: want error, got nil")
	}
}

func TestReadBasic(t *testing.T) {
	path := writeFixture(t, "book.csv", "name,age\nAda,36\nGrace,85\n")
	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(wb.Sheets) != 1 {
		t.Fatalf("len(wb.Sheets) = %d, want 1", len(wb.Sheets))
	}
	if wb.Sheets[0].Name != "Sheet1" {
		t.Errorf("sheet name = %q, want \"Sheet1\"", wb.Sheets[0].Name)
	}
	if len(wb.Sheets[0].Rows) != 3 {
		t.Fatalf("len(Rows) = %d, want 3", len(wb.Sheets[0].Rows))
	}

	c, ok := cellAt(t, wb, 1, 0)
	if !ok {
		t.Fatal("cell (row 1, col 0) not found")
	}
	if v, ok := c.StringValue(); !ok || v != "Ada" {
		t.Errorf("StringValue() = (%q, %v), want (\"Ada\", true)", v, ok)
	}

	rows, cols := wb.Sheets[0].Dimensions()
	if rows != 3 || cols != 2 {
		t.Errorf("Dimensions() = (%d, %d), want (3, 2)", rows, cols)
	}
}

func TestReadEmptyFieldIsEmptyCell(t *testing.T) {
	path := writeFixture(t, "book.csv", "a,b\n1,\n")
	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	c, ok := cellAt(t, wb, 1, 1)
	if !ok {
		t.Fatal("cell (row 1, col 1) not found")
	}
	if c.Kind != model.Empty {
		t.Errorf("Kind = %v, want %v", c.Kind, model.Empty)
	}
}

func TestReadAllEmptyRowIsNotSkipped(t *testing.T) {
	// A row of two empty fields (",") is a real, delimited record - not
	// a blank line - and must not be confused with one.
	path := writeFixture(t, "book.csv", "a,b\n,\n1,2\n")
	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(wb.Sheets[0].Rows) != 3 {
		t.Fatalf("len(Rows) = %d, want 3", len(wb.Sheets[0].Rows))
	}
	if wb.Sheets[0].Rows[1].Index != 1 {
		t.Errorf("Rows[1].Index = %d, want 1", wb.Sheets[0].Rows[1].Index)
	}
	c, ok := cellAt(t, wb, 1, 0)
	if !ok {
		t.Fatal("cell (row 1, col 0) not found")
	}
	if c.Kind != model.Empty {
		t.Errorf("Kind = %v, want %v", c.Kind, model.Empty)
	}
}

func TestReadBlankLineIsSkipped(t *testing.T) {
	// A genuinely blank line (no delimiters at all) is skipped by
	// encoding/csv itself; row indices are sequential over the records
	// actually returned, not the file's original line numbers.
	path := writeFixture(t, "book.csv", "a,b\n1,2\n\n3,4\n")
	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(wb.Sheets[0].Rows) != 3 {
		t.Fatalf("len(Rows) = %d, want 3", len(wb.Sheets[0].Rows))
	}
	want := []int{0, 1, 2}
	for i, r := range wb.Sheets[0].Rows {
		if r.Index != want[i] {
			t.Errorf("Rows[%d].Index = %d, want %d", i, r.Index, want[i])
		}
	}
	c, ok := cellAt(t, wb, 2, 0)
	if !ok {
		t.Fatal("cell (row 2, col 0) not found")
	}
	if v, ok := c.StringValue(); !ok || v != "3" {
		t.Errorf("StringValue() = (%q, %v), want (\"3\", true)", v, ok)
	}
}

func TestReadRaggedRowIsError(t *testing.T) {
	path := writeFixture(t, "book.csv", "a,b,c\n1,2\n")
	if _, err := Read(path); err == nil {
		t.Fatal("Read() with a ragged row: want error, got nil")
	}
}

func TestReadStripsUTF8BOM(t *testing.T) {
	bom := "\xEF\xBB\xBF"
	path := writeFixture(t, "book.csv", bom+"name\nAda\n")
	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	c, ok := cellAt(t, wb, 0, 0)
	if !ok {
		t.Fatal("cell (row 0, col 0) not found")
	}
	if v, ok := c.StringValue(); !ok || v != "name" {
		t.Errorf("StringValue() = (%q, %v), want (\"name\", true) - a leading BOM leaked into the value", v, ok)
	}
}

func TestReadTSV(t *testing.T) {
	path := writeFixture(t, "book.tsv", "name\tage\nAda\t36\n")
	wb, err := ReadTSV(path)
	if err != nil {
		t.Fatalf("ReadTSV: %v", err)
	}
	c, ok := cellAt(t, wb, 1, 1)
	if !ok {
		t.Fatal("cell (row 1, col 1) not found")
	}
	if v, ok := c.StringValue(); !ok || v != "36" {
		t.Errorf("StringValue() = (%q, %v), want (\"36\", true)", v, ok)
	}
}

func TestReadTSVMissingFile(t *testing.T) {
	if _, err := ReadTSV(filepath.Join(t.TempDir(), "missing.tsv")); err == nil {
		t.Fatal("ReadTSV() with a missing file: want error, got nil")
	}
}
