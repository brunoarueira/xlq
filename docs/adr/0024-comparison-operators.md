# 24. Comparison operators: `==`, `!=`, `<`, `>`, `<=`, `>=`

## Status

Accepted

## Context

Issue #86 (M7): `map`/`select` (ADR-0008's other deferred item,
alongside arithmetic/comparisons) need a *filter-expression* argument -
`select(.Likes > 100)` - which is a substantially bigger grammar change
than #85's literal-only builtin arguments (recursive parsing, not a
flat argument list). `select` is close to useless without comparison
operators to build that predicate from at all, so this ADR splits them
out as their own, independently-shippable prerequisite, rather than
bundling two large decisions into one PR - a different ADR (tracked
separately, filed once this one lands) decides the `map`/`select`
mechanism itself.

Comparisons are decided as a **general grammar feature**, not
something invented just for `select`'s internal use: once implemented,
`.A1 == .B1` will be a valid, directly-evaluable filter on its own -
the same way jq's own `.a == .b` is, producing a bare boolean - not
something that only parses inside a `select(...)` argument. This ADR
is design-only; the lexer/parser/evaluator changes it decides land in
a separate implementation PR. Deciding comparisons as their own,
general feature (rather than special-cased syntax only valid inside a
future `select`) means they can ship and be tested completely
independently of `map`/`select` existing at all, keeping that
implementation PR small per the feature's own rollout plan.

Verified against real jq (1.8.2) before deciding the semantics below,
not assumed:

- Pipe binds *looser* than comparison: `jq '.a | . == 1'` on
  `{"a":1}` is `true` - `.a` evaluates first, then `. == 1` runs with
  `.` rebound to `.a`'s result, not `.a | .` followed by some
  comparison-vs-pipe ambiguity.
- `==`/`!=` never error, even across different types: `null | . ==
  5`, `"5" | . == 5`, and `null | . == null` are `false`, `false`, and
  `true` - cross-type equality is simply "not equal," never a type
  error.
- Ordering (`<`/`>`/`<=`/`>=`) never errors in jq either, via a total
  type ordering across every JSON type (`null < false < true <
  numbers < strings < arrays < objects`): `"abc" | . > 5` and `null |
  . < 5` are both `true`, purely from string-outranks-number and
  number-outranks-null, not from any numeric or lexicographic
  comparison of the actual values.

`map`/`select` here are also already decided (by issue #86's own
framing, confirmed as the intended design rather than copying jq's
more primitive, compositional one - see the forthcoming `map`/`select`
ADR) to directly iterate an array and filter/transform its elements,
unlike jq's own `select`, which is non-iterating on its own and only
filters array elements via the separate `.[] | select(...)` idiom this
project has no equivalent of. That design lives in its own ADR, but
`select(.Likes > 100)` being useful for exactly the "does this record's
field clear a threshold" case is this ADR's own motivating use.

## Decision

### Equality matches jq exactly: never errors, cross-type is simply unequal

`==`/`!=` compare any two values, including across types, and never
return an error - matching jq's own behavior verified above. This is
unambiguous and harmless regardless of type: knowing that a number and
a string are "not equal" guesses nothing about what the user meant,
unlike an ordering comparison would (below). `null == null` is `true`;
`null` compared to anything else is `false`, the same uneventful case
as any other cross-type comparison.

### Ordering deliberately diverges from jq: a cross-type comparison (other than with `null`) is an error, not a guessed ranking

jq's total type-ordering lets `"abc" > 5` silently evaluate to `true`
purely because strings outrank numbers in its ranking - this is
exactly the kind of guess this project's own standing rule (ADR-0010:
never guess a cell's type from its text; `tonumber`/`todate`, #37/#42,
both error rather than coerce) exists to avoid. A `<`/`>`/`<=`/`>=`
comparison between two different, non-`null` types is a clear error
here instead - almost always a real mistake (e.g. a column that needed
piping through `tonumber` first) worth surfacing, not a silently
well-defined-but-meaningless `true`/`false`.

