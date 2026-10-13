package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// buildFixture writes fn's changes to a fresh excelize file and returns
// the path to the saved .xlsx.
func buildFixture(t *testing.T, fn func(f *excelize.File)) string {
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

// run executes the root command with args, returning stdout.
//
// HOME (and XDG_CONFIG_HOME, which os.UserConfigDir prefers on Linux
// when non-empty) are pointed at an empty per-test directory first, so
// internal/config.DefaultPath never resolves to a config file that
// genuinely exists on whatever machine happens to run these tests -
// every test here exercises CLI-flag-driven behavior only, unless it
// explicitly passes its own --config. APPDATA is overridden the same
// way for the same reason on Windows, where os.UserConfigDir() uses it
// instead of HOME.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("APPDATA", t.TempDir())
	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestRootFilterCell(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "hello"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	out, err := run(t, ".Sheet1.A1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "\"hello\"\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootFilterWholeSheetGrid(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", 1.0); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
		if err := f.SetCellValue("Sheet1", "B1", 2.0); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	out, err := run(t, ".Sheet1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "[\n  [\n    1,\n    2\n  ]\n]\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootFilterPipe(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "value"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	out, err := run(t, ".Sheet1 | .A1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "\"value\"\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootFilterMissingSheetIsNull(t *testing.T) {
	path := buildFixture(t, func(*excelize.File) {})

	out, err := run(t, ".NoSuchSheet", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "null\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootFilterInvalidSyntax(t *testing.T) {
	path := buildFixture(t, func(*excelize.File) {})
	if _, err := run(t, "Sheet1", path); err == nil {
		t.Error("Execute with a filter missing its leading '.': want error, got nil")
	}
}

func TestRootFilterMissingFile(t *testing.T) {
	if _, err := run(t, ".Sheet1", filepath.Join(t.TempDir(), "missing.xlsx")); err == nil {
		t.Error("Execute with a missing file: want error, got nil")
	}
}

func TestRootFilterEvalError(t *testing.T) {
	path := buildFixture(t, func(*excelize.File) {})
	if _, err := run(t, ".Sheet1.Sheet_1", path); err == nil {
		t.Error("Execute with an invalid field name: want error, got nil")
	}
}

func TestRootWrongArgCount(t *testing.T) {
	// With SilenceUsage set, a plain cobra.ExactArgs error would leave a
	// bare `xlq` (or any wrong arg count) with no hint on how to get
	// help at all; the Args validator wraps it with a usage pointer.
	for _, args := range [][]string{{}, {".Sheet1"}} {
		_, err := run(t, args...)
		if err == nil {
			t.Fatalf("Execute(%v): want error, got nil", args)
		}
		if want := "Run 'xlq --help' for usage"; !strings.Contains(err.Error(), want) {
			t.Errorf("Execute(%v) error = %q, want it to contain %q", args, err.Error(), want)
		}
	}
}

func TestRootSheetsSubcommandStillWorks(t *testing.T) {
	path := buildFixture(t, func(*excelize.File) {})
	out, err := run(t, "sheets", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "Sheet1\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootFilterCompactDropsEmptyColumn(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", 1.0); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
		// B1 is left absent, so the sheet grid has a null column there.
		if err := f.SetCellValue("Sheet1", "C1", 3.0); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	out, err := run(t, "--compact", ".Sheet1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "[\n  [\n    1,\n    3\n  ]\n]\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootFilterCompactOutputPrintsOneLine(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", 1.0); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
		if err := f.SetCellValue("Sheet1", "B1", 2.0); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	out, err := run(t, "-c", ".Sheet1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "[[1,2]]\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootFilterRawOutputStripsQuotesFromString(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "hello"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	out, err := run(t, "-r", ".Sheet1.A1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "hello\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootFilterRawOutputLeavesNonStringAsJSON(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", 42.0); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	out, err := run(t, "-r", ".Sheet1.A1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "42\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootFilterCompactOutputAndRawOutputTogether(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "hi"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	out, err := run(t, "-c", "-r", ".Sheet1.A1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "hi\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootFilterInputFormatOverridesExtension(t *testing.T) {
	// A CSV file saved with a misleading .xlsx extension, read correctly
	// only because --input-format overrides what the extension implies.
	path := filepath.Join(t.TempDir(), "misnamed.xlsx")
	if err := os.WriteFile(path, []byte("A,B\n1,2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out, err := run(t, "--input-format", "csv", ".Sheet1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "[\n  [\n    \"A\",\n    \"B\"\n  ],\n  [\n    \"1\",\n    \"2\"\n  ]\n]\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootFilterInputFormatInvalidValue(t *testing.T) {
	path := buildFixture(t, func(*excelize.File) {})
	if _, err := run(t, "--input-format", "yaml", ".Sheet1", path); err == nil {
		t.Error("Execute with --input-format yaml: want error, got nil")
	}
}

func TestRootFilterUnrecognizedExtensionSuggestsInputFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.bin")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := run(t, ".Sheet1", path)
	if err == nil {
		t.Fatal("Execute on an unrecognized extension: want error, got nil")
	}
	if want := "--input-format"; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}
}

func TestSheetsSubcommandInputFormatOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "misnamed.xlsx")
	if err := os.WriteFile(path, []byte("A,B\n1,2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out, err := run(t, "--input-format", "csv", "sheets", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "Sheet1\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootFilterDelimiterOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.csv")
	if err := os.WriteFile(path, []byte("A;B\n1;2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out, err := run(t, "--delimiter", ";", "-c", ".Sheet1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := `[["A","B"],["1","2"]]` + "\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootFilterDelimiterMustBeOneCharacter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.csv")
	if err := os.WriteFile(path, []byte("A,B\n1,2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := run(t, "--delimiter", ";;", ".Sheet1", path); err == nil {
		t.Error("Execute with --delimiter ';;': want error, got nil")
	}
}

// TestRootFilterDelimiterExplicitlyEmptyIsRejected guards against
// --delimiter "" being indistinguishable from not passing --delimiter
// at all - an explicitly empty value is a flag the user did set, to
// zero characters, and must fail the one-character check like any
// other wrong length, not silently fall back to the default delimiter.
func TestRootFilterDelimiterExplicitlyEmptyIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.csv")
	if err := os.WriteFile(path, []byte("A,B\n1,2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := run(t, "--delimiter", "", ".Sheet1", path); err == nil {
		t.Error(`Execute with --delimiter "": want error, got nil`)
	}
}

// TestRootFilterDelimiterInvalidCharacterRejected guards against a
// delimiter character encoding/csv itself would refuse (here, a
// newline) only being caught once a filter actually reads the file -
// SheetNames' csv/tsv fast path never parses the file at all, so
// without validateDelimiter also rejecting it, `sheets` would silently
// accept a delimiter that reading the same file would error on.
func TestRootFilterDelimiterInvalidCharacterRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.csv")
	if err := os.WriteFile(path, []byte("A,B\n1,2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := run(t, "--delimiter", "\n", ".Sheet1", path); err == nil || !strings.Contains(err.Error(), "--delimiter") {
		t.Errorf(`Execute with --delimiter "\n": want a --delimiter error, got %v`, err)
	}
	if _, err := run(t, "--delimiter", "\n", "sheets", path); err == nil || !strings.Contains(err.Error(), "--delimiter") {
		t.Errorf(`Execute sheets with --delimiter "\n": want a --delimiter error, got %v`, err)
	}
}

func TestRootFilterDelimiterRejectedForXLSX(t *testing.T) {
	path := buildFixture(t, func(*excelize.File) {})
	if _, err := run(t, "--delimiter", ";", ".Sheet1", path); err == nil {
		t.Error("Execute with --delimiter on an .xlsx file: want error, got nil")
	}
}

func TestSheetsSubcommandDelimiterOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.csv")
	if err := os.WriteFile(path, []byte("A;B\n1;2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	out, err := run(t, "--delimiter", ";", "sheets", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "Sheet1\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootFilterTablePrintsAlignedGrid(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "Name"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
		if err := f.SetCellValue("Sheet1", "B1", "Score"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
		if err := f.SetCellValue("Sheet1", "A2", "Alice"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
		if err := f.SetCellValue("Sheet1", "B2", 90.0); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	out, err := run(t, "--table", ".Sheet1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "Name   Score\nAlice  90\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootFilterTableWithRecords(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "Name"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
		if err := f.SetCellValue("Sheet1", "A2", "Alice"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	out, err := run(t, "--table", ".Sheet1 | records", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "Name\nAlice\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootFilterTableScalar(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "hello"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	out, err := run(t, "--table", ".Sheet1.A1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "hello\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootFilterTableRejectedWithCompactOutput(t *testing.T) {
	path := buildFixture(t, func(*excelize.File) {})
	if _, err := run(t, "--table", "-c", ".Sheet1", path); err == nil {
		t.Error("Execute with --table -c: want error, got nil")
	}
}

func TestRootFilterTableRejectedWithRawOutput(t *testing.T) {
	path := buildFixture(t, func(*excelize.File) {})
	if _, err := run(t, "--table", "-r", ".Sheet1.A1", path); err == nil {
		t.Error("Execute with --table -r: want error, got nil")
	}
}

func TestRootFilterCSVOnGrid(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "Name"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
		if err := f.SetCellValue("Sheet1", "B1", "Score"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	out, err := run(t, "--csv", ".Sheet1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "Name,Score\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootFilterCSVOnRecords(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "Name"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
		if err := f.SetCellValue("Sheet1", "A2", "Alice"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	out, err := run(t, "--csv", ".Sheet1 | records", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "Name\nAlice\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootFilterTSVUsesTabDelimiter(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "a"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
		if err := f.SetCellValue("Sheet1", "B1", "b"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	out, err := run(t, "--tsv", ".Sheet1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "a\tb\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRootFilterCSVWholeWorkbookIsError(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "a"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})
	if _, err := run(t, "--csv", ".", path); err == nil {
		t.Error("Execute --csv on the whole workbook: want error, got nil")
	}
}

func TestRootFilterCSVAndTSVMutuallyExclusive(t *testing.T) {
	path := buildFixture(t, func(*excelize.File) {})
	if _, err := run(t, "--csv", "--tsv", ".Sheet1", path); err == nil {
		t.Error("Execute with --csv --tsv: want error, got nil")
	}
}

func TestRootFilterCSVRejectedWithTable(t *testing.T) {
	path := buildFixture(t, func(*excelize.File) {})
	if _, err := run(t, "--csv", "--table", ".Sheet1", path); err == nil {
		t.Error("Execute with --csv --table: want error, got nil")
	}
}

func TestRootFilterCSVRejectedWithCompactOutput(t *testing.T) {
	path := buildFixture(t, func(*excelize.File) {})
	if _, err := run(t, "--csv", "-c", ".Sheet1", path); err == nil {
		t.Error("Execute with --csv -c: want error, got nil")
	}
}
