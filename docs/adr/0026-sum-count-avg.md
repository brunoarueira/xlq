# 26. `sum`, `count`, `avg`: zero-argument reduce builtins

## Status

Accepted

## Context

Issue #38 (M7): aggregates (`sum`, `count`, `avg`, `group_by`) were
deferred until `map`/`select` existed (ADR-0025, merged), since
`group_by` needs the same filter-expression-argument mechanism
`map`/`select` already built. This ADR covers the three simpler
members of that group - `sum`, `count`, `avg` - as their own,
independently-shippable piece; `group_by` is substantially more
involved (a key-selector argument, a grouping/ordering model of its
own) and gets a separate ADR, the same split ADR-0024 made between
comparisons and `map`/`select` itself.

Unlike `group_by`, `sum`/`count`/`avg` need no filter-expression
argument at all: each reduces the *current* array value (`.`) to a
single scalar, the same "no Base of its own, operates on the pipeline's
current value directly" shape `records`/`tonumber`/`todate` already
have (ADR-0022) - a `Builtin{Name: "sum"}` node, zero new grammar,
dispatched from `applyBuiltin`'s existing switch. Projecting a field
out of a records array first is already `map`'s own job
(`.Sheet1 | records | map(.Likes) | sum`), the same composition jq's
own idiom (`map(.field) | add`) uses, since jq's `add` is likewise a
zero-argument reduction of the current array - verified below.

All three are *reduce* operations (an array to one scalar), decided
together because their null-handling and empty-array behavior need to
be consistent with each other, not because they share an
implementation.

Verified against real jq (1.8.2) before deciding the semantics below,
since `sum`/`count` have a direct jq equivalent to compare against
(`add`/`length`) - `avg` does not (`jq -n '[1,2,3] | avg'` fails to
compile: "avg/0 is not defined"), so `avg`'s own design here is this
project's own addition, not a jq port:

- `add` (this project's `sum`) sums an array, treating `null` elements
  as contributing `0`: `[1,null,3] | add` is `4`, not an error. A
  non-number, non-null element errors: `[1,"2",3] | add` is "number
  (1) and string ("2") cannot be added." An empty array sums to
  `null`, not `0`: `[] | add` is `null`.
- `length` (this project's `count`) is a plain, shape-agnostic element
  count, including `null` elements: `[null,null,1] | length` is `3`,
  `[] | length` is `0`.

## Decision

### `count`: shape-agnostic length - an array-only, jq-compatible subset of `length`

`count` accepts either array shape this evaluator produces (`[]record`
from `records`, `[]any` from `map`, a whole row/column, or a slice) and
returns its element count as a number, including `null` elements - a
blank cell is still a row or array position, not an absent one, and
there's nothing to disambiguate the way there is for `sum`/`avg`
below. Anything else (a scalar, a single record, `nil`, a sheet, a
workbook) is a clear error, the same "wrong shape, say so" choice
`map`/`select` already make (ADR-0025) - a deliberate, narrower scope
than jq's own `length`, which is polymorphic (a string's character
count, a number's absolute value, an object's key count, `null` as
`0`); this project's `count` only ever means "how many elements," and
only for the one shape ("an array") that question is unambiguous for.

### `sum`: `[]any` only, `null` contributes 0 - jq-inspired, numbers only

`sum` requires `[]any` specifically, not `[]record` - summing a record
directly has no single defined meaning (which field would it sum?). A
`[]record` input gets a clear, actionable error naming the fix
(`map(.Likes) | sum`), not a generic "wrong type" message.

`null` elements contribute `0` to the running total, matching jq's own
`add` (verified above) - and consistent with this project's own
established precedent for `null` in a sparse-data context: ADR-0024
already treats `null` specially in an ordering comparison (always
`false`, never an error) specifically so a sparse column's blank cells
don't abort the whole operation. Any other non-number element (a
string, a bool, a record) is a clear error, not a guessed `0` - unlike
jq's own `add`, which is polymorphic (it also concatenates an array of
strings, an array of arrays, or merges an array of objects), `sum`
here is deliberately numbers-only: inferring "the user must have meant
string concatenation" from an array's element type would be exactly
the kind of guess this project otherwise refuses to make. Only `null`
gets special treatment, because only `null` unambiguously means
"nothing was here," the same distinction `tonumber`/`todate` already
draw between `null` (a real absence) and a malformed value (a mistake
worth surfacing).

An array with no non-`null` numbers at all - whether because it's
empty, or because every element is `null` - sums to `null`, not `0`,
matching jq's own `add` for both cases (verified above: `[] | add` and
`[null,null] | add` are both `null`; a number only ever appears once a
real, non-`null` element does) - so "no data to sum" and "summed some
data, and it was zero" stay distinguishable in the output, rather than
colliding on the same value.

