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
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "x"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})
	cfgPath := writeTestConfig(t, "compact: true\ntable: true\n")

	// compact is independent of the table/csv/tsv/compact-output/
	// raw-output group: an explicit --csv must suppress config's
	// table:true (since they're mutually exclusive) but must NOT
	// suppress config's compact:true, which composes with every
	// format.
	withoutExplicitCSV, err := run(t, "--config", cfgPath, "--csv", ".Sheet1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	withCompactOnly, err := run(t, "--config", cfgPath, "--csv", "--compact", ".Sheet1", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if withoutExplicitCSV != withCompactOnly {
		t.Errorf("config's compact:true wasn't applied: --csv alone = %q, --csv --compact = %q, want equal", withoutExplicitCSV, withCompactOnly)
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
