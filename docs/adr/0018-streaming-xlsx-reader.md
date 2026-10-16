# 18. Streaming xlsx reader: lazy `Sheet.Rows` via Go iterators

## Status

Accepted

## Context

Issue #18 (M6): `internal/xlsx.Read` already uses excelize's own
streaming `Rows()` cursor internally - the XML itself is never fully
parsed into memory at once - but `readSheet` then appends every row it
sees into a plain `[]model.Row` slice regardless, so `model.Sheet`
(and therefore `model.Workbook`) still fully materializes the entire
file before a filter ever runs. A query like `.Sheet1.A1` against a
2-million-row sheet currently costs as much memory and time as reading
every one of those rows, no matter how small the actual answer is -
excelize's own streaming doesn't help xlq at all once the result lands
in `model.Sheet`.

Scoped to xlsx specifically, matching the issue's own title.
`internal/csv` and `internal/ods` keep their existing eager internals -
ADR-0010 already bounds `internal/ods` with its own size/cell budgets
(hardened across a `cubic-dev-ai` review round on PR #29), and neither
format has excelize's kind of row-level streaming primitive to build
on in the first place. This ADR changes `model.Sheet`'s own *type* so
all three readers produce a uniform shape either way (below); only
`internal/xlsx`'s construction of it actually becomes lazy.

The mechanism this ADR rests on - Go 1.23+ range-over-func iterators
combined with excelize's own `Rows` cursor lifecycle - was verified
empirically against a real file before being decided here, not
assumed from how either is documented: a standalone probe confirmed
(1) excelize's gap-row padding (a row number absent from the XML still
yields an iteration, with an empty `Columns()` result, so a 0-based Go
counter tracks the real row index correctly across gaps - already
relied on by `internal/xlsx`'s current, eager reader and unchanged by
this ADR), (2) breaking out of a `for row := range sheet.Rows` loop
early correctly runs a `defer rows.Close()` inside the iterator
closure, and (3) calling `f.Rows(name)` a second time for the same
sheet - needed because some operations require more than one pass,
see below - opens a fresh, independent, correctly-ordered cursor; nothing
about the first pass leaks into or blocks the second.

## Decision

### `model.Sheet.Rows` becomes `iter.Seq2[Row, error]`, not `[]Row`

```go
type Sheet struct {
	Name string
	Rows iter.Seq2[Row, error]
}
```

`iter.Seq2`, not the plain single-value `iter.Seq`, because a lazy read
can fail mid-stream (a malformed cell, a row past Excel's own limits,
excelize's own `Rows.Error()`) in a way a pre-built slice never could -
every consumer now does:

```go
for row, err := range sheet.Rows {
	if err != nil {
		return nil, err
	}
	// use row
}
```

`Row.Cells` stays a plain `[]Cell` - unlike row count (which a real
file can push past a million), Excel's own column limit (16,384)
already bounds a single row's cell count to something a few tens of
kilobytes wide at most, the same reasoning `maxExcelCol` already
leans on elsewhere; there's no comparable unboundedness to guard
against within one row.

### `model.NewSheet(name string, rows []Row) Sheet` - the on-ramp for every already-eager producer

```go
func NewSheet(name string, rows []Row) Sheet {
	return Sheet{
		Name: name,
		Rows: func(yield func(Row, error) bool) {
			for _, r := range rows {
				if !yield(r, nil) {
					return
				}
			}
		},
	}
}
```

This is how `internal/csv`, `internal/ods`, `sheetRange` (ADR-0012,
which builds a new, already-small, already-bounded `model.Sheet` from
a slice it accumulates itself), and every existing test fixture
construct a `Sheet` going forward - all of them keep working with a
plain `[]Row` internally, completely unaware that `Sheet.Rows` is
"lazy" in the type-system sense. Only `internal/xlsx`'s own
construction is genuinely backed by a live excelize cursor instead of
an already-built slice. This is what keeps the blast radius of an
otherwise-architectural change contained: it's one shared adapter
function, not a different construction pattern duplicated everywhere
a `Sheet` gets built.

### `internal/xlsx`'s own construction never reads a row until asked - and opens its own file, fresh, per pass

A first draft of this closure captured an already-open `*excelize.File`
- wrong, caught in review: `xlsx.Read` has always closed its file via
`defer f.Close()` before returning, which is fine when every row gets
read *during* that call, but is exactly backwards for a lazy design,
where nothing reads a row until *after* `Read` has already returned
(and so already closed the file) - every later `Sheet.Rows` iteration
would run against an already-closed file and fail outright. There's no
way to defer that close until "whenever the caller is done with every
lazy sheet" without giving `model.Workbook` its own explicit
open/close lifetime - a real option, but a bigger one, threading a new
resource-management responsibility through `internal/input` and
`internal/cli` for every caller of `xlsx.Read` to get right. Simpler,
and self-contained to this package alone: each sheet's closure
captures the file *path* (and the two cheap, already-known-upfront
values below it needs), and opens its own fresh `excelize.File` - closed
again via its own `defer` - every time it's actually ranged over:

```go
func lazySheet(path, name string, date1904 bool, dateStyles map[int]bool) model.Sheet {
	return model.Sheet{
		Name: name,
		Rows: func(yield func(model.Row, error) bool) {
			f, err := excelize.OpenFile(path)
			if err != nil {
				yield(model.Row{}, err)
				return
			}
			defer func() { _ = f.Close() }()
			rows, err := f.Rows(name)
			if err != nil {
				yield(model.Row{}, err)
				return
			}
			defer func() { _ = rows.Close() }()
			for rowIndex := 0; rows.Next(); rowIndex++ {
				cols, err := rows.Columns()
				if err != nil {
					yield(model.Row{}, err)
					return
				}
				var cells []model.Cell
				for colIndex, display := range cols {
					cell, ok, err := readCell(f, name, colIndex, rowIndex, display, date1904, dateStyles)
					if err != nil {
						yield(model.Row{}, err)
						return
					}
					if ok {
						cells = append(cells, cell) // readCell already returns cell.At(colIndex)
					}
				}
				if len(cells) > 0 && !yield(model.Row{Index: rowIndex, Cells: cells}, nil) {
					return
				}
			}
			if err := rows.Error(); err != nil {
				yield(model.Row{}, err)
			}
		},
	}
}
```

`date1904` (a workbook-wide property) is read once, upfront, by
`xlsx.Read` itself - which still opens the file once to get
`f.GetSheetList()`/`f.GetWorkbookProps()`, exactly as today, then
closes it immediately, before building these closures. `dateStyles`
isn't read upfront at all - `xlsx.Read` creates it empty, the same way
it does today, and it's populated lazily, one style ID at a time, by
`isDateStyled` as cells are actually read, same as today. Both values
are captured by every sheet's closure (`dateStyles` specifically as a
shared, mutable, *initially empty* map, captured *by reference*, keyed
by style ID rather than by any particular open `*excelize.File`):
every sheet's pass still benefits from a style-ID lookup another pass -
of this sheet, or another one - already resolved, with no need for the
file itself to stay open for that sharing to work; this project has no
concurrent sheet access anywhere, so a bare shared map needs no mutex).

