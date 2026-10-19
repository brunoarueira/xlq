# 21. Column letter access on records (`.["TOP POSTS"] | records | .[0].G`)

## Status

Accepted

## Context

Issue #40 (M7): let one `records` object (ADR-0011) be addressed by
column letter in addition to its header name, so a column doesn't
need a distinguishing header to be reachable by position - e.g.
`.Sheet1 | records | .[0].G` should work whether or not column G's
real header happens to be named something else, the same way
`wholeColumn`'s own `.Sheet1.G` already lets you reach a column
without counting.

Verified before deciding anything below:

- A record, today, is a plain `map[string]any` keyed only by header
  name (`sheetRecords`, `internal/eval/eval.go`): once built, it
  retains no column-index information at all, so `applyField`'s
  `map[string]any` case (`v[name]`) can only ever find a real header
  key - "G" resolves to a value only if some column's *own* header
  happens to render to exactly that text.
- `sheetHeaders` (same file) already rejects the whole sheet with
  `"eval: records: duplicate header %q at columns %s and %s"` the
  moment any two columns would produce the same rendered header name -
  including a column's own letter-fallback name (used when its header
  cell is absent, per ADR-0011) colliding with a different column's
  real header that happens to literally be that letter. This is
  already covered by existing tests
  (`TestEvalRecordsDuplicateHeaderRejected`,
  `TestEvalRecordsNumericAndTextHeaderCollide`). Consequence: **once
  `sheetHeaders` succeeds, every header name across the whole record is
  already globally unique** - there is no live case where "G" is a
  real header belonging to one column while column G *also* needs its
  own letter-fallback name; one of the two would already have failed
  to build at all.
- `eval.Eval` is the *only* call site of this package from the rest of
  the module (`internal/cli/filter.go`), and it already runs every
  result through `toJSON` before returning - `internal/output`'s
  JSON/CSV/table renderers only ever see `Eval`'s already-converted
  return value, never an internal evaluation-time type directly. So a
  new evaluation-time record representation needs exactly one
  conversion point (inside `toJSON`) to stay fully invisible to every
  existing output path.
- #56/#57 (array slicing, just merged) already made `applyIndex` and
  `applySlice` generic over `[]T` for exactly this array element type
  (`indexArray[T any]`, `resolveSliceBounds`) - whatever type `records`
  produces, indexing and slicing it needs no new logic, only the type
  parameter to change.
- `columnIndex` (same file) has no overflow protection of its own -
  `parseCellReference`'s own doc comment already documents why an A1
  reference's column is rejected for being longer than 3 letters
  *before* `columnIndex` ever runs on it, rather than relying on
  `maxExcelCol` alone: its running base-26 total can silently overflow
  an `int` well before that point for an unbounded letter sequence. Any
  new caller of `columnIndex` on externally-supplied text needs the
  same guard, not just a bounds check on its result.

The central tension: making `.G` resolve by letter needs the record to
carry column-position information somewhere, but `records`' own public
output shape (`map[string]any`, name-keyed only) must stay exactly as
ADR-0011 defined it - adding letter-keyed aliases directly into that
same map would duplicate data under two keys, which would then leak
into `-o json`/`-o csv` output (whatever renders a record's keys would
suddenly show both "Likes" and "G" for the same value).

## Decision

### A new, unexported `record` type carries headers alongside the public map, instead of inside it

```go
// record is one row produced by sheetRecords (ADR-0011). byName is
// the exact map[string]any every existing consumer already expects -
// toJSON flattens record (and []record) down to it, so -o json/-o csv
// /-o table never see this type at all, only its byName map. headers
// is the same column-index -> name slice sheetHeaders already builds,
// carried alongside (not merged into byName) so applyField's
// column-letter fallback can resolve a position without duplicating
// data under two keys in the map that actually gets serialized.
type record struct {
	byName  map[string]any
	headers []string // shared across every record from one sheetRecords call
}
```

