package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
)

// Table renders v - any shape eval.Eval can produce - as a human-
// readable, column-aligned table for terminal use, instead of JSON.
// The returned string has no trailing newline, matching how xlq's own
// JSON encoding is printed (the caller adds one).
//
//   - [][]any (a sheet grid, or a range): one table row per grid row.
//   - []map[string]any (a records array, ADR-0011): one table row per
//     record, headed by the union of every record's keys, sorted -
//     the same alphabetical order xlq's own JSON output already uses
//     for any object's keys, so --table doesn't introduce a second,
//     different ordering convention.
//   - map[string]any: either the whole-workbook result (every value
//     itself a [][]any, keyed by sheet name) or a single record
//     (ADR-0011, every value a scalar). The former renders as one
//     table per sheet, each headed by its name; the latter as a two-
//     column key/value table. Keys are sorted the same way.
//   - []any (a single row or column): a one-column table, one value
//     per line.
//   - anything else (a scalar or nil): printed as cellText would
//     render it inside a table cell - no table structure needed for a
//     single value.
func Table(v any) string {
	switch val := v.(type) {
	case map[string]any:
		return mapTable(val)
	case [][]any:
		return gridTable(val)
	case []map[string]any:
		return recordsTable(val)
	case []any:
		return columnTable(val)
	default:
		return cellText(v)
	}
}

func gridTable(grid [][]any) string {
	return tabwrite(func(w *tabwriter.Writer) {
		for _, row := range grid {
			cells := make([]string, len(row))
			for i, c := range row {
				cells[i] = cellText(c)
			}
			_, _ = fmt.Fprintln(w, strings.Join(cells, "\t"))
		}
	})
}

func recordsTable(records []map[string]any) string {
	if len(records) == 0 {
		return ""
	}
	headers := sortedUnionKeys(records)
	return tabwrite(func(w *tabwriter.Writer) {
		_, _ = fmt.Fprintln(w, strings.Join(headers, "\t"))
		for _, rec := range records {
			cells := make([]string, len(headers))
			for i, h := range headers {
				cells[i] = cellText(rec[h])
			}
			_, _ = fmt.Fprintln(w, strings.Join(cells, "\t"))
		}
	})
}

func columnTable(items []any) string {
	return tabwrite(func(w *tabwriter.Writer) {
		for _, item := range items {
			_, _ = fmt.Fprintln(w, cellText(item))
		}
	})
}

func mapTable(m map[string]any) string {
	if len(m) == 0 {
		return ""
	}
	keys := sortedKeys(m)

	if isWorkbookShaped(m) {
		sections := make([]string, len(keys))
		for i, k := range keys {
			grid, _ := m[k].([][]any)
			sections[i] = fmt.Sprintf("== %s ==\n%s", k, gridTable(grid))
		}
		return strings.Join(sections, "\n\n")
	}

	return tabwrite(func(w *tabwriter.Writer) {
		for _, k := range keys {
			_, _ = fmt.Fprintln(w, k+"\t"+cellText(m[k]))
		}
	})
}

// isWorkbookShaped reports whether every value in m is itself a
// [][]any grid - the shape a whole-workbook result has, as opposed to
// a single record (ADR-0011), whose values are scalars.
func isWorkbookShaped(m map[string]any) bool {
	for _, v := range m {
		if _, ok := v.([][]any); !ok {
			return false
		}
	}
	return len(m) > 0
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedUnionKeys(records []map[string]any) []string {
	seen := make(map[string]bool)
	for _, r := range records {
		for k := range r {
			seen[k] = true
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// tabwrite runs write against a tab-delimited tabwriter.Writer and
// returns the column-aligned result, with no trailing newline. A cell
// is tab-terminated, not tab-separated (tabwriter's own convention,
// matching how write's cells are joined here: joining N cells with
// "\t" only inserts a tab *between* them), so a row's last cell is
// never column-aligned - padding a short last cell to match a wider
// one elsewhere in that column only ever happens via the *next*
// cell's own leading alignment, not trailing space on a short line.
// Any trailing space tabwriter does still produce (aligning a cell
// that isn't a row's last one) is just unused decoration once nothing
// meaningful follows it on that line, so it's trimmed per line here.
func tabwrite(write func(w *tabwriter.Writer)) string {
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	write(w)
	_ = w.Flush()

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

// cellText renders v as it should appear inside one table cell: nil as
// blank, a string as-is, and anything else (a number, a bool) via its
// own JSON text - the same text that value would already render as in
// JSON output. A tab or newline embedded in the value would otherwise
// break the table's alignment, so both are flattened to a single
// space.
func cellText(v any) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		b, err := json.Marshal(v)
		if err != nil {
			s = fmt.Sprintf("%v", v)
		} else {
			s = string(b)
		}
	}
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return s
}
