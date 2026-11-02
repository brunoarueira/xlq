package xlsx

import (
	"archive/zip"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// openZip opens path (built via the build helper) as a *zip.Reader
// for sheetXMLPath's own tests - a plain xlsx file is a zip archive.
func openZip(t *testing.T, path string) *zip.ReadCloser {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("zip.OpenReader(%q): %v", path, err)
	}
	t.Cleanup(func() { _ = zr.Close() })
	return zr
}

func TestSheetXMLPathSingleSheet(t *testing.T) {
	path := build(t, func(f *excelize.File) {})
	zr := openZip(t, path)

	got, err := sheetXMLPath(&zr.Reader, "Sheet1")
	if err != nil {
		t.Fatalf("sheetXMLPath: %v", err)
	}
	if got != "xl/worksheets/sheet1.xml" {
		t.Errorf("sheetXMLPath(%q) = %q, want %q", "Sheet1", got, "xl/worksheets/sheet1.xml")
	}
}

// TestSheetXMLPathAfterReorderRenameDelete guards the actual reason
// this function exists rather than a naming-convention shortcut
// (sheetN.xml matching the Nth sheet): confirmed directly that a
// workbook whose first sheet was deleted, and whose second sheet was
// then renamed, ends up with no sheet1.xml in the archive at all -
// the renamed sheet still correctly resolves to sheet2.xml, its real,
// original relationship target, not a guess from its current
// (now first) position in the sheet list.
func TestSheetXMLPathAfterReorderRenameDelete(t *testing.T) {
	path := build(t, func(f *excelize.File) {
		must(t, func() error { _, err := f.NewSheet("Second"); return err }())
		must(t, func() error { _, err := f.NewSheet("Third"); return err }())
		must(t, f.DeleteSheet("Sheet1"))
		must(t, f.SetSheetName("Second", "Renamed"))
	})
	zr := openZip(t, path)

	got, err := sheetXMLPath(&zr.Reader, "Renamed")
	if err != nil {
		t.Fatalf("sheetXMLPath: %v", err)
	}
	if got != "xl/worksheets/sheet2.xml" {
		t.Errorf("sheetXMLPath(%q) = %q, want %q (its real relationship target, not a position guess)", "Renamed", got, "xl/worksheets/sheet2.xml")
	}

	got, err = sheetXMLPath(&zr.Reader, "Third")
	if err != nil {
		t.Fatalf("sheetXMLPath: %v", err)
	}
	if got != "xl/worksheets/sheet3.xml" {
		t.Errorf("sheetXMLPath(%q) = %q, want %q", "Third", got, "xl/worksheets/sheet3.xml")
	}
}

func TestSheetXMLPathUnknownSheet(t *testing.T) {
	path := build(t, func(f *excelize.File) {})
	zr := openZip(t, path)

	_, err := sheetXMLPath(&zr.Reader, "DoesNotExist")
	if err == nil {
		t.Fatal("sheetXMLPath(unknown sheet): want error, got nil")
	}
	if !strings.Contains(err.Error(), "DoesNotExist") {
		t.Errorf("error = %q, want it to name the missing sheet", err)
	}
}

// TestSheetXMLPathCaseSensitive confirms sheetName is matched
// exactly, not case-insensitively - unlike eval.findSheet's own
// case-insensitive fallback for a user-typed filter reference, every
// caller here already has an exact name from GetSheetList().
func TestSheetXMLPathCaseSensitive(t *testing.T) {
	path := build(t, func(f *excelize.File) {})
	zr := openZip(t, path)

	if _, err := sheetXMLPath(&zr.Reader, "sheet1"); err == nil {
		t.Error("sheetXMLPath(\"sheet1\") with the real sheet named \"Sheet1\": want an error (exact match only), got nil")
	}
}
