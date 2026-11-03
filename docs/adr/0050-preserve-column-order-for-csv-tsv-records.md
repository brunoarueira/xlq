# 50. Preserve a sheet's own column order for `--csv`/`--tsv` records output

## Status

Accepted

## Context

#175: verifying the exact "load an `.xlsx` into Postgres via `psql
\copy`" pipeline a Show HN post was about to advertise found real,
silent friction. `COPY ... FROM STDIN CSV HEADER` (and most other
bulk-load tools' own CSV path) matches columns *positionally* against
the destination table - `HEADER` only tells it to skip the first
line, it never maps columns by name. `xlq --csv '.Sheet1 | records'`
orders its header row alphabetically by field name, not in the
sheet's own left-to-right column order - confirmed directly: a
destination table declared in the sheet's own natural order (`name
text, likes int`) silently rejected (or, worse, with different column
types, silently *misfiled*) the load until the table was redeclared
to match xlq's own alphabetical order instead, something no user
would discover until a load already put data in the wrong columns.

This is a known, deliberate tradeoff, not a bug introduced here -
ADR-0011 explicitly calls it out: a `map[string]any`'s keys come out
alphabetically once JSON-encoded (Go's own encoder doesn't preserve
map insertion order), and introducing an ordered-object type to
recover the original order was judged "more machinery than the
problem has earned" at the time, matching the same call ADR-0008
already made for the whole-workbook object (keyed by sheet name).
That reasoning still holds for JSON and `--table` output, where key
order is either genuinely immaterial (JSON) or already deliberately
kept consistent with JSON's own convention (`--table`, ADR-0013's own
stated reason for sorting there too). It doesn't hold for `--csv`/
`--tsv` specifically: a positional consumer has nothing else to go on,
and "the sheet's own left-to-right order" is both the more intuitive
default and the one that needs zero column-order wrangling to load
cleanly.

## Decision

`--csv`/`--tsv` records output uses the sheet's own real column
order, not a sorted union of keys - `--table` and plain JSON output
are unaffected, still sorting as before.

The order itself was never actually lost at the point ADR-0011
worried about (a `map[string]any` already flattened for JSON) - it's
available earlier, and already *tracked* internally: `record`
(`internal/eval`'s own unexported type, ADR-0011/ADR-0021) already
carries a `headers []string` field, shared across every record one
`sheetRecords` call produces, used today only for `applyField`'s own
column-letter fallback. `toJSON`'s own conversion of `record`/
`[]record` to the public `map[string]any`/`[]map[string]any` shape is
exactly where that order was being discarded - "purely an evaluation-
time detail, never part of the JSON/CSV/table shape," per its own
existing comment.

`eval.EvalPreservingOrder(expr, wb) (result any, headerOrder []string,
err error)` is `eval.Eval` plus that one extra, optional return value:
`headerOrder` is non-nil only when the evaluated expression's result,
right before `toJSON` flattens it, is a `record` or `[]record` -
nil for every other shape (a grid, a scalar, a workbook, `group_by`'s
own nested shape, anything a real transformation like `map(.Likes)`
turned into something that's no longer `[]record` at all), where no
single column order is even well-defined. `Eval` itself becomes a
thin wrapper discarding the second value, so every existing caller
(JSON, `--table`, `--ndjson`, and `internal/eval`'s own test suite) is
completely unaffected.

This composes for free with `records | select(...)`: `selectOver`'s
own generic implementation keeps each surviving element - the
`headers` field included - completely unchanged, so filtering rows
after `records` still gets the real order, not just bare `records`
on its own. It does *not* compose with `map` (whose result generally
isn't `[]record` at all once a real transformation runs) or
`group_by` (`[]any` of `[]record`, a shape one level too nested for
this check) - both simply fall back to `nil`, exactly the existing
behavior, not a regression for either.

`internal/cli`'s own evaluation call becomes `EvalPreservingOrder`
unconditionally (one evaluation per file either way, never a second
call) rather than branching between it and plain `Eval` by output
mode - `headerOrder` is simply read only by the `--csv`/`--tsv`
branches and ignored by every other one.

`output.CSV`/`output.TSV` keep their own existing signature and
behavior completely unchanged (`headerOrder` nil, i.e. still sorted) -
`output.CSVOrdered`/`output.TSVOrdered` are new, additive functions
taking an explicit `headerOrder []string`, used only by
`internal/cli`. `headerOrder`, when non-nil, is trusted as-is, not
unioned or re-validated against the records' own actual keys: its one
caller only ever passes what `EvalPreservingOrder` returned for the
exact same result, which is always either `nil` or an exact match -
adding defensive-looking union/validation logic for a mismatch this
one call path can't actually produce would be unearned complexity, not
real robustness.

## Consequences

- `internal/eval/eval.go`: `EvalPreservingOrder` (new, exported);
  `Eval` now calls it and discards the second value - no other
  behavior change to `Eval` itself.
- `internal/output/csv.go`: `recordRows` takes an additional
  `headerOrder []string` parameter (nil preserves today's sorted
  behavior exactly); `CSVOrdered`/`TSVOrdered` (new).
- `internal/cli/filter.go`: the per-file evaluation call switches to
  `EvalPreservingOrder`; the `--csv`/`--tsv` dispatch branches call
  `output.CSVOrdered`/`TSVOrdered` with the resulting `headerOrder`
  instead of `output.CSV`/`TSV`.
- `--table` and plain/`--ndjson` JSON output are byte-for-byte
  unchanged - this is scoped to `--csv`/`--tsv` alone, per #175's own
  explicit framing, not a reversal of ADR-0011's broader reasoning
  about `map[string]any`'s own key order.
