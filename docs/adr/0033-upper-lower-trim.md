# 33. `upper`, `lower`, `trim`: jq-parity string builtins

## Status

Accepted

## Context

Issue #124: generic string manipulation builtins, motivated by two
real cases discussed on the issue - normalizing a field (e.g. a
`Status` column) before writing it to a database or calling an API
that expects a specific casing, and cleaning up spreadsheet header
text. Issue #125 (`headers(f)`, blocked on this one) is what lets any
of these apply to `records`' keys specifically; this ADR is only about
the builtins themselves, usable anywhere a value flows through the
filter (`.Status | lower`, `.Sheet1 | records | map(.Status | lower)`,
or nested inside `headers(...)` once #125 lands).

Issue #124 proposed a larger set in one batch; this ADR covers only the
three with a direct, unambiguous jq equivalent to verify against -
`snake_case`/`camel_case` have no jq precedent at all and need their
own design decisions (word-boundary splitting, acronym handling), so
they get a separate ADR and PR, the same split ADR-0025/ADR-0026/
ADR-0027 already made between `map`/`select`, `sum`/`count`/`avg`, and
`group_by`. `replace(old, new)` likewise gets its own, smaller ADR.

Verified against real jq (1.8.2) before deciding anything below, the
same discipline `tonumber`/`todate` were held to (ADR-0022, ADR-0023):

- `jq 'ascii_downcase'`/`'ascii_upcase'` on a number, `null`, or a bool
  all error: `"explode input must be a string"` (both route through
  jq's own `explode`/`implode` internally). A string passes through
  case-converted.
- `jq 'trim'` (jq 1.7+) strips only leading/trailing whitespace,
  preserving internal runs (`"  Hello   World  " | trim` is
  `"Hello   World"`, not `"Hello World"`) and errors on a non-string
  input: `"trim input must be a string"`.

## Decision

### Names: `upper`/`lower`, not jq's `ascii_upcase`/`ascii_downcase`

jq's own naming is about a real distinction jq draws (ASCII-only case
folding, with no non-ASCII alternative offered at all) - this project
has no such distinction to name around, and spreadsheet data routinely
isn't ASCII-only (accented names, non-English headers). `upper`/`lower`
use Go's `strings.ToUpper`/`strings.ToLower`, which are Unicode-aware
by default, not ASCII-limited - a deliberate divergence from jq's own
scope, not an oversight. `trim` keeps jq's own name and exact semantics
(`strings.TrimSpace`, leading/trailing only) unchanged, since there's
no equivalent reason to diverge there.

### Errors, not pass-through, on a non-string input - matching jq and `tonumber`'s own precedent

All three error on anything that isn't a string, including `null`
(no "null propagates" exception) - the same shape `tonumber` already
chose (ADR-0022) and the same behavior real jq has for all three.
A spreadsheet cell holding a number or bool piped into `upper` is a
filter mistake worth surfacing immediately, not a silent no-op.

### Dispatch: zero-argument builtins, no new grammar

All three are `Builtin{Name: ..., Args: nil}` nodes added to
`internal/filter`'s existing `builtinNames` set and
`internal/eval`'s `applyBuiltin` switch (ADR-0022) - the identical
shape `tonumber` already has. No parser change beyond the one-line
`builtinNames` registration.

## Consequences

`.Status | lower`, `.A1 | upper`, `.B2 | trim` all work today. Combined
with `map` (ADR-0025): `.Sheet1 | records | map(.Status | lower | trim)`
normalizes a whole column before, e.g., piping the result into another
tool. `snake_case`/`camel_case` and `replace(old, new)` are tracked as
separate, follow-up ADRs/PRs under the same issue (#124); `headers(f)`
(#125) is blocked on at least this ADR landing, not on the other two.
