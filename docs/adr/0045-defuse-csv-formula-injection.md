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

Full versioned escaping of every cell everywhere was considered and
rejected as broader than the actual risk: a *header* name or record
*key* comes from the original file's own header row (or a JSON
object's own keys), a narrower and already-more-trusted surface than
free-text data cells - the realistic injection vector is a data
*value*, not a column name. Escaping headers too would just make
ordinary output noisier for no real safety gain.

Tab and CR are sometimes also cited alongside `=`/`+`/`-`/`@` as
formula triggers, but far less consistently across real spreadsheet
software, and a field whose *raw first character* is a literal tab or
CR is a much rarer, harder-to-construct real-world shape than one
starting with `=`. Scoping the fix to the well-established four-
character set avoids claiming protection this change hasn't actually
confirmed it provides.

## Decision

Any string *data value* (not a header name or record key) written by
`--csv`, `--tsv`, or `--table` whose first character is `=`, `+`,
`-`, or `@` gets a literal leading single quote (`'`) prepended before
being written out - the same "force text" convention spreadsheet
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
helper applied at each call site that renders an actual data value -
not the ones rendering a header name, a record key, or a map key.
`docs/reference.md`'s "Output modes" section documents this. A data
cell whose real content happens to start with `=`/`+`/`-`/`@` now
round-trips with one extra leading `'` in `--csv`/`--tsv`/`--table`
output - a real, deliberate behavior change, but a narrow and
well-precedented one.
