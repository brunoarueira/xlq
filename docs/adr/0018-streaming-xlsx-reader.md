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

### `internal/xlsx`'s own construction never reads a row until asked

```go
func lazySheet(f *excelize.File, name string, date1904 bool, dateStyles map[int]bool) model.Sheet {
	return model.Sheet{
		Name: name,
		Rows: func(yield func(model.Row, error) bool) {
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

`xlsx.Read` itself becomes near-instant regardless of file size: it
builds one of these per sheet (cheap - a closure, not a read) and
returns, without ever calling `f.Rows` at all until something actually
ranges over a `Sheet.Rows`. This is the concrete fix for #18's stated
goal - reading a workbook no longer has a cost proportional to its
size; *evaluating a filter against it* does, exactly as much as that
filter's own shape requires, per the per-operation analysis below.

### Per-operation analysis: genuine early exit, single-pass-but-full-scan, or unavoidable full scan

Every existing `internal/eval` function that touches `sheet.Rows`
needed checking individually - "make it lazy" doesn't uniformly speed
up every operation, since some of them read the *whole* sheet by
design, not by accident:

- **`cellScalar`** (`.Sheet1.A100000`) already returns the moment it
  finds (or fails to find) the target row - no `Dimensions()` call at
  all. Genuine early exit: cost is proportional to the target row's
  *position*, not the sheet's total size. The clearest win.
- **`sheetRange`** (ADR-0012, `.Sheet1["A3:C9"]`) can stop as soon as
  it passes the declared bottom-right row (`sheet.Rows` is ascending
  by `Index`, already documented and unchanged). Genuine early exit,
  bounded by the range's own declared size - already separately capped
  by `maxExcelRow`/`maxExcelCol` (PR #55).
- **`wholeColumn`** and **`sheetGrid`/`sheetRecords`** (`.Sheet1.B`,
  `.Sheet1`, `records`) all currently call `sheet.Dimensions()` *then*
  iterate `sheet.Rows` separately - a free second pass today, since
  both read the same in-memory slice. Against a lazy `Rows`, that
  pattern would mean two *separate XML scans* instead of one. Rewritten
  to a single pass instead: accumulate into a growable structure while
  tracking the maximum row/column index seen so far, checking
  `maxDenseCells`/cumulative-workbook-budget incrementally as the
  projected size grows rather than only once up front - which is
  strictly *better* than today for an oversized sheet (it aborts the
  moment the budget is exceeded, instead of needing a full
  `Dimensions()` pre-pass to even know to reject it). No early exit
  (every row must still be seen), but no second scan either, and the
  already-fixed PR #24 cumulative-across-sheets budget check must
  carry over exactly - `toJSON`'s whole-workbook loop threads a running
  total into each sheet's own accumulation, not a fresh per-sheet
  budget, matching that fix precisely rather than quietly reopening it.
- **`wholeRow`** (`.Sheet1[5]`) is the one genuine exception: it sizes
  its result to the *sheet's* overall column count, not row 5's own -
  existing, documented, already-released behavior this ADR doesn't
  change. That means even after finding row 5, the scan must continue
  to the sheet's actual last row to learn its true width from whichever
  row turns out to be the widest. No early exit is possible here
  without changing an observable result shape, which is out of scope -
  this ADR optimizes memory (nothing but the current row's own cells is
  ever retained) and leaves `wholeRow`'s own time complexity as a full
  scan, same as today in that respect.

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

A workbook can now be opened and have its sheet list inspected in
roughly constant time regardless of size; a query addressing a single
cell or a small range on an otherwise enormous sheet now costs
roughly proportional to where that cell/range sits, not the sheet's
total size; a query needing the whole sheet (`.Sheet1`, `records`, a
whole column) costs the same amount of I/O as today but holds
substantially less in memory at once, since a row's cells are
discarded immediately after being folded into the result instead of
staying resident as part of a fully-materialized `[]Row`. `wholeRow`
is the one operation that doesn't get faster, by design, since making
it faster would mean changing what it returns.
