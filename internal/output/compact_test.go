package output

import (
	"reflect"
	"testing"
)

func TestCompactGridDropsAllNilRow(t *testing.T) {
	in := [][]any{
		{"a", 1.0},
		{nil, nil},
		{"b", 2.0},
	}
	want := [][]any{
		{"a", 1.0},
		{"b", 2.0},
	}
	if got := Compact(in); !reflect.DeepEqual(got, want) {
		t.Errorf("Compact(%v) = %v, want %v", in, got, want)
	}
}

func TestCompactGridDropsAllNilColumn(t *testing.T) {
	in := [][]any{
		{"a", nil, 1.0},
		{"b", nil, 2.0},
	}
	want := [][]any{
		{"a", 1.0},
		{"b", 2.0},
	}
	if got := Compact(in); !reflect.DeepEqual(got, want) {
		t.Errorf("Compact(%v) = %v, want %v", in, got, want)
	}
}

func TestCompactGridDropsRowAndColumnTogether(t *testing.T) {
	in := [][]any{
		{nil, nil},
		{nil, "x"},
	}
	want := [][]any{
		{"x"},
	}
	if got := Compact(in); !reflect.DeepEqual(got, want) {
		t.Errorf("Compact(%v) = %v, want %v", in, got, want)
	}
}

func TestCompactGridAllNilBecomesEmpty(t *testing.T) {
	in := [][]any{
		{nil, nil},
		{nil, nil},
	}
	got := Compact(in).([][]any)
	if len(got) != 0 {
		t.Errorf("Compact(all-nil grid) = %v, want an empty grid", got)
	}
}

func TestCompactSliceDropsNilEntries(t *testing.T) {
	in := []any{"a", nil, "b", nil}
	want := []any{"a", "b"}
	if got := Compact(in); !reflect.DeepEqual(got, want) {
		t.Errorf("Compact(%v) = %v, want %v", in, got, want)
	}
}

func TestCompactWorkbookRecursesIntoEachSheet(t *testing.T) {
	in := map[string]any{
		"Sheet1": [][]any{{"a", nil}, {nil, nil}},
	}
	want := map[string]any{
		"Sheet1": [][]any{{"a"}},
	}
	if got := Compact(in); !reflect.DeepEqual(got, want) {
		t.Errorf("Compact(%v) = %v, want %v", in, got, want)
	}
}

func TestCompactScalarAndNilUnchanged(t *testing.T) {
	if got := Compact("hello"); got != "hello" {
		t.Errorf("Compact(\"hello\") = %v, want \"hello\"", got)
	}
	if got := Compact(nil); got != nil {
		t.Errorf("Compact(nil) = %v, want nil", got)
	}
	if got := Compact(42.0); got != 42.0 {
		t.Errorf("Compact(42.0) = %v, want 42.0", got)
	}
}