The trade-off this accepts: a query touching N sheets now opens the
file N times (once per sheet actually ranged over), each paying
`excelize.OpenFile`'s own workbook-level parsing cost again (see the
qualification below) - instead of once, as today. For the case this
ADR specifically optimizes (a narrow query against one sheet of an
otherwise enormous file), that's a single open, same as today. For a
query needing every sheet, the sheets' own row/cell data still
dominates total cost once any of them has real size, so N modest
reopens are the accepted price for not having to give
`model.Workbook` an explicit lifetime of its own.

Reading a workbook no longer materializes any sheet's row data up
front: `xlsx.Read` builds one of these closures per sheet (cheap - it
captures values, it doesn't read anything) and returns. Opening an
xlsx workbook is not thereby made free or constant-cost regardless of
file size, though - `excelize.OpenFile` itself still does real
workbook-level parsing (shared strings, styles, the sheet list), work
this ADR doesn't touch or reduce. What changes is specifically
deferring *worksheet-row* decoding until something actually ranges
over that sheet's `Rows`, exactly as much as that filter's own shape
requires, per the per-operation analysis below.

### Per-operation analysis: genuine early exit, single-pass-but-full-scan, or unavoidable full scan

Every existing `internal/eval` function that touches `sheet.Rows`
needed checking individually - "make it lazy" doesn't uniformly speed
up every operation, since some of them read the *whole* sheet by
design, not by accident. `model.Sheet.Dimensions()` itself is **removed**
as part of this: every one of its five callers (all in
`internal/eval`, nothing outside it - a handful of test files also
call it purely as an assertion convenience, and switch to draining
`Rows` directly) is rewritten below to track whatever bound it needs
*while* doing its own pass over the now-fallible `Rows`, rather than
calling a separate, error-blind helper first. A `Dimensions() (int,
int)` with no error return literally cannot report a cursor failure
once rows can fail mid-stream - keeping it would mean either silently
returning partial, wrong dimensions on a failed read, or giving it an
error return that nothing would call anymore anyway, once every actual
caller does its own single-pass accumulation instead.

- **`cellScalar`** (`.Sheet1.A100000`) needs an explicit early-stop
  condition it doesn't have today: ranging over `sheet.Rows` (ascending
  by `Index`, unchanged) and returning the moment the target row is
  found - but also `break`-ing the moment a row's `Index` is seen to
  *exceed* the target, since at that point the target row is
  conclusively absent (sparse - no cells were ever set in it) and
  waiting for the iterator to drain further would scan the rest of the
  sheet for nothing. Today's eager version has no such check because
  it's never needed one - scanning the rest of an in-memory slice
  looking for a row that isn't there costs nothing extra; against a
  lazy, XML-backed iterator it would silently give up the entire
  early-exit benefit for exactly the ordinary case of looking up a
  cell in a row that happens to be blank. With the check: genuine early
  exit, cost proportional to the target row's position, not the
  sheet's total size - the clearest win, once actually implemented with
  this fix.