`sheetRecords` returns `[]record` instead of `[]map[string]any`; its
own body changes only to build `record{byName: ..., headers: headers}`
per row instead of a bare map, keeping `headers` - already computed
once, right before the data-row loop - as a single shared slice every
record in the result points to (cheap: a Go slice header, not a copy,
per record).

### Field access: real header name first, a valid column letter as fallback

```go
case record:
	if v, ok := val.byName[name]; ok {
		return v, nil
	}
	// len(name) > 3 is rejected before columnIndex ever computes it,
	// the same guard parseCellReference already applies (and documents
	// why) for the identical reason: columnIndex's running total can
	// silently overflow an int for an unbounded letter sequence, well
	// before maxExcelCol would otherwise reject it - an unbounded
	// field name must never reach it at all.
	if columnLetters.MatchString(name) && len(name) <= 3 {
		if idx := columnIndex(name); idx >= 0 && idx < maxExcelCol && idx < len(val.headers) {
			return val.byName[val.headers[idx]], nil
		}
	}
	return nil, nil
```

Exact header-name match always wins, with no ambiguity to resolve:
since `sheetHeaders` already guarantees every header name in `byName`
is globally unique (verified above), a key that exists there can never
be "the wrong column's" value - falling back to position only happens
when the name isn't a key at all. This does mean a column whose real
header happens to collide with a *different* column's letter (e.g.
column C's header is literally "G", while column G itself has its own
distinct real header, say "Likes") makes `.G` resolve to column C's
value, not column G's - an accepted, documented limitation: the letter
form is a convenience for a column that doesn't have a name to
distinguish it, not a guaranteed positional accessor that overrides an
unrelated column's own real name. A field name that's neither a real
header nor a valid, in-range column letter stays `null`, matching
every other "missing key" case this package already has.

### `sheetRecords`'s own signature, and every type switch touching it, updates to `[]record`

`applyIndex`'s and `applySlice`'s `[]map[string]any` cases become
`[]record` - no other change to either, since both are already generic
over the array's element type (`indexArray[T any]`,
`resolveSliceBounds` takes only a length). `toJSON` gains the one
conversion point that keeps this invisible everywhere else:

```go
case record:
	return v.byName, nil
case []record:
	out := make([]map[string]any, len(v))
	for i, r := range v {
		out[i] = r.byName
	}
	return out, nil
```

Nothing about `-o json`, `-o csv`, `-o table`, or plain JSON output
changes - each already only ever sees `Eval`'s return value, which is
`toJSON`'s output, which is the same `map[string]any`/`[]map[string]any`
shape ADR-0011 already defined.

`describeType` (used to build every "cannot index/field/slice %s"
error) has its own existing `[]map[string]any`/`map[string]any` cases
("a records array"/"a record") - once `sheetRecords` returns `record`/
`[]record` instead, those two cases become unreachable dead code for
that purpose, and any error describing a record-shaped value would
fall through to its `default: fmt.Sprintf("a %T", v)`, printing
`"a eval.record"`/`"a []eval.record"` instead. Those two cases are
replaced, not kept alongside - nothing else in this package ever
produces a bare `map[string]any`/`[]map[string]any` as an evaluation-
time value once this lands:

```go
case []record:
	return "a records array"
case record:
	return "a record"
```

## Consequences

`internal/eval` gains one new, unexported type and a handful of
updated type switches (`applyField`, `applyIndex`, `applySlice`,
`toJSON`, `describeType`); `sheetRecords`'s return type is the only
exported-adjacent surface that changes, and it's already internal to
this package - `filter.Expr`, `Eval`'s own signature, and every output
format are untouched. `.Sheet1 | records | .[0].G` now resolves a column by
letter even when that column has a real, different header name,
closing the gap #40 asks for; the one documented edge case (a
different column's real header literally equal to the letter you
wanted) is accepted rather than engineered around, since resolving it
either way would require guessing user intent `sheetHeaders`'s own
existing duplicate-rejection doesn't otherwise need to.
