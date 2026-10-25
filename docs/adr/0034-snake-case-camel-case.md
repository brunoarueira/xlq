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

Rule 2 is itself suppressed for one specific shape: a lone lowercase
`s` that ends the whole word-rune run - a plural acronym. Applied
naively, rule 2 turns `URLs` into `UR`/`Ls` and `IDs` into `I`/`Ds`,
since the rule only looks one character ahead and a single trailing
`s` satisfies "an uppercase run followed by a lowercase letter" just
as well as a real word would. A real word is practically never just
`s` on its own, so treating a *lone, run-ending* `s` as the plural
suffix instead - keeping it attached to the acronym - fixes both
(`URLs` and `IDs` each stay one word; `RelatedURLs` correctly splits
into `Related`/`URLs`), without touching rule 2's normal behavior:
`URLString` still splits into `URL`/`String`, since the letter right
after that run is `t`, not a lone `s`. Found in review (cubic), not
caught while designing the original two rules - confirmed both the bug
and the fix against the real build before changing anything.

Deliberately not handled, as a result of staying this narrowly scoped:
a plural acronym fused with another capitalized word with no separator
at all (`APIsAvailable`) still splits oddly (`AP`/`Is`/`Available`) -
genuinely ambiguous without a dictionary of known acronyms, and rare
enough in real header/field names not to chase further here.

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

### A combining mark counts as a word rune, so it isn't dropped as a separator

Also found in review (cubic): a combining mark (Unicode category
`Mn`/`Mc` - an accent or vowel sign stored as its own code point,
rather than fused into a single precomposed character) isn't a letter
or a digit, so the original rule - anything that isn't a letter or
digit is a separator - treated one as a separator too. That's wrong in
two ways at once: the mark itself is silently dropped (never added to
any word), and a word gets split in the middle of what should be one
character, exactly where the mark sits. This matters beyond rare
NFD-decomposed Latin text (`café` stored as `e` + U+0301) - Devanagari,
Thai, and other Indic scripts routinely spell ordinary words with
combining marks as a normal part of their encoding, not an edge case:
`नमस्ते` has two (`U+094D`, `U+0947`), and the original rule would have
mangled it into `नमस`/`त` with both marks gone, not just miscased. A
mark is now a word rune too, confirmed via the real implementation
(not just reasoned about) to ride along with whatever word it's
already in: it's never itself upper/lower, so it can't trigger rule 1
or rule 2 on its own.

This doesn't make case-boundary detection decomposition-aware in
general: a mark sitting *between* a base letter and a following
capital blocks rule 1 from firing there, since the rule looks at the
literal previous rune, which is now the mark, not the base letter
before it (`"caféName"`, decomposed, stays one word - unlike the
precomposed `"CaféName"`, which splits into `Café`/`Name`). Fixing
that would need tracking each word-rune run's last *base* character
separately from the literal previous rune - not done here, since the
bug being fixed is data loss (marks disappearing, words splitting
mid-character), not imperfect recognition of a boundary that
decomposition happens to obscure.

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