- **`sheetRange`** (ADR-0012, `.Sheet1["A3:C9"]`) can stop as soon as
  it passes the declared bottom-right row, the same reasoning as
  `cellScalar`'s fix above. Genuine early exit, bounded by the range's
  own declared size - already separately capped by
  `maxExcelRow`/`maxExcelCol` (PR #55).
- **`wholeColumn`**, **`sheetGrid`/`sheetRecords`**, and **`toJSON`**'s
  own whole-workbook loop (`.Sheet1.B`, `.Sheet1`, `records`, bare `.`)
  all currently call `sheet.Dimensions()` *then*
  iterate `sheet.Rows` separately - a free second pass today, since
  both read the same in-memory slice. Against a lazy `Rows`, that
  pattern would mean two *separate XML scans* instead of one (setting
  aside that `Dimensions()` itself no longer exists). Rewritten to a
  single pass instead: accumulate into a growable structure while
  tracking the maximum row/column index seen so far, checking
  `maxDenseCells`/cumulative-workbook-budget incrementally as the
  projected size grows rather than only once up front - which is
  strictly *better* than today for an oversized sheet (it aborts the
  moment the budget is exceeded, instead of needing a full dimensions
  pre-pass to even know to reject it). No early exit (every row must
  still be seen), but no second scan either, and the already-fixed PR
  #24 cumulative-across-sheets budget check must carry over exactly -
  `toJSON`'s whole-workbook loop threads a running total into each
  sheet's own accumulation, not a fresh per-sheet budget, matching that
  fix precisely rather than quietly reopening it.
- **`wholeRow`** (`.Sheet1[5]`) is the one genuine exception: it sizes
  its result to the *sheet's* overall column count, not row 5's own -
  existing, documented, already-released behavior this ADR doesn't
  change. That means even after finding row 5, the scan must continue
  to the sheet's actual last row to learn its true width from whichever
  row turns out to be the widest - still a single pass (track the
  target row's own cells *and* the running max column width together
  as rows arrive), just one with no early exit. No early exit is
  possible here without changing an observable result shape, which is
  out of scope - this ADR optimizes memory (nothing but the current
  row's own cells is ever retained) and leaves `wholeRow`'s own time
  complexity as a full scan, same as today in that respect.

### Signature changes this forces, and why they're bounded

`wholeColumn`/`wholeRow` gain an `error` return (a lazy read can fail;
a slice never could), which ripples to exactly their two call sites in
`applyField`/`applyIndex` (`internal/eval/eval.go`) - the only two
places that called them. Nothing public-facing changes: `eval.Eval`'s
own signature, and every filter's evaluation *semantics* (ADR-0008),
are unchanged.

## Explicitly deferred

- Streaming `internal/csv`/`internal/ods` themselves - out of this
  issue's own scope (its title says xlsx). `internal/ods`'s existing
  256 MiB/10M-cell budget (ADR-0010) already bounds the risk that
  motivated this issue for that format; `internal/csv` has no
  comparable risk profile flagged anywhere yet.
- Any change to `model.Workbook.Sheets` itself, which stays a plain,
  eagerly-built `[]Sheet` - the sheet *list* (names, in order) is cheap
  to know upfront (excelize already parses `workbook.xml` eagerly on
  `OpenFile`); only each sheet's *row data* was ever the actual cost,
  and each `Sheet` value in that slice already carries its own lazy
  `Rows` untouched until something ranges over it.
- Re-deriving `wholeRow`'s column-count semantics to allow early exit -
  a genuine behavior change, not a performance one, and not asked for
  here.

## Consequences

Implementing this touches `internal/model` (the type change, plus
`NewSheet`), `internal/xlsx` (the actual lazy construction - the only
place that gets genuinely faster/lighter), `internal/csv`/`internal/ods`
(switch their own `Sheet` construction to `model.NewSheet`, no
behavior change), `internal/eval` (every function touching
`sheet.Rows`, rewritten per the per-operation analysis above), and
every existing test across all of those packages that builds a
`model.Sheet` struct literal directly - a mechanical but wide-reaching
update, not a design change, since `NewSheet` preserves exactly the
same "here's a slice of rows" authoring style tests already use. Given
the breadth, implementation is expected to land as more than one PR
against this feature branch, not a single one.

`xlsx.Read` itself now returns as soon as it knows the sheet list -
no sheet's row data is read at all until something actually asks for
it - rather than, as today, only after reading every row of every
sheet; `excelize.OpenFile`'s own workbook-level parsing (shared
strings, styles) is unchanged and still size-dependent, the same
qualification as above. A query addressing a single cell or a small
range on an otherwise enormous sheet now costs
roughly proportional to where that cell/range sits, not the sheet's
total size; a query needing the whole sheet (`.Sheet1`, `records`, a
whole column) costs the same amount of I/O as today but holds
substantially less in memory at once, since a row's cells are
discarded immediately after being folded into the result instead of
staying resident as part of a fully-materialized `[]Row`. `wholeRow`
is the one operation that doesn't get faster, by design, since making
it faster would mean changing what it returns.
