# 8. Filter language grammar v1: identity, A1 addressing, pipe

## Status

Accepted

## Context

ADR-0004 deliberately deferred `xlq`'s actual `xlq '<filter>' file.xlsx`
invocation - the whole reason the tool exists - to its own ADR, once
the data model it would evaluate against was designed. That model now
exists (ADR-0007), and #5 asks for the smallest useful grammar: identity
(`.`), sheet/field access, indexing into rows and columns, and the pipe
operator (`|`). Arithmetic, string functions, `map`/`select`, and
formula evaluation are explicitly out of scope for v1 - this ADR only
needs to cover enough for something to parse, evaluate against a
`model.Workbook`, and print, so #6 (lexer/parser) and #7 (evaluator)
have an unambiguous spec to build against.

The one real open question is how a filter addresses a specific
row/column/cell. Two options are both reasonable:

- **0-based array indexing**, matching `model.Row.Index`/`model.Cell.
  Column` directly: `.Sheet1[0][0]`. No translation layer between
  grammar and model, and it composes naturally with jq-style slicing
  and iteration once those exist.
- **A1-style references**, matching spreadsheet convention:
  `.Sheet1.A1`. Immediately familiar to the people `xlq` is actually
  for - anyone who already thinks in Excel coordinates - at the cost
  of a small amount of parsing (turning `"B12"` into a row/column
  pair) and a 1-based/0-based split against the internal model.

xlq's whole premise is a jq/yq-style tool *for spreadsheet users*.
Making someone learn a computed 0-based coordinate system before they
can write their first filter undermines that. A1-style addressing is
the decision here.

## Decision

### Grammar (EBNF)

```
filter   = pipeline
pipeline = path ( "|" path )*
path     = "." suffix*
suffix   = "." IDENT
         | "[" STRING "]"
         | "[" INT "]"

IDENT  = letter (letter | digit | "_")*
STRING = a double-quoted string, backslash-escaped
INT    = ["-"] digit+
```

Whitespace between tokens is insignificant. `.` alone (zero suffixes)
is the identity filter.

### AST

Four node kinds cover all of v1:

- `Identity` - the bare `.`
- `Field{Base Expr, Name string}` - `.IDENT` or `["some string"]`
- `Index{Base Expr, N int}` - `[123]`
- `Pipe{Left, Right Expr}` - `left | right`

`.IDENT` and `["IDENT-as-a-string"]` parse to the same `Field` node -
jq's own convention, kept for the same reason: a sheet named `Q1
Report` (spaces) or `2026` (starts with a digit) doesn't fit `IDENT`,
and still needs a way in.

### Evaluation semantics

The filter's input is always a `model.Workbook`; `.` returns it
unchanged. What a `Field`/`Index` node actually *does* depends on the
runtime type of the value it's applied to - the grammar doesn't
distinguish "sheet access" from "cell access" at parse time, the
evaluator does, exactly the way jq's own `.foo` means something
different on an object than on an array:

| Applied to | `Field{Name}` | `Index{N}` |
| --- | --- | --- |
| `Workbook` | look up a `Sheet` by name, case-insensitively (see below); `null` if none matches | error - a workbook isn't row-indexable, index a sheet instead |
| `Sheet` | `Name` matches `^[A-Za-z]+$` (a column letter or letters, case-insensitive): the whole column, as a dense array of scalars, one per row from row 1 through the sheet's last row (`Sheet.Dimensions()`), `null` for any cell not present. `Name` matches `^[A-Za-z]+[0-9]+$`: the single cell at that A1 address, as a scalar, `null` if absent. Anything else: error | `N >= 1` (spreadsheet row numbers, matching A1's own 1-based rows): the whole row, as a dense array of scalars, one per column from column 1 through the sheet's last column. `N <= 0`: error |
| a row/column array, or a scalar | error - not indexable further in v1 | error - not indexable further in v1 |
| `null` | `null` (propagates) | `null` (propagates) |

Sheet names, column letters, and A1 references are all case-insensitive
(`.sheet1.a1` and `.Sheet1.A1` are the same reference), matching Excel:
it doesn't allow two sheet names in the same workbook differing only by
case, so this can't introduce genuine ambiguity against a well-formed
file. A hand-crafted file that violates that (e.g. both `Sheet1` and
`SHEET1` present) is out of scope for v1's semantics to define; an
exact-case match, if one exists, wins, otherwise it's the first
case-insensitive match in workbook order.

A cell's scalar value is its *computed* value per ADR-0007 - a
formula's raw expression isn't exposed by any v1 operator, since
formula evaluation/introspection is explicitly out of scope. A
Number/String/Bool cell becomes the corresponding JSON number/string/
bool, a Date becomes an RFC3339 string, and an Empty or altogether
absent cell becomes JSON `null`.

`left | right`: evaluate `left`, then evaluate `right` with `.`
rebound to `left`'s result, not the original top-level input -
standard jq pipe semantics.

### Explicitly deferred

- Arithmetic, comparisons, string functions.
- `map`, `select`, and other higher-order filters.
- Formula introspection (the raw expression, not just the computed
  value).
- Range references (`A1:C10`) and slicing.
- Indexing further into an already-produced row/column array (e.g.
  plucking element 2 out of a row array). Once that exists, it should
  almost certainly be 0-based, jq-style, since at that point it's
  indexing a plain array, not a spreadsheet coordinate - a deliberate,
  deferred exception to "everything in this grammar is 1-based," not
  an oversight.

## Consequences

`xlq '.Sheet1.B2' file.xlsx` and `xlq '.Sheet1["B2"]' file.xlsx` are
equivalent and both work immediately for anyone who already thinks in
spreadsheet coordinates - no 0-based translation to learn for the
common case. The grammar is small enough (four AST node kinds, one
EBNF block, one evaluation table) that #6 and #7 both have a complete
spec to build against without further design work of their own.

The one deliberate wrinkle: row numbers are 1-based (matching A1)
everywhere in v1, but per-element indexing into a resulting row/column
array, whenever it's added, will be 0-based (matching every other
array in the language, and jq). Shipping both conventions live in the
same surface area at once - e.g. `.Sheet1[0][0]`-style cell access
alongside `.Sheet1.A1` - would have been confusing enough to avoid
entirely for now, which is why that capability is deferred rather than
added alongside A1 addressing in this same ADR.

`.` on the whole workbook returns a JSON object keyed by sheet name;
JSON object key order isn't guaranteed by the spec (Go's encoder sorts
them), so a workbook's sheet order - which does matter, per
ADR-0007's `Workbook.Sheets` being an ordered list - isn't recoverable
from that top-level shape. Not a problem for `.Sheet1`-style targeted
queries, the overwhelmingly common case; worth remembering if the
whole-workbook shape ever needs to preserve order.
