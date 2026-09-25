# 7. Spreadsheet data model: Workbook, Sheet, Row, Cell

## Status

Accepted

## Context

Every other M1/M2 issue - the query language, `.xlsx` reading, output
formatting - builds on top of some in-memory representation of a
spreadsheet. That representation needs to exist, on its own, before
any of it can start, and it needs to be independent of `excelize`
specifically: `internal/cli` talks to excelize today (ADR-0004), but
the model the query engine operates on shouldn't force every future
reader (CSV, ODS, a second `.xlsx` library) to look like excelize's
API.

Three concrete questions need an answer:

1. How is a cell's value represented - one Go type, or a tagged union
   over string/number/bool/date/formula/empty?
2. Are formulas exposed as their computed value, their raw expression,
   or both?
3. Are a sheet's dimensions and empty cells dense or sparse?

## Decision

- New `internal/model` package (per ADR-0002, its own package since
  it's a distinct concern from `internal/cli`). It has no dependency
  on excelize or any other I/O library - converting into it is a
  reader's job, not the model's.

- **Cell value: a tagged union**, not a bare `any`/`interface{}`
  exposed to callers. `CellKind` is an enum (`Empty`, `String`,
  `Number`, `Bool`, `Date`) that says which of a `Cell`'s values is
  meaningful; the value itself is stored in an unexported `any` field
  and reached only through typed accessors (`StringValue() (string,
  bool)`, `NumberValue() (float64, bool)`, ...) that return `ok=false`
  on a Kind mismatch. This keeps the zero-allocation simplicity of a
  single field without letting a caller read, say, `NumberValue` on a
  string cell and silently get `0`.

- **Formulas expose both**, but not as a separate Kind. A formula
  cell's `Kind` and value are always its *computed* result - so a
  formula that evaluates to a number behaves exactly like a number
  cell everywhere except one extra field, `Formula string` (the raw
  expression, e.g. `"=SUM(A1:A2)"`, empty for non-formula cells).
  `Cell.IsFormula()` is the only place callers need to care about the
  distinction. This means the query engine and every future consumer
  can filter/compare on values without special-casing formulas, while
  the raw expression is still there for anything that wants to
  introspect or round-trip it.

- **Sparse, not dense.** A `Row` only holds the `Cell`s it actually
  has (each carrying its own 0-based `Column`); a `Sheet` only holds
  the `Row`s that have at least one such cell (each carrying its own
  0-based `Index`). Nothing allocates a dense rows x columns grid.
  `Sheet.Dimensions()` derives the bounding box on demand for the
  (common) case a caller wants one, e.g. for rendering a fixed grid or
  a `--csv` export - but that's a derived query, not the storage
  shape, so a spreadsheet with a handful of cells at row 1 and row
  1,000,000 costs proportional to its actual cells, not 10^6 rows.

- Row/column indices are 0-based inside the model, regardless of the
  1-based/`A1`-style addressing `.xlsx` uses at the file-format
  boundary. Converting between the two is the reader's job.

## Consequences

The query engine (and any future reader/writer) can be built and
tested against `internal/model` without excelize in the loop at all.
Adding a new `CellKind` later (e.g. an explicit error type for
`#DIV/0!`) is additive - existing switches over `Kind` need a new
case, but the sparse row/column shape and the Formula field are
unaffected. The tagged-union-via-accessor-methods choice costs a
small amount of boilerplate (one pair of methods per Kind) in
exchange for callers not being able to misread a cell's value by
reaching into the wrong field.
