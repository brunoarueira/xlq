package xlsx

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/brunoarueira/xlq/internal/filter"
)

func TestResolveTargetValid(t *testing.T) {
	cases := []struct {
		name        string
		target      filter.Expr
		wantSheet   string
		wantCellRef string
	}{
		{
			"plain sheet and cell",
			filter.Field{Base: filter.Field{Base: filter.Identity{}, Name: "Sheet1"}, Name: "A1"},
			"Sheet1", "A1",
		},
		{
			"bracket-string sheet name",
			filter.Field{Base: filter.Field{Base: filter.Identity{}, Name: "Q1 Report"}, Name: "Z99"},
			"Q1 Report", "Z99",
		},
		{
			"Excel's actual maximum address is in bounds",
			filter.Field{Base: filter.Field{Base: filter.Identity{}, Name: "Sheet1"}, Name: "XFD1048576"},
			"Sheet1", "XFD1048576",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sheet, cellRef, err := ResolveTarget(tc.target)
			if err != nil {
				t.Fatalf("ResolveTarget(%#v): %v", tc.target, err)
			}
			if sheet != tc.wantSheet || cellRef != tc.wantCellRef {
				t.Errorf("ResolveTarget(%#v) = (%q, %q), want (%q, %q)", tc.target, sheet, cellRef, tc.wantSheet, tc.wantCellRef)
			}
		})
	}
}

func TestResolveTargetInvalid(t *testing.T) {
	cases := []struct {
		name   string
		target filter.Expr
	}{
		{"bare identity, no sheet or cell at all", filter.Identity{}},
		{"sheet only, no cell", filter.Field{Base: filter.Identity{}, Name: "Sheet1"}},
		{"a whole column, not a single cell", filter.Field{Base: filter.Field{Base: filter.Identity{}, Name: "Sheet1"}, Name: "A"}},
		{"a range, not a single cell", filter.Field{Base: filter.Field{Base: filter.Identity{}, Name: "Sheet1"}, Name: "A1:C3"}},
		{"an index instead of a field for the cell", filter.Index{Base: filter.Field{Base: filter.Identity{}, Name: "Sheet1"}, N: 0}},
		{"a pipe inside the target", filter.Pipe{Left: filter.Field{Base: filter.Identity{}, Name: "Sheet1"}, Right: filter.Records{}}},
		{"a records step as the base instead of Identity", filter.Field{Base: filter.Field{Base: filter.Records{}, Name: "Sheet1"}, Name: "A1"}},
		{"row 0 isn't a real A1 address", filter.Field{Base: filter.Field{Base: filter.Identity{}, Name: "Sheet1"}, Name: "A0"}},
		{"row past Excel's maximum", filter.Field{Base: filter.Field{Base: filter.Identity{}, Name: "Sheet1"}, Name: "A1048577"}},
		{"column past Excel's maximum (XFD)", filter.Field{Base: filter.Field{Base: filter.Identity{}, Name: "Sheet1"}, Name: "XFE1"}},
		{"column letters longer than 3", filter.Field{Base: filter.Field{Base: filter.Identity{}, Name: "Sheet1"}, Name: "ABCD1"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := ResolveTarget(tc.target); err == nil {
				t.Errorf("ResolveTarget(%#v): want error, got nil", tc.target)
			}
		})
	}
}

