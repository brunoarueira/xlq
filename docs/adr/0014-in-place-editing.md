# 14. In-place `.xlsx` editing: assignment grammar and the write path

## Status

Accepted

## Context

Every filter so far is read-only: parse an expression, evaluate it
against a `model.Workbook`, print the result. `-i`/`--in-place`
(yq's own convention, and this issue's title) needs two things
nothing in the codebase has yet:

- An **assignment** form in the filter grammar - `.Sheet1.A1 = "value"`
  - not just paths that read a value.
- A **write path** - nothing writes a spreadsheet file anywhere in
  xlq today; every reader (`internal/xlsx`, `internal/csv`,
  `internal/ods`) is one-directional.

The write path is the bigger risk. `model.Workbook` (ADR-0007) is a
deliberately reduced representation - cell kind, value, formula - with
no styling, merged cells, charts, images, data validation, or anything
else OOXML can hold. Reading a file into `model.Workbook`, mutating
the model, and writing a *new* `.xlsx` from that model would silently
discard everything the model never captured - exactly the kind of
"in-place edit" that quietly destroys a file's look and other content.
This ADR's central decision is built around avoiding that trap
entirely, not around how to preserve more of the model.

## Decision

### Scope: `.xlsx` only, one assignment, literal values only

Matching the issue's own title, only `.xlsx` is in scope - `internal/csv`
and `internal/ods` have no writer at all, and adding one is out of
scope here. `-i` with a non-`.xlsx` format (detected or explicit) is a
clear error.

A single invocation supports exactly one assignment, to exactly one
cell, to a literal value - not a pipeline, not multiple assignments
chained, not a range/row/column, not an expression referencing another
cell's value. Each of those is real, plausible future scope (see
Explicitly deferred) that doesn't need deciding to deliver a useful
v1: setting one cell's value is `yq -i`'s own most common case.

### Grammar

A filter is now either the existing read-only `pipeline`, or a new,
separate `assignment` - never both, never composed with a pipe:

```
filter     = pipeline | assignment
assignment = path "=" value
value      = STRING | NUMBER | INT | "true" | "false" | "null"
NUMBER     = ["-"] digit+ "." digit+
```

`INT` is the existing integer token (`tokInt`, used today for bracket
indices like `.Sheet1[5]`) accepted in value position too - not a new
token, just a new place an existing one is legal. Without it, `value`
would have no alternative for a bare integer at all (`NUMBER` now
requires a fraction, see below), making `.Sheet1.A1 = 5` - arguably
the single most common cell write - a parse error. An `INT` value
parses to the same `float64` an equivalent `NUMBER` would (`5` and
`5.0` produce an identical cell value), so `Assign.Value`'s `float64`
case doesn't care which token produced it.

`STRING`, `true`, `false`, and `null` need no new lexing: a string
already lexes the same way it does anywhere else in the grammar, and
`true`/`false`/`null` already lex as ordinary `tokIdent` tokens (the
same way `records` does, ADR-0011) - recognizing them as literals
rather than field names is purely a parser-level distinction, at the
one place `value` is parsed. `NUMBER` is the one genuine lexer
addition: the existing integer lexer (used for bracket indices,
`.Sheet1[5]`) only ever consumes an optional leading `-` and digits,
with `.` already claimed by the top-level `.`-path token. A decimal
value literal needs its own token, produced by its own lexer rule, not
a widened integer token - bracket indices should never accept a
decimal point, and reusing one token for both roles risks `.Sheet1[5.5]`
lexing successfully only to fail confusingly somewhere later instead
of being rejected as the wrong kind of token to begin with. The
fraction is mandatory, not optional, in `NUMBER`'s own grammar - a
bare `5` must keep lexing as the existing `tokInt`, exactly as it does
today inside `.Sheet1[5]`, with `NUMBER` only ever firing for a value
with a decimal point. Making the fraction optional would let `5` lex
as either token depending on the implementation, silently breaking the
"every existing filter's parse result is completely unchanged"
consequence below.

### AST

One new node:

```go
// Assign is "path = value": in-place editing only, never evaluated as
// a read-only query. Target reuses the same Field/Index nodes a read
// path already builds - resolving whether it actually denotes a
// single sheet+cell (the only valid shape) happens once, at
// resolution time (see below), not duplicated into the grammar.
type Assign struct {
	Target Expr
	Value  any // string, float64, bool, or nil - already fully parsed
}
```

`Target` must resolve to exactly `Field{Field{Identity, sheetName},
cellRef}` - a sheet name (dot or bracket-string form, either is fine,
same as everywhere else, ADR-0008) followed by a single A1 cell
reference. A pipe, a whole row/column, a range, or a `records` step
anywhere in `Target` is a clear error at resolution time: none of
those identify one real, writable cell unambiguously.

### The write path doesn't go through `model.Workbook`

In-place editing opens the target file **directly via excelize**, not
through `internal/xlsx.Read`. Resolving `Assign.Target` to a (sheet,
cell) pair is a small, dedicated walk over the AST - not
`eval.Eval`, which is for read-only queries against an already-reduced
`model.Workbook` and has no reason to exist on this path at all.

Concretely: open the file with `excelize.OpenFile`, resolve the target
sheet name against `f.GetSheetList()` the same way the read path's own
`findSheet` (`internal/eval/eval.go`) does - an exact match wins,
otherwise the first case-insensitive match - so `xlq -i
'.sheet1.A1 = "x"'` and the read-only `.sheet1.A1` agree on which
sheet `sheet1` means on a workbook whose sheet is actually named
`Sheet1`. No match at all (case-insensitive included) is a clear error
- v1 never creates a sheet that doesn't exist, only modifies one that
does. Then call `f.SetCellValue(sheet, cellRef, value)` with the already-typed
value, and save. Every bit of the original file `SetCellValue` doesn't
touch - formatting, other cells, charts, merged ranges, anything else
OOXML holds - survives untouched, because the whole file stays in
excelize's own full in-memory representation the entire time; nothing
is ever reduced to `model.Workbook` and reconstructed from it. Setting
a cell beyond the sheet's current used range (e.g. `.Sheet1.Z99` on a
small sheet) is ordinary spreadsheet behavior and needs no special
handling - excelize already extends the sheet for a normal
`SetCellValue` call the same way typing into that cell in Excel would.

