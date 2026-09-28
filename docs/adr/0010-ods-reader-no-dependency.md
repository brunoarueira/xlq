# 10. ODS reader: no third-party library, standard library only

## Status

Accepted

## Context

#10 asks for an `.ods` (OpenDocument Spreadsheet) reader producing
`model.Workbook`, and flags up front that this needs a real
library-choice decision, unlike `.xlsx` (ADR-0004 picked
`qax-os/excelize` without much debate - it's the clear de facto
standard). ODS support in the Go ecosystem is nowhere near as settled,
so the options were actually surveyed rather than assumed:

- **`knieriem/odf`/`odf/ods`** - the oldest, most-imported option (4
  known importers), using `encoding/xml` under the hood. Last commit
  2019; the format hasn't meaningfully changed since, so that's not
  disqualifying on its own. Disqualifying in practice: its public API
  (`Table.Strings()`, `Cell.PlainText()`) only ever returns text. There
  is no way to ask it "what type is this cell" - a number, a date, and
  a string that happens to look like one are all indistinguishable
  through this library.
- **`AlexJarrah/go-ods`** - actively maintained (pushed within the last
  few months), reasonably well-written (checked directly: clean error
  handling, atomic writes, decent doc comments), MIT-licensed, and its
  read API (`Cell.String()`/`.Float64()`/`.Bool()`/`.Time()`) does read
  the real ODF `office:value-type`-backed fields under the hood.
  Disqualifying anyway: none of that type information is exposed
  through the public API. There's no `Cell.ValueType()` or equivalent -
  the underlying typed struct fields live behind an unexported
  `Document.content` field. Building `model.Cell.Kind` on top of this
  library would mean guessing a cell's type by calling `.Float64()`
  and seeing whether it errors, exactly the "does this text look like
  a number" ambiguity a cell's real, explicit type should make
  unnecessary (see also: the raw-vs-formatted-value lesson from the
  xlsx reader, ADR-0007's Cell design) - and it would misclassify any
  string cell that happens to contain only digits.
- **`RHNTCH/go-ods`, `mukbeast4/go-ods`** - both created within the
  last few months, near-zero adoption (0-2 stars, 0-1 forks), no track
  record. Adopting either would mean taking on an unvetted dependency
  for a project that has otherwise been deliberately conservative
  about dependencies (`cobra` and `excelize`, ADR-0004, both
  overwhelmingly established).

No candidate exists that's both trustworthy (an active track record,
real adoption) *and* exposes the real per-cell type information
`model.Cell`'s tagged union needs to avoid guessing. That combination
is exactly what made `excelize` an easy pick for `.xlsx` and is
exactly what's missing here.

ODS's actual complexity, for xlq's narrow read-only, values-only needs,
is modest: it's a zip file containing `content.xml`, whose spreadsheet
data is `<table:table>` (has `table:name`, a real per-sheet name,
unlike CSV) containing `<table:table-row>` containing
`<table:table-cell>`, each cell typed via `office:value-type`
(`float`/`percentage`/`currency`/`date`/`time`/`boolean`/`string`) with
its value in the correspondingly-named attribute
(`office:value`/`office:date-value`/`office:boolean-value`) or, for
strings, in nested `<text:p>` text. This is `encoding/xml` territory
Go's standard library already handles well - confirmed empirically
before committing to this: a quick unmarshal test showed Go's default
element/attribute matching resolves `office:value-type`,
`table:number-columns-repeated`, etc. by local name regardless of
namespace prefix, so no manual namespace handling is needed.

## Decision

Implement `internal/ods` against `content.xml` directly, using only
`archive/zip` and `encoding/xml` from the standard library - no new
module dependency for this reader.

Cell typing follows `office:value-type` explicitly (never inferred
from text), the same standard the xlsx reader already holds itself to:
`float`/`percentage`/`currency` -> `model.Number`, `boolean` ->
`model.Bool`, `date` -> `model.Date` (parsed from `office:date-value`),
`string` -> `model.String` (from `<text:p>` content). `time`
(duration, e.g. `PT2H30M`) has no matching `model.CellKind` - a
point-in-time and a duration aren't the same thing, and durations are
rare enough in practice that inventing a mapping isn't worth it - it's
kept as `model.String` with its raw ISO-8601 duration text.

ODF's own sparse-compression mechanism -
`table:number-columns-repeated`/`table:number-rows-repeated`, which
collapses a run of identical cells/rows (almost always blank filler
padding a sheet out to a spreadsheet application's default extent,
e.g. over a million rows) into one XML element with a repeat count -
is *never* expanded into that many `model.Cell`/`model.Row` values. A
cell/row with no real value or formula is skipped regardless of its
repeat count, matching `model.Sheet`'s own "only present cells/rows"
sparse design and costing O(1) per XML element regardless of how large
the count is. Only a repeat count on a cell/row that *does* have real
content is expanded (rare, and in practice always small); as a safety
net against a malformed or adversarial file claiming an enormous
repeat count on real content, expansion is capped at the same
10,000,000-cell budget `internal/eval` already enforces for dense
results (see PR #24) - past that, `Read` returns an error rather than
attempting the allocation.

## Consequences

No new dependency for this reader, and no ambiguity in a `model.Cell`'s
`Kind` - it's read from the file's own explicit type, never guessed.
The trade-off: `internal/ods` re-implements a slice of what a library
like `AlexJarrah/go-ods` already has code for, and if xlq ever needs to
*write* ODS files, this reader doesn't help with that at all (whereas
that library also writes) - a decision worth revisiting with its own
ADR if a write path is ever needed. `time`-typed (duration) cells
losing their numeric-duration meaning and becoming a raw string is a
narrow, documented gap - fine for v1, where formula evaluation and
arithmetic on cell values aren't in scope at all yet.
