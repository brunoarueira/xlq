# 34. `snake_case`, `camel_case`: word-boundary case conversion

## Status

Accepted

## Context

Issue #124, second of three PRs under it (ADR-0033 shipped
`upper`/`lower`/`trim` first). Unlike those three, `snake_case`/
`camel_case` have no jq equivalent at all to verify against - jq has no
built-in word-boundary case conversion - so the word-splitting rules
below are this project's own design, not a port.

The motivating cases from #124 are header cleanup (`PriorityOrder` ->
`priority_order`) and field normalization before writing to a database
or API that expects a specific casing. Both need the same underlying
operation: split a string into words, then rejoin them differently.
Rather than two independent implementations, both builtins share one
`splitCaseWords` helper and differ only in how they rejoin its output -
the design decision below is really about what counts as a word
boundary, not about `snake_case` or `camel_case` individually.

## Decision

### A word boundary is either an explicit separator or a case/digit transition

Any run of characters that's neither a letter nor a digit (space, `-`,
`_`, any other punctuation) is a separator and never appears in the
output. Within a run of letters/digits, with no explicit separator,
two further rules insert a boundary:

1. **Before an uppercase letter that directly follows a lowercase
   letter or a digit** - `priorityOrder` splits at `O` into
   `priority`/`Order`; `q3Report` splits at `R` into `q3`/`Report` (the
   digit counts as "lowercase-like" for this rule, so a letter-to-digit
   transition itself is *not* a boundary - `q3` stays one word, not
   `q`/`3`).
2. **Before the last letter of an uppercase run, when that run is
   immediately followed by a lowercase letter** - `HTTPServer` splits
   at the second `S` into `HTTP`/`Server`. Without this rule, rule 1
   alone never fires inside an all-uppercase run (every letter in it is
   upper-after-upper, not lower-after-upper), so an acronym followed by
   a word would never split at all (`HTTPServer` would stay one word)
   or, naively splitting every uppercase letter, split one letter at a
   time (`H`/`T`/`T`/`P`/`S`/`erver`) - neither is useful. A standalone
   acronym with nothing lowercase after it (`URL`) triggers neither
   rule and stays one word, matching the common convention (`URL` ->
   `url`, not `u`/`r`/`l`).

Verified by hand-tracing both rules against every case above, plus
`ACCOUNT_NAME` (splits only on the explicit `_`, giving `ACCOUNT`/
`NAME` - neither rule fires inside a fully-uppercase word with no
following lowercase run), `already_snake_case` (idempotent - already
split on its own underscores), and `V2Format` (`V2`/`Format` - an
upper-to-digit transition, `V`->`2`, triggers neither rule, so the
digit stays attached to the letter before it, consistent with rule 1
only firing *into* an uppercase letter, never out of one).

### Unicode-aware, not ASCII-only - consistent with ADR-0033

`unicode.IsUpper`/`IsLower`/`IsDigit`/`IsLetter` and
`unicode.ToUpper`/`ToLower`, not an ASCII-only check - the same
divergence from jq's own ASCII-only `ascii_upcase`/`ascii_downcase`
ADR-0033 already made, for the same reason (spreadsheet data routinely
isn't ASCII-only). `CaféName` splits into `Café`/`Name` (the accented
`é` is a lowercase letter, so the `é`->`N` transition is rule 1).

### Rejoining: `snake_case` lowercases and joins with `_`; `camel_case` lowercases, capitalizes every word but the first, and concatenates

`snake_case("PriorityOrder")` is `"priority_order"`. `camel_case
("priority_order")` is `"priorityOrder"` - the first word is always
fully lowercased (even a single all-uppercase word: `camel_case
("URL")` is `"url"`, not `"uRL"` or `"Url"`), matching the common
convention that camelCase's first word carries no capital at all.

An input with no letters/digits at all (only separators, or empty)
produces an empty string from both - there are no words to join.

### Errors, dispatch: same shape as ADR-0033

Both error on a non-string input, including `null` - no
"propagates as null" exception, the same rule `upper`/`lower`/`trim`
already follow (ADR-0033) and the same real-jq-inspired philosophy
`tonumber`/`todate` established first (ADR-0022). Both are
zero-argument `Builtin` nodes, registered in `internal/filter`'s
`builtinNames` and dispatched from `internal/eval`'s `applyBuiltin` -
no new grammar.

## Consequences

`.Sheet1.A1 | snake_case`, `.Sheet1 | records | map(.Status | camel_case)`,
and (once #125 lands) `headers(snake_case)` all work the same way
`upper`/`lower`/`trim` already do. `replace(old, new)` is the last
piece tracked under #124, with its own ADR and PR.

Out of scope, as decided (not merely deferred): a configurable acronym
dictionary (so `ID`/`URL`/`HTTP` are recognized as acronyms even when
followed immediately by another uppercase letter with no lowercase cue
at all, e.g. disambiguating `IOError` as `IO`/`Error` rather than
`I`/`O`/`Error` - the current rules already handle this one correctly
by rule 2, but a case like `ID3` immediately followed by another
all-caps word, with no digit or lowercase letter anywhere to anchor a
split, is genuinely ambiguous and not solved here); no locale-specific
casing (Turkish `İ`/`I` dotting, etc.) - `unicode.ToUpper`/`ToLower`
use Go's default, locale-independent case mapping, the same scope
`upper`/`lower` already settled for (ADR-0033).
