package output

import (
	"bytes"
	stdcsv "encoding/csv"
	"fmt"
)

// CSV renders v - any shape eval.Eval can produce - as comma-delimited
// CSV text (RFC 4180, via encoding/csv.Writer - quoting and escaping
// are that package's own, not hand-rolled). TSV is identical, tab-
// delimited instead; both follow the flattening rules ADR-0013
// decided:
//
//   - [][]any (a sheet grid, or a range): one row per grid row, in
//     column order. No header row - the grid has no column names of
//     its own; pipe through records first for one.
//   - []map[string]any (a records array, ADR-0011): a header row (the
//     sorted union of every record's keys, matching Table's own
//     ordering), then one row per record.
//   - map[string]any: a single record (ADR-0011, every value a
//     scalar) is treated as a one-record array - a header row, then
//     one data row - rather than a separate key/value shape. The
//     whole-workbook result (every value itself a [][]any, keyed by
//     sheet name) is an error: a workbook has no single tabular shape.
//   - []any (a row or column): one row per element, each a single-
//     field row - matching Table's own one-column rendering of the
//     same shape.
//   - a scalar or nil: a single row, single field.
//
// Unlike Table, a field's text is scalarText's, not cellText's: a tab
// or embedded newline doesn't need flattening here, since
// encoding/csv's own quoting already represents either correctly.
func CSV(v any) (string, error) {
	return delimited(v, ',')
}

// TSV is CSV, tab-delimited instead of comma-delimited.
func TSV(v any) (string, error) {
	return delimited(v, '\t')
}

func delimited(v any, delim rune) (string, error) {
	rows, err := delimitedRows(v)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	w := stdcsv.NewWriter(&buf)
	w.Comma = delim
	for _, row := range rows {
		if err := w.Write(row); err != nil {
			return "", fmt.Errorf("output: encode row: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", fmt.Errorf("output: encode: %w", err)
	}
	return buf.String(), nil
}

func delimitedRows(v any) ([][]string, error) {
	switch val := v.(type) {
	case map[string]any:
		if isWorkbookShaped(val) {
			return nil, fmt.Errorf("output: a whole workbook has no single tabular shape for csv/tsv output; query a specific sheet, range, or records result instead")
		}
		return recordRows([]map[string]any{val}), nil
	case [][]any:
		return gridRows(val), nil
	case []map[string]any:
		return recordRows(val), nil
	case []any:
		return columnRows(val), nil
	default:
		return [][]string{{scalarText(v)}}, nil
	}
}

func gridRows(grid [][]any) [][]string {
	rows := make([][]string, len(grid))
	for i, row := range grid {
		fields := make([]string, len(row))
		for j, c := range row {
			fields[j] = scalarText(c)
		}
		rows[i] = fields
	}
	return rows
}

// recordRows returns a header row (the sorted union of every record's
// keys) followed by one row per record - or no rows at all for an
// empty records slice, including a single "empty record" (len(m)==0),
// the same degenerate case Table's mapTable already treats this way.
func recordRows(records []map[string]any) [][]string {
	if len(records) == 0 {
		return nil
	}
	headers := sortedUnionKeys(records)
	if len(headers) == 0 {
		return nil
	}

	rows := make([][]string, 0, len(records)+1)
	rows = append(rows, headers)
	for _, rec := range records {
		fields := make([]string, len(headers))
		for i, h := range headers {
			fields[i] = scalarText(rec[h])
		}
		rows = append(rows, fields)
	}
	return rows
}

func columnRows(items []any) [][]string {
	rows := make([][]string, len(items))
	for i, item := range items {
		rows[i] = []string{scalarText(item)}
	}
	return rows
}
