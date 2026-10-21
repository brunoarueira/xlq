# 25. `map`/`select`: higher-order filters with a filter-expression argument

## Status

Accepted

## Context

Issue #86 (M7): ADR-0008 deferred `map`/`select` and other higher-order
filters, alongside arithmetic/comparisons, as a group. #85 (ADR-0022)
deliberately carved out a narrower, separate mechanism for builtins
that take only **literal** arguments (`tonumber`, `todate(format)`) -
this ADR covers the larger half #85 explicitly didn't: a builtin whose
argument is itself a **filter expression**, evaluated once per element
of an array against a different, sub-scoped current value. #38
(aggregates: `sum`, `count`, `avg`, `group_by`) is blocked on this for
`group_by`, which needs the same shape for its own key-selector
argument (`group_by(.Category)`) - not implemented by this ADR, but
confirmed below to fit without further grammar changes.

Issue #86 itself raised whether this ADR should also scope in
comparisons (`==`/`!=`/`</>/<=/>=`), since `select` is close to useless
without them. ADR-0024 already answered that, shipped and merged as
its own, independently-useful prerequisite - this ADR builds on it
directly rather than redeciding it. The issue also notes `select`
predicates "often combine with `and`/`or`/`not`" - this ADR
deliberately does **not** add boolean combinators. A single-condition
`select(.Likes > 100)` is already a complete, useful filter on its own
(the issue's own motivating example), and `and`/`or`/`not` is a
further, separable grammar addition (precedence, short-circuiting) with
its own design questions - deferred the same way ADR-0024 deferred
`map`/`select` itself, to keep this PR's scope narrow. A `select(...)`
with a combined condition currently requires chaining
(`select(.A > 1) | select(.B == "x")`), which already works once
`select` exists.

This project has no `.[]` iterate operator (unlike jq). jq's own
`map(f)` is defined as `[.[] | f]` - it already iterates and collects
internally - so this project's `map` matches jq's effective behavior
even though jq expresses it compositionally. jq's `select(f)`,
however, is `if f then . else empty end` - it does **not** iterate on
its own; filtering an array needs the separate `.[] | select(f)`
idiom. Since this project has no equivalent of `.[]`, `select` here is
designed to iterate and filter an array directly, by itself - a
deliberate divergence from jq's own primitive, already noted as
forthcoming context in ADR-0024.

All of the design below was verified by prototyping the real
lexer/parser/evaluator changes on a disposable branch (not just
reasoned on paper) before writing this ADR: built, tested (513
pre-existing tests still pass unchanged), and manually run against a
real CSV file through the built CLI for every case described below,
including the error cases and the `toJSON` fix.

### A correctness bug this ADR's own prototype found and fixes

`map`'s result is always a `[]any` (see below), and if its
per-element filter expression evaluates to something still shaped
like a `record` or `[]record` (e.g. `map(.)`, the identity map), that
value ends up **nested inside** a `[]any`, rather than being the
`[]any`'s own top-level type. `internal/eval`'s
`toJSON` function - the one place that ever flattens a `record`
(`ADR-0011`'s internal `byName`/`headers` struct) into the
`map[string]any` shape `encoding/json` can actually serialize -
previously only handled `record`/`[]record` at the **top level** of a
result, documented as "anything else (a row/column array, a scalar,
nil) is already there." That assumption was true before this ADR: no
existing operation could put an unflattened `record` inside a `[]any`.
`map` is the first one that can.

Verified empirically, with a plain Go program, before writing this
down: marshaling `[]any{record{...}, record{...}}` via
`encoding/json.Marshal` produces `[{},{}]` - silently empty objects,
since `record`'s fields are unexported. `map(.)` on a `[]record` would
have hit exactly this, silently losing every field in the output.

Fixed as part of this ADR's own decision, not a separate one: `toJSON`
gains a `case []any:` that recurses into every element via `toJSON`
itself, instead of assuming a `[]any`'s contents are already
JSON-ready. This is a correctness fix enabled by this ADR's own
feature existing, not a pre-existing bug - nothing before `map` could
construct the situation it fixes.

## Decision

### Grammar: `map`/`select` are pipeline steps whose argument is a nested filter expression

```
step_operand   = path | builtin | filter_arg_builtin
filter_arg_builtin = ( "map" | "select" ) "(" filter_arg ")"
filter_arg     = step_operand ( cmp_op operand )? ( "|" step_operand ( cmp_op operand )? )*
```

`filter_arg_builtin` is a new alternative for `step_operand` - valid
wherever a builtin already is (after a `|`), **not** as a whole
filter's first segment, matching every existing builtin, and (see
below) not as a comparison's `operand` either. `filter_arg` reuses the
pipeline-step grammar recursively: its own first segment can be a
path, a literal-arg builtin, or another `map`/`select` (nesting is
free, not specifically useful today, but not worth forbidding either),
each optionally followed by a comparison (ADR-0024) - unlike a whole
filter's own first segment, which must be a path. Inside `map`/
`select`, `.` is already bound to one array element by the time the
argument runs, so starting from a bare builtin (`map(tonumber)`,
converting each element directly) or bare `.` (`map(.)`, the identity)
is meaningful in a way it isn't at the top level, where `.` starts
bound to the whole workbook.

