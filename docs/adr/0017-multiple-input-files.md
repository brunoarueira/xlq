# 17. Multiple input files in one invocation: map, not slurp

## Status

Accepted

## Context

Issue #33 asks for comparing two exports in one invocation (e.g.
`xlq '.DISCOVERY' old.xlsx new.xlsx`) and names two candidate designs
without choosing between them: accepting more than the current exactly
two positional args, or a jq-style `--slurp` flag combining every
file into one value the filter addresses as an array of workbooks.
These aren't just two spellings of the same feature - `--slurp` needs
new grammar (something has to let a filter say "workbook 0" vs
"workbook 1," which nothing in ADR-0008's grammar does today, since
every filter currently assumes it starts from exactly one workbook),
while running the *same*, *unmodified* filter once per file and
collecting the results doesn't touch the grammar at all. Asked the
user directly given the real divergence between the two: map-the-
filter-over-each-file was chosen, specifically because it needs zero
grammar changes and directly serves the stated diff/compare use case
(each file's result sits at its own array index, ready to compare).

The exactly-two-positional-args shape itself was never an ADR
decision to begin with - ADR-0004 explicitly deferred the whole `xlq
'<filter>' <file>` invocation to a later ADR once the grammar existed,
and no ADR since has pinned the argument *count*; `cobra.ExactArgs(2)`
is simply how the root command's `RunE` was implemented once ADR-0008
gave it a grammar to invoke. This ADR is the first to actually decide
the argument-count question, rather than changing a prior ADR's
decision the way ADR-0014 and ADR-0016 each did.

## Decision

### `xlq '<filter>' <file>...` - one or more files, same filter, one or more results

`root.go`'s `Args` validator becomes `cobra.MinimumNArgs(2)` (filter
plus at least one file), replacing `cobra.ExactArgs(2)`. For exactly
one file, behavior is **completely unchanged** - the result is printed
bare, exactly as today, not wrapped in a one-element array; every
existing invocation, test, and README example keeps meaning exactly
what it already means. For two or more files, `xlq` evaluates the
same parsed filter against each file independently, in the order the
files were given, and the top-level result becomes a JSON array of
each file's own result: `[result_for_file_1, result_for_file_2, ...]`.
This is a map, not a reduction or a diff - `xlq` doesn't compare the
elements itself, it just puts each file's own answer at that file's
own array index, ready for the user (or `jq` piped after it, or a
script) to compare `.[0]` against `.[1]`.