func TestWriteCellSetsValueAndPreservesOtherContent(t *testing.T) {
	path := build(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "original"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
		if err := f.SetCellValue("Sheet1", "B1", 42.0); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	if err := WriteCell(path, "Sheet1", "A1", "updated"); err != nil {
		t.Fatalf("WriteCell: %v", err)
	}

	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer func() { _ = f.Close() }()

	got, err := f.GetCellValue("Sheet1", "A1")
	if err != nil {
		t.Fatalf("GetCellValue(A1): %v", err)
	}
	if got != "updated" {
		t.Errorf("A1 = %q, want %q", got, "updated")
	}

	// B1 was never the write's target - it must survive untouched,
	// since the write path never reduces the file to model.Workbook
	// and reconstructs it (that would silently drop anything the
	// model doesn't capture).
	gotB1, err := f.GetCellValue("Sheet1", "B1")
	if err != nil {
		t.Fatalf("GetCellValue(B1): %v", err)
	}
	if gotB1 != "42" {
		t.Errorf("B1 = %q, want %q (untouched)", gotB1, "42")
	}
}

func TestWriteCellValueTypes(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"string", "hello", "hello"},
		{"number", 5.5, "5.5"},
		{"integer-valued number", 5.0, "5"},
		{"bool true", true, "TRUE"},
		{"bool false", false, "FALSE"},
		{"null clears the cell", nil, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := build(t, func(f *excelize.File) {
				if err := f.SetCellValue("Sheet1", "A1", "placeholder"); err != nil {
					t.Fatalf("SetCellValue: %v", err)
				}
			})

			if err := WriteCell(path, "Sheet1", "A1", tc.value); err != nil {
				t.Fatalf("WriteCell: %v", err)
			}

			f, err := excelize.OpenFile(path)
			if err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			defer func() { _ = f.Close() }()

			got, err := f.GetCellValue("Sheet1", "A1")
			if err != nil {
				t.Fatalf("GetCellValue: %v", err)
			}
			if got != tc.want {
				t.Errorf("A1 = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWriteCellSheetNameCaseInsensitiveFallback(t *testing.T) {
	path := build(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "x"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	// The workbook's actual sheet is "Sheet1"; requesting the
	// lowercase "sheet1" must still resolve to it, matching the read
	// path's own findSheet semantics (exact match wins, otherwise the
	// first case-insensitive match).
	if err := WriteCell(path, "sheet1", "A1", "updated"); err != nil {
		t.Fatalf("WriteCell: %v", err)
	}

	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer func() { _ = f.Close() }()

	got, err := f.GetCellValue("Sheet1", "A1")
	if err != nil {
		t.Fatalf("GetCellValue: %v", err)
	}
	if got != "updated" {
		t.Errorf("A1 = %q, want %q", got, "updated")
	}
}

func TestWriteCellUnknownSheetRejected(t *testing.T) {
	path := build(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "x"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	if err := WriteCell(path, "NoSuchSheet", "A1", "x"); err == nil {
		t.Error("WriteCell with an unknown sheet: want error, got nil")
	}
}

// TestWriteCellLeavesOriginalUntouchedOnFailure confirms the safety
// model (docs/adr/0014-in-place-editing.md): if the write fails for any
// reason, the original file is never modified at all.
func TestWriteCellLeavesOriginalUntouchedOnFailure(t *testing.T) {
	path := build(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "original"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if err := WriteCell(path, "NoSuchSheet", "A1", "x"); err == nil {
		t.Fatal("WriteCell with an unknown sheet: want error, got nil")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(before) != string(after) {
		t.Error("original file was modified despite the write failing")
	}

	// No leftover temp file either - the failure happened before a
	// temp file was ever created (resolving the sheet name happens
	// before SaveAs).
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(path) {
			t.Errorf("unexpected leftover file in target directory: %s", e.Name())
		}
	}
}

// TestWriteCellFollowsSymlink confirms that editing through a symlink
// updates the real target file and leaves the symlink itself intact -
// not replaced by a regular file. os.Rename replaces a directory
// entry, not a file: renaming the write's temp file directly over a
// symlink's own name would turn the symlink into a plain file,
// silently breaking it while leaving whatever it pointed at
// untouched.
func TestWriteCellFollowsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privileges on Windows")
	}

	realPath := build(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "original"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})
	linkPath := filepath.Join(t.TempDir(), "link.xlsx")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	if err := WriteCell(linkPath, "Sheet1", "A1", "updated"); err != nil {
		t.Fatalf("WriteCell: %v", err)
	}

	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("linkPath is no longer a symlink after WriteCell - it was replaced by a regular file")
	}
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if target != realPath {
		t.Errorf("symlink now points to %q, want %q (unchanged)", target, realPath)
	}

	f, err := excelize.OpenFile(realPath)
	if err != nil {
		t.Fatalf("OpenFile(realPath): %v", err)
	}
	defer func() { _ = f.Close() }()
	got, err := f.GetCellValue("Sheet1", "A1")
	if err != nil {
		t.Fatalf("GetCellValue: %v", err)
	}
	if got != "updated" {
		t.Errorf("the real target's A1 = %q, want %q", got, "updated")
	}
}

// TestWriteCellFailureAfterStagingLeavesOriginalUntouched exercises a
// failure that happens after the temp file has already been created
// and written (SaveAs succeeds), not merely before staging begins
// (unlike TestWriteCellLeavesOriginalUntouchedOnFailure, which fails
// at the very first step, resolving the sheet name). The temp
// directory's path is made to exceed excelize's own 207 UTF-16-unit
// SaveAs limit, so SaveAs itself fails deterministically and
// portably, with the already-created temp file still on disk at that
// point - the same safety property (original untouched, temp file
// cleaned up) must hold here too.
func TestWriteCellFailureAfterStagingLeavesOriginalUntouched(t *testing.T) {
	// excelize's SaveAs itself enforces the 207-character limit this
	// test needs to trigger, so the fixture can't be created directly
	// at its final, over-long path - build it short, then move it
	// (a plain filesystem rename, which excelize's length check
	// doesn't apply to) into the long directory.
	shortPath := build(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "original"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})
	longDir := filepath.Join(t.TempDir(), strings.Repeat("a", 220))
	if err := os.MkdirAll(longDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(longDir, "book.xlsx")
	if err := os.Rename(shortPath, path); err != nil {
		t.Fatalf("Rename (fixture): %v", err)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	err = WriteCell(path, "Sheet1", "A1", "updated")
	if err == nil {
		t.Fatal("WriteCell with an over-long temp path: want error, got nil")
	}
	if !strings.Contains(err.Error(), "save") {
		t.Errorf("error = %q, want it to report the save step failing", err.Error())
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(before) != string(after) {
		t.Error("original file was modified despite the save failing")
	}

	entries, err := os.ReadDir(longDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(path) {
			t.Errorf("unexpected leftover file in target directory: %s", e.Name())
		}
	}
}

func TestWriteCellPreservesFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file permissions don't apply on Windows")
	}

	path := build(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "x"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	if err := WriteCell(path, "Sheet1", "A1", "updated"); err != nil {
		t.Fatalf("WriteCell: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	// os.CreateTemp always creates its file 0600 regardless of the
	// original's mode; without an explicit chmod before the rename, a
	// successful edit would silently narrow this to 0600.
	if got := info.Mode().Perm(); got != 0644 {
		t.Errorf("file mode after WriteCell = %o, want %o (the original's own mode)", got, 0644)
	}
}
