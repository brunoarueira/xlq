package output

import "testing"

func TestCSVGrid(t *testing.T) {
	in := [][]any{
		{"Name", "Score"},
		{"Alice", 90.0},
		{"Bob", nil},
	}
	want := "Name,Score\nAlice,90\nBob,\n"
	got, err := CSV(in)
	if err != nil {
		t.Fatalf("CSV(%v) returned error: %v", in, err)
	}
	if got != want {
		t.Errorf("CSV(%v) = %q, want %q", in, got, want)
	}
}

func TestCSVGridEmpty(t *testing.T) {
	got, err := CSV([][]any{})
	if err != nil {
		t.Fatalf("CSV(empty grid) returned error: %v", err)
	}
	if got != "" {
		t.Errorf("CSV(empty grid) = %q, want %q", got, "")
	}
}

func TestCSVRecords(t *testing.T) {
	in := []map[string]any{
		{"Name": "Alice", "Score": 90.0},
		{"Name": "Bob", "Score": nil},
	}
	// Keys sort alphabetically: Name before Score.
	want := "Name,Score\nAlice,90\nBob,\n"
	got, err := CSV(in)
	if err != nil {
		t.Fatalf("CSV(%v) returned error: %v", in, err)
	}
	if got != want {
		t.Errorf("CSV(%v) = %q, want %q", in, got, want)
	}
}

func TestCSVRecordsUnionsKeysAcrossRecords(t *testing.T) {
	in := []map[string]any{
		{"A": "1"},
		{"B": "2"},
	}
	want := "A,B\n1,\n,2\n"
	got, err := CSV(in)
	if err != nil {
		t.Fatalf("CSV(%v) returned error: %v", in, err)
	}
	if got != want {
		t.Errorf("CSV(%v) = %q, want %q", in, got, want)
	}
}

func TestCSVRecordsEmpty(t *testing.T) {
	got, err := CSV([]map[string]any{})
	if err != nil {
		t.Fatalf("CSV(empty records) returned error: %v", err)
	}
	if got != "" {
		t.Errorf("CSV(empty records) = %q, want %q", got, "")
	}
}

func TestCSVSingleRecordIsHeaderPlusOneRow(t *testing.T) {
	// A single record (e.g. "records | .[0]") is treated as a
	// one-record array, not a 2-column key/value table.
	in := map[string]any{"Name": "Alice", "Score": 90.0}
	want := "Name,Score\nAlice,90\n"
	got, err := CSV(in)
	if err != nil {
		t.Fatalf("CSV(%v) returned error: %v", in, err)
	}
	if got != want {
		t.Errorf("CSV(%v) = %q, want %q", in, got, want)
	}
}

func TestCSVWholeWorkbookIsError(t *testing.T) {
	in := map[string]any{
		"Sheet1": [][]any{{"a", 1.0}},
		"Data":   [][]any{{"b", 2.0}},
	}
	if _, err := CSV(in); err == nil {
		t.Error("CSV(whole workbook): want error, got nil")
	}
}

func TestCSVMapEmpty(t *testing.T) {
	got, err := CSV(map[string]any{})
	if err != nil {
		t.Fatalf("CSV(empty map) returned error: %v", err)
	}
	if got != "" {
		t.Errorf("CSV(empty map) = %q, want %q", got, "")
	}
}

func TestCSVColumn(t *testing.T) {
	in := []any{"a", 1.0, nil, true}
	want := "a\n1\n\ntrue\n"
	got, err := CSV(in)
	if err != nil {
		t.Fatalf("CSV(%v) returned error: %v", in, err)
	}
	if got != want {
		t.Errorf("CSV(%v) = %q, want %q", in, got, want)
	}
}

func TestCSVColumnEmpty(t *testing.T) {
	got, err := CSV([]any{})
	if err != nil {
		t.Fatalf("CSV(empty column) returned error: %v", err)
	}
	if got != "" {
		t.Errorf("CSV(empty column) = %q, want %q", got, "")
	}
}

func TestCSVScalar(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"hello", "hello\n"},
		{42.0, "42\n"},
		{true, "true\n"},
		{nil, "\n"},
	}
	for _, tc := range cases {
		got, err := CSV(tc.in)
		if err != nil {
			t.Fatalf("CSV(%#v) returned error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("CSV(%#v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestCSVPreservesEmbeddedTabsAndNewlines confirms CSV output doesn't
// flatten a cell's embedded tab or newline the way Table does - csv's
// own quoting already represents either correctly, so flattening here
// would only destroy real data.
func TestCSVPreservesEmbeddedTabsAndNewlines(t *testing.T) {
	in := [][]any{{"a\tb", "c\nd"}}
	// A tab isn't special to a comma-delimited writer, so that field
	// isn't quoted; the embedded newline is (encoding/csv's own rule).
	want := "a\tb,\"c\nd\"\n"
	got, err := CSV(in)
	if err != nil {
		t.Fatalf("CSV(%v) returned error: %v", in, err)
	}
	if got != want {
		t.Errorf("CSV(%v) = %q, want %q", in, got, want)
	}
}

func TestCSVQuotesFieldsContainingTheDelimiter(t *testing.T) {
	in := [][]any{{"a,b", "c"}}
	want := "\"a,b\",c\n"
	got, err := CSV(in)
	if err != nil {
		t.Fatalf("CSV(%v) returned error: %v", in, err)
	}
	if got != want {
		t.Errorf("CSV(%v) = %q, want %q", in, got, want)
	}
}

func TestTSVUsesTabDelimiter(t *testing.T) {
	in := [][]any{{"a", "b"}}
	want := "a\tb\n"
	got, err := TSV(in)
	if err != nil {
		t.Fatalf("TSV(%v) returned error: %v", in, err)
	}
	if got != want {
		t.Errorf("TSV(%v) = %q, want %q", in, got, want)
	}
}

func TestTSVWholeWorkbookIsError(t *testing.T) {
	in := map[string]any{"Sheet1": [][]any{{"a"}}}
	if _, err := TSV(in); err == nil {
		t.Error("TSV(whole workbook): want error, got nil")
	}
}
