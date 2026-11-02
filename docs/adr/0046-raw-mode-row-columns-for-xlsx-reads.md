# 46. Read xlsx rows in raw mode, not formatted, to cut large-file read time

## Status

Accepted

## Context

A user reported `xlq '.Data' large.xlsx` taking 13 seconds against a
real, 105,012-row Open Data export. Profiled directly against a
synthetic file of the same shape (105,012 rows x 14 columns, mixed
string/number/date/bool columns): `input.Read` itself is fast
(190ms - ADR-0018's lazy design works as intended), and essentially
all the time - 26s on this profiling run - is inside the row/cell walk
that happens once something actually ranges over a `Sheet.Rows`.

A CPU profile of that walk showed ~71% of total time inside excelize's
own `Rows.Columns()` call (`internal/xlsx/xlsx.go`'s `lazySheet`,
called once per row), and roughly a fifth of the *total* runtime
specifically inside excelize's `formattedValue`/`applyBuiltInNumFmt` -
computing a **formatted display string** for every cell in the row.
`readCell` immediately discards that formatted text for every
numeric/date/bool cell: it fetches the cell's *raw* value in a
*separate* call (`f.GetCellValue(..., RawCellValue: true)`) and only
actually uses the formatted text for a string-typed cell (shared
string, inline string, or a cached formula-error result) or as a
last-resort fallback. On top of the per-row `Columns()` call,
`readCell` makes 3-5 more separate excelize API calls *per cell*
(`GetCellFormula`, the raw `GetCellValue`, `GetCellType`, and
conditionally `GetCellStyle`+`GetStyle` for date detection) - each one
potentially re-touching excelize's own per-cell resolution machinery.

excelize's `Rows.Columns()` accepts the same `Options` every other
read method does, including `RawCellValue` (confirmed directly in its
source, `rows.go`: `rows.rawCellValue = rows.f.getOptions(opts...).RawCellValue`,
threaded into `rowXMLHandler`) - meaning the formatted-value
computation `readCell` pays for and then throws away isn't load-
bearing; it's possible to ask excelize to skip it at the source
instead of discarding it after the fact.

## Decision

`lazySheet` calls `rows.Columns(excelize.Options{RawCellValue: true})`
instead of `rows.Columns()` with no options, and `readCell` no longer
makes its own separate `GetCellValue(..., RawCellValue: true)` call -
the raw value it already receives from `Columns()` is reused directly
for both of its previous roles (presence detection, and the string-
cell value previously read from the formatted `display` text).

This isn't a smaller-scope, safer-looking change than it sounds:
prototyped and verified directly, not assumed. All 702 existing
tests, including every xlsx-specific correctness case (shared
strings, inline strings, hidden/custom number formats, cached and
live-computed formulas, sparse sheets, error-producing formulas, ISO
dates) passed unchanged against the prototype - meaning raw-mode
`Columns()` text is equivalent to the formatted `display` text had
been for every case this project's own suite exercises. Re-profiled
against the same synthetic 105,012-row fixture: **27.16s -> 6.26s**
(~4.3x), with byte-identical JSON output before and after.

A dedicated benchmark - `internal/xlsx`'s own, following
`internal/eval/eval_bench_test.go`'s established pattern and
explicitly-left gap ("internal/xlsx... has its own concerns" - a
benchmark for it was anticipated, never built) - is added as part of
implementing this, so the improvement is measurable in CI history
going forward, not just a one-off local profiling run, and so a
future change can't silently regress it back.

The existing correctness suite (`internal/xlsx/xlsx_test.go`, already
pure behavior tests - fixture in, expected `model.Cell` out, no
mocking or call-count assertions) needs no changes of its own: it's
already the right regression guard for "did this change what gets
read," independent of how it gets read, and that's exactly what it
confirmed above.

## Consequences

`internal/xlsx/xlsx.go`'s `lazySheet` and `readCell` change: one
fewer excelize API call per cell, and a cheaper `Columns()` call
itself. `readCell`'s signature, its "display vs. raw" framing, and
its doc comments need real cleanup as part of the implementation (the
validated prototype left `display` as a bare alias for `raw`, not
production-shaped) - tracked as its own issue, not done here. No
observable behavior changes for any input this project's test suite
covers; a large xlsx file (the actual motivating case) reads
roughly 4x faster.