### `avg`: this project's own addition - `null` is excluded, not zeroed

`avg` has no jq equivalent to match, so its own null-handling is
decided directly from what's useful for a sparse spreadsheet column,
not copied from anywhere: unlike `sum`, a `null` element is **excluded
entirely** from both the running total and the count used to divide,
not treated as a `0` contribution. Averaging a sparse `Likes` column
should reflect the values that are actually present - treating a blank
cell as a zero data point would pull the average down artificially
every time a cell happens to be unset, which is a different (and
almost always unwanted) question from "what's the mean of the numbers
that are here." `sum` and `avg` deliberately disagree on this because
they answer different questions, not from any inconsistency: a total
should include every present contribution (0 for a blank, matching
jq), while a mean should only be computed over values that exist.

An array with no non-`null` numbers at all (including an empty array)
averages to `null`, the same "no data" signal `sum` now uses for both
an empty array and an all-`null` one, rather than a divide-by-zero
error. Any other non-number, non-`null` element is a clear error, the
same as `sum`. Like `sum`, `avg` requires `[]any`, not `[]record`, with
the same actionable error naming `map` as the fix.

```go
func applyCount(cur any) (any, error) {
	switch v := cur.(type) {
	case []record:
		return float64(len(v)), nil
	case []any:
		return float64(len(v)), nil
	default:
		return nil, fmt.Errorf("eval: count requires an array, got %s", describeType(cur))
	}
}

func applySum(cur any) (any, error) {
	v, ok := cur.([]any)
	if !ok {
		return nil, fmt.Errorf("eval: sum requires an array of numbers, got %s; project a field first, e.g. map(.Likes) | sum", describeType(cur))
	}
	var total float64
	var sawNumber bool
	for _, elem := range v {
		switch n := elem.(type) {
		case float64:
			total += n
			sawNumber = true
		case nil:
			// Contributes 0 - see Decision above.
		default:
			return nil, fmt.Errorf("eval: sum: %s is not a number", describeType(elem))
		}
	}
	if !sawNumber {
		return nil, nil
	}
	return total, nil
}

func applyAvg(cur any) (any, error) {
	v, ok := cur.([]any)
	if !ok {
		return nil, fmt.Errorf("eval: avg requires an array of numbers, got %s; project a field first, e.g. map(.Likes) | avg", describeType(cur))
	}
	var total float64
	var count int
	for _, elem := range v {
		switch n := elem.(type) {
		case float64:
			total += n
			count++
		case nil:
			// Excluded from both total and count - see Decision above.
		default:
			return nil, fmt.Errorf("eval: avg: %s is not a number", describeType(elem))
		}
	}
	if count == 0 {
		return nil, nil
	}
	return total / float64(count), nil
}
```

`applyBuiltin` gains three cases, each validating zero arguments then
dispatching, the exact pattern `records`/`tonumber` already use:

```go
case "sum":
	if len(args) != 0 {
		return nil, fmt.Errorf("eval: sum takes no arguments, got %d", len(args))
	}
	return applySum(cur)
case "count":
	if len(args) != 0 {
		return nil, fmt.Errorf("eval: count takes no arguments, got %d", len(args))
	}
	return applyCount(cur)
case "avg":
	if len(args) != 0 {
		return nil, fmt.Errorf("eval: avg takes no arguments, got %d", len(args))
	}
	return applyAvg(cur)
```

`internal/filter`'s `builtinNames` gains `"sum"`, `"count"`, and
`"avg"` - no grammar change beyond that; they parse exactly like
`records` already does.

This design was verified by prototyping the real evaluator changes on
a disposable branch before writing it down: built, all 549 existing
tests still passed unchanged, and manually run via dedicated Go tests
against a realistic sparse numeric workbook (a header row plus a blank
`Likes` cell among real `model.NewNumberCell` values, not CSV text) -
confirming `map(.Likes) | sum` is `450` (not erroring on the blank),
`map(.Likes) | avg` is `225` (dividing by 2 non-null values, not 3),
`map(.Likes) | count` is `3` (including the blank), and that
`select(...)` matching nothing produces `sum`/`avg` of `null` and
`count` of `0`.

## Consequences

- `internal/filter`: `builtinNames` gains three entries, no new AST
  node, no new grammar.
- `internal/eval`: `applyBuiltin` gains three dispatch cases;
  `applyCount`/`applySum`/`applyAvg` are new, standalone functions.
- `group_by` (tracked separately, its own forthcoming ADR) is
  unaffected by any of this - it's decided entirely in terms of
  `FilterArg` (ADR-0025), not these three reduce builtins, though a
  `group_by(.Category) | map(count)`-style composition (counting each
  group) is one of its natural motivating uses once it exists.
