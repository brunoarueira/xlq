package output

import (
	"encoding/json"
	"fmt"
	"strings"
)

// NDJSON renders v - any shape eval.Eval can produce - as newline-
// delimited JSON: one compact JSON value per line, with no enclosing
// array and no commas between lines (docs/adr/0049-ndjson-output-mode.md).
// Every line, including the last, ends in "\n" - the same "every row
// ends in \n, the caller uses Fprint not Fprintln" convention CSV/TSV
// already follow (csv.go): a dangling, unterminated last line would be
// the one thing that makes a line-oriented format's own final line
// unreadable to a line-splitting consumer (e.g. bq load's own NDJSON
// reader). The per-shape mapping mirrors ADR-0013's own CSV/TSV table
// exactly, one shape at a time:
//
//   - []map[string]any (a records array, ADR-0011): one line per
//     record, each a compact JSON object - no header row, since each
//     line already names its own fields. An empty records array
//     produces no output at all.
//   - map[string]any, a single record (every value a scalar): one
//     line, the record itself - the one-record case of the shape
//     above, not a separate one.
//   - map[string]any, the whole-workbook result (every value a
//     [][]any): an error. No single tabular shape, matching CSV/TSV.
//   - [][]any (a sheet grid, or a range): one line per grid row, each
//     a compact JSON array of that row's values.
//   - []any (a row or column): one line per element, each that
//     element's own compact JSON value.
//   - a scalar or nil: a single line, that value's own compact JSON
//     text.
//
// Unlike CSV/TSV/Table, this does not run defuseFormula (ADR-0045)
// over any value: that defense is specific to a spreadsheet
// application reopening CSV/TSV/table text and misreading a leading
// =/+/-/@ as a formula, a threat that doesn't apply to an NDJSON
// file - the same reasoning ADR-0045 already gives for why plain
// JSON output is unaffected. NDJSON is JSON-flavored for this
// purpose, not CSV-flavored.
func NDJSON(v any) (string, error) {
	lines, err := ndjsonLines(v)
	if err != nil {
		return "", err
	}
	if len(lines) == 0 {
		return "", nil
	}
	var sb strings.Builder
	for _, line := range lines {
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

func ndjsonLines(v any) ([]string, error) {
	switch val := v.(type) {
	case map[string]any:
		if isWorkbookShaped(val) {
			return nil, fmt.Errorf("output: a whole workbook has no single tabular shape for ndjson output; query a specific sheet, range, or records result instead")
		}
		return recordNDJSONLines([]map[string]any{val})
	case [][]any:
		return gridNDJSONLines(val)
	case []map[string]any:
		return recordNDJSONLines(val)
	case []any:
		return columnNDJSONLines(val)
	default:
		line, err := jsonLine(v)
		if err != nil {
			return nil, err
		}
		return []string{line}, nil
	}
}

func jsonLine(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("output: encode line: %w", err)
	}
	return string(b), nil
}

func recordNDJSONLines(records []map[string]any) ([]string, error) {
	lines := make([]string, len(records))
	for i, rec := range records {
		line, err := jsonLine(rec)
		if err != nil {
			return nil, err
		}
		lines[i] = line
	}
	return lines, nil
}

func gridNDJSONLines(grid [][]any) ([]string, error) {
	lines := make([]string, len(grid))
	for i, row := range grid {
		line, err := jsonLine(row)
		if err != nil {
			return nil, err
		}
		lines[i] = line
	}
	return lines, nil
}

func columnNDJSONLines(items []any) ([]string, error) {
	lines := make([]string, len(items))
	for i, item := range items {
		line, err := jsonLine(item)
		if err != nil {
			return nil, err
		}
		lines[i] = line
	}
	return lines, nil
}