### Safety: write to a temp file, verify, then rename over the original

This command overwrites the user's own file with no undo. The save
itself: `SaveAs` to a temporary file in the **same directory** as the
target (so the following rename is same-filesystem) and
**named with an `.xlsx` suffix** - e.g. `xlq-tmp-*.xlsx` via
`os.CreateTemp(dir, "xlq-tmp-*.xlsx")` - not an arbitrary pattern.
excelize's `SaveAs` rejects a path whose extension isn't a supported
workbook type (checked against `f.Path`'s extension, lowercased), so a
temp name without `.xlsx` as its literal extension, including
something like `report.xlsx.tmp` (extension is the last dot segment,
`.tmp`), fails the save outright and the sanity-check re-open below
would never even run. Then re-open that temp file with a fresh `excelize.OpenFile` as a sanity
check that it's actually readable, and only then `os.Rename` it over
the original path. If anything fails before the rename - the edit
itself, the save, or the re-open check - the original file is never
touched. A bug that produces a subtly corrupted file is caught right
there, not discovered the next time the user happens to reopen it,
possibly much later with no other backup.

Before that rename, the original file's mode is read (`os.Stat`) and
applied to the temp file (`os.Chmod`) so the replacement keeps the
original's permissions - `os.CreateTemp` creates its file `0600`
regardless of the target's own mode, and `os.Rename` replaces the
destination inode wholesale, so without this step a successful edit
would silently narrow an originally `0644`/group-readable file to
owner-only `0600`. Ownership (uid/gid) is left as whatever the temp
file already has (the invoking user) - matching it would need
platform-specific, usually privileged, APIs, and xlq already has to
run as whatever user owns the file to read and rewrite it at all, so
this isn't a meaningful new gap the way the mode change is.

`os.Rename` is atomic on Unix (a same-filesystem rename always is,
POSIX-guaranteed) but, per Go's own documentation, **not** guaranteed
atomic on Windows, which this project ships for. The safety model here
doesn't depend on that guarantee, though: the original is only ever
touched by the rename call itself, after every prior step (edit, save,
re-open check, chmod) has already succeeded against the *temporary*
file - so the only Windows-specific risk is the rename call itself
failing partway (e.g. if the target is open and locked by another
process, which Windows - unlike Unix - can refuse to replace at all),
and a failed rename leaves the original exactly as it was, with a
leftover temp file as the only side effect. There's no scenario where
a partial rename leaves the original in a half-written state.

### CLI surface

`-i`/`--in-place` is a bool flag on the root command. With it set, the
filter argument must parse as an `assignment`, not a `pipeline` - a
clear error otherwise ("--in-place requires an assignment filter, e.g.
'.Sheet1.A1 = \"value\"'"), not an attempt to interpret a read-only
filter as a no-op edit. On success, xlq prints nothing to stdout,
matching `yq -i`'s own silence - the side effect (the file changed) is
the result, not something to additionally narrate.

### Explicitly deferred

- `|=` (update from the cell's current value, e.g. `.count |= . + 1`).
  Needs arithmetic/expressions, which ADR-0008 already deferred
  entirely - nothing to decide here until that lands.
- Assigning a whole row, column, or range at once (an array-valued
  right-hand side).
- Resolving `Target` through `records` or a range - both are derived,
  re-indexed views; mapping a write back through either to the
  original cell they came from is new design work of its own.
- Multiple assignments in one invocation.
- Deleting a cell/row/column outright, as opposed to setting it to
  `null`.
- `.csv`/`.tsv`/`.ods` in-place editing - no writer exists for any of
  them yet.
- Any further excelize-specific read-path hardening. Opening the
  target file for in-place editing goes through the same `excelize`
  calls the existing reader does, so it inherits the same read-path
  risk surface already tracked separately (M8, currently parked) -
  this ADR doesn't re-open that.

## Consequences

`xlq -i '.Sheet1.A1 = "hello"' report.xlsx` opens `report.xlsx`,
sets `A1` to `"hello"`, and overwrites the file - every other sheet,
cell, and piece of formatting untouched, because nothing ever passed
through `model.Workbook` on the way. `xlq -i '.Sheet1' report.xlsx`
(a read-only filter) is a clear error, not a silent no-op, since `-i`
requires an assignment.

The grammar gains one real token (`NUMBER`, for decimal value
literals) and one AST node (`Assign`); every existing filter's parse
result is completely unchanged, since `assignment` is a wholly separate
top-level alternative to `pipeline`, never substituted into it.

Implementing this needs: the `NUMBER` lexer rule and `value`/`assignment`
parsing (`internal/filter`), the `Assign` AST node (`internal/filter`),
the target-resolution walk and the open/set/save-to-temp/verify/rename
write path (`internal/xlsx`, next to the existing reader, since it's
`.xlsx`-specific), the `-i` flag plus its assignment-only validation
(`internal/cli`), and one small addition to `internal/eval`: `Eval`
rejects an `Assign` outright, with a clear "use --in-place" message,
rather than falling through to its generic unknown-expression-type
case - the write path itself still never touches `eval` or `model` at
all, exactly as planned; this is purely about giving a user who passes
an assignment filter without `-i` a specific error instead of a
confusing internal-looking one.
