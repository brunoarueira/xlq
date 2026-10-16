package cli

import (
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestRootMultiFileMapsFilterOverEachFile(t *testing.T) {
	old := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "old"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})
	newer := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "new"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	out, err := run(t, ".Sheet1.A1", old, newer)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "[\n  \"old\",\n  \"new\"\n]\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootSingleFileResultStaysUnwrapped(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "x"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	// Exactly one file must print the bare result, not a one-element
	// array - ADR-0017's explicit promise that a single-file invocation
	// is completely unchanged.
	out, err := run(t, ".Sheet1.A1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "\"x\"\n"; out != want {
		t.Errorf("output = %q, want %q (bare, not wrapped in an array)", out, want)
	}
}

func TestRootMultiFileThreeOrMoreFiles(t *testing.T) {
	paths := make([]string, 3)
	for i := range paths {
		i := i
		paths[i] = buildFixture(t, func(f *excelize.File) {
			if err := f.SetCellValue("Sheet1", "A1", i); err != nil {
				t.Fatalf("SetCellValue: %v", err)
			}
		})
	}

	out, err := run(t, append([]string{"-c", ".Sheet1.A1"}, paths...)...)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "[0,1,2]\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootMultiFileTableCSVTSVRejected(t *testing.T) {
	a := buildFixture(t, func(*excelize.File) {})
	b := buildFixture(t, func(*excelize.File) {})

	for _, flag := range []string{"--table", "--csv", "--tsv"} {
		t.Run(flag, func(t *testing.T) {
			_, err := run(t, flag, ".Sheet1", a, b)
			if err == nil {
				t.Fatalf("Execute with %s and two files: want error, got nil", flag)
			}
			if !strings.Contains(err.Error(), "don't support multiple files") {
				t.Errorf("error = %q, want it to explain multiple files aren't supported", err.Error())
			}
		})
	}
}

func TestRootMultiFileInPlaceRejected(t *testing.T) {
	a := buildFixture(t, func(*excelize.File) {})
	b := buildFixture(t, func(*excelize.File) {})

	_, err := run(t, "-i", `.Sheet1.A1 = "x"`, a, b)
	if err == nil {
		t.Fatal("Execute with -i and two files: want error, got nil")
	}
	if !strings.Contains(err.Error(), "--in-place requires exactly one file") {
		t.Errorf("error = %q, want it to explain -i needs exactly one file", err.Error())
	}
}

func TestRootMultiFileCompactAppliesPerFileNotToTheArray(t *testing.T) {
	// file1: A1 and C1 set, B1 empty (an empty column to compact away).
	// file2: nothing set at all (its own result is already "empty").
	// --compact must compact each file's own grid individually, and
	// must never drop a whole file's result from the array just
	// because that result happens to end up empty - the array's
	// length and index-to-file correspondence must survive regardless.
	file1 := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "x"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
		if err := f.SetCellValue("Sheet1", "C1", "y"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})
	file2 := buildFixture(t, func(*excelize.File) {})

	out, err := run(t, "--compact", "-c", ".Sheet1", file1, file2)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// file1's empty B column is dropped; file2's wholly-empty sheet
	// compacts to an empty grid - but it's still present, as the
	// array's second element, not removed.
	if want := `[[["x","y"]],[]]` + "\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// TestRootMultiFileArrayKeepsANullElement specifically exercises a
// per-file result that's Go nil (not merely an empty slice, which
// marshals to "[]" and would pass even if the array-wrapping step
// wrongly dropped nil entries the way output.Compact's own []any case
// already does for an ordinary row/column). A result array index
// must survive being null exactly as readily as any other value -
// dropping it would both shrink the array and break its
// correspondence to the file list.
func TestRootMultiFileArrayKeepsANullElement(t *testing.T) {
	withValue := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "x"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})
	withoutValue := buildFixture(t, func(*excelize.File) {}) // A1 genuinely absent -> null

	out, err := run(t, "-c", ".Sheet1.A1", withValue, withoutValue)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := `["x",null]` + "\n"; out != want {
		t.Errorf("output = %q, want %q (the null element must still be present)", out, want)
	}
}

func TestRootMultiFileReadErrorNamesFileAndPosition(t *testing.T) {
	good := buildFixture(t, func(*excelize.File) {})
	// A supported (.xlsx) extension, so this actually exercises a
	// missing-file failure inside input.Read itself - an unsupported
	// extension would instead fail format detection before ever
	// attempting to open the file, a different failure path entirely
	// that happens to produce a similarly-shaped error message.
	missing := good + ".does-not-exist.xlsx"

	_, err := run(t, ".Sheet1", good, missing)
	if err == nil {
		t.Fatal("Execute with a missing second file: want error, got nil")
	}
	if !strings.Contains(err.Error(), "file 2") || !strings.Contains(err.Error(), missing) {
		t.Errorf("error = %q, want it to name file 2 and its path (%q)", err.Error(), missing)
	}
}

func TestRootMultiFileRawOutputFallsThroughToJSON(t *testing.T) {
	a := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "x"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})
	b := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "y"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	// -r only unwraps a bare string result; the multi-file top-level
	// result is always an array, so -r falls through to ordinary JSON
	// printing - the same way it already does for any non-string
	// single-file result, with no special-casing needed.
	out, err := run(t, "-r", "-c", ".Sheet1.A1", a, b)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := `["x","y"]` + "\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}
