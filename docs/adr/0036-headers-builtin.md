# 36. `headers(f)`: renaming `records`' keys

## Status

Accepted

## Context

Issue #125, blocked on #124 (the string builtins `headers(f)` is meant
to carry) until all three of its PRs landed. `records` (ADR-0011)
always keys each row object by the sheet's literal row-1 header text -
there's no way to normalize those keys (`Account` -> `account`,
`PriorityOrder` -> `priority_order`) without editing the source file's
header row directly.

Two shapes were considered and rejected before settling on this one -
already recorded in the issue and in docs/reference.md's "String
builtins" section, repeated here since this is where the decision
actually gets made permanent:

- `records(snake_case)`, reusing `map`/`select`/`group_by`'s own
  single-filter-expression-argument mechanism (ADR-0025) directly.
  Rejected on devx grounds: unlike `group_by(.Category)` or
  `todate(format)`, nothing about `records`'s own name suggests what
  an argument would transform - rows? every cell? just headers?
- `header_snake_case`-style prefixed builtins, one per string builtin
  from #124. Rejected: it would need a prefixed twin for every
  existing and future string builtin, duplicating the whole builtin
  surface for no benefit over the plain versions.
- `records(headers: f)`, a keyword/named argument. Rejected along with
  the above: nothing else in this grammar has keyword arguments, and
  inventing that syntax for a single, then-hypothetical use wasn't
  justified.

## Decision

### `headers(f)`, a new filter-expression-arg builtin, usable standalone or nested inside `records(...)`

`headers` joins `map`/`select`/`group_by` in `filterArgBuiltins`
(ADR-0025's mechanism) - no new grammar for `headers` itself. Its
contract: evaluate `f` once per header-row string (after the sheet's
own column-letter fallback for an unnamed column - ADR-0011), with `.`
rebound to that string, and either:

- **Standalone**, as an ordinary pipeline step: `.Sheet1 | headers
  (snake_case)` returns the transformed names as a plain array -
  useful for previewing a transform before committing to it.
- **Nested inside `records`'s own argument**: `.Sheet1 | records
  (headers(snake_case))` uses the transformed names as the resulting
  objects' keys.

### `records` gets bespoke parsing, not `filterArgBuiltins` membership

`records` can't simply join `filterArgBuiltins` the way `headers`
does: that mechanism (`parseFilterArgBuiltin`) always requires a
parenthesized argument, but bare `records`/`records()` (ADR-0011/
ADR-0022) must keep working with no argument at all. `records` keeps
its own `parseRecords`, a new function: no parens (or empty parens) is
still the existing zero-argument `Builtin{Name: "records"}`; a
non-empty parenthesized argument is now parsed the same way `map`/
`select`'s is (`parseFilterArgExpr`), producing `FilterArg{Name:
"records", Arg: <parsed expression>}` - the exact same AST node
`headers`/`map`/`select`/`group_by` already use for their own single
filter-expression argument, reused rather than inventing a new type
for it.

`parseStep` itself never reaches `builtinNames` for `records` anymore -
its own dedicated case there, above, intercepts the name first. But
`records` stays *in* `builtinNames` (found missing in an earlier draft,
in review - cubic): `parseOperand`, the separate function that parses a
comparison's right-hand operand (`.A1 == records`, ADR-0024), still
checks that map directly, and `records` as a bare comparison operand
is a pre-existing capability from before this ADR, not something to
regress. `records("x", 5, true, false, null)` (an existing parser test
demonstrating the shared literal-value grammar in general, using
`records` only as a convenient vehicle, per ADR-0022's own context) no
longer parses *as a pipeline step* - the demo moved to `tonumber`, an
unaffected literal-arg builtin, since the test was never really about
`records` specifically. It would still parse via `parseOperand` (a
comparison operand's own literal-argument grammar is unchanged), for
whatever that's worth - always an eval-time error regardless, the same
as before this ADR.

### `records`' argument must be `headers(...)` - checked at eval time, not parse time

`parseRecords` accepts *any* filter expression as `records`' argument,
the same way `parseFilterArgBuiltin` does for `map`/`select`/
`group_by` - the parser has no way to know `records`' own argument
contract is narrower than "any filter expression," the same split
every other builtin already has between parse-time shape and eval-time
meaning (ADR-0022's own "Argument count/type validation happens at
eval time, not parse time").
`records(.A1)` and `records(tonumber)` both parse fine, then fail at
eval time with a clear "argument must be headers(...)" error - verified
directly against the real build.

### Resource limits apply uniformly, including to standalone `headers(f)`

`headers(f)` and `records` share one single-pass sheet scan
(`sheetHeaderScan`, extracted from what was previously all of
`sheetRecords`), which checks the same `maxDenseCells` budget
(ADR-0030/ADR-0031) regardless of which builtin is asking - so
`.Sheet1 | headers(f)` alone can't be used to read an unbounded amount
of an untrusted file just because it only returns a short array of
names. The column count this scan determines also matches what
`records` itself would use (a later, wider data row can introduce a
column the header row never had) - confirmed with a sheet whose data
row is wider than its header row: the extra column's letter fallback
(`B`, `C`, ...) gets the same transform a real header name would.

### Duplicate-name checking moved out of `sheetHeaders`, now `records`-only

Before this ADR, `sheetHeaders` itself rejected two columns resolving
to the same name - a hard requirement once names become an object's
keys. `headers(f)` alone has no such requirement (its output is a
plain array, which can legitimately contain the same string twice), so
the check (`checkUniqueHeaders`) is now a separate step `sheetRecords`
calls after `sheetHeaders` returns, not something `sheetHeaders` does
unconditionally. Confirmed: `.Sheet1 | headers(lower)` on a sheet with
both `account` and `Account` as real headers returns `["account",
"account"]` without error, while `.Sheet1 | records(headers(lower))`
on the same sheet errors with the existing duplicate-header message -
a real behavior difference between the two, not an oversight, since
only one of them needs the output to be keys.

## Consequences

`.Sheet1 | headers(snake_case)` and `.Sheet1 | records(headers
(snake_case))` both work, composing with any #124 string builtin (or
any filter expression at all, though only a string-producing one makes
sense - anything else errors, e.g. `headers(. == "Account")` producing
a boolean errors with "transform must produce a string"). This closes
out #125 and, with it, the whole arc from #124's original "how do we
normalize headers" question.
