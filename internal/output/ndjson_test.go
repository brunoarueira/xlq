package output

import "testing"

func TestNDJSONRecords(t *testing.T) {
	in := []map[string]any{
		{"Name": "Alice", "Score": 90.0},
		{"Name": "Bob", "Score": nil},
	}
	// Each line is its own object, keys sorted (encoding/json's own
	// map-key behavior) - no separate header row, unlike CSV/TSV.
	want := "{\"Name\":\"Alice\",\"Score\":90}\n{\"Name\":\"Bob\",\"Score\":null}\n"
	got := mustNDJSON(t, in)
	if got != want {
		t.Errorf("NDJSON(%v) = %q, want %q", in, got, want)
	}
}

func TestNDJSONRecordsDifferentKeysPerLine(t *testing.T) {
	// Unlike CSV/TSV, NDJSON needs no union of keys across records -
	// each line is independently valid JSON with only its own fields.
	in := []map[string]any{
		{"A": "1"},
		{"B": "2"},
	}
	want := "{\"A\":\"1\"}\n{\"B\":\"2\"}\n"
	got := mustNDJSON(t, in)
	if got != want {
		t.Errorf("NDJSON(%v) = %q, want %q", in, got, want)
	}
}

func TestNDJSONRecordsEmpty(t *testing.T) {
	got := mustNDJSON(t, []map[string]any{})
	if got != "" {
		t.Errorf("NDJSON(empty records) = %q, want %q", got, "")
	}
}

func TestNDJSONSingleRecordIsOneLine(t *testing.T) {
	in := map[string]any{"Name": "Alice", "Score": 90.0}
	want := "{\"Name\":\"Alice\",\"Score\":90}\n"
	got := mustNDJSON(t, in)
	if got != want {
		t.Errorf("NDJSON(%v) = %q, want %q", in, got, want)
	}
}

func TestNDJSONWholeWorkbookIsError(t *testing.T) {
	in := map[string]any{
		"Sheet1": [][]any{{"a", 1.0}},
		"Data":   [][]any{{"b", 2.0}},
	}
	if _, err := NDJSON(in); err == nil {
		t.Error("NDJSON(whole workbook): want error, got nil")
	}
}

// TestNDJSONMapEmpty intentionally diverges from CSV's own
// TestCSVMapEmpty: CSV can't represent "a data row with zero
// columns" at all (there's no header to put a comma between), so it
// renders nothing; NDJSON's single-record case is still exactly one
// line, since "{}" is a perfectly valid, meaningful encoding of "a
// record with no fields" - JSON has no analogous representational
// gap to route around.
func TestNDJSONMapEmpty(t *testing.T) {
	got := mustNDJSON(t, map[string]any{})
	if got != "{}\n" {
		t.Errorf("NDJSON(empty map) = %q, want %q", got, "{}\n")
	}
}

func TestNDJSONGrid(t *testing.T) {
	in := [][]any{
		{"Alice", 90.0},
		{"Bob", nil},
	}
	want := "[\"Alice\",90]\n[\"Bob\",null]\n"
	got := mustNDJSON(t, in)
	if got != want {
		t.Errorf("NDJSON(%v) = %q, want %q", in, got, want)
	}
}

func TestNDJSONGridEmpty(t *testing.T) {
	got := mustNDJSON(t, [][]any{})
	if got != "" {
		t.Errorf("NDJSON(empty grid) = %q, want %q", got, "")
	}
}

func TestNDJSONColumn(t *testing.T) {
	in := []any{"a", 1.0, nil, true}
	want := "\"a\"\n1\nnull\ntrue\n"
	got := mustNDJSON(t, in)
	if got != want {
		t.Errorf("NDJSON(%v) = %q, want %q", in, got, want)
	}
}

func TestNDJSONColumnEmpty(t *testing.T) {
	got := mustNDJSON(t, []any{})
	if got != "" {
		t.Errorf("NDJSON(empty column) = %q, want %q", got, "")
	}
}

func TestNDJSONScalar(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"hello", "\"hello\"\n"},
		{42.0, "42\n"},
		{true, "true\n"},
		{nil, "null\n"},
	}
	for _, tc := range cases {
		got := mustNDJSON(t, tc.in)
		if got != tc.want {
			t.Errorf("NDJSON(%#v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestNDJSONDoesNotDefuseFormula confirms NDJSON, unlike CSV/TSV/
// Table, leaves a leading =/+/-/@ untouched - that defense (ADR-0045)
// is specific to a spreadsheet application reopening CSV/TSV/table
// text, a threat that doesn't apply to an NDJSON file, the same
// reasoning ADR-0045 already gives for plain JSON output.
func TestNDJSONDoesNotDefuseFormula(t *testing.T) {
	records := []map[string]any{{"header": "=cmd()"}}
	want := "{\"header\":\"=cmd()\"}\n"
	got := mustNDJSON(t, records)
	if got != want {
		t.Errorf("NDJSON(%v) = %q, want %q (not defused)", records, got, want)
	}
}

func mustNDJSON(t *testing.T, v any) string {
	t.Helper()
	got, err := NDJSON(v)
	if err != nil {
		t.Fatalf("NDJSON(%v) returned error: %v", v, err)
	}
	return got
}
