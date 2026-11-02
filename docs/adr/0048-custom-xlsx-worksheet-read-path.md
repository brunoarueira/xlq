# 48. A custom worksheet read path, narrowing ADR-0004's reliance on excelize

## Status

Accepted

## Context

ADR-0046/ADR-0047 already cut large-xlsx read time substantially
(~27s -> ~9-10s on a 105,012-row representative fixture). #164's own
investigation found the remaining cost is structural: `readCell`'s
`GetCellFormula` and `GetCellType` calls both route through excelize's
`getCellStringFunc`, which materializes an entire *second*,
reflection-based copy of the worksheet (`workSheetReader`'s
`xlsxWorksheet` struct) completely independent of the lightweight
streaming `Rows`/`Columns()` walk ADR-0046 already uses, plus a
per-cell mutex-lock-and-binary-search lookup into it. #164 closed
without a fix because eliminating just `GetCellFormula` didn't help -
prototyped and measured directly: `GetCellType` pays the identical
tax, so skipping only one call left total time essentially unchanged
(~9.4s -> ~9.1s, within noise).

Two real, independent datasets have since confirmed this matters and
scales predictably, not as a one-off quirk of a single file's shape:
a 105,012-row file at ~9-10s, and a 407,077-row file (3.88x the rows)
at ~31.18s (3.06-3.28x the time) - linear, not worse, but linear in
the wrong direction as files grow.

Investigated further whether a cheaper path exists inside excelize
itself, not assumed: `GetCellType`'s own classification logic turned
out to be trivial - the raw OOXML `t` attribute (`"b"`, `"d"`, `"n"`,
`"e"`, `"s"`, `"str"`, `"inlineStr"`) through a fixed 7-entry table,
confirmed directly in excelize's own source (`cellTypes` in `cell.go`).
That's not the risk it first looked like. The actual blocker is that
*nothing* in excelize exposes the raw `t` attribute, or any of the
other pieces a cheaper read would need, publicly: `Rows`' own struct
fields (`decoder`, `sst`, `f`, ...) are all unexported, and so are
`getCellStringFunc`, `workSheetReader`, `sharedStringsReader`, and
`getSheetXMLPath`. There's no thin wrapper or partial reuse available -
confirmed by reading excelize's actual exported surface, not assumed.
Getting there means independently reimplementing four pieces, each
confirmed necessary on its own:

1. **Sheet name -> XML path resolution.** Currently excelize's own
   unexported `getSheetXMLPath`, built from `xl/workbook.xml` +
   `xl/_rels/workbook.xml.rels` relationships. `GetSheetList`/
   `GetSheetIndex`/`GetSheetMap` give names and indices, never the
   underlying path - no public equivalent exists.
2. **Shared-string resolution.** A shared-string cell's raw `v` is a
   numeric index into `xl/sharedStrings.xml`, not the text itself.
   excelize's own resolver (`sharedStringsReader`/`getFromStringItem`)
   is unexported.
3. **Row/cell XML streaming**, extracting raw value, type attribute,
   and formula in one pass - mirrors excelize's own unexported
   `cellXMLHandler`/`rowXMLHandler`, which already do exactly this
   internally and simply never expose it.
4. **Type classification** from the raw `t` attribute (trivial, per
   above) - needs #3's raw attribute first.

## Decision

`internal/xlsx` is to gain its own worksheet reader (M14: #167, #168,
#169 - not yet built, this ADR records the decision before that work
starts, per this project's own convention), reading `xl/workbook.xml`
and its relationships, `xl/sharedStrings.xml`, and
`xl/worksheets/sheetN.xml` directly via `archive/zip` and
`encoding/xml` - the same direct-XML-streaming approach
`internal/ods` already uses for ODS (ADR-0010) - to replace
`lazySheet`'s current excelize-`Rows`-based row loop. This narrows,
not reverses, ADR-0004's original choice to use excelize: excelize is
to stay in use for everything this new reader won't need to touch -

- `Read`'s own upfront `OpenFile` call (the sheet list, workbook
  properties, and - deliberately kept - its own `UnzipSizeLimit`/
  `UnzipXMLSizeLimit` enforcement, ADR-0031/ADR-0047, as the first,
  authoritative size check before the new reader ever runs on a file).
  That first check alone isn't sufficient, though: the new reader's
  own, separate `archive/zip` reads of `xl/sharedStrings.xml` and
  each worksheet's XML must independently re-apply a `--max-size`-
  aware bound too (`#168`'s own scope already says so) - `OpenFile`'s
  check doesn't protect a second, later zip read it has no knowledge
  of, and a workbook that passed the first check can still have an
  individual part large enough to matter on its own.
- `GetStyle(styleID)` - resolving a style ID to its definition
  (`xl/styles.xml`, workbook-wide, confirmed via `stylesReader`, not
  `workSheetReader`) is genuinely independent of the per-worksheet
  materialization problem. `GetCellStyle` is not: confirmed directly
  in excelize's own source that it also calls `workSheetReader`, the
  same cost `GetCellFormula`/`GetCellType` have - keeping it would
  undermine the whole point of this change, since it would remain the
  one caller still forcing that materialization to happen at all. The
  new reader captures a cell's style index (its `s` attribute) itself,
  during the same streaming pass as value/type/formula - free, since
  that attribute is already being parsed anyway - and only
  `GetStyle(styleID)` stays on excelize.
- live formula calculation for an uncached formula (`CalcCellValue` -
  genuinely complex to reimplement correctly, explicitly out of
  scope), and
- the entire write path (`internal/xlsx/inplace.go`'s `WriteCell`,
  `-i` editing) - a completely separate code path, unaffected.

`model.Row`/`model.Cell`'s own shape won't change. This is meant to be
a read-path implementation swap behind `xlsx.Read`'s existing
signature; nothing downstream (`internal/eval`, `internal/output`, the
CLI) should need to know or care.

## Consequences

A real, ongoing maintenance cost, not a one-time implementation cost:
`internal/xlsx` will come to own a second, independent implementation
of xlsx's core read structure (sheet resolution, shared strings,
row/cell streaming), diverging from ADR-0004's original "use excelize,
don't reimplement" reasoning for this one slice of it. A future OOXML
edge case (merged-cell value semantics, inline rich text runs,
namespace variations across Excel/LibreOffice/Google-Sheets-exported
files, array/shared formula quirks) becomes ours to find and fix, not
excelize's - excelize is a mature, widely-used library specifically
because it already handles a long tail of these; the new reader starts
without that track record.

The full existing `internal/xlsx/xlsx_test.go` suite is the regression
gate: this change must not alter a single observable outcome it
already locks in, the same discipline ADR-0046/ADR-0047 followed -
every existing test should pass unchanged against the new reader
before anything else is considered. Implementation is scoped as its
own milestone (M14), not folded into M13, given the size and risk
difference from everything else M13 shipped; the expected performance
gain (eliminating `GetCellFormula`/`GetCellType`'s shared per-cell tax
on both confirmed real-world datasets) is deliberately not claimed
with a number here - that's measured once built, not assumed up
front.
