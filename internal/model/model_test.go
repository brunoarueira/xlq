package model

import (
	"testing"
	"time"
)

func TestCellKindString(t *testing.T) {
	cases := map[CellKind]string{
		Empty:        "empty",
		String:       "string",
		Number:       "number",
		Bool:         "bool",
		Date:         "date",
		CellKind(99): "unknown",
	}
	for kind, want := range cases {
		if got := kind.String(); got != want {
			t.Errorf("CellKind(%d).String() = %q, want %q", kind, got, want)
		}
	}
}

func TestCellAccessors(t *testing.T) {
	if v, ok := NewStringCell("hi").StringValue(); !ok || v != "hi" {
		t.Errorf("StringValue() = (%q, %v), want (\"hi\", true)", v, ok)
	}
	if v, ok := NewNumberCell(3.5).NumberValue(); !ok || v != 3.5 {
		t.Errorf("NumberValue() = (%v, %v), want (3.5, true)", v, ok)
	}
	if v, ok := NewBoolCell(true).BoolValue(); !ok || !v {
		t.Errorf("BoolValue() = (%v, %v), want (true, true)", v, ok)
	}
	date := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	if v, ok := NewDateCell(date).DateValue(); !ok || !v.Equal(date) {
		t.Errorf("DateValue() = (%v, %v), want (%v, true)", v, ok, date)
	}
	if v := NewEmptyCell().Value(); v != nil {
		t.Errorf("Value() on empty cell = %v, want nil", v)
	}
}

func TestCellAccessorKindMismatch(t *testing.T) {
	c := NewStringCell("hi")
	if _, ok := c.NumberValue(); ok {
		t.Error("NumberValue() on a String cell returned ok=true, want false")
	}
	if _, ok := c.BoolValue(); ok {
		t.Error("BoolValue() on a String cell returned ok=true, want false")
	}
	if _, ok := c.DateValue(); ok {
		t.Error("DateValue() on a String cell returned ok=true, want false")
	}
}

func TestCellAt(t *testing.T) {
	c := NewNumberCell(1).At(4)
	if c.Column != 4 {
		t.Errorf("Column = %d, want 4", c.Column)
	}
}

func TestCellFormula(t *testing.T) {
	c := NewNumberCell(3).WithFormula("=SUM(A1:A2)")
	if !c.IsFormula() {
		t.Error("IsFormula() = false, want true")
	}
	if c.Formula != "=SUM(A1:A2)" {
		t.Errorf("Formula = %q, want \"=SUM(A1:A2)\"", c.Formula)
	}
	if v, ok := c.NumberValue(); !ok || v != 3 {
		t.Errorf("NumberValue() on formula cell = (%v, %v), want (3, true)", v, ok)
	}

	plain := NewNumberCell(3)
	if plain.IsFormula() {
		t.Error("IsFormula() on a non-formula cell = true, want false")
	}
}

func TestSheetDimensionsEmpty(t *testing.T) {
	var s Sheet
	rows, cols := s.Dimensions()
	if rows != 0 || cols != 0 {
		t.Errorf("Dimensions() = (%d, %d), want (0, 0)", rows, cols)
	}
}

func TestSheetDimensionsSparse(t *testing.T) {
	// A sheet with a single cell far from the origin: dimensions
	// should reflect the bounding box, not a dense allocation.
	s := Sheet{
		Name: "Sheet1",
		Rows: []Row{
			{Index: 0, Cells: []Cell{NewStringCell("h").At(0)}},
			{Index: 999, Cells: []Cell{NewNumberCell(42).At(7)}},
		},
	}
	rows, cols := s.Dimensions()
	if rows != 1000 {
		t.Errorf("rows = %d, want 1000", rows)
	}
	if cols != 8 {
		t.Errorf("cols = %d, want 8", cols)
	}
}

func TestWorkbook(t *testing.T) {
	wb := Workbook{
		Sheets: []Sheet{
			{Name: "Sheet1", Rows: []Row{{Index: 0, Cells: []Cell{NewStringCell("a")}}}},
		},
	}
	if len(wb.Sheets) != 1 {
		t.Fatalf("len(wb.Sheets) = %d, want 1", len(wb.Sheets))
	}
	if wb.Sheets[0].Name != "Sheet1" {
		t.Errorf("Sheets[0].Name = %q, want \"Sheet1\"", wb.Sheets[0].Name)
	}
}