`null` is still special-cased, but not via jq's ranking: comparing
`null` to anything (including another `null`) with an ordering
operator is simply `false`, never an error. This is deliberately
*not* symmetric with equality's treatment of `null` (where `null ==
null` is `true`) - the practical case this serves is a sparse
spreadsheet column: `select(.Likes > 100)` on a record whose `Likes`
is missing (`null`, an entirely ordinary, expected case for sparse
data - unlike a genuine type mismatch) should quietly exclude that
record, not abort the whole `select` with an error over one absent
field, and not require every filter to separately guard against
`null` first.

Only `number` and `string` (lexicographic) support ordering between
two non-`null` values of the *same* type; `bool`, an array, or a
record on either side of an ordering comparison (with a non-`null`
other side) is an error too - this project has no established, useful
ordering for those types, and inventing one now, speculatively, isn't
worth the risk of it being wrong later.

```go
// applyComparison implements Comparison (ADR-0024): == and != never
// error (cross-type is simply unequal, matching jq); ordering
// (</>/<=/>=) errors on a non-null type mismatch instead of guessing a
// ranking jq's own total type order would otherwise supply, but
// treats null specially - compared with anything via an ordering
// operator, including another null, it's always false, never an
// error, so a sparse column's absent value quietly fails a threshold
// check rather than aborting it.
//
// model.Sheet and model.Workbook are rejected before reflect.DeepEqual
// ever runs on them, for both == and != - caught in review, verified
// empirically: Sheet carries a non-nil Rows iter.Seq2 closure, and
// Go's reflect.DeepEqual only ever considers two func values equal
// when *both* are nil, so DeepEqual(sheet, sheet) - the exact same
// value compared to itself - is false, not true. Comparing a whole
// sheet or workbook was never a meaningful operation this project
// defines anyway (the same reasoning ADR-0020 already gave for
// excluding them from slicing), so this is a clear error pointing at
// a specific cell or field instead, not a silently wrong answer.
func applyComparison(op string, left, right any) (any, error) {
	if op == "==" || op == "!=" {
		for _, v := range []any{left, right} {
			switch v.(type) {
			case model.Sheet, model.Workbook:
				return nil, fmt.Errorf("eval: cannot compare %s with %q; compare a specific cell or field instead", describeType(v), op)
			}
		}
		eq := reflect.DeepEqual(left, right)
		if op == "!=" {
			return !eq, nil
		}
		return eq, nil
	}
	if left == nil || right == nil {
		return false, nil
	}
	switch l := left.(type) {
	case float64:
		r, ok := right.(float64)
		if !ok {
			return nil, fmt.Errorf("eval: cannot compare %s and %s with %q", describeType(left), describeType(right), op)
		}
		return compareOrdered(op, l, r), nil
	case string:
		r, ok := right.(string)
		if !ok {
			return nil, fmt.Errorf("eval: cannot compare %s and %s with %q", describeType(left), describeType(right), op)
		}
		return compareOrdered(op, l, r), nil
	default:
		return nil, fmt.Errorf("eval: %s does not support ordering with %q", describeType(left), op)
	}
}

