# 22. Extensible builtins: `name` or `name(literal, ...)`

## Status

Accepted

## Context

Issue #85 (M7): `records` (ADR-0011) is xlq's only builtin today, and
it's hard-coded as a special case in the parser:

```go
func (p *parser) parseStep() (Expr, error) {
	if p.peek().kind == tokIdent && p.peek().text == "records" {
		p.advance()
		return Records{}, nil
	}
	return p.parsePath()
}
```

#37 (`tonumber`) and #42 (explicit date parsing with a format
argument) both need a new named builtin, and there's no general way to
add one today without repeating this same special-casing - a new
`Records`-shaped AST type, a new hard-coded keyword check in
`parseStep`, and a new case in `eval`'s own switch - per builtin.

This is deliberately **not** the full `map`/`select` design ADR-0008
also deferred (tracked separately, #86): that needs a *filter
expression* as an argument (`select(.Likes > 100)`), which drags in
comparison operators and arithmetic (also deferred, separately, by
ADR-0008) and is a substantially bigger, recursive grammar change.
What #37/#42 actually need is smaller - a builtin that takes zero or
more **literal** arguments (a string, a number, `true`/`false`/`null` -
reusing the exact value grammar ADR-0014 already added for assignment,
never a nested filter expression):

- `tonumber` - zero arguments: `.Sheet1.B2 | tonumber`.
- An explicit date-parsing builtin (#42) - at least one string-literal
  argument, the format: `.Sheet1.B2 | todate("M/D/YYYY")`.

Verified before deciding anything below: `parseValue` (`internal/
filter/parser.go`, added by ADR-0014 for assignment's right-hand side)
already parses exactly this literal grammar - `STRING | NUMBER | INT |
"true" | "false" | "null"` - and already returns the same Go value
shape (`string`, `float64`, `bool`, `nil`) a builtin's own arguments
need. No new value-literal parsing logic is needed, only a place to
call the existing one from.

## Decision

### Syntax: a bare name for zero arguments, `name(arg, ...)` otherwise - matching jq exactly

jq itself writes a niladic builtin bare, with no empty parens
(`tonumber`, `length`, `keys`), and a builtin that takes arguments with
a parenthesized, comma-separated list. This project already follows jq
syntax wherever the shape already exists (ADR-0020's slice bounds,
ADR-0021's letter-fallback semantics); the same reasoning applies
here, which also means `records` needs no syntax change at all to move
onto this mechanism:

```
step     = path | builtin
builtin  = IDENT ( "(" ( value ( "," value )* )? ")" )?
```

(`value` is ADR-0014's own production, unchanged: `STRING | NUMBER |
INT | "true" | "false" | "null"`.)

### Three new lexer tokens: `(`, `)`, `,`

```go
const (
	...
	tokLParen
	tokRParen
	tokComma
)
```

Each needs its own `token.describe` case (`"("`, `")"`, `","`) -
skipped once already on this project (ADR-0020's `tokColon`, caught in
review on PR #81) and not worth repeating: without it, a malformed
builtin call's parser error would name an empty string instead of the
actual missing punctuation.

### One AST node replaces `Records`, not a second one alongside it

```go
// Builtin is "name" or "name(arg, ...)" (ADR-0022): a named operation
// with no Base of its own (it operates on the pipeline's current
// value, like Identity) - valid only as a pipeline step after a "|",
// the same restriction Records had. Args are always literals (ADR-
// 0014's value grammar), never a nested filter expression - that's
// map/select's own, separate, larger design (#86).
type Builtin struct {
	Name string
	Args []any
}

func (Builtin) exprNode() {}
```

`records` migrates onto this (`Builtin{Name: "records"}`) rather than
keeping its own bespoke type alongside the new general one - it has no
syntax or semantics `Builtin` doesn't already cover, and keeping both
would mean two ways to represent the exact same thing. `filter.Records`
is removed; `internal/eval`'s `case filter.Records:` becomes a `"records"`
case inside the new builtin dispatch (below).

### Parsing: any bare `IDENT` reaching `parseStep` is unambiguously a builtin reference

`parseStep` is only ever called for a pipeline step after a `|`
(`Parse`'s own first segment always goes through `parsePath`, which
requires a leading `.`). An identifier with no leading dot can
therefore never be a valid path start - so seeing one here already
means "builtin," with no lookahead or special-casing needed to tell it
apart from anything else:

```go
func (p *parser) parseStep() (Expr, error) {
	if p.peek().kind == tokIdent {
		name := p.advance().text
		args, err := p.parseBuiltinArgs()
		if err != nil {
			return nil, err
		}
		return Builtin{Name: name, Args: args}, nil
	}
	return p.parsePath()
}

// parseBuiltinArgs parses an optional "(" value ("," value)* ")"
// argument list. No "(" at all means zero arguments (the bare-keyword
// form), not a parse error - "(" immediately followed by ")" (empty
// parens) is also zero arguments, both forms accepted identically.
func (p *parser) parseBuiltinArgs() ([]any, error) {
	if p.peek().kind != tokLParen {
		return nil, nil
	}
	p.advance()
	var args []any
	if p.peek().kind != tokRParen {
		for {
			v, err := p.parseValue()
			if err != nil {
				return nil, err
			}
			args = append(args, v)
			if p.peek().kind != tokComma {
				break
			}
			p.advance()
		}
	}
	if p.peek().kind != tokRParen {
		return nil, fmt.Errorf("filter: expected %q at position %d, found %s", ")", p.peek().pos, p.peek().describe())
	}
	p.advance()
	return args, nil
}
```

### Dispatch: a switch in `internal/eval`, not a map-based registry

Consistent with every other operation in this package (`applyField`,
`applyIndex`, `applySlice` are all type/name switches, not dispatch
tables):

```go
func applyBuiltin(name string, args []any, cur any) (any, error) {
	switch name {
	case "records":
		if len(args) != 0 {
			return nil, fmt.Errorf("eval: records takes no arguments, got %d", len(args))
		}
		return applyRecords(cur)
	default:
		return nil, fmt.Errorf("eval: unknown builtin %q", name)
	}
}
```

`tonumber`/`todate` add their own `case` here when #37/#42 actually
implement them - this ADR decides the mechanism and migrates `records`
onto it, not those builtins themselves.

### Argument count/type validation happens at eval time, not parse time

The parser has no per-builtin arity or type table - it only knows "a
list of literals was given," the same way it doesn't know a sheet
reference is actually valid until evaluation. Each `case` in
`applyBuiltin` validates its own arguments, the same place `applyIndex`
already validates a row number's own range (`n < 1` is a runtime
error, not a parse-time one).

### Conversion failure is an error, not `null` - decided now for #37/#42 to inherit

"Never guess a cell's type from its text" (ADR-0010, the CSV reader's
own design) is about *implicit* inference during reading; `tonumber`
is the opposite - an explicit, deliberate request to convert. Matching
jq's own `tonumber` (which errors, "Cannot convert string to number",
rather than producing `null`), an unconvertible value is a genuine
error here too: silently returning `null` would mask a real data
problem (a typo'd number, a malformed date) instead of surfacing it,
unlike a missing record key or an out-of-range index, which represent
ordinary *absence*, not a type mismatch. `tonumber` on an
already-numeric value is a no-op pass-through, also matching jq.

## Consequences

`internal/filter` gains three lexer tokens, one AST node (`Builtin`,
replacing `Records` entirely), and a small parser addition
(`parseBuiltinArgs`, reusing `parseValue` unchanged); every existing
filter - including every use of `records` - parses to the same
evaluated result as before, just via `Builtin{Name: "records"}`
instead of `Records{}`. `internal/eval` gains `applyBuiltin` as the one
dispatch point every future literal-argument builtin is added to,
replacing what would otherwise be a new hard-coded AST case per
builtin. `map`/`select`/`group_by` (#86) are explicitly not unblocked
by this - they need a filter-expression argument this mechanism
doesn't parse at all, and stay on their own, separate design.
