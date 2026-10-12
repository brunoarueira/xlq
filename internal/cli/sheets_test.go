package cli

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/brunoarueira/xlq/internal/input"
)

func TestSheetNames(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	if _, err := f.NewSheet("Data"); err != nil {
		t.Fatalf("NewSheet: %v", err)
	}

	path := filepath.Join(t.TempDir(), "book.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	got, err := input.SheetNames(path, "", 0)
	if err != nil {
		t.Fatalf("input.SheetNames: %v", err)
	}

	want := []string{"Sheet1", "Data"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("input.SheetNames() = %v, want %v", got, want)
	}
}

func TestSheetNamesMissingFile(t *testing.T) {
	if _, err := input.SheetNames(filepath.Join(t.TempDir(), "missing.xlsx"), "", 0); err == nil {
		t.Fatal("input.SheetNames() with a missing file: want error, got nil")
	}
}