func compareOrdered[T float64 | string](op string, l, r T) bool {
	switch op {
	case "<":
		return l < r
	case ">":
		return l > r
	case "<=":
		return l <= r
	case ">=":
		return l >= r
	}
	panic("unreachable")
}
```

(`reflect.DeepEqual` for `==`/`!=` handles every value shape this
package's evaluator produces *that `eval()`'s own AST walk can still
reach by the time a `Comparison` evaluates its operands* - `float64`,
`string`, `bool`, `nil`, `[]any`, the internal `record`/`[]record`
shape (ADR-0021) - correctly and uniformly, without needing a bespoke
case per type the way the ordering branch does. `model.Sheet` and
`model.Workbook` are the two exceptions, excluded explicitly above
rather than silently mishandled.)

`describeType` gains a `case model.Sheet: return "a sheet"` alongside
its existing `model.Workbook` case, needed for the error message
above - nothing has ever called `describeType` on a bare `model.Sheet`
before now (`applyField`/`applyIndex`/`applySlice` all handle `model.Sheet`
explicitly, never falling through to `describeType`'s own generic
`default`), so this gap existed unnoticed until this ADR's own error
path needed it.

### Grammar: a new binary-operator layer, between pipe and path/builtin, matching jq's own precedence

A first draft of this grammar only let an operand be a `path` or
`builtin` - missing that the whole motivating case, `select(.Likes >
100)`, compares a field against a bare **literal**, not another path.
Fixed by reusing `value` (`STRING | NUMBER | INT | "true" | "false" |
"null"`, the same production `parseBuiltinArgs`/assignment already
parse) as a third operand alternative - but only on the right-hand
side of a comparison, not as a filter's own first segment: `100 ==
.A1` (a literal *starting* the whole filter) isn't supported, a
deliberate, narrower scope than full symmetry, since every motivating
case here is "field, then compare against a constant," never the
reverse, and a filter's first segment already has to be a `path` today
for unrelated reasons (ADR-0008) that this ADR doesn't touch:

```
pipeline     = first ( "|" step )*
first        = path ( cmp_op operand )?
step         = step_operand ( cmp_op operand )?
step_operand = path | builtin
operand      = path | builtin | value
cmp_op       = "==" | "!=" | "<" | ">" | "<=" | ">="
```

A filter's first segment is still always a `path` (never a bare
builtin - "bare `tonumber`, with no preceding `.something |`, isn't a
valid filter on its own" stays true), optionally followed by a
comparison against a second operand; each step after a `|` is the same
shape, but its own left side may be a builtin too (`.A1 | tonumber ==
5`, piping into `tonumber` and comparing *that* result to `5` - pipe's
looser precedence, verified against jq above, is what makes this parse
as `.A1 | (tonumber == 5)`, not `(.A1 | tonumber) == 5`... which,
expanding the pipe, are the same value either way here, but matters
once the right-hand side of `|` is itself more than a single step).

A literal operand becomes a new `Literal` node, not a bare Go value
sitting where an `Expr` is otherwise expected - keeping both sides of
`Comparison` uniformly `Expr`, the same way every other binary-shaped
node in this grammar already works:

```go
// Literal is a bare value (STRING | NUMBER | INT | true | false |
// null) used as a comparison's right-hand operand (ADR-0024) - "the
// number/string to compare against," not a path into the data.
type Literal struct {
	Value any
}

func (Literal) exprNode() {}
```

No comparison chaining (`a == b == c`) and no boolean combinators
(`and`/`or`/`not`) - both explicitly out of scope for this ADR, left
for a later, separate decision once there's a concrete need; nothing
currently open requires either.

Verified end-to-end with a standalone simulation of this grammar
before writing it down (not just reasoned about on paper): every case
above parses as shown, `.A1 = 5` (assignment) is unaffected, `.A1 ==
== .B1` (double comparison) and `100 == .A1` (a literal starting the
whole filter) both correctly fail to parse, and plain `.A1 |
tonumber` - no comparison at all - still parses exactly as it does
today. The parser changes are small and localized to `Parse` itself
and two new helpers:

```go
func Parse(input string) (Expr, error) {
	toks, err := lex(input)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: toks}

	path, err := p.parsePath()
	if err != nil {
		return nil, err
	}

	if p.peek().kind == tokEquals {
		// ... assignment, unchanged ...
	}

	first, err := p.maybeComparison(path)
	if err != nil {
		return nil, err
	}

	expr := first
	for p.peek().kind == tokPipe {
		p.advance()
		right, err := p.parseStep()
		if err != nil {
			return nil, err
		}
		expr = Pipe{Left: expr, Right: right}
	}
	// ... trailing-token check, unchanged ...
	return expr, nil
}

// parseStep parses one pipeline step after a "|" (ADR-0011/ADR-0022):
// a path or builtin reference, now optionally followed by a
// comparison against a second operand (ADR-0024).
func (p *parser) parseStep() (Expr, error) {
	var left Expr
	var err error
	if p.peek().kind == tokIdent && builtinNames[p.peek().text] {
		name := p.advance().text
		var args []any
		if args, err = p.parseBuiltinArgs(); err != nil {
			return nil, err
		}
		left = Builtin{Name: name, Args: args}
	} else if left, err = p.parsePath(); err != nil {
		return nil, err
	}
	return p.maybeComparison(left)
}