`filter_arg_builtin` is deliberately **not** added to `operand` (a
comparison's right-hand side, ADR-0024) - `.A1 == map(...)` has no
motivating use case, so it's left out, the same narrower-than-full-
symmetry call ADR-0024 already made for a bare literal starting a
filter's first segment.

The argument is never optional - `map`/`select` with no parentheses
at all, or with empty parentheses, are both parse errors. Unlike
`parseBuiltinArgs`'s literal arguments (ADR-0022), where omitting
`(...)` entirely means "zero arguments," there's no meaningful
"default" filter expression for a transform or a predicate.

```go
var filterArgBuiltins = map[string]bool{
	"map":    true,
	"select": true,
}

// parseStep parses one pipeline step after a "|": a path, a literal-arg
// builtin reference (ADR-0011, generalized by ADR-0022), or a
// filter-expression-arg builtin (this ADR), now optionally followed by
// a comparison against a second operand (ADR-0024). None of these are
// valid as a filter's first segment - Parse always parses that via
// parsePath, never parseStep.
func (p *parser) parseStep() (Expr, error) {
	var left Expr
	var err error
	switch {
	case p.peek().kind == tokIdent && filterArgBuiltins[p.peek().text]:
		if left, err = p.parseFilterArgBuiltin(); err != nil {
			return nil, err
		}
	case p.peek().kind == tokIdent && builtinNames[p.peek().text]:
		name := p.advance().text
		var args []any
		if args, err = p.parseBuiltinArgs(); err != nil {
			return nil, err
		}
		left = Builtin{Name: name, Args: args}
	default:
		if left, err = p.parsePath(); err != nil {
			return nil, err
		}
	}
	return p.maybeComparison(left)
}

// parseFilterArgBuiltin parses "name(filter)": map or select, where
// the parenthesized argument is a recursive filter expression - never
// omitted, since there's no meaningful "zero arguments" default for a
// predicate or transform.
func (p *parser) parseFilterArgBuiltin() (Expr, error) {
	name := p.advance().text
	if p.peek().kind != tokLParen {
		return nil, fmt.Errorf("filter: %s requires a filter-expression argument, e.g. %s(.Field), at position %d", name, name, p.peek().pos)
	}
	p.advance()
	arg, err := p.parseFilterArgExpr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokRParen {
		return nil, fmt.Errorf("filter: expected %q at position %d, found %s", ")", p.peek().pos, p.peek().describe())
	}
	p.advance()
	return FilterArg{Name: name, Arg: arg}, nil
}

// parseFilterArgExpr parses the pipeline inside a map/select's
// parentheses: structurally identical to Parse's own pipeline loop,
// except its first segment is parsed via parseStep (a path, a builtin,
// or a nested map/select, each optionally compared), not parsePath.
func (p *parser) parseFilterArgExpr() (Expr, error) {
	expr, err := p.parseStep()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokPipe {
		p.advance()
		right, err := p.parseStep()
		if err != nil {
			return nil, err
		}
		expr = Pipe{Left: expr, Right: right}
	}
	return expr, nil
}
```

### AST: one new node, shared by both builtins

```go
// FilterArg is "name(arg)": a builtin whose single argument is itself
// a recursive filter expression - unlike Builtin's own Args ([]any
// literals, ADR-0022) - evaluated once per element of the array it
// runs on, with "." rebound to that element. map and select.
type FilterArg struct {
	Name string
	Arg  Expr
}
```

### Evaluation: iterate `[]record` or `[]any`, erroring on anything else

Both `map` and `select` only operate on the two array shapes this
project's evaluator already produces (`[]record` from `records`,
`[]any` from a whole row/column or a slice) - a scalar, a single
`record`, a `model.Sheet`, or a `model.Workbook` is a clear error, the
same "wrong shape, say so" choice `applySlice` already makes for the
same two types. `nil` is **also** an error here, unlike `applySlice`
(`case nil: return nil, nil` - a missing base silently stays absent)
- deliberately not following that particular precedent, because jq's
own `map`/`select` don't either: verified beforehand, `jq -n '5 |
map(.+1)'` errors with "Cannot iterate over number," and `jq -n 'null
| map(.)'` likewise errors with "Cannot iterate over null," not a
silent `null` result or a guessed empty array.

