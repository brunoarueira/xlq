package cli

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xuri/excelize/v2"
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

	got, err := sheetNames(path)
	if err != nil {
		t.Fatalf("sheetNames: %v", err)
	}

	want := []string{"Sheet1", "Data"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sheetNames() = %v, want %v", got, want)
	}
}

func TestSheetNamesMissingFile(t *testing.T) {
	if _, err := sheetNames(filepath.Join(t.TempDir(), "missing.xlsx")); err == nil {
		t.Fatal("sheetNames() with a missing file: want error, got nil")
	}
}
