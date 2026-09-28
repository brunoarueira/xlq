package cli

import (
	"bytes"
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
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
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
