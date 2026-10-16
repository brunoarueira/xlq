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

	empty := NewEmptyCell()
	if _, ok := empty.StringValue(); ok {
		t.Error("StringValue() on an Empty cell returned ok=true, want false")
	}
	if _, ok := empty.NumberValue(); ok {
		t.Error("NumberValue() on an Empty cell returned ok=true, want false")
	}
	if _, ok := empty.BoolValue(); ok {
		t.Error("BoolValue() on an Empty cell returned ok=true, want false")
	}
	if _, ok := empty.DateValue(); ok {
		t.Error("DateValue() on an Empty cell returned ok=true, want false")
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

// TestCellAccessorRequiresMatchingKind guards against an accessor
// trusting the stored Go type alone: Kind is exported and mutable, so
// an accessor must gate on it rather than just type-asserting the
// private value.
func TestCellAccessorRequiresMatchingKind(t *testing.T) {
	c := NewStringCell("hi")
	c.Kind = Number
	if _, ok := c.StringValue(); ok {
		t.Error("StringValue() with Kind reassigned to Number returned ok=true, want false")
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

// drainRows is CollectRows, failing the test immediately on an error
// instead of returning one (NewSheet's own iterator never actually
// produces one, since ranging over an in-memory slice can't fail -
// this is just the straightforward way to assert its content).
func drainRows(t *testing.T, s Sheet) []Row {
	t.Helper()
	got, err := CollectRows(s)
	if err != nil {
		t.Fatalf("unexpected error ranging over Sheet.Rows: %v", err)
	}
	return got
}

func TestNewSheetEmpty(t *testing.T) {
	s := NewSheet("Sheet1", nil)
	if s.Name != "Sheet1" {
		t.Errorf("Name = %q, want %q", s.Name, "Sheet1")
	}
	if got := drainRows(t, s); got != nil {
		t.Errorf("drainRows() = %v, want nil", got)
	}
}

func TestNewSheetReproducesItsRowsInOrder(t *testing.T) {
	want := []Row{
		{Index: 0, Cells: []Cell{NewStringCell("h").At(0)}},
		{Index: 999, Cells: []Cell{NewNumberCell(42).At(7)}},
	}
	s := NewSheet("Sheet1", want)

	got := drainRows(t, s)
	if len(got) != len(want) {
		t.Fatalf("drainRows() returned %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Index != want[i].Index {
			t.Errorf("row %d: Index = %d, want %d", i, got[i].Index, want[i].Index)
		}
	}
}

// TestNewSheetStopsOnBreak guards the same "an early break must
// actually stop the iterator" contract NewSheet's own doc comment
// relies on - ranging over it and breaking partway through must not
// panic or keep yielding.
func TestNewSheetStopsOnBreak(t *testing.T) {
	s := NewSheet("Sheet1", []Row{
		{Index: 0}, {Index: 1}, {Index: 2},
	})
	var seen []int
	for r, err := range s.Rows {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		seen = append(seen, r.Index)
		if r.Index == 1 {
			break
		}
	}
	if want := []int{0, 1}; len(seen) != len(want) || seen[0] != want[0] || seen[1] != want[1] {
		t.Errorf("seen = %v, want %v", seen, want)
	}
}

func TestWorkbook(t *testing.T) {
	wb := Workbook{
		Sheets: []Sheet{
			NewSheet("Sheet1", []Row{{Index: 0, Cells: []Cell{NewStringCell("a")}}}),
		},
	}
	if len(wb.Sheets) != 1 {
		t.Fatalf("len(wb.Sheets) = %d, want 1", len(wb.Sheets))
	}
	if wb.Sheets[0].Name != "Sheet1" {
		t.Errorf("Sheets[0].Name = %q, want \"Sheet1\"", wb.Sheets[0].Name)
	}
}
