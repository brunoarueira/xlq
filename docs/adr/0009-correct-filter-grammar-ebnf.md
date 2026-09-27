# 9. Correct ADR-0008's path grammar

## Status

Accepted

## Context

Implementing #6 (the lexer/parser) surfaced a transcription bug in
ADR-0008's EBNF, not a change of decision - every example in that ADR
(`.Sheet1`, `.Sheet1.A1`, `.["Q1 Report"]`, ...) and its evaluation-
semantics table are correct and unchanged; only the compact grammar
block itself was wrong.

As written, ADR-0008 has:

```
path     = "." suffix*
suffix   = "." IDENT
         | "[" STRING "]"
         | "[" INT "]"
```

`path`'s own leading `"."` and `suffix`'s dot-form `"." IDENT` are two
*separate* `"."` tokens. Read literally, `.Sheet1` would need to lex as
two dots followed by `Sheet1` - it only has one - so this grammar can't
actually derive any of the ADR's own examples. The bug was caught by
the parser's own test suite (`.Sheet1` failed to parse) before it ever
reached `main`.

Per `CONTRIBUTING.md`, an accepted ADR isn't edited - a later decision
that changes course gets a new ADR instead. This isn't a changed
decision (nothing about identity, A1 addressing, or pipe semantics is
different), but it's still ADR-0008's own text that's wrong, so the
fix goes here rather than as a silent edit to 0008.

## Decision

Replace ADR-0008's `path`/`suffix` production with:

```
path = "." ( head tail* )?
head = IDENT
     | "[" STRING "]"
     | "[" INT "]"
tail = "." IDENT
     | "[" STRING "]"
     | "[" INT "]"
```

The segment immediately after the anchor `"."` (`head`) doesn't need a
`"."` of its own - the anchor dot already serves as the separator, the
same way jq's own `.foo` is one dot token followed by one identifier
token, not two dots. Every segment after that (`tail`) needs its own
leading `"."` for the identifier form; the bracket forms never need
one, whether they're the first segment or a later one. `.` alone (no
`head` at all) is the identity filter, unchanged from ADR-0008.

Nothing else in ADR-0008 changes: the AST (`Identity`/`Field`/`Index`/
`Pipe`), the evaluation-semantics table, and everything marked as
deferred all stand as written there.

## Consequences

None functionally - `internal/filter`'s parser already implements this
corrected grammar (that's how the bug was caught), and every example
in ADR-0008 parses as that ADR says it should. Anyone reading
ADR-0008's EBNF block on its own should also read this ADR; a future
reader assembling the full picture of the v1 grammar needs both.
