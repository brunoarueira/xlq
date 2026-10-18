package input

import (
	"archive/zip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"

	"github.com/brunoarueira/xlq/internal/model"
)

func TestParseFormatValid(t *testing.T) {
	cases := []struct {
		in   string
		want Format
	}{
		{"xlsx", XLSX},
		{"XLSX", XLSX},
		{"csv", CSV},
		{"Csv", CSV},
		{"tsv", TSV},
		{"ods", ODS},
		{"ODS", ODS},
	}
	for _, tc := range cases {
		got, err := ParseFormat(tc.in)
		if err != nil {
			t.Errorf("ParseFormat(%q) returned error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("ParseFormat(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseFormatInvalid(t *testing.T) {
	for _, in := range []string{"txt", "xls", "json", ""} {
		if _, err := ParseFormat(in); err == nil {
			t.Errorf("ParseFormat(%q): want error, got nil", in)
		}
	}
}

func buildXLSX(t *testing.T, path string) {
	t.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	if err := f.SetCellValue("Sheet1", "A1", "hello"); err != nil {
		t.Fatalf("SetCellValue: %v", err)
	}
	if _, err := f.NewSheet("Data"); err != nil {
		t.Fatalf("NewSheet: %v", err)
	}
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
}

func buildCSV(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// wrapODSSpreadsheet and buildODS mirror internal/ods's own test
// helpers (unexported there, so not reusable directly) - just enough
// to prove this package dispatches to ods.Read, not to re-verify ods's
// own parsing, which internal/ods's tests already cover.
func wrapODSSpreadsheet(body string) string {
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

func buildODS(t *testing.T, path, contentXML string) {
	t.Helper()
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
}

const odsTableXML = `<table:table table:name="Sheet1">
  <table:table-row>
    <table:table-cell office:value-type="string"><text:p>hello</text:p></table:table-cell>
  </table:table-row>
</table:table>`

func TestReadDetectsByExtension(t *testing.T) {
	dir := t.TempDir()

	xlsxPath := filepath.Join(dir, "book.xlsx")
	buildXLSX(t, xlsxPath)

	csvPath := filepath.Join(dir, "book.csv")
	buildCSV(t, csvPath, "a,b\n1,2\n")

	tsvPath := filepath.Join(dir, "book.tsv")
	buildCSV(t, tsvPath, "a\tb\n1\t2\n")

	odsPath := filepath.Join(dir, "book.ods")
	buildODS(t, odsPath, wrapODSSpreadsheet(odsTableXML))

	for _, path := range []string{xlsxPath, csvPath, tsvPath, odsPath} {
		if _, err := Read(path, "", 0); err != nil {
			t.Errorf("Read(%q, \"\") returned error: %v", path, err)
		}
	}
}

func TestReadUnknownExtensionErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.txt")
	if err := os.WriteFile(path, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := Read(path, "", 0)
	if err == nil {
		t.Fatal("Read with an unrecognized extension: want error, got nil")
	}
	if !strings.Contains(err.Error(), "--input-format") {
		t.Errorf("error = %q, want it to mention --input-format", err.Error())
	}
}

func TestReadExplicitFormatOverridesExtension(t *testing.T) {
	// A CSV file saved with a .txt extension - the motivating case from
	// the issue: a misnamed file, resolved by an explicit override
	// rather than extension sniffing.
	path := filepath.Join(t.TempDir(), "misnamed.txt")
	buildCSV(t, path, "a,b\n1,2\n")

	wb, err := Read(path, CSV, 0)
	if err != nil {
		t.Fatalf("Read(%q, CSV): %v", path, err)
	}
	if len(wb.Sheets) != 1 || wb.Sheets[0].Name != "Sheet1" {
		t.Errorf("Read(%q, CSV) = %+v, want a single Sheet1", path, wb)
	}
}

func TestReadInvalidExplicitFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.csv")
	buildCSV(t, path, "a,b\n1,2\n")
	if _, err := Read(path, Format("yaml"), 0); err == nil {
		t.Error(`Read with format "yaml": want error, got nil`)
	}
}

// TestReadDelimiterOverride confirms an explicit delimiter overrides
// the format's own default - a semicolon-separated export, detected as
// csv by extension, read correctly only because delim overrides the
// comma a bare .csv would otherwise assume. The motivating case from
// the issue: a locale where comma is the decimal separator.
func TestReadDelimiterOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.csv")
	buildCSV(t, path, "a;b\n1;2\n")

	wb, err := Read(path, "", ';')
	if err != nil {
		t.Fatalf("Read(%q, \"\", ';'): %v", path, err)
	}
	rows, err := model.CollectRows(wb.Sheets[0])
	if err != nil {
		t.Fatalf("CollectRows: %v", err)
	}
	if len(wb.Sheets) != 1 || len(rows) != 2 {
		t.Fatalf("Read(%q, \"\", ';') = %+v, want 2 rows", path, wb)
	}
}

// TestReadDelimiterOverridesTSVDefaultToo confirms the override applies
// regardless of which delimiter-based format it's layered on - a tsv
// file whose actual separator is a semicolon, not a tab, still reads
// correctly once delim overrides tsv's own tab default.
func TestReadDelimiterOverridesTSVDefaultToo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.tsv")
	buildCSV(t, path, "a;b\n1;2\n")

	wb, err := Read(path, "", ';')
	if err != nil {
		t.Fatalf("Read(%q, \"\", ';'): %v", path, err)
	}
	rows, err := model.CollectRows(wb.Sheets[0])
	if err != nil {
		t.Fatalf("CollectRows: %v", err)
	}
	if len(wb.Sheets) != 1 || len(rows) != 2 {
		t.Fatalf("Read(%q, \"\", ';') = %+v, want 2 rows", path, wb)
	}
}

// TestReadDelimiterRejectedForXLSXAndODS confirms a non-zero delimiter
// is a clear error for a format with no delimiter concept at all,
// rather than being silently ignored.
func TestReadDelimiterRejectedForXLSXAndODS(t *testing.T) {
	dir := t.TempDir()

	xlsxPath := filepath.Join(dir, "book.xlsx")
	buildXLSX(t, xlsxPath)
	if _, err := Read(xlsxPath, "", ';'); err == nil {
		t.Error("Read(xlsx, delim=';'): want error, got nil")
	}

	odsPath := filepath.Join(dir, "book.ods")
	buildODS(t, odsPath, wrapODSSpreadsheet(odsTableXML))
	if _, err := Read(odsPath, "", ';'); err == nil {
		t.Error("Read(ods, delim=';'): want error, got nil")
	}
}

// TestSheetNamesDelimiterRejectedForXLSX confirms SheetNames validates
// delim the same way Read does, even though csv/tsv's own sheet name
// doesn't depend on it - passing --delimiter alongside xlsx should
// still be a clear error here, not silently accepted.
func TestSheetNamesDelimiterRejectedForXLSX(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.xlsx")
	buildXLSX(t, path)
	if _, err := SheetNames(path, "", ';'); err == nil {
		t.Error("SheetNames(xlsx, delim=';'): want error, got nil")
	}
}

// TestDelimiterInvalidCharactersRejected guards against a delimiter
// character encoding/csv itself would refuse (a double quote, carriage
// return, newline, or invalid rune) only surfacing once a filter
// actually reads the file - Read and SheetNames share validateDelimiter,
// so both must reject the same characters the same way, not just
// whichever one happens to parse the file.
func TestDelimiterInvalidCharactersRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.csv")
	buildCSV(t, path, "a,b\n1,2\n")

	for _, delim := range []rune{'"', '\r', '\n', utf8.RuneError} {
		if _, err := Read(path, "", delim); err == nil {
			t.Errorf("Read(delim=%q): want error, got nil", delim)
		}
		if _, err := SheetNames(path, "", delim); err == nil {
			t.Errorf("SheetNames(delim=%q): want error, got nil", delim)
		}
	}
}

func TestSheetNamesDetectsByExtensionAndFormat(t *testing.T) {
	dir := t.TempDir()

	xlsxPath := filepath.Join(dir, "book.xlsx")
	buildXLSX(t, xlsxPath)
	got, err := SheetNames(xlsxPath, "", 0)
	if err != nil {
		t.Fatalf("SheetNames(xlsx): %v", err)
	}
	if want := []string{"Sheet1", "Data"}; !reflect.DeepEqual(got, want) {
		t.Errorf("SheetNames(xlsx) = %v, want %v", got, want)
	}

	csvPath := filepath.Join(dir, "book.csv")
	buildCSV(t, csvPath, "a,b\n1,2\n")
	got, err = SheetNames(csvPath, "", 0)
	if err != nil {
		t.Fatalf("SheetNames(csv): %v", err)
	}
	if want := []string{"Sheet1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("SheetNames(csv) = %v, want %v", got, want)
	}

	odsPath := filepath.Join(dir, "book.ods")
	buildODS(t, odsPath, wrapODSSpreadsheet(odsTableXML))
	got, err = SheetNames(odsPath, "", 0)
	if err != nil {
		t.Fatalf("SheetNames(ods): %v", err)
	}
	if want := []string{"Sheet1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("SheetNames(ods) = %v, want %v", got, want)
	}
}

// TestSheetNamesCSVStillChecksFileExists guards against the fixed-name
// shortcut for csv/tsv (SheetName is always known without parsing)
// skipping existence entirely - a missing file must still error the
// same way a missing xlsx or ods one does, not silently report
// "Sheet1" for a file that was never there.
func TestSheetNamesCSVStillChecksFileExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.csv")
	if _, err := SheetNames(path, "", 0); err == nil {
		t.Error("SheetNames on a missing .csv file: want error, got nil")
	}
}

// TestSheetNamesCSVRejectsDirectory guards against a gap a cubic-
// dev-ai review caught: os.Stat alone can't tell a directory from a
// regular file - a directory exists and stats successfully, so a
// directory misleadingly named "dir.csv" would otherwise report
// "Sheet1" instead of erroring the way actually reading it (or the
// xlsx/ods branches, which do open/parse) already would.
func TestSheetNamesCSVRejectsDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dir.csv")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if _, err := SheetNames(path, "", 0); err == nil {
		t.Error("SheetNames on a directory named *.csv: want error, got nil")
	}
}

func TestSheetNamesUnknownExtensionErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.bin")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := SheetNames(path, "", 0); err == nil {
		t.Error("SheetNames with an unrecognized extension: want error, got nil")
	}
}
