package csv

import (
	"os"
	"path/filepath"
	"strings"
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
	rows := rowsOf(t, wb.Sheets[0])
	for _, r := range rows {
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

// rowsOf drains sheet's rows into a slice, failing the test on any
// error - internal/csv's own reader never produces one once Read has
// already succeeded, so this is purely a convenience for assertions.
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
// used to do directly - removed along with Sheet.Rows becoming lazy,
// since a caller that needs it can trivially derive it like this
// (mirroring internal/eval's own per-operation accumulation, ADR-0018).
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
	if got := rowsOf(t, wb.Sheets[0]); len(got) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(got))
	}

	c, ok := cellAt(t, wb, 1, 0)
	if !ok {
		t.Fatal("cell (row 1, col 0) not found")
	}
	if v, ok := c.StringValue(); !ok || v != "Ada" {
		t.Errorf("StringValue() = (%q, %v), want (\"Ada\", true)", v, ok)
	}

	rows, cols := dimensions(t, wb.Sheets[0])
	if rows != 3 || cols != 2 {
		t.Errorf("dimensions() = (%d, %d), want (3, 2)", rows, cols)
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
	rows := rowsOf(t, wb.Sheets[0])
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	if rows[1].Index != 1 {
		t.Errorf("rows[1].Index = %d, want 1", rows[1].Index)
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
	rows := rowsOf(t, wb.Sheets[0])
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	want := []int{0, 1, 2}
	for i, r := range rows {
		if r.Index != want[i] {
			t.Errorf("rows[%d].Index = %d, want %d", i, r.Index, want[i])
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

func TestReadWithDelimiterSemicolon(t *testing.T) {
	path := writeFixture(t, "book.csv", "name;age\nAda;36\n")
	wb, err := ReadWithDelimiter(path, ';')
	if err != nil {
		t.Fatalf("ReadWithDelimiter: %v", err)
	}
	c, ok := cellAt(t, wb, 1, 1)
	if !ok {
		t.Fatal("cell (row 1, col 1) not found")
	}
	if v, ok := c.StringValue(); !ok || v != "36" {
		t.Errorf("StringValue() = (%q, %v), want (\"36\", true)", v, ok)
	}
}

func TestReadWithDelimiterMissingFile(t *testing.T) {
	if _, err := ReadWithDelimiter(filepath.Join(t.TempDir(), "missing.csv"), ';'); err == nil {
		t.Fatal("ReadWithDelimiter() with a missing file: want error, got nil")
	}
}

// TestReadRejectsOverMaxMaterializedCells guards #51's cell-count cap
// (docs/adr/0031-memory-ceiling-for-untrusted-files.md): a file within
// --max-size's own byte limit but built from an enormous number of
// tiny fields, where each materialized model.Cell/model.Row's own
// struct overhead - not the file's raw byte size - is what actually
// risks memory. maxMaterializedCells is temporarily lowered rather
// than writing a genuinely 10-million-cell fixture.
func TestReadRejectsOverMaxMaterializedCells(t *testing.T) {
	old := maxMaterializedCells
	maxMaterializedCells = 5
	defer func() { maxMaterializedCells = old }()

	path := writeFixture(t, "big.csv", "a,b,c\n1,2,3\n4,5,6\n")
	if _, err := Read(path); err == nil {
		t.Fatal("Read over the materialized-cell cap: want error, got nil")
	} else if !strings.Contains(err.Error(), "over the limit") {
		t.Errorf("error = %q, want it to mention the cell limit", err)
	}
}

// TestReadWithinMaxMaterializedCellsStillSucceeds confirms the cap
// only rejects a file once it's actually exceeded, not a file right at
// (or under) the limit - a fixture with exactly as many cells as the
// (lowered, for this test) cap allows must still read successfully.
func TestReadWithinMaxMaterializedCellsStillSucceeds(t *testing.T) {
	old := maxMaterializedCells
	maxMaterializedCells = 6 // exactly 2 rows x 3 cols below
	defer func() { maxMaterializedCells = old }()

	path := writeFixture(t, "ok.csv", "a,b,c\n1,2,3\n")
	wb, err := Read(path)
	if err != nil {
		t.Fatalf("Read at exactly the materialized-cell cap: %v", err)
	}
	if len(wb.Sheets) != 1 {
		t.Fatalf("len(wb.Sheets) = %d, want 1", len(wb.Sheets))
	}
}

// TestReadRejectsSingleOversizedRecord guards the gap a cubic-dev-ai
// review caught in maxMaterializedCells's own first version: it was
// checked only after encoding/csv.Reader.Read returned a full record,
// so a single record with more fields than the cap would already have
// paid that whole allocation cost before the check ever ran. One
// unbroken line (no newline at all) with more fields than the
// (lowered) cap must still be rejected - not just many separate
// smaller records, which TestReadRejectsOverMaxMaterializedCells
// already covers.
func TestReadRejectsSingleOversizedRecord(t *testing.T) {
	old := maxMaterializedCells
	maxMaterializedCells = 5
	defer func() { maxMaterializedCells = old }()

	path := writeFixture(t, "oneline.csv", strings.Repeat("x,", 50))
	if _, err := Read(path); err == nil {
		t.Fatal("Read with a single record over the materialized-cell cap: want error, got nil")
	} else if !strings.Contains(err.Error(), "over the limit") {
		t.Errorf("error = %q, want it to mention the field/cell limit", err)
	}
}

// TestFieldCountLimitedReaderStopsAtLimit unit-tests
// fieldCountLimitedReader directly: given a source with far more
// delimiters than max, its very first Read call - not some later one,
// and not only once the underlying source is fully drained - must
// already report the error, proving it detects the violation while
// streaming rather than only after a caller (encoding/csv.Reader, in
// ReadWithDelimiter's real usage) has already materialized everything.
func TestFieldCountLimitedReaderStopsAtLimit(t *testing.T) {
	src := strings.NewReader(strings.Repeat("1,", 1000))
	r := &fieldCountLimitedReader{r: src, delim: ',', max: 5}

	buf := make([]byte, 2000)
	_, err := r.Read(buf)
	if err == nil {
		t.Fatal("Read: want an error once the delimiter count exceeds max within the first Read call, got nil")
	}
	if !strings.Contains(err.Error(), "over the limit") {
		t.Errorf("error = %q, want it to mention the limit", err)
	}

	// Once tripped, it must keep failing on every subsequent call too,
	// not just the one call where the limit was first exceeded.
	if _, err := r.Read(buf); err == nil {
		t.Error("Read after already tripping the limit: want it to keep failing, got nil")
	}
}
