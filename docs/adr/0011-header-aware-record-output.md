# 11. Header-aware record output: the `records` filter

## Status

Accepted

## Context

Every sheet is a 2D array today (ADR-0008): a query against a real
analytics export needs positional indexes like
`.["TOP POSTS"][3][6]`, which breaks silently the moment a file's
column order changes, and tells a reader nothing about what column 6
actually is. Real exports (a LinkedIn analytics download prompted this
issue) are full of exactly the shape this punishes: named columns a
person would naturally refer to by name, not position.

That same export also shows the shape isn't uniform. `TOP POSTS` has a
title row, a blank row, then a header row at row 3 - and two tables
side by side in the same rows (columns A-C and E-G), sharing header
text like "Post URL" between them. `DISCOVERY` and similar tabs are
plain two-column metric/value lists with no header row at all. Any
design here has to be honest about which of these it actually helps.

#36 (range references) is a prerequisite for addressing `TOP POSTS`'s
two tables independently; this ADR doesn't attempt to solve that
itself, per its own scope ("how this interacts with multi-table
sheets (see the ranges ADR)").

## Decision

### Grammar

A pipe step gains a second form: a bare builtin keyword, alongside the
existing `path` form.

```
pipeline = path ( "|" step )*
step     = path | builtin
builtin  = "records"
```

Everything else in ADR-0008/0009's grammar is unchanged. `records` can
only appear after a `|` - it transforms a value already produced by
what's to its left, so it can never be the first (or only) step in a
filter.

A Field-based alternative - `.Sheet1.records`, reusing the existing
`.IDENT` suffix - was rejected: ADR-0008's column-letter pattern
(`^[A-Za-z]+$`) matches *any* run of letters with no length limit, so
`records` is itself a syntactically valid (if absurdly distant) column
reference. A bare keyword in pipe-step position avoids that ambiguity
entirely, and leaves `.Sheet1`'s own Field/Index semantics completely
untouched - `records` is purely additive, and only engaged when a
filter explicitly pipes into it.

### Input and output

`records` operates on the same internal `model.Sheet` value
`applyField`/`applyIndex` already work with - before the final
`toJSON` conversion to a dense grid, not after - so it has the sheet's
real sparse row/cell structure to work from, not an already-flattened
JSON array.

| Applied to | Result |
| --- | --- |
| `model.Sheet` | an array of record objects (see below) |
| `null` | `null` (propagates) |
| anything else (a `Workbook`, an already-produced row/column array, a scalar) | error - not valid input to `records` |

A record is a `map[string]any`, one per **data row** (every row with
`Row.Index >= 1`, in sheet order), keyed by header name, valued by
that cell's already-defined `cellValue()` conversion - exactly what
`.Sheet1.A1`-style addressing would already produce for that cell. A
data row entirely absent from `Sheet.Rows` (every cell blank)
contributes no record at all; `model.Sheet`'s sparse representation
already guarantees this for free, there's no `Row` to iterate for it.
A data row that's present but missing some cells still produces a
record covering every header, `null` for the missing ones - consistent
with how `wholeRow`/`wholeColumn` already fill absent cells with
`null`.

This is the conventional "CSV-as-JSON" shape (one object per row),
matching the issue's own framing ("objects keyed by header"), and
deliberately not a columnar (object-of-arrays) shape - see Explicitly
deferred.

A `map[string]any`'s keys, once JSON-encoded, come out sorted
alphabetically, not in the sheet's original column order - the same
tradeoff ADR-0008 already accepts for the whole-workbook object (keyed
by sheet name), for the same reason: Go's JSON encoder doesn't
preserve map insertion order, and introducing an ordered-object type
here to recover it would be more machinery than the problem has
earned given that ADR-0008 precedent. Column order isn't recoverable
from a single record's JSON shape; it still is from `records`' own
array order (one element per data row, in sheet order).

### Header row

Always row 1 (`Row.Index == 0`) of whatever value `records` is applied
to. No flag, no auto-detection heuristic.

Auto-detecting a header row was considered and rejected: it's the same
kind of guess the project has already ruled out in every other place a
guess like this could creep in - ADR-0010 and the CSV reader both
refuse to infer a cell's *type* from its text, for the same reason a
heuristic here would be unreliable and hard to predict. A fixed
convention (row 1) is simple, predictable, and matches the default
behavior of nearly every CSV/table tool a user of `xlq` has likely
already used.

### Header names

- A non-blank header cell's name is its own `cellValue()` rendering,
  **stringified into a map key**: a `string` cell's value as-is, a
  `float64` cell via its JSON-number text (`42` becomes `"42"`), a
  `bool` via `"true"`/`"false"`, a `Date` via the same RFC3339 text
  `.Sheet1.A1` addressing would already produce. No new parsing or
  type inference - only turning an already-computed value into a
  string key.
- A blank header cell (or a header row shorter than
  `Sheet.Dimensions().cols`) uses that column's own A1 letter as its
  key instead (column C's blank header becomes key `"C"`) - reusing an
  addressing convention the user already knows, so every column always
  contributes a key rather than being silently dropped.
- A header row that's entirely absent from `Sheet.Rows` degrades to
  every column using its letter - the same fallback as a single blank
  cell, just for the whole row at once.
- Two columns resolving to the same *rendered key* - duplicate header
  text, a numeric header colliding with literal text that renders the
  same way (a `42` cell and a `"42"` cell are one collision, not two
  distinct headers), or a blank-placeholder letter colliding with a
  real header elsewhere - is an **error**, naming both column letters
  and the repeated key. The duplicate check always runs on the final
  rendered string, never on the pre-render `cellValue()`, so a
  cross-kind collision is caught the same way a same-kind one is. A
  plain Go map would otherwise silently keep only the last-written
  value, quietly dropping a column's data - precisely the risk a
  sheet like `TOP POSTS` carries once its own header row is isolated
  (see Consequences): it repeats "Post URL" and "Post Publish Date"
  across its two side-by-side tables. Silently losing data on a
  header collision isn't consistent with the project's existing
  refusal to guess or discard silently.

### Resource bound

`records` must apply the same `maxDenseCells` budget (internal/eval,
10,000,000 cells) that `sheetGrid`/`toJSON` already enforce, checked
against `Sheet.Dimensions()` before building the result. It operates
on `model.Sheet` directly and therefore bypasses `sheetGrid`'s own
check entirely - without its own check, a sheet too large for `.Sheet1`
itself to produce (today, a clear, immediate error) could still build
an unbounded records array. Noting this explicitly for whoever
implements #39, since it's exactly the shape of resource-exhaustion
gap a past review already caught once, in a different package
(`internal/ods`, PR #24).

### Explicitly deferred

- Choosing a header row other than row 1 directly, via a flag or a
  parameter on `records` itself. The real-world need for this
  (`TOP POSTS`'s header on row 3) is better served by composing with
  ranges once #36 lands - isolate the one table's rows/columns first,
  then pipe the narrower result into `records`, which only ever sees
  that slice's own row 1. Not solved by this ADR.
- Addressing a record's field by its original column letter rather
  than its header text (e.g. `.["TOP POSTS"] | records | .[0].G`) -
  #40's own question once #39 lands.
- A columnar (object-of-arrays) output shape. Reachable later as
  `records | map(.Impressions)` once `map` exists (#38's own
  prerequisite), rather than wiring a second output shape into
  `records` itself.
- Any interaction with `map`/`select`/aggregates beyond `records`
  producing a plain array/object value for them to eventually operate
  on like any other.

## Consequences

`.Sheet1 | records` works immediately and usefully for the common
case: a sheet whose literal row 1 is a header row, with no gaps or
duplicate names - the majority of simple tabular exports.

It is not useful, and would be actively wrong, on a sheet with no real
header row at all - `DISCOVERY`'s plain metric/value layout, for
instance, would have its first data point consumed as a header if
piped into `records`. That's an acceptable limitation: `records` is
opt-in, applied by explicit choice, the same way `pandas.read_csv`'s
own `header=0` default is also wrong for a headerless file unless the
caller says otherwise.

`records` on the full `TOP POSTS` sheet doesn't reach the duplicate-
header case at all: its row 1 is a single-cell title row, not the
real header row (row 3), so `records` would use that title as the
lone header and fall back to column letters everywhere else - a
useless result, but not an error, and not the sheet's two tables
merged together either. Reaching row 3's genuine duplicate headers
("Post URL" and "Post Publish Date", repeated across its two
side-by-side tables) needs #36 (ranges) to isolate one table's rows
first; only then would `records` see a single header row and -
without also narrowing to that table's own columns - raise the
duplicate-header error this ADR defines. Resolving that fully (one
table's data, not an error) is #36 plus #41's job, not this ADR's.

The grammar gains exactly one new production (a pipe step may be a
bare builtin keyword, not only a path) and one new keyword
(`records`); every existing filter's meaning is unchanged. Implementing
it (#39) needs: a lexer/parser change recognizing `records` in step
position (`internal/filter`), a new AST node, and a new evaluator case
operating on `model.Sheet` with its own `maxDenseCells`-style bound
(`internal/eval`) - no changes to `internal/model` or any reader.
