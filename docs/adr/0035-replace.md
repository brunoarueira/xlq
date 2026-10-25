# 35. `replace(old, new)`: literal substring replacement

## Status

Accepted

## Context

Issue #124, last of three PRs under it (ADR-0033 shipped `upper`/
`lower`/`trim`, ADR-0034 shipped `snake_case`/`camel_case`). A common
remaining case from #124's motivating examples: normalizing a field
before writing it to a database or calling an API - stripping a
thousands separator (`"1,234" -> "1234"`, before `tonumber`), or
swapping a delimiter a downstream system doesn't accept.

jq has no direct equivalent: `gsub`/`sub` are regex-based, and
`ltrimstr`/`rtrimstr` only handle a fixed prefix/suffix, not an
arbitrary substring anywhere in the string. A plain literal
substring-for-substring replacement, with no regex involved at all, is
this project's own addition - deliberately not regex-based, since
every motivating case is a fixed literal (a specific delimiter
character, a specific separator), and a regex engine is a much bigger
surface (syntax to document, a new failure mode for a malformed
pattern) for a need none of the motivating cases actually have.

## Decision

### Two literal string arguments, the ADR-0022 mechanism - not a filter-expression argument

`replace(old, new)` takes two **literal** strings, parsed the same way
`todate(format)`'s single literal argument already is (ADR-0022) - not
a filter-expression argument the way `map`/`select`/`group_by`'s
argument is (ADR-0025). There's no motivating case here for either
`old` or `new` to be computed per-element rather than fixed at the call
site, the same reasoning that kept `todate`'s format argument literal.

### Every occurrence is replaced, not just the first

Matches Go's own `strings.ReplaceAll` (and Python's `str.replace`
without a count) rather than JavaScript's plain `String.prototype.
replace` (first occurrence only, unless using a global regex or
`replaceAll`) - languages genuinely disagree on this default, so it's
called out explicitly rather than left to whichever standard library
function happened to back the implementation. Replacing every
occurrence is the more useful default for the motivating cases (a
thousands-separator comma can appear more than once in one number;
a delimiter swap needs every instance changed, not just the first).

### `old` must be a non-empty string; `new` must be a string

Both arguments error if not a string. `old` additionally errors if
empty: Go's `strings.ReplaceAll` with an empty `old` inserts `new`
between every rune of the input (including before the first and after
the last) - confirmed directly (`strings.ReplaceAll("abc", "", "-")` is
`"-a-b-c-"`) - which is never what any of #124's motivating cases want
and would be a silently strange result for a typo'd empty argument, so
it's rejected outright rather than passed through to Go's own
behavior.

### The current input must be a string; errors, not propagates-as-null, on anything else

Same rule every other string builtin here follows (`upper`/`lower`/
`trim`, ADR-0033; `snake_case`/`camel_case`, ADR-0034) - `null`, a
number, and a boolean are all errors, not a silent no-op.

## Consequences

`.Sheet1.A1 | replace(",", "")` strips every comma before, e.g.,
piping into `tonumber`. `.Sheet1 | records | map(.Delimiter | replace
(";", "|"))` swaps a delimiter across a whole column. This closes out
#124's three-PR string-builtins batch; #125 (`headers(f)`) is unblocked
now that all of it has landed.