var cmpTokens = map[tokenKind]string{
	tokEq: "==", tokNe: "!=", tokLt: "<", tokLe: "<=", tokGt: ">", tokGe: ">=",
}

// maybeComparison checks for a trailing comparison operator after an
// already-parsed left operand (ADR-0024), parsing the right-hand
// operand if one follows. Returns left unchanged if no operator
// follows - this is how both Parse's own first segment and parseStep
// stay optional-comparison, not comparison-required.
func (p *parser) maybeComparison(left Expr) (Expr, error) {
	op, ok := cmpTokens[p.peek().kind]
	if !ok {
		return left, nil
	}
	p.advance()
	right, err := p.parseOperand()
	if err != nil {
		return nil, err
	}
	return Comparison{Op: op, Left: left, Right: right}, nil
}

// parseOperand parses a comparison's right-hand operand: a path, a
// builtin reference, or a literal value (ADR-0024) - never another
// comparison (no chaining) and never the first thing in the whole
// filter (Parse always starts from parsePath, not parseOperand).
func (p *parser) parseOperand() (Expr, error) {
	if p.peek().kind == tokDot {
		return p.parsePath()
	}
	if p.peek().kind == tokIdent && builtinNames[p.peek().text] {
		name := p.advance().text
		args, err := p.parseBuiltinArgs()
		if err != nil {
			return nil, err
		}
		return Builtin{Name: name, Args: args}, nil
	}
	v, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	return Literal{Value: v}, nil
}
```

### New tokens: `==`, `!=`, `<`, `<=`, `>`, `>=`, each with its own `token.describe` case up front

`=` already lexes for assignment (ADR-0014) - the lexer checks for a
second `=` immediately following before deciding between `tokEquals`
(assignment) and the new `tokEq` (`==`), rather than adding a separate,
easily-confused symbol. `!` has no existing meaning at all (never a
valid filter character), so it's only ever recognized as the first half
of `!=` - a lone `!` stays a lexer error, the same as today, leaving
`not` genuinely absent rather than silently half-supported.

```go
const (
	...
	tokEq   // ==
	tokNe   // !=
	tokLt   // <
	tokLe   // <=
	tokGt   // >
	tokGe   // >=
)
```

### AST: one `Comparison` node, `Op` stored as the operator's own text

```go
// Comparison is "Left OP Right" (ADR-0024): ==, !=, <, >, <=, or >=.
// Op is the operator's own text, the same pattern Builtin.Name already
// uses rather than a separate enum type for six fixed strings.
type Comparison struct {
	Op          string
	Left, Right Expr
}

func (Comparison) exprNode() {}
```

### Evaluation: two new, small cases in `eval()`'s own switch

```go
case filter.Literal:
	return e.Value, nil
case filter.Comparison:
	left, err := eval(e.Left, cur)
	if err != nil {
		return nil, err
	}
	right, err := eval(e.Right, cur)
	if err != nil {
		return nil, err
	}
	return applyComparison(e.Op, left, right)
```

Both operands evaluate against the *same* `cur` (the comparison's own
input, not threaded left-to-right the way `Pipe` does) - `.A == .B`
compares two fields of the same current value, not `.B` evaluated
against `.A`'s result.

## Consequences

`internal/filter` gains six lexer tokens (and their `describe` cases),
two AST nodes (`Comparison`, `Literal`), and a grammar layer between
pipe and path/builtin that every existing filter still parses through
unchanged (no comparison operator present, no behavior change).
`internal/eval` gains `applyComparison`, dispatched from `eval()`'s own
switch like `Field`/`Index`/`Slice` already are, plus a trivial
`Literal` case, and `describeType` gains the `model.Sheet` case this
ADR's own error path needed. `map`/`select` (tracked separately) can
now be decided
purely in terms of their own filter-expression-argument mechanism and
per-element evaluation model, with a working, independently-useful
comparison layer already under them - once this ADR's own
implementation PR lands, `.Sheet1 | records | .[0].Likes > 100` is a
complete, valid filter on its own, with or without `map`/`select` ever
landing. Chaining and boolean combinators stay explicitly deferred.
