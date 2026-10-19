# 20. Array slicing: jq-style `.[START:END]`

## Status

Accepted

## Context

Issue #56 (M7): ADR-0008 deferred two different things together under
one line, "range references and slicing" - ADR-0012 resolved the first
half (`["A1:C10"]`-style sheet-coordinate ranges, #36/#41). Array
slicing by position - jq's own `.[1:10]` - is still fully open, and
ADR-0008 separately deferred a second, related gap: indexing further
into an already-produced row/column array (`[]any`) at all isn't
supported today - only `[]map[string]any` (a `records` array,
ADR-0011) can be indexed with a plain `[N]`; `.Sheet1.A[0]` currently
errors with "not indexable further in v1". Both gaps are resolved
together here, since slicing `[]any` would be an odd, narrow carve-out
if plain indexing into the same type still didn't work right next to
it.

The motivating case: once a sheet is converted via `records` (#39),
each record is one row (e.g. one post, in a LinkedIn `TOP POSTS`
export). "Give me the top 10 posts" is a completely ordinary analytics
query, and today there's no way to express it other than indexing each
record individually.

Verified against the current implementation before deciding anything
below, not assumed from the grammar docs alone:

- `internal/filter`'s lexer has no `:` token at all yet - `[`, `]`,
  `.`, `|`, `=`, idents, strings, and INT/NUMBER literals are
  everything `lex` recognizes
  (`internal/filter/lexer.go`).
- `applyIndex` (`internal/eval/eval.go`) only handles `model.Sheet`
  (dispatching to `wholeRow`, 1-based spreadsheet row semantics) and
  `[]map[string]any` (0-based, jq-style, negative-from-the-end,
  out-of-range is null - ADR-0011's own convention); `[]any` falls to
  the `default:` "not indexable further in v1" error.
- `eval()`'s own AST walk never produces a `[][]any` as an
  intermediate value - `sheetGrid` (which returns one) is only ever
  called from `toJSON`, at the very end of `Eval`, after every
  `Field`/`Index` node has already run. So a `Slice` node's `Base` can
  only ever actually be `nil`, `[]any` (from `wholeColumn`/`wholeRow`),
  `[]map[string]any` (from `records`), `model.Sheet`, or
  `model.Workbook` at evaluation time - nothing else reaches it.

## Decision

### Syntax: `"[" INT? ":" INT? "]"`, a new alternative alongside the existing bracket forms

```
path     = "." ( head tail* )?
head     = IDENT | "[" STRING "]" | "[" INT "]" | "[" slice "]"
tail     = "." IDENT | "[" STRING "]" | "[" INT "]" | "[" slice "]"
slice    = INT? ":" INT?
```

A new `tokColon` token for `:` is the only lexer addition - `INT`
already lexes a leading `-` (`lexNumber`, unchanged), so a negative
slice bound (`.[-5:-1]`) needs no new lexer work at all.

The parser needs exactly one token of lookahead to stay unambiguous
with the existing `"[" INT "]"` form: `parseBracketSuffix` already
consumes an `INT` for a plain index; it now also checks whether a
`tokColon` immediately follows. If not, nothing changes - `.Sheet1[5]`
parses exactly as it always has. If a colon follows (or opens the
slice form directly, for `.[:10]`), the surrounding segment is a
`Slice`, not an `Index`:

```go
func (p *parser) parseBracketSuffix(base Expr) (Expr, error) {
	if p.peek().kind == tokColon {
		return p.parseSlice(base, nil)
	}
	switch p.peek().kind {
	case tokString:
		// ... unchanged
	case tokInt:
		tok := p.advance()
		n, err := strconv.Atoi(tok.text)
		if err != nil {
			return nil, fmt.Errorf("filter: invalid integer %q at position %d", tok.text, tok.pos)
		}
		if p.peek().kind == tokColon {
			return p.parseSlice(base, &n)
		}
		result = Index{Base: base, N: n}
	// ... unchanged
	}
}
```

`parseSlice` consumes the `:`, then an optional `INT` for the end
bound (absent if the next token is `]`), then the closing `]`:

```go
func (p *parser) parseSlice(base Expr, start *int) (Expr, error) {
	p.advance() // ":"
	var end *int
	if p.peek().kind == tokInt {
		tok := p.advance()
		n, err := strconv.Atoi(tok.text)
		if err != nil {
			return nil, fmt.Errorf("filter: invalid integer %q at position %d", tok.text, tok.pos)
		}
		end = &n
	}
	if p.peek().kind != tokRBracket {
		return nil, fmt.Errorf("filter: expected %q at position %d, found %s", "]", p.peek().pos, p.peek().describe())
	}
	p.advance()
	return Slice{Base: base, Start: start, End: end}, nil
}
```

`Slice.Start`/`Slice.End` are `*int`: nil means "no bound given"
(`.[1:]`, `.[:10]`, `.[:]`), distinct from an explicit `0` - jq's own
distinction, and one `applySlice` (below) needs to tell apart from "an
explicit start of 0" even though they currently resolve the same way,
since a future bound-dependent feature shouldn't have to guess which
one a caller meant.

```go
// Slice is "[START:END]": jq-style positional slicing on Base. Either
// bound may be omitted (nil): ".[1:]", ".[:10]", ".[:]".
type Slice struct {
	Base       Expr
	Start, End *int
}

func (Slice) exprNode() {}
```

### Which values support it: `[]any` and `[]map[string]any`, not `model.Sheet`

Both already-produced array shapes get slicing: `records`' own
`[]map[string]any` (the issue's own motivating case) and `[]any`
(`wholeColumn`/`wholeRow`'s result - `.Sheet1.A[1:10]`, the top 10
cells of column A). Resolving the ADR-0008 gap this ADR opened with,
plain `[N]` indexing is extended to `[]any` too, in the same change,
with the exact convention `[]map[string]any` already has (0-based,
negative-from-the-end, out-of-range is null) - adding slicing but
leaving plain indexing still broken for the same type right next to it
would be an arbitrary, confusing carve-out:

```go
func applyIndex(base any, n int) (any, error) {
	switch v := base.(type) {
	// ... model.Workbook, model.Sheet unchanged
	case []map[string]any:
		return indexArray(v, n)
	case []any:
		return indexArray(v, n)
	default:
		// ... unchanged
	}
}

// indexArray implements jq-style 0-based indexing, generic over any
// slice element type: a negative index counts from the end; still out
// of range (too high, or too negative even after counting back) is
// null, not an error.
func indexArray[T any](v []T, n int) (any, error) {
	if n < 0 {
		n += len(v)
	}
	if n < 0 || n >= len(v) {
		return nil, nil
	}
	return v[n], nil
}
```

`model.Sheet` is explicitly **excluded** from `Slice`, with a clear
error rather than a third, conflicting way to express a row range:
`.Sheet1[5]` already means "spreadsheet row 5" (1-based, `wholeRow`),
so `.Sheet1[5:10]` sitting right next to it but meaning "0-based array
positions 5 through 9" would be a genuine foot-gun on the exact same
receiver type - and a row range is already expressible, unambiguously,
via ADR-0012's own `.Sheet1["A5:J10"]`. `applySlice`'s `model.Sheet`
case (and `model.Workbook`, for the same reason one level up) returns:

```go
return nil, fmt.Errorf("eval: cannot slice a sheet with [START:END]; use an A1-style range reference instead, e.g. [\"A1:C10\"]")
```

`nil` propagates as `nil` (missing-sheet-field-then-slice stays null,
matching every other operation's "field/index on null is null"
convention); anything else (a scalar, a single record) is "not
sliceable" the same way `applyIndex`'s `default` case already reads.

### Index semantics: 0-based, end-exclusive, negative-from-the-end, clamped

Matching jq exactly, and ADR-0011's own `.[N]` convention where it
overlaps:

- `Start`/`End` default to `0`/`len(v)` when omitted.
- A negative bound counts from the end (`-1` is the last element),
  same as `[N]`'s own existing convention.
- Unlike `[N]`, an out-of-range bound **clamps** rather than producing
  `null` or erroring: slicing is forgiving where single-element
  indexing already has a well-defined "absent" result (`null`) to fall
  back on, but there's no equally obvious single "absent slice" value
  - clamping (jq's own actual behavior) means `.[0:1000]` on a
  3-element array is simply `.[0:3]`, not an error or `null`.
- If, after resolving negatives and clamping, `end <= start`, the
  result is an empty array of the same element type - not an error,
  matching jq (`.[2:0]` is `[]`, not a failure).

```go
func resolveSliceBounds(start, end *int, length int) (int, int) {
	s := clampBound(start, 0, length)
	e := clampBound(end, length, length)
	if e < s {
		e = s
	}
	return s, e
}

func clampBound(bound *int, def, length int) int {
	if bound == nil {
		return def
	}
	n := *bound
	if n < 0 {
		n += length
	}
	if n < 0 {
		n = 0
	}
	if n > length {
		n = length
	}
	return n
}
```

### What a slice evaluates to: the same array type as its input

Slicing `[]map[string]any` gives `[]map[string]any`; slicing `[]any`
gives `[]any` - both compose with further indexing/field access
exactly the way the unsliced array already does, since nothing about
the element type changes, only which elements are present.

## Consequences

`internal/filter`'s grammar gains one new token (`:`) and one new AST
node (`Slice`); every existing filter still parses identically - the
one-token lookahead after an `INT` inside brackets is the only parser
change, and it falls through to the existing `Index` path whenever no
colon follows. `internal/eval` gains `applySlice` (dispatched from
`eval()`'s own switch, the same way `Index`/`Field` already are) and a
small, non-breaking generalization of `applyIndex` to also accept
`[]any`, closing the half of ADR-0008's deferred gap that isn't about
slicing at all. Nothing about `model.Sheet`'s own `[N]`/`["A1:C10"]`
dispatch changes - slicing a sheet directly is a clear error pointing
at the range-reference syntax that already covers that need, not a
second, conflicting way to express the same thing.
