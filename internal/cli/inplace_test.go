package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestRootInPlaceSetsCellAndPrintsNothing(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "original"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	out, err := run(t, "-i", `.Sheet1.A1 = "updated"`, path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty (yq -i's own silence on success)", out)
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

func TestRootInPlaceLongFlag(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "original"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	if _, err := run(t, "--in-place", `.Sheet1.A1 = 42`, path); err != nil {
		t.Fatalf("Execute: %v", err)
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
	if got != "42" {
		t.Errorf("A1 = %q, want %q", got, "42")
	}
}

func TestRootInPlaceRequiresAssignmentFilter(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "x"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	_, err := run(t, "-i", ".Sheet1.A1", path)
	if err == nil {
		t.Fatal("Execute with a read-only filter under -i: want error, got nil")
	}
	if !strings.Contains(err.Error(), "--in-place requires an assignment") {
		t.Errorf("error = %q, want it to explain --in-place needs an assignment filter", err.Error())
	}
}

func TestRootInPlaceRejectsNonXLSX(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.csv")
	if err := os.WriteFile(path, []byte("A,B\n1,2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := run(t, "-i", `.Sheet1.A1 = "x"`, path)
	if err == nil {
		t.Fatal("Execute -i against a .csv file: want error, got nil")
	}
	if !strings.Contains(err.Error(), "--in-place only supports .xlsx") {
		t.Errorf("error = %q, want it to explain -i only supports xlsx", err.Error())
	}
}

func TestRootInPlaceRejectsOutputFlags(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "x"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	cases := []string{"--compact", "--compact-output", "--raw-output", "--table", "--csv", "--tsv"}
	for _, flag := range cases {
		t.Run(flag, func(t *testing.T) {
			_, err := run(t, "-i", flag, `.Sheet1.A1 = "x"`, path)
			if err == nil {
				t.Fatalf("Execute -i with %s: want error, got nil", flag)
			}
			if !strings.Contains(err.Error(), "--in-place cannot be combined") {
				t.Errorf("error = %q, want it to explain -i can't combine with output flags", err.Error())
			}
		})
	}
}

func TestRootInPlaceRejectsDelimiter(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "x"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	_, err := run(t, "-i", "--delimiter", ";", `.Sheet1.A1 = "x"`, path)
	if err == nil {
		t.Fatal("Execute -i with --delimiter: want error, got nil")
	}
	if !strings.Contains(err.Error(), "--delimiter") {
		t.Errorf("error = %q, want it to mention --delimiter", err.Error())
	}
}

func TestRootInPlaceTargetMustBeSingleCell(t *testing.T) {
	path := buildFixture(t, func(f *excelize.File) {
		if err := f.SetCellValue("Sheet1", "A1", "x"); err != nil {
			t.Fatalf("SetCellValue: %v", err)
		}
	})

	_, err := run(t, "-i", `.Sheet1[0] = "x"`, path)
	if err == nil {
		t.Fatal("Execute -i with a non-cell target: want error, got nil")
	}
}
