# 45. Defuse spreadsheet formula injection in --csv/--tsv/--table output

## Status

Accepted

## Context

A hands-on red-team pass over xlq's attack surface (#153, 2026-11-01)
found that `--csv`, `--tsv`, and `--table` write a string cell's value
into the output exactly as-is, with no escaping. A field whose text
starts with `=`, `+`, `-`, or `@` passes through unmodified - the
well-known [OWASP "CSV Injection"](https://owasp.org/www-community/attacks/CSV_Injection)
class: Excel, LibreOffice, and Google Sheets all auto-interpret a
leading `=`/`+`/`-`/`@` as a formula by default or historically, so
reopening that output in a spreadsheet app runs it.

This matters for xlq specifically, not just in the abstract: its
whole purpose (`docs/index.md`'s own framing) is "take a messy export,
clean it up, hand it off." The attacker-controlled data is exactly
the shape xlq is built to process - a public form export, a support-
ticket spreadsheet, survey free-text - piped through `xlq --csv`/
`--table` and handed to a human who opens it in a spreadsheet app.
JSON output was never going to be reopened that way, so it's
unaffected by anything below.

A header name (`records`' own field names, ADR-0011) or a map key is
no safer to leave unescaped than a data value: both come from the
same file, which isn't trusted in the first place - nothing requires
an attacker's payload to live in a data cell rather than a column
name. The fix below applies to every rendered field uniformly, not
just data values - an earlier version of this decision exempted
headers/keys on the premise that they're a narrower, more-trusted
surface, which doesn't actually hold once the whole input file is the
thing being treated as untrusted.

Tab and CR are sometimes also cited alongside `=`/`+`/`-`/`@` as
formula triggers, but far less consistently across real spreadsheet
software, and a field whose *raw first character* is a literal tab or
CR is a much rarer, harder-to-construct real-world shape than one
starting with `=`. Scoping the fix to the well-established four-
character set avoids claiming protection this change hasn't actually
confirmed it provides.

Escaping headers raises its own correctness requirement: the mapping
from a header's real text to its escaped output must stay injective
(no two distinct real header names ever escape to the same text), or
two genuinely different columns become indistinguishable in the
output's own header row - `records`' lookup by the real, unescaped
name stays correct either way, but a human or downstream program
reading the output can no longer tell the columns apart. Confirmed
directly: escaping only `=`/`+`/`-`/`@` let two distinct real headers,
`"=x"` and `"'=x"`, both escape to `"'=x"`. The fix also escapes an
already-present leading quote (doubling it, the same way a quote
embedded in a quoted CSV field is already doubled), which restores
injectivity - every distinct input still maps to a distinct output.

A genuine number must never be caught by this: `scalarText` renders a
non-string value (a float, a bool) via `json.Marshal`, and that's the
only way the rendered text can ever start with `-` without having
come from a string cell - JSON numbers never start with `+` or `@`.
Confirmed directly against a real negative-number cell (`-12.5`) that
an earlier version of this fix prefixed into the text `'-12.5`,
turning a number into text on every negative value. A bare number -
whatever its original type - can't itself be a working formula
regardless, so the fix must leave anything that's already valid JSON
alone.

## Decision

Every string field rendered by `--csv`, `--tsv`, or `--table` - a
data value, a `records` header name, or a map key alike - whose text
starts with `=`, `+`, `-`, `@`, or an existing leading `'` itself
(for injectivity) gets a literal leading single quote (`'`) prepended
before being written out, *unless* that text is
already valid JSON (`encoding/json`'s own `json.Valid`) - the case
that rules out a genuine number reaching this undefused. The leading-
quote prefix is the same "force text" convention spreadsheet
applications themselves use internally to mark a cell as not-a-
formula. Applied unconditionally, no opt-in flag: matches xlq's
existing secure-by-default posture (`--timeout`/`--max-size`,
ADR-0030/ADR-0031, are also on by default, not something a user has
to discover and enable for themselves). The transformation is narrow
and fully legible on inspection - a cell's real, intended content
starting with a literal `=`/`+`/`-`/`@` is a rare shape in practice,
and the escaped field is still completely valid, correctly
round-trippable CSV/TSV; nothing is silently dropped or corrupted.

## Consequences

`internal/output/table.go` and `internal/output/csv.go` gain a shared
helper applied at every call site that renders a field's text - data
value, header, or key. `docs/reference.md`'s "Output modes" section
documents this. A string cell whose real content happens to start
with `=`/`+`/`-`/`@` now round-trips with one extra leading `'` in
`--csv`/`--tsv`/`--table` output - a real, deliberate behavior
change, but a narrow and well-precedented one; a genuine number is
exempt regardless of sign.
