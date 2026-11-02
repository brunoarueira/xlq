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

	got, err := sheetXMLPath(&zr.Reader, "Sheet1", 0)
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

	got, err := sheetXMLPath(&zr.Reader, "Renamed", 0)
	if err != nil {
		t.Fatalf("sheetXMLPath: %v", err)
	}
	if got != "xl/worksheets/sheet2.xml" {
		t.Errorf("sheetXMLPath(%q) = %q, want %q (its real relationship target, not a position guess)", "Renamed", got, "xl/worksheets/sheet2.xml")
	}

	got, err = sheetXMLPath(&zr.Reader, "Third", 0)
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

	_, err := sheetXMLPath(&zr.Reader, "DoesNotExist", 0)
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

	if _, err := sheetXMLPath(&zr.Reader, "sheet1", 0); err == nil {
		t.Error("sheetXMLPath(\"sheet1\") with the real sheet named \"Sheet1\": want an error (exact match only), got nil")
	}
}

// TestSheetXMLPathRejectsOversizedPart guards #51's own concern
// (docs/adr/0031-memory-ceiling-for-untrusted-files.md): workbook.xml
// and the rels part are no exception to the same bound every other
// part this new reader touches gets - a crafted file can declare any
// part's size arbitrarily, regardless of what that part conventionally
// holds.
func TestSheetXMLPathRejectsOversizedPart(t *testing.T) {
	path := build(t, func(f *excelize.File) {})
	zr := openZip(t, path)

	if _, err := sheetXMLPath(&zr.Reader, "Sheet1", 1); err == nil {
		t.Fatal("sheetXMLPath with maxSize=1: want an error (workbook.xml is well over 1 byte), got nil")
	} else if !strings.Contains(err.Error(), "limit") {
		t.Errorf("error = %q, want it to mention the size limit", err)
	}
}

func TestResolveRelationshipTarget(t *testing.T) {
	tests := []struct {
		target, want string
	}{
		// The common, same-directory-relative case excelize's own
		// output actually uses - confirmed directly.
		{"worksheets/sheet2.xml", "xl/worksheets/sheet2.xml"},
		// A package-absolute target (a leading "/") is relative to the
		// zip root, not to xl/ - prepending "xl/" to it, as naive
		// string concatenation would, is wrong.
		{"/xl/worksheets/sheet1.xml", "xl/worksheets/sheet1.xml"},
		// ".." segments must actually resolve, not end up as literal
		// path components no real zip entry name would ever contain.
		{"../worksheets/sheet1.xml", "worksheets/sheet1.xml"},
	}
	for _, tt := range tests {
		if got := resolveRelationshipTarget(tt.target); got != tt.want {
			t.Errorf("resolveRelationshipTarget(%q) = %q, want %q", tt.target, got, tt.want)
		}
	}
}
