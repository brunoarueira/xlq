# 27. `group_by(f)`: grouping by equality, in first-encountered order

## Status

Accepted

## Context

Issue #38 (M7), the last of its four aggregates: `group_by(f)` groups
an array's elements by `f`'s per-element result. ADR-0025 already
confirmed `group_by` fits its `FilterArg` shape (`Name: "group_by",
Arg: Expr`) without any further grammar change, and ADR-0026 covered
the other three (`sum`/`count`/`avg`, simpler zero-argument reduce
builtins) as their own, independently-shippable piece. This ADR
decides `group_by` itself: the grammar needs nothing new (`group_by`
is just one more entry in `filterArgBuiltins`, parsed by the exact same
`parseFilterArgBuiltin`/`parseFilterArgExpr` machinery `map`/`select`
already built), so this ADR is almost entirely about the evaluation
model - how grouping actually works, and what it costs.

Verified against real jq (1.8.2) before deciding:

- `group_by(f)` groups array elements by `f`'s result, returning an
  array of arrays. Each group's own element order matches the input's
  original order (`[{"a":1,"b":"x"},{"a":2,"b":"y"},{"a":1,"b":"z"}] |
  group_by(.a)` groups the two `a:1` elements as `[{"a":1,"b":"x"},
  {"a":1,"b":"z"}]`, `x` before `z`, matching their input order).
- The groups themselves come out **sorted by key**, via jq's own total
  type ordering (`null < false < true < numbers < strings < arrays <
  objects`) - the same ranking ADR-0024 already decided *not* to adopt
  for this project's own ordering comparisons, because it answers "is
  a string bigger than a number" with a guess, not a real comparison.

## Decision

### Grouping uses equality, not ordering - so groups come out in first-encountered order, not sorted

