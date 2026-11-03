package output

import (
	"strings"
	"testing"
)

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

// TestCSVDefusesFormulaEverywhere confirms CSV applies defuseFormula
// to every field it renders - a data value, a records header, or a
// single-record map's own key alike (ADR-0045) - but never to a
// genuine negative number, the same scope and exemption Table's own
// test confirms.
func TestCSVDefusesFormulaEverywhere(t *testing.T) {
	grid := [][]any{{"=A1", "+B1", -12.5}}
	if got, want := mustCSV(t, grid), "'=A1,'+B1,-12.5\n"; got != want {
		t.Errorf("CSV(grid) = %q, want %q", got, want)
	}

	records := []map[string]any{{"=header": "@value"}}
	if got, want := mustCSV(t, records), "'=header\n'@value\n"; got != want {
		t.Errorf("CSV(records) = %q, want %q (header and value both defused)", got, want)
	}

	column := []any{"-A1"}
	if got, want := mustCSV(t, column), "'-A1\n"; got != want {
		t.Errorf("CSV(column) = %q, want %q", got, want)
	}

	scalar := "=A1"
	if got, want := mustCSV(t, scalar), "'=A1\n"; got != want {
		t.Errorf("CSV(scalar) = %q, want %q", got, want)
	}

	// The map[string]any path (a single record, ADR-0011) is a
	// separate branch from []map[string]any above - not exercised by
	// any of the other cases here, so a regression specific to it
	// wouldn't otherwise be caught.
	m := map[string]any{"=key": "=value"}
	if got, want := mustCSV(t, m), "'=key\n'=value\n"; got != want {
		t.Errorf("CSV(single record) = %q, want %q (key and value both defused)", got, want)
	}
}

// TestCSVDefusedHeadersStayDistinct guards the collision defuseFormula
// must not introduce: two records whose real header names are "=x"
// and "'=x" - distinct before escaping - must still render as two
// distinct header columns after escaping, not both collapse onto the
// same "'=x" text. Confirmed directly that without doubling an
// existing leading quote, they did collapse - the underlying
// rec[h] lookup stayed correct either way, but the output's own
// header row became ambiguous.
func TestCSVDefusedHeadersStayDistinct(t *testing.T) {
	records := []map[string]any{
		{"=x": "v1", "'=x": "v2"},
	}
	got := mustCSV(t, records)
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) == 0 {
		t.Fatalf("CSV(%v) = %q, want at least a header row", records, got)
	}
	headers := strings.Split(lines[0], ",")
	if len(headers) != 2 || headers[0] == headers[1] {
		t.Errorf("CSV(%v) header row = %q, want two distinct columns, got %v", records, lines[0], headers)
	}
}

func mustCSV(t *testing.T, v any) string {
	t.Helper()
	got, err := CSV(v)
	if err != nil {
		t.Fatalf("CSV(%v) returned error: %v", v, err)
	}
	return got
}

// TestCSVOrderedUsesGivenOrderNotSorted guards #175's own fix:
// headerOrder, when given, wins over the sorted-union default.
// headerOrder here is "Name" before "Likes" - deliberately not
// alphabetical ("Likes" < "Name") - so a bug that silently fell back
// to sorting would be caught by the output's own column order, not
// masked by headerOrder happening to already match the sorted one.
func TestCSVOrderedUsesGivenOrderNotSorted(t *testing.T) {
	records := []map[string]any{
		{"Name": "Alice", "Likes": 150.0},
	}
	got, err := CSVOrdered(records, []string{"Name", "Likes"})
	if err != nil {
		t.Fatalf("CSVOrdered returned error: %v", err)
	}
	want := "Name,Likes\nAlice,150\n"
	if got != want {
		t.Errorf("CSVOrdered(%v) = %q, want %q", records, got, want)
	}
}

// TestCSVOrderedNilMatchesPlainCSV confirms CSVOrdered(v, nil)
// behaves exactly like CSV(v) - the "no hint available" case must
// stay the existing, already-tested sorted behavior.
func TestCSVOrderedNilMatchesPlainCSV(t *testing.T) {
	records := []map[string]any{{"B": 1.0, "A": 2.0}}
	plain := mustCSV(t, records)
	ordered, err := CSVOrdered(records, nil)
	if err != nil {
		t.Fatalf("CSVOrdered returned error: %v", err)
	}
	if ordered != plain {
		t.Errorf("CSVOrdered(v, nil) = %q, want %q (same as CSV(v))", ordered, plain)
	}
}

// TestCSVOrderedSingleRecord confirms headerOrder also applies to the
// map[string]any (single-record) path, not just []map[string]any.
func TestCSVOrderedSingleRecord(t *testing.T) {
	record := map[string]any{"Name": "Alice", "Likes": 150.0}
	got, err := CSVOrdered(record, []string{"Name", "Likes"})
	if err != nil {
		t.Fatalf("CSVOrdered returned error: %v", err)
	}
	want := "Name,Likes\nAlice,150\n"
	if got != want {
		t.Errorf("CSVOrdered(%v) = %q, want %q", record, got, want)
	}
}

func TestTSVOrderedUsesGivenOrder(t *testing.T) {
	records := []map[string]any{{"Name": "Alice", "Likes": 150.0}}
	got, err := TSVOrdered(records, []string{"Name", "Likes"})
	if err != nil {
		t.Fatalf("TSVOrdered returned error: %v", err)
	}
	want := "Name\tLikes\nAlice\t150\n"
	if got != want {
		t.Errorf("TSVOrdered(%v) = %q, want %q", records, got, want)
	}
}

// TestCSVOrderedDefusesHeaderOrder confirms a header name coming
// from headerOrder is defuseFormula'd (ADR-0045) exactly like a
// sorted header already is - the formula-injection defense can't
// have a gap just because the header came from a different source.
func TestCSVOrderedDefusesHeaderOrder(t *testing.T) {
	records := []map[string]any{{"=header": "value"}}
	got, err := CSVOrdered(records, []string{"=header"})
	if err != nil {
		t.Fatalf("CSVOrdered returned error: %v", err)
	}
	want := "'=header\nvalue\n"
	if got != want {
		t.Errorf("CSVOrdered(%v) = %q, want %q", records, got, want)
	}
}
