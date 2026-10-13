package output

import "testing"

func TestTableGrid(t *testing.T) {
	in := [][]any{
		{"Name", "Score"},
		{"Alice", 90.0},
		{"Bob", nil},
	}
	want := "Name   Score\n" +
		"Alice  90\n" +
		"Bob"
	if got := Table(in); got != want {
		t.Errorf("Table(%v) = %q, want %q", in, got, want)
	}
}

func TestTableGridEmpty(t *testing.T) {
	if got := Table([][]any{}); got != "" {
		t.Errorf("Table(empty grid) = %q, want %q", got, "")
	}
}

func TestTableRecords(t *testing.T) {
	in := []map[string]any{
		{"Name": "Alice", "Score": 90.0},
		{"Name": "Bob", "Score": nil},
	}
	// Keys sort alphabetically: Name before Score.
	want := "Name   Score\n" +
		"Alice  90\n" +
		"Bob"
	if got := Table(in); got != want {
		t.Errorf("Table(%v) = %q, want %q", in, got, want)
	}
}

func TestTableRecordsUnionsKeysAcrossRecords(t *testing.T) {
	// Not all records share every key - the header is the union,
	// sorted, and a record missing a key renders that cell blank.
	in := []map[string]any{
		{"A": "1"},
		{"B": "2"},
	}
	want := "A  B\n" +
		"1\n" +
		"   2"
	if got := Table(in); got != want {
		t.Errorf("Table(%v) = %q, want %q", in, got, want)
	}
}

func TestTableRecordsEmpty(t *testing.T) {
	if got := Table([]map[string]any{}); got != "" {
		t.Errorf("Table(empty records) = %q, want %q", got, "")
	}
}

func TestTableSingleRecord(t *testing.T) {
	// A single record (e.g. "records | .[0]") is a map[string]any whose
	// values are scalars - a two-column key/value table, not a
	// one-sheet-per-table rendering.
	in := map[string]any{"Name": "Alice", "Score": 90.0}
	want := "Name   Alice\n" +
		"Score  90"
	if got := Table(in); got != want {
		t.Errorf("Table(%v) = %q, want %q", in, got, want)
	}
}

func TestTableWholeWorkbook(t *testing.T) {
	// The whole-workbook result: every value is itself a [][]any grid,
	// keyed by sheet name - rendered as one table per sheet.
	in := map[string]any{
		"Sheet1": [][]any{{"a", 1.0}},
		"Data":   [][]any{{"b", 2.0}},
	}
	want := "== Data ==\n" +
		"b  2\n" +
		"\n" +
		"== Sheet1 ==\n" +
		"a  1"
	if got := Table(in); got != want {
		t.Errorf("Table(%v) = %q, want %q", in, got, want)
	}
}

func TestTableMapEmpty(t *testing.T) {
	if got := Table(map[string]any{}); got != "" {
		t.Errorf("Table(empty map) = %q, want %q", got, "")
	}
}

func TestTableColumn(t *testing.T) {
	in := []any{"a", 1.0, nil, true}
	want := "a\n1\n\ntrue"
	if got := Table(in); got != want {
		t.Errorf("Table(%v) = %q, want %q", in, got, want)
	}
}

func TestTableColumnEmpty(t *testing.T) {
	if got := Table([]any{}); got != "" {
		t.Errorf("Table(empty column) = %q, want %q", got, "")
	}
}

func TestTableScalar(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"hello", "hello"},
		{42.0, "42"},
		{true, "true"},
		{nil, ""},
	}
	for _, tc := range cases {
		if got := Table(tc.in); got != tc.want {
			t.Errorf("Table(%#v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTableFlattensEmbeddedTabsAndNewlines(t *testing.T) {
	// A tab or newline embedded in a cell's own text would otherwise
	// break the table's column alignment.
	in := [][]any{
		{"a\tb", "c\nd"},
	}
	want := "a b  c d"
	if got := Table(in); got != want {
		t.Errorf("Table(%v) = %q, want %q", in, got, want)
	}
}