```go
func applyFilterArg(name string, arg filter.Expr, cur any) (any, error) {
	switch name {
	case "map":
		return applyMap(arg, cur)
	case "select":
		return applySelect(arg, cur)
	default:
		return nil, fmt.Errorf("eval: unknown filter-expression builtin %q", name)
	}
}
```

**`map`** always produces a `[]any`, regardless of `cur`'s own element
type - `arg` is free to transform each element into something else
entirely (the motivating example, `map(.Likes)`, turns a `[]record`
into a `[]any` of scalars), so there's no single element type left to
preserve:

```go
func applyMap(arg filter.Expr, cur any) (any, error) {
	switch v := cur.(type) {
	case []record:
		return mapOver(arg, v)
	case []any:
		return mapOver(arg, v)
	default:
		return nil, fmt.Errorf("eval: map requires an array, got %s", describeType(cur))
	}
}

func mapOver[T any](arg filter.Expr, elems []T) ([]any, error) {
	result := make([]any, len(elems))
	for i, elem := range elems {
		v, err := eval(arg, elem)
		if err != nil {
			return nil, err
		}
		result[i] = v
	}
	return result, nil
}
```

**`select`** keeps each passing element **unchanged** (not replaced by
`arg`'s result, unlike `map`) - select filters, it doesn't transform -
so `cur`'s own slice type is preserved (`[]record` stays `[]record`,
`[]any` stays `[]any`), unlike `map`'s always-`[]any` result. `arg`
must evaluate to an actual `bool`; any other result (including a
jq-style "truthy" value like `0`, `""`, or `[]`) is an error, not a
guessed coercion - this project already refuses to guess a type from
context elsewhere (ADR-0010's cell-type rule; ADR-0024's own ordering
decision, which rejected jq's permissive total ordering for the same
reason), so adopting jq's "everything but `false`/`null` is truthy"
here would be the same kind of guess. In practice this is barely a
restriction: `select`'s argument is almost always a comparison
(ADR-0024), which already produces a `bool` on its own.

```go
func applySelect(arg filter.Expr, cur any) (any, error) {
	switch v := cur.(type) {
	case []record:
		return selectOver(arg, v)
	case []any:
		return selectOver(arg, v)
	default:
		return nil, fmt.Errorf("eval: select requires an array, got %s", describeType(cur))
	}
}

func selectOver[T any](arg filter.Expr, elems []T) ([]T, error) {
	// make(..., 0), not "var result []T": an all-filtered-out result
	// must still be a non-nil, empty slice - encoding/json marshals a
	// nil slice as "null", not "[]", and an empty match set is a
	// normal, valid result, not an absent one.
	result := make([]T, 0, len(elems))
	for _, elem := range elems {
		v, err := eval(arg, elem)
		if err != nil {
			return nil, err
		}
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("eval: select's filter must evaluate to a boolean, got %s", describeType(v))
		}
		if b {
			result = append(result, elem)
		}
	}
	return result, nil
}
```

`eval()`'s own dispatch gains one case:

```go
case filter.FilterArg:
	return applyFilterArg(e.Name, e.Arg, cur)
```

### `group_by`'s shape (confirmed, not implemented here)

`group_by(f)` (#38) groups an array's elements by `f`'s result per
element, returning an array of arrays - its key-selector argument is
structurally identical to `map`/`select`'s own: a single recursive
filter expression, evaluated per element. It fits `FilterArg{Name:
"group_by", Arg: Expr}` directly, with no further grammar change -
confirming the issue's own question. `applyFilterArg` gains a third
case when #38 actually implements it; this ADR doesn't design
`group_by`'s own grouping/key semantics beyond this shape check.

## Consequences

- `internal/filter`: one new AST node (`FilterArg`), one new builtin
  name set (`filterArgBuiltins`, disjoint from `builtinNames`),
  `parseStep` extended to recognize it, two new parser functions
  (`parseFilterArgBuiltin`, `parseFilterArgExpr`).
- `internal/eval`: `applyFilterArg`, `applyMap`/`mapOver`,
  `applySelect`/`selectOver`, one new `eval()` dispatch case.
- `internal/eval`'s `toJSON` gains a `case []any:` that recurses into
  every element - a correctness fix this ADR's own feature makes
  necessary (see Context), not scaffolding unrelated to it. Every
  existing caller's behavior is unchanged: before this ADR, nothing
  could produce a `[]any` containing anything `toJSON` would have
  flattened differently than passing it through as-is.
- `map`/`select` are decided as this project's own, deliberately
  non-jq-compositional primitives (iterate-and-transform,
  iterate-and-filter, each in one call), not a port of jq's `.[]` +
  `map`/`select` composition - this project has no `.[]` at all.
- `and`/`or`/`not` remain out of scope, same as `group_by`'s own
  deeper semantics - both are separate, future additions once this
  mechanism exists and ships.
