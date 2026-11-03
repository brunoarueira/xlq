# 49. NDJSON output mode: `--ndjson`

## Status

Accepted

## Context

#174: verifying a "load an .xlsx into BigQuery/Postgres from the
shell, no script" pipeline for a Show HN post found a real gap, not a
documentation nit. `bq load --source_format=NEWLINE_DELIMITED_JSON`
(and most other bulk-load tools: Snowflake's `COPY INTO` with a JSON
file format, most log/stream processors) expects NDJSON - one compact
JSON value per line, with no enclosing array and no commas between
lines - and xlq has no output mode that produces it. The closest
existing option, `-c`/`--compact-output`, still wraps a `records`
result in a single-line *array* (`[{"a":1},{"b":2}]`): confirmed
directly, that's one line of valid JSON, not NDJSON, and `bq load`
rejects it as such.

Today's workaround needs an extra dependency and an extra step:

```sh
xlq -c '.Sheet1 | records' report.xlsx | jq -c '.[]' > report.ndjson
```

which is one tool too many for a filter language that already builds
and holds the exact per-record shape NDJSON needs, immediately before
wrapping it in `[...]` for plain JSON output.

## Decision

A new output mode, `--ndjson`, discrete and mutually exclusive with
`--table`/`--csv`/`--tsv` and with `-c`/`-r` (which only ever meant
something for JSON output) - the same convention `--csv`/`--tsv`
already established (ADR-0013), not a fourth bespoke flag shape.

`output.NDJSON(v any) (string, error)` renders v - any shape
`eval.Eval` can produce - as one compact JSON value per line, each
line terminated by `\n` including the last (matching `output.CSV`/
`output.TSV`'s own "every row ends in `\n`, the caller uses `Fprint`
not `Fprintln`" convention - a dangling, unterminated last line would
be the one thing that makes a line-oriented format's own final line
unreadable to a line-splitting consumer). The per-shape mapping
mirrors ADR-0013's own CSV/TSV table exactly, one shape at a time,
rather than inventing a new decision for each:

| Shape | NDJSON output |
| --- | --- |
| `[]map[string]any` (a records array, ADR-0011) | One line per record, each a compact JSON object - no header row: unlike CSV/TSV, each line already names its own fields, which is NDJSON's whole point. An empty records array produces no output at all (the empty string), matching `recordRows`' own "no rows" case. |
| `map[string]any`, single record (ADR-0011, every value a scalar) | One line: the record itself. Treated as the one-record case of the row above, not a separate shape - consistent with `delimitedRows`' own treatment of a lone record as a one-record array - **with one intentional exception**: an empty map (`len(m)==0`) emits one line, `{}`, not nothing. CSV/TSV's own equivalent case emits no rows at all, because a header-less CSV data row can't be represented at all; NDJSON has no such gap - `{}` is a perfectly valid, meaningful encoding of "a record with no fields," so this doesn't inherit CSV's workaround. |
| `map[string]any`, whole workbook (every value a `[][]any`) | **Error.** Same reasoning as CSV/TSV (ADR-0013): a workbook has no single tabular shape, and nothing says which sheet's own shape the caller wanted. |
| `[][]any` (a sheet grid, or a range) | One line per grid row, each a compact JSON array of that row's values - the direct analog of CSV/TSV's own headerless, positional flattening for this shape, just JSON-shaped instead of comma-shaped. |
| `[]any` (a row or column) | One line per element, each that element's own compact JSON value - matching CSV/TSV's own one-field-per-line rendering of the same shape. |
| a scalar or `nil` | A single line: that value's own compact JSON text. |

Unlike `--csv`/`--tsv`/`--table`, NDJSON output does **not** run
`defuseFormula` (ADR-0045) over any value: that defense exists for a
threat specific to CSV/TSV/table text - a spreadsheet application
(Excel, LibreOffice, Google Sheets) reopening the output and
interpreting a leading `=`/`+`/`-`/`@` as a formula. An NDJSON file
isn't something any of those applications parses as cell data in the
first place, the same reasoning ADR-0045 itself already gives for why
plain JSON output is unaffected - NDJSON is JSON-flavored, not
CSV-flavored, for this purpose, and should follow the same rule JSON
already does, not the CSV/TSV/table one.

Encoding itself is `encoding/json.Marshal` per line, the same encoder
every other xlq output path (plain JSON, `-c`, `scalarText`) already
uses - no custom serialization, no attempt at a leaner wire format
than JSON already is.

## Consequences

- `internal/output/ndjson.go` (new), mirroring `csv.go`'s own
  structure (a shape-dispatching `ndjsonLines(v any) ([]string, error)`
  feeding a single join/encode step) but encoding each line as JSON,
  not CSV fields.
- `internal/cli`: `outputOptions` gains an `ndjson bool` field;
  `validate()`'s own output-mode mutual-exclusion group
  (`table`/`csv`/`tsv`, now plus `ndjson`) and the multi-file
  rejection both extend to include it; `root.go` registers `--ndjson`
  and wires it through exactly where `--csv`/`--tsv` already are.
- `internal/config`: `Config` gains an `NDJSON bool` field
  (`yaml:"ndjson"`), merged into `mergeConfigDefaults`'s existing
  `compact-output`/`raw-output`/`table`/`csv`/`tsv` mutual-exclusion
  group (now six members, not five), not a separate one.
- `docs/reference.md`'s own Output modes section gains `--ndjson`
  alongside `--table`/`--csv`/`--tsv` - this Markdown is hand-
  maintained and needs this change, unlike the generated man page
  (ADR-0044: GenManTree's own walk of the Cobra flag tree), which
  picks up `--ndjson`'s help text automatically and needs none.
- The existing `jq -c '.[]'` workaround still works exactly as before
  - this adds a mode, it doesn't deprecate the pipeline that got by
  without it.
