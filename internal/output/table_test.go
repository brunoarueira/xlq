package output

import "testing"

func TestTableGrid(t *testing.T) {
	in := [][]any{
		{"Name", "Score"},
		{"Alice", 90.0},
		{"Bob", nil},
	}
	// "Bob" ends with trailing spaces: it's tab-terminated (there's a
	// second, blank cell after it in that row), so tabwriter pads it to
	// match "Alice"'s column width - genuine alignment padding, not
	// something to trim away (see TestTableDoesNotStripGenuineTrailingSpace).
	want := "Name   Score\n" +
		"Alice  90\n" +
		"Bob    "
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
	// Keys sort alphabetically: Name before Score. "Bob" has trailing
	// padding spaces for the same reason as TestTableGrid's.
	want := "Name   Score\n" +
		"Alice  90\n" +
		"Bob    "
	if got := Table(in); got != want {
		t.Errorf("Table(%v) = %q, want %q", in, got, want)
	}
}

func TestTableRecordsUnionsKeysAcrossRecords(t *testing.T) {
	// Not all records share every key - the header is the union,
	// sorted, and a record missing a key renders that cell blank. "1"
	// has trailing padding (tab-terminated, column B's blank cell
	// follows it), but "2" doesn't need any (it's the last column).
	in := []map[string]any{
		{"A": "1"},
		{"B": "2"},
	}
	want := "A  B\n" +
		"1  \n" +
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

// TestTableGridPreservesTrailingBlankRow confirms a grid's own
// trailing blank row still appears as a blank line - stripping every
// trailing newline rather than only the one Flush itself appends
// would collapse it into nothing, printing a range ending in a blank
// row as though that row had never been selected.
func TestTableGridPreservesTrailingBlankRow(t *testing.T) {
	in := [][]any{{"a"}, {}}
	want := "a\n"
	if got := Table(in); got != want {
		t.Errorf("Table(%v) = %q, want %q", in, got, want)
	}
}

// TestTableDoesNotStripGenuineTrailingSpace confirms a cell's own
// trailing space survives even when it's a row's last cell - never
// tabwriter-aligned (tab-terminated, not tab-separated - see
// tabwrite's own comment), so it must not be mistaken for alignment
// padding and trimmed away.
func TestTableDoesNotStripGenuineTrailingSpace(t *testing.T) {
	in := [][]any{
		{"a", "hello "},
		{"bb", "x"},
	}
	want := "a   hello \nbb  x"
	if got := Table(in); got != want {
		t.Errorf("Table(%v) = %q, want %q", in, got, want)
	}
}

// TestTableFlattensHeaderAndKeyTabsAndNewlines confirms a records
// header (ADR-0011's own header-cell text) or a map key is flattened
// the same way cellText already flattens an ordinary data cell -
// either can legitimately contain a tab or newline, which would
// otherwise break the table's alignment just the same.
func TestTableFlattensHeaderAndKeyTabsAndNewlines(t *testing.T) {
	records := []map[string]any{{"a\tb": "1"}}
	if got, want := Table(records), "a b\n1"; got != want {
		t.Errorf("Table(%v) = %q, want %q", records, got, want)
	}

	m := map[string]any{"a\nb": "1"}
	if got, want := Table(m), "a b  1"; got != want {
		t.Errorf("Table(%v) = %q, want %q", m, got, want)
	}
}
