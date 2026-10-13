package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// writeTestConfig writes contents to a config file in its own
// directory and returns its path, for use with --config.
func writeTestConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestConfigAppliesAsDefault(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "x"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
		if err := f.SetCellValue("Sheet1", "B1", "y"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})
	cfgPath := writeTestConfig(t, "table: true\n")

	out, err := run(t, "--config", cfgPath, ".Sheet1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Table output (output.Table), not JSON - config's table:true
	// applied as a default. JSON would render this as a "[[...]]"
	// array; table output is "x  y\n" (space-padded by tabwriter).
	if want := "x  y\n"; out != want {
		t.Errorf("output = %q, want %q (table output, meaning config's table:true applied)", out, want)
	}
}

func TestConfigExplicitFlagOverridesConfig(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "x"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})
	cfgPath := writeTestConfig(t, "table: true\n")

	// An explicit --csv must fully override config's table:true - not
	// just leave table unset while also being true, which would fail
	// outputOptions.validate()'s mutual-exclusion check instead of
	// honoring the override.
	out, err := run(t, "--config", cfgPath, "--csv", ".Sheet1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "x\n"; out != want {
		t.Errorf("output = %q, want %q (csv output, not a validation error)", out, want)
	}
}

func TestConfigCompactComposesWithExplicitFormatFlag(t *testing.T) {
	// A1="x", C1="y", with column B entirely empty across the sheet's
	// only row - a genuinely droppable column for output.Compact to
	// remove. A fixture with no empty row/column at all can't tell a
	// correctly-applied compact:true from one silently never applied:
	// both would produce identical output.
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "x"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
		if err := f.SetCellValue("Sheet1", "C1", "y"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})
	cfgPath := writeTestConfig(t, "compact: true\ntable: true\n")

	// compact is independent of the table/csv/tsv/compact-output/
	// raw-output group: an explicit --csv must suppress config's
	// table:true (since they're mutually exclusive) but must NOT
	// suppress config's compact:true, which composes with every
	// format.
	out, err := run(t, "--config", cfgPath, "--csv", ".Sheet1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "x,y\n"; out != want {
		t.Errorf("output = %q, want %q (the empty B column dropped by config's compact:true, despite --csv suppressing table's own config default)", out, want)
	}
}

func TestConfigSuppressedUnderInPlace(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "original"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})
	// table:true would normally make -i's own mutual-exclusion check
	// fail; input-format: csv would normally make runInPlace reject a
	// real .xlsx file. Neither should ever be applied once -i is
	// explicit (ADR-0015).
	cfgPath := writeTestConfig(t, "table: true\ninput-format: csv\n")

	out, err := run(t, "--config", cfgPath, "-i", `.Sheet1.A1 = "updated"`, path)
	if err != nil {
		t.Fatalf("Execute with -i and a config file that would otherwise conflict with it: %v", err)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
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

func TestConfigDoesNotApplyToUnrelatedSheetsFlags(t *testing.T) {
	path := buildFixture(t, func(*excelize.File) {})
	// sheets has no --table flag at all; config setting it must not
	// error or otherwise affect a sheets invocation.
	cfgPath := writeTestConfig(t, "table: true\n")

	out, err := run(t, "--config", cfgPath, "sheets", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "Sheet1\n" {
		t.Errorf("output = %q, want %q", out, "Sheet1\n")
	}
}

func TestConfigInputFormatAppliesToSheets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(path, []byte("A,B\n1,2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfgPath := writeTestConfig(t, "input-format: csv\n")

	out, err := run(t, "--config", cfgPath, "sheets", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out == "" {
		t.Error("output is empty, want the csv's single sheet name")
	}
}

func TestConfigMissingAtDefaultLocationIsNotAnError(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "x"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	// No --config passed, and run() already points HOME/XDG_CONFIG_HOME
	// at an empty directory - the default config path doesn't exist.
	if _, err := run(t, ".Sheet1.A1", path); err != nil {
		t.Fatalf("Execute with no config file at the default location: %v", err)
	}
}

func TestConfigExplicitPathMissingIsAnError(t *testing.T) {
	path := buildFixture(t, func(*excelize.File) {})
	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	_, err := run(t, "--config", missing, ".Sheet1", path)
	if err == nil {
		t.Fatal("Execute with an explicit --config pointing at a missing file: want error, got nil")
	}
	if !strings.Contains(err.Error(), "--config") {
		t.Errorf("error = %q, want it to mention --config", err.Error())
	}
}

func TestConfigUnknownKeyRejected(t *testing.T) {
	path := buildFixture(t, func(*excelize.File) {})
	cfgPath := writeTestConfig(t, "delimeter: \";\"\n") // typo

	_, err := run(t, "--config", cfgPath, ".Sheet1", path)
	if err == nil {
		t.Fatal("Execute with an unrecognized config key: want error, got nil")
	}
}

// TestConfigExplicitFalseInPlaceDoesNotSuppressConfig guards against a
// prior bug: the -i suppression check used Flags().Changed, which
// is also true for an explicit but no-op "--in-place=false" - wrongly
// suppressing every config default even though -i isn't actually
// active. The check must read the flag's actual value instead.
func TestConfigExplicitFalseInPlaceDoesNotSuppressConfig(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "x"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})
	cfgPath := writeTestConfig(t, "table: true\n")

	out, err := run(t, "--config", cfgPath, "--in-place=false", ".Sheet1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "x\n"; out != want {
		t.Errorf("output = %q, want %q (config's table:true should still apply)", out, want)
	}
}

// TestConfigExplicitFalseFormatFlagDoesNotSuppressGroup guards against
// the same class of bug as above, for the five-flag group: an explicit
// but no-op "--table=false" must not suppress a config csv:true
// default, the way an explicitly *active* --csv/--table/etc. does.
func TestConfigExplicitFalseFormatFlagDoesNotSuppressGroup(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "x"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})
	cfgPath := writeTestConfig(t, "csv: true\n")

	out, err := run(t, "--config", cfgPath, "--table=false", ".Sheet1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "x\n"; out != want {
		t.Errorf("output = %q, want %q (config's csv:true should still apply)", out, want)
	}
}

// TestConfigNotLoadedForCompletionRequest guards against a config
// file's own mistake (here, an unrecognized key) silently breaking
// shell completion on every keystroke - completion has nothing to do
// with config-defaultable flags at all, so it must never even attempt
// to load the file.
func TestConfigNotLoadedForCompletionRequest(t *testing.T) {
	cfgPath := writeTestConfig(t, "delimeter: \";\"\n") // typo, would error if loaded

	out, err := run(t, "--config", cfgPath, "completion", "bash")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out == "" {
		t.Error("completion script is empty")
	}
}
