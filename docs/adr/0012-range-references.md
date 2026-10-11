# 12. Range references: `A1:C10`, and composing with `records`

## Status

Accepted

## Context

ADR-0008 deliberately deferred range references ("Range references
(`A1:C10`) and slicing"). A real export shows the need sooner than
expected: the LinkedIn `TOP POSTS` sheet has two tables side by side in
the same rows - columns A-C and E-G - so there is currently no way to
address one table without its neighbour. ADR-0011 hit the same wall
from the other direction: `records` treats whatever it's piped from
row 1 as the header row, but `TOP POSTS`'s real header sits on row 3,
and the whole sheet's row 1 is a single-cell title - isolating just
one table's rows and columns first is the only way to make `records`
useful on a sheet shaped like this.

This ADR decides the range syntax, what a range evaluates to, and how
it composes with `records`. Implementing it is #41, blocked on this.

## Decision

### Syntax

A range is a *third* pattern recognized on the **existing** `Field`
node applied to a `model.Sheet` - no new grammar, no new AST node.
ADR-0008 already dispatches `Field{Name}` on a sheet by matching
`Name` against a column-letter pattern or a cell-reference pattern;
this adds a third: two cell references joined by a colon.

```
.Sheet1["A3:C9"]
```

Only the bracket-string form works - `.Sheet1.A3:C9` can't, since
`:` isn't a character `IDENT`'s lexer rule accepts, so it would never
lex as a single token in dot-suffix position. This isn't a limitation
worth fixing: it costs nothing, since `["A3:C9"]` and `.A3:C9` would
otherwise be exactly equivalent the same way `.IDENT` and
`["IDENT-as-a-string"]` already are for every other name form, and
teaching the lexer about `:` for this one case isn't worth the
grammar surface it would add.

### Evaluation

A range reference's two halves are each a cell reference, validated
and parsed exactly like a standalone `.Sheet1.A1` already is (reusing
`parseCellReference`, including its existing row `>= 1` check) -
`.Sheet1["A0:C9"]` is rejected the same way `.Sheet1.A0` already is.
The two corners don't need to be given top-left-to-bottom-right:
`.Sheet1["C9:A3"]` and `.Sheet1["A3:C9"]` are the same range, matching
Excel's own order-independent range notation - whichever corner comes
first, the smaller row/column bound anchors the top-left.

A range evaluates to **a new `model.Sheet`**, re-indexed so its own
top-left cell becomes row 0, column 0 - not a dense grid, not a new
Go type. Concretely: every cell within the range's row/column bounds
is copied into a new `model.Sheet` with its `Row.Index`/`Cell.Column`
each reduced by the range's starting row/column; a row or column that
has nothing real to re-index simply isn't added (sparse, the same way
a whole sheet already is).

The new sheet's `Name` is `"<original>!<range>"` (`TOP POSTS!A3:C9`,
following Excel's own qualified-reference convention), purely for
clearer error messages - the identity of the underlying sheet it came
from isn't otherwise tracked or exposed.

Returning a `model.Sheet`, rather than inventing a fourth result
shape, means **every existing operator already works on a range for
free**, with no new code anywhere else in `internal/eval`:

- A bare range (`.Sheet1["A3:C9"]`, nothing piped after it) converts
  to a dense 2D grid via `toJSON`'s existing `case model.Sheet`
  branch - the same conversion a whole sheet already gets.
- Further `.A1`-style, whole-column, or whole-row access on a range
  result reuses `applyField`/`applyIndex`'s existing `model.Sheet`
  cases unchanged - **relative to the range**, not the original sheet:
  `.Sheet1["C3:E9"].A1` is the range's own top-left cell, i.e. the
  original sheet's `C3`, not its actual `A1`. Worth calling out
  explicitly since it's an easy thing to assume wrong.
- `records` (ADR-0011) needs no changes at all: `.Sheet1["A3:C9"] |
  records` just works, because `records` already accepts any
  `model.Sheet` and treats *its* row 0 as the header - which, for a
  range result, is the range's own first row, not the whole sheet's.
  This is the direct fix for `TOP POSTS`: `.["TOP POSTS"]["A3:C100"] |
  records` isolates one table's rows and columns before `records` ever
  sees a header row, so the two tables' shared column names
  (`"Post URL"`, `"Post Publish Date"`) never collide, because the
  other table's columns were never part of the slice to begin with.
- The existing `maxDenseCells` budget (`internal/eval`, 10,000,000
  cells) already applies to a range with no new guard needed:
  `Dimensions()` on the re-indexed sheet reflects the real cells
  present within the range, the same way it already does for a whole
  sheet, so `sheetGrid`/`sheetRecords`'s existing checks see it
  exactly like any other `model.Sheet` and reject an oversized one the
  same way.

### Explicitly deferred

- Open-ended ranges - a whole-row span (`"3:9"`), a whole-column span
  (`"A:C"`), or one open corner (`"A3:C"`). The issue's own motivating
  case (`TOP POSTS`) needs only the fully-bounded two-corner form; an
  open-ended form can be added later as a pure addition to the same
  regex-dispatch mechanism, without revisiting anything decided here.
- Ranges spanning more than one sheet (Excel's `Sheet1:Sheet3!A1:C10`
  3-D references). Nothing in the motivating case needs this.
- Slicing a `records` array, or any other already-produced value, by a
  range-like syntax. A range is specifically a `model.Sheet` concept;
  composing *into* `records` (slice first, then pipe) is the
  supported direction, not the reverse.

## Consequences

`.["TOP POSTS"]["A3:C100"] | records` works today, immediately after
#41 implements this, because `records` requires no changes of its own.
This is the actual unblock the headers ADR's own "Explicitly deferred"
section was written to need: isolating one table's header row without
teaching `records` anything about multi-table sheets itself.

`.Sheet1["A1:C10"]` is interchangeable with a whole sheet everywhere
that matters - dense-grid conversion, cell/row/column access, piping
into `records` - because it *is* a `model.Sheet`, not a special case
threaded through every function that currently handles one. The only
place that needed new code at all is the three-way regex dispatch in
`applyField`.

The one sharp edge: relative addressing. `.Sheet1["C3:E9"].A1` means
the range's own corner, not the original sheet's `A1` - worth
documenting prominently wherever this ships (README, `--help`), since
it's the one place this design's "a range is just a sheet" framing
could surprise someone reaching for absolute coordinates instead.