A file that fails to open or parse aborts the whole invocation
immediately (fail-fast, consistent with today's single-file behavior)
with an error naming which file and its position (`file 2 (new.xlsx):
<underlying error>`) - no partial results, and no new shape (like a
per-element error marker) for a caller to have to handle.

### `--compact` compacts each file's result individually, before the array exists

`output.Compact`'s existing `[]any` case treats a plain array as "a
row or column of scalars, drop nil entries" - exactly correct for
today's single-file row/column results, but wrong for a multi-file
results array: dropping a nil result would shrink the array and break
the one-to-one correspondence between array index and file argument
that's the entire point of this feature. `output.Compact` itself isn't
changed at all; `--compact`, when more than one file is given, is
instead applied to *each file's own result* before that result goes
into the array, not to the array afterward. The array's length and
index-to-file correspondence are invariant under `--compact`,
regardless of file count.

### `--table`/`--csv`/`--tsv` are a clear error for more than one file

Both `output.Table` and the CSV/TSV path already have a case for a
plain `[]any` - "a single row or column," one line per element - and
a multi-file results array is also, structurally, a plain `[]any` to
Go: nothing distinguishes "the user's filter legitimately produced a
column of scalars" from "this is N files' worth of arbitrary,
possibly complex results" once code execution has moved on from
printing the right JSON array. Without a guard, `--table` would
silently flatten each file's entire result (even a large grid or
records array) into one mangled table cell via JSON stringification,
and `--csv`/`--tsv` would do the same per CSV field - wrong output
with no error, not caught by any existing test because this shape
never existed before. So the guard lives one level up, before
`output.Table`/`CSV` is ever called: more than one file combined with
any of `--table`/`--csv`/`--tsv` is a `runFilter`-level error ("these
don't support multiple files - no single tabular shape for N
independent results; use JSON, or run xlq once per file"), matching
ADR-0013's own precedent for rejecting a whole workbook's CSV output
("no single tabular shape") rather than guessing at a flattening.

This checks each flag's *effective*, config-merged value
(`GetBool`, after ADR-0015's `mergeConfigDefaults` has already run),
not just what was explicitly typed - a user with `table: true` in
their config file hits this guard on a plain multi-file invocation
exactly as if they'd typed `--table` themselves, since from
`runFilter`'s own position the two are indistinguishable (ADR-0015's
entire point). `--table=false` (or `--csv=false`/`--tsv=false`) is the
existing, already-designed escape hatch for that specific invocation:
an explicit flag of any value marks it `Changed`, which
`mergeConfigDefaults`'s own `mergeBool` already skips applying a
config default onto - no new mechanism needed here, just worth
calling out since it's the answer to "I have a table default, how do
I diff two files in JSON just this once."

### `-c`/`-r` need no change

`-c` (`compactJSON`, one-line JSON) only affects how the final value
- scalar or array, one file or many - gets marshaled; nothing about it
assumes a particular shape. `-r` (`rawOutput`) already only unwraps a
result that's a bare string, printing anything else as JSON - for two
or more files the top-level result is always an array, never a
string, so `-r` naturally falls through to ordinary JSON printing, the
same way it already does today for any single-file result that isn't
a string (e.g. `-r '.Sheet1'` on a grid). No special-casing needed for
either flag.

### `-i`/`--in-place` stays exactly-one-file

`-i` already requires `len(args) == 2` in spirit (filter + the one
file it edits); made explicit now that more than two args is otherwise
valid. Editing multiple files in one invocation - identically or
otherwise - is new, real design work ADR-0014 never scoped and this
ADR doesn't either; `-i` with more than one file is a clear error.

### `--input-format`/`--delimiter` apply uniformly to every file

No per-file override syntax - both flags already resolve to one
value per invocation (an explicit override, or empty meaning
"detect/default"), and that one value is used for every file's own
`input.Read` call. A mix of formats across files (e.g. diffing an
`.xlsx` against a `.csv`) is supported by this design: when *neither*
flag resolves to a non-empty/non-zero value, each file's format is
still detected independently from its own extension, exactly as
`input.Read` already does for the one file a single invocation can
name today - this ADR doesn't change that per-file detection logic at
all, just lets more than one file reach it in the same invocation.

That "neither flag set" qualifier matters in exactly the same way the
table/csv/tsv guard's does: these two also resolve to their
config-merged, effective value, not just what was explicitly typed. A
persistent config `input-format` default forces *every* file in the
set to that one resolved format regardless of its own extension - what
happens to a file that genuinely isn't that format then depends on its
actual bytes, not on this ADR: it might be silently misread as
something plausible-looking but wrong, or the chosen format's own
reader might reject it outright with a read/parse error, since
`input.Read` has no way to tell "this format came from a default" from
"the user meant this file specifically" either way. A persistent
config `delimiter` default interacts with whichever format each file
*resolves* to, not its extension - `input.validateDelimiter` already
rejects a non-zero delimiter outright for a file that resolves to
`xlsx`/`ods` (no delimiter concept to override there), whether that
resolution came from the file's own extension or from an `input-format`
override forcing it there; a delimiter default and an `input-format`
override of `csv`/`tsv` together face no such conflict, since nothing
then resolves to `xlsx`/`ods` at all, regardless of a file's actual
extension. `--input-format ""`, explicitly passed, clears an
`input-format` default for one invocation (the same `Changed`-based
escape hatch as `--table=false`); `delimiter` has no equivalent -
`inputDelimiter` already treats an explicit empty value as a distinct,
deliberately-rejected error case (established back when `--delimiter`
first shipped, #28), not a reset signal. A persistent `delimiter`
default and a multi-file invocation that includes any file resolving
to `xlsx`/`ods` are simply incompatible unless the config file itself
is edited first.

### `sheets` is unaffected

`sheets <file>` keeps `cobra.ExactArgs(1)` - it already answers a
different question (what sheets does *this* file have) that multiple
files don't change the shape of; nothing in #33 asks for a multi-file
`sheets`.

### No new flag to opt in

Passing more than one file is what triggers this, matching the
issue's own example exactly - no `--slurp`-equivalent flag exists
(there's nothing to slurp; see the grammar-vs-map distinction above),
and no separate flag is needed to request "map over N files" instead
of "read one file," since the file count itself already says which
one the user means.

## Consequences

`xlq '.Sheet1 | records' old.xlsx new.xlsx` prints a two-element JSON
array, each element that file's own records array - comparing them is
exactly as easy as it would be for any other two JSON values, with no
new xlq-specific diff mechanism to learn. Every single-file invocation
that exists today, including every README example and existing test,
keeps working identically - this ADR only adds behavior for a file
count nothing could produce before (`cobra.ExactArgs(2)` made two-or-
more files impossible to pass at all).

Implementing this needs: `root.go`'s `Args` and `RunE` (the argument
count check and threading `[]string` instead of one `path` through to
`runFilter`/`runInPlace`), `runFilter` itself (the per-file
read/eval/compact loop and the array-wrapping decision at N==1 vs
N>1), and the new `--table`/`--csv`/`--tsv` guard - no changes needed
to `internal/eval`, `internal/filter`, `internal/model`, or
`internal/output` at all, since every one of those keeps operating on
exactly one file's result at a time, the same way they do today.