`group_by` only ever needs to answer "do these two keys belong to the
same group," which is an equality question - it never needs to answer
"which key comes first," an ordering question. Since this project
already has a well-defined, unrestricted equality (`==`, ADR-0024 -
never errors, any two values, any types), while its ordering is
*deliberately* restricted (number/string only, erroring instead of
guessing a cross-type ranking) specifically to avoid jq's own
guessed-ranking behavior - building `group_by` on equality alone, and
emitting groups in the order their key was **first encountered** in
the input (not sorted), sidesteps needing any ordering decision for
group keys at all. This is a deliberate divergence from jq's own
sorted-group output, not an oversight: sorting would require either
restricting keys to the same number/string-only subset ordering
already uses (losing `bool`/`null` as group keys, both completely
reasonable things to group by - "has this record's flag set," "is this
field blank") or reintroducing exactly the guessed cross-type ranking
ADR-0024 refused to add elsewhere. First-encountered order is also
simply more predictable for a CLI user: the output order follows the
input's own order, not a ranking the user has to already know this
project's own type hierarchy to predict.

### Key type: `float64`, `string`, `bool`, or `nil` only - the types Go can natively use as a map key

Grouping needs to look up "have I seen this key before" for every
element, and the only efficient way to do that is a real Go map keyed
by the group key - which requires the key to be a Go-comparable value.
`float64`, `string`, `bool`, and `nil` all satisfy that directly (a
`map[any]...` with these as the dynamic type works correctly, using
Go's own `==`, not `reflect.DeepEqual`, but all four types already
compare by value under plain `==` with no divergence from
`DeepEqual`'s own semantics for these specific types). A `record`, an
array, a `model.Sheet`, or a `model.Workbook` is rejected with a clear
error instead - not just because a Go map key can't be a slice or a
map (a real, hard panic risk if attempted), but because grouping
records-by-a-whole-record, or by a whole sub-array, isn't the
motivating use case `group_by(.Category)` ever needed in the first
place; a scalar field is what every realistic key looks like. This
also sidesteps the `model.Sheet`/`model.Workbook` `reflect.DeepEqual`
bug ADR-0024 found and fixed for `==` - those two types never reach
this code path to trigger it.

```go
func applyGroupBy(arg filter.Expr, cur any) (any, error) {
	switch v := cur.(type) {
	case []record:
		return groupByOver(arg, v)
	case []any:
		return groupByOver(arg, v)
	default:
		return nil, fmt.Errorf("eval: group_by requires an array, got %s", describeType(cur))
	}
}

func groupByOver[T any](arg filter.Expr, elems []T) ([]any, error) {
	var order []any
	groups := map[any][]T{}
	for _, elem := range elems {
		key, err := eval(arg, elem)
		if err != nil {
			return nil, err
		}
		switch key.(type) {
		case float64, string, bool, nil:
		default:
			return nil, fmt.Errorf("eval: group_by: %s cannot be used as a group key", describeType(key))
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], elem)
	}
	result := make([]any, len(order))
	for i, key := range order {
		result[i] = groups[key]
	}
	return result, nil
}
```

Like `map`/`select`, `group_by` accepts `[]record` or `[]any` and
requires a real array - a scalar, a single record, `nil`, a sheet, or
a workbook is a clear error (verified against jq too: `jq -n '5 |
group_by(.)'` errors "Cannot iterate over number," the same shape of
error `map` already matches). Unlike `select`, each group preserves
`T` (the element type of the array `group_by` ran on) - a `[]record`
groups into a `[]any` of `[]record`s, a `[]any` groups into a `[]any`
of `[]any`s - not flattened or transformed, since grouping doesn't
change what an element *is*, only how it's collected. An empty input
array produces an empty result (zero groups), the natural
fall-through of the loop never running.

`internal/filter` needs only one line: `group_by` added to
`filterArgBuiltins`. `internal/eval`'s `applyFilterArg` gains one
dispatch case.

### `--csv`/`--table` render a group as one JSON-text row, not a nested table - documented, not specially handled

A `group_by` result is a genuinely nested shape (an array of arrays,
each possibly an array of records) that has no single, obvious flat
tabular form - unlike `map`/`select`'s own output, which is always one
flat array. `internal/output`'s existing `[]any` handling (`scalarText`
on each element) already copes with this without crashing or losing
data: verified via the built CLI, each group renders as one row whose
single field is that group's own JSON text (`[{"Category":"fruit",...},
{"Category":"fruit",...}]`), not an empty object the way the
unflattened-record bug ADR-0025 fixed would have produced - there's no
`record` struct reaching `encoding/json` unconverted here, just an
already-flattened `[]map[string]any` being `json.Marshal`ed into one
cell's text, which is correct, if not pretty. This ADR deliberately
doesn't invent a nested-table rendering for `--csv`/`--table` - a
genuinely new design question with no jq precedent to draw on (jq has
no CSV/table output at all) - leaving JSON as the recommended way to
consume a `group_by` result; a prettier tabular rendering, if ever
wanted, is its own future, separately-scoped addition, not bundled in
here.

This design was verified by prototyping the real lexer/parser/
evaluator changes on a disposable branch before writing it down: built,
all 576 existing tests still passed unchanged, and manually run
against two real CSV files through the built CLI - grouping
`records`-shaped data by a string field (including a blank-category
row grouping under a `null` key on its own), composing with
`map(count)` to count each group, grouping a plain `[]any` column by
identity, and confirming the error cases (a record used as a key, a
scalar or `null` input) and the `--table` rendering described above.

## Consequences

- `internal/filter`: `filterArgBuiltins` gains one entry, no new AST
  node (reuses `FilterArg`, ADR-0025), no new grammar.
- `internal/eval`: `applyFilterArg` gains one dispatch case;
  `applyGroupBy`/`groupByOver` are new, standalone functions.
- This closes out #38: `sum`, `count`, `avg` (ADR-0026, merged) and now
  `group_by` are all decided. `group_by(.Category) | map(count)` -
  counting each group - and `group_by(.Category) | map(map(.Likes) |
  sum)` - summing each group - are both already expressible with
  nothing further needed, composing entirely out of builtins this
  project already has.
