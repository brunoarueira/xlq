# 19. Correct ADR-0018's streaming-reader claims

## Status

Accepted

## Context

A `cubic-dev-ai` review round on PR #79 (the integration PR merging
the `streaming-xlsx-reader` feature branch into `main`) caught six
inaccuracies in ADR-0018's text, independently verified before being
accepted here - none of them a changed decision, every one either a
stale claim left over from an earlier draft, a wrong citation, or an
estimate that turns out to be off once actually measured:

1. ADR-0018 never declares that it supersedes `Sheet.Dimensions()` and
   the old `[]Row`-shaped `Sheet.Rows` as described by ADR-0007
   (introduced them), ADR-0008 (describes whole-column/whole-row
   access via `Dimensions()`), ADR-0011 (`records` checks
   `Sheet.Dimensions()` and an absence from `Sheet.Rows`), and
   ADR-0012 (range references measure the result via `Dimensions()`).
   All four still read, unedited, as if that's the live contract.
2. Its Context section attributes `internal/ods`'s 256 MiB
   decompressed-`content.xml` cap to ADR-0010, which only documents
   the unrelated 10,000,000-cell expansion cap; the 256 MiB figure
   exists only in `internal/ods/ods.go`'s own `maxContentXMLSize`.
3. It frames `f.Rows(name)`'s second-call-reopens-cleanly behavior as
   something "some operations require" for more than one pass, but
   every operation ADR-0018 itself rewrites is single-pass by design -
   no current operation actually needs it.
4. Its own code example for ranging `sheet.Rows` uses the field
   directly, which panics on a zero-value `Sheet{}` (nil `Rows`) - the
   exact bug PR #77's own review round caught and fixed with
   `Sheet.All()`, after this example had already been written against
   the pre-fix shape.
5. It estimates a maximally-wide row's `[]Cell` at "a few tens of
   kilobytes" - measured via `unsafe.Sizeof`, `Cell` is 48 bytes, so
   16,384 of them is ~768 KiB, over an order of magnitude more.
6. Its Consequences section claims every whole-sheet operation
   (`.Sheet1`, `records`, a whole column) discards each row's cells
   immediately after folding them into the result. `sheetRecords` is
   an exception: it can't know a row's final shape until the whole
   sheet has streamed by, so it buffers every data row - cells
   included - until then.

Per `CONTRIBUTING.md`'s own convention, an accepted ADR isn't edited -
a later decision (or, as here, a later correction) gets a new ADR
instead, the way ADR-0009 corrected ADR-0008's grammar without
touching it. None of the six items above changes what ADR-0018
actually decided or what's implemented; they correct what the
document says about it.

## Decision

1. **Supersession, declared explicitly, now**: ADR-0018 supersedes
   ADR-0007's, ADR-0008's, ADR-0011's, and ADR-0012's description of
   `Sheet.Dimensions()` and `Sheet.Rows` as `[]Row`. Wherever any of
   those four ADRs mentions `Dimensions()` or treats `Sheet.Rows` as a
   plain slice, read it as historical: describing the shape as it
   stood before ADR-0018, not the current one. Nothing else in any of
   those four ADRs - the data model's field layout, the filter
   grammar, `records`' header-matching rules, range-reference
   semantics - changes; only the removed method and the now-lazy field
   do.
2. Read ADR-0018's Context section as: `internal/ods` is already
   bounded by its own size/cell budgets - `maxContentXMLSize` (256
   MiB, decompressed `content.xml`, code-only) and
   `maxMaterializedCells` (10,000,000 cells, ADR-0010's own expansion
   cap) - not "ADR-0010 already bounds it" undifferentiated.
3. Read its third verified-mechanism point as: calling `f.Rows(name)`
   a second time for the same sheet opens a fresh, independent,
   correctly-ordered cursor, verified because it's what makes it safe
   for two different operations (or two evaluations of the same
   filter) to each range over a `Sheet`'s `Rows` independently - not
   because any one rewritten operation needs more than one pass itself
   (none do).
4. Read its `for row, err := range sheet.Rows` example as
   `range sheet.All()` - the nil-safe accessor PR #77 added, which
   every actual consumer in `internal/model` and `internal/eval` uses.
5. Read its "a few tens of kilobytes wide at most" estimate for a
   maximally-wide row as: `Cell` is 48 bytes (`unsafe.Sizeof`), so
   16,384 of them - Excel's own column limit - is already ~768 KiB of
   struct storage alone, before any string/value backing allocation a
   cell's own fields point to. The bound this reasoning actually
   relies on is `Cell`'s *count* being capped, not any particular byte
   figure; it's still dwarfed by what a million-plus-row `[]Row` would
   cost if ever fully resident, which is the actual risk ADR-0018
   addresses.
6. Read its claim that a whole-sheet operation holds "substantially
   less in memory at once, since a row's cells are discarded
   immediately" as true for `sheetGrid`/`wholeColumn`/`wholeRow`'s own
   accumulation, but not for `records`: `sheetRecords` buffers every
   data row - cells included - until the whole sheet has streamed by,
   since it can't assign a row's cells to their header keys before
   knowing the final column count. What `records` avoids is the
   *second pass* a `Dimensions()` call would otherwise have forced, not
   row-retention itself.

## Consequences

None functionally - `internal/xlsx`, `internal/model`, and
`internal/eval` already implement exactly what ADR-0018 decided; this
only corrects what its text claims about that implementation and about
its own relationship to four earlier ADRs. Anyone reading ADR-0018 on
its own should also read this one, the same way ADR-0008 needs
ADR-0009 to get its grammar right; `docs/adr/README.md`'s table is
updated to point there.
