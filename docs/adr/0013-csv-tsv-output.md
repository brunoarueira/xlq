# 13. CSV/TSV output: flattening rules, header row, delimiter

## Status

Accepted

## Context

#32 split `-c`/`-r` (shipped, #46) from `--csv`/`--tsv` specifically
because the latter need a real decision: unlike JSON, which can
represent any of `eval.Eval`'s result shapes directly, CSV/TSV are
flat, single-table formats with no native way to represent a nested
grid, a workbook keyed by sheet name, or a bare scalar. That decision
was deferred further, waiting on header-aware records (#35/#39) to
land first - it has (merged as #39) - so this ADR makes the call.

## Decision

### Flag convention and delimiter

`--csv` and `--tsv` are discrete bool flags, matching the convention
`-c`/`--compact-output` and `-r`/`--raw-output` already established
(#32/#46) over a single `--output-format` enum - and the same
convention `--table` (#13) already followed. All five of
`--table`/`--csv`/`--tsv`/`-c`/`-r` are mutually exclusive: `--table`,
`--csv`, and `--tsv` are different output *modes*, not JSON formatting
tweaks, so combining any two is a CLI error, not a silent override.

The delimiter is fixed - comma for `--csv`, tab for `--tsv` - matching
`internal/csv`'s own reader naming (`Read`/`ReadTSV`). Escaping is
`encoding/csv.Writer`'s own (RFC 4180: a field containing the
delimiter, a quote, or a newline is quoted, with doubled internal
quotes) - not hand-rolled, the same way the reader side already
delegates parsing to `encoding/csv.Reader` rather than a hand-rolled
splitter.

### Flattening, by result shape

| Shape | CSV/TSV output |
| --- | --- |
| `[][]any` (a sheet grid, or a range) | One row per grid row, fields in column order. No header row - the grid has no column names of its own; pipe through `records` first for one. |
| `[]map[string]any` (a records array) | A header row (the sorted union of every record's keys - the same order `--table` and xlq's own JSON output already use, so this doesn't introduce a third convention), then one row per record. |
| `map[string]any`, single record (every value a scalar) | Treated as a one-record array: a header row, then one data row. Keeps a lone record consistent with the records-array case above rather than inventing a third shape (e.g. a 2-column key/value table, which `--table` uses instead specifically because it's meant to be *read*, not re-consumed). |
| `map[string]any`, whole workbook (every value a `[][]any`) | **Error.** A workbook has no single tabular shape - its sheets can have different widths, and nothing says which one the user wants, or that concatenating them makes sense. Query a specific sheet, range, or records result instead. |
| `[]any` (a row or column) | One row per element, each a single-field row - matching `--table`'s own one-column rendering of the same shape, so switching between `--table` and `--csv`/`--tsv` on the same filter doesn't change which shape maps to which layout. |
| a scalar or `nil` | A single row, single field - usable (if trivial) CSV rather than an error, matching `--table`'s own graceful handling of a bare scalar. |

A field's text is the same scalar rendering `--table`'s `cellText`
already uses (a string as-is, a number/bool via its own JSON text, nil
as empty) - minus the tab/newline flattening `cellText` applies for
*terminal alignment* specifically, which CSV doesn't need: a value
with an embedded newline or tab is already handled correctly by
`encoding/csv`'s own quoting, so flattening it here would only
destroy real data for no reason.

### Explicitly deferred

- Writing multiple sheets as separate CSV blocks, or one file per
  sheet. The whole-workbook case is an error, full stop; if a
  multi-sheet CSV export is ever wanted, that's new scope, not this
  ADR's flattening decision stretched to cover it.
- A configurable output delimiter (`--delimiter` only ever applies to
  *input*, per #28). `--csv`/`--tsv` already fix their own delimiter
  to match the format they're named after.

## Consequences

`xlq --csv '.Sheet1' file.xlsx` and `xlq --csv '.Sheet1 | records' file.xlsx`
produce visibly different output for the same sheet - the first a
header-less positional grid, the second header-plus-rows - and that
difference is the point: CSV output doesn't guess whether a sheet
has a meaningful header row the way `records` deliberately doesn't
either (ADR-0011's own row-1 assumption). The user composes `records`
in when they want a header, the same way ranges compose in when they
want to isolate one table first (ADR-0012).

`xlq --csv '.' file.xlsx` is a clear, immediate error rather than an
attempt to guess a reasonable multi-sheet shape - consistent with the
project's standing refusal to guess at structure it can't be sure of
(ADR-0010, the CSV reader's own no-type-inference design, `records`'
own duplicate-header rejection).
