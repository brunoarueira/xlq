# 47. Raise excelize's per-part XML limit, keeping it below --max-size

## Status

Accepted

## Context

Testing #159's real-world impact against a genuinely large,
string-heavy file (#163, 2026-11-02) found a second, separate cost:
once a workbook's shared-strings table exceeds `unzipXMLSizeLimit`
(16 MiB, `internal/xlsx/xlsx.go`'s own fixed constant) decompressed -
easy to hit with ~100K rows across many string columns - excelize
spills it to a temporary file (`ReadZipReader`/`unzipToTemp` in its
own source) instead of keeping it in memory, and every subsequent
cell's string lookup (`getFromStringItem`) becomes a `Pread` against
that file instead of an in-memory slice index. Profiled a
representative fixture matching the real file's actual column shape:
this accounted for ~58-88% of total time.

A first draft of this ADR, caught before merge, set
`UnzipXMLSizeLimit` equal to `UnzipSizeLimit` (both `maxSize`)
entirely, reasoning that `UnzipSizeLimit`'s own cumulative check
(confirmed directly in excelize's `ReadZipReader` source: it sums
`unzipSize` across every part in the zip and checks that running
total on every one, before `UnzipXMLSizeLimit` ever decides whether
one specific already-permitted part stays resident in memory or gets
spilled to disk) already bounds the *total* ever decompressed, so
raising the per-part limit to match it "doesn't let an adversarial
file use any more memory than it already could." That conflated two
different things: the cumulative total ever decompressed across every
part (unchanged either way) and how much of that total a *single*
part can claim resident in the process's own memory *at once*. Those
aren't the same bound. Under the original 16 MiB cap, an adversarial
file crafted to inflate just one part - most easily the shared-strings
table, since it's just many repeated string values - could never claim
more than 16 MiB of actual process memory for that part, however large
`--max-size` itself was set; the file's *total* claim could still
reach `maxSize`, but only by spreading across many separately-capped,
mostly-disk-backed parts, not as one contiguous in-memory allocation.
Removing the per-part cap entirely gives up that specific defense-in-
depth layer: a single part could then legitimately claim the *entire*
`maxSize` budget (default 256 MiB) in one allocation.

The real fix the motivating case needed was a *higher* per-part cap,
not *no* cap. Measured directly, not guessed: a representative large
(105,012-row, string-heavy) fixture's real shared-strings table needs
~62.6 MiB decompressed.

## Decision

`unzipXMLSizeLimit` is raised from 16 MiB to 128 MiB - comfortable
headroom over the ~62.6 MiB measured real-world need, while staying
meaningfully below the 256 MiB default `--max-size` a single part
could otherwise claim outright. `excelizeOptions` keeps the original
`min(unzipXMLSizeLimit, maxSize)` structure (a maxSize smaller than
128 MiB still caps the XML limit down to it, exactly as before) - only
the fixed constant's value changes, not the shape of the guarantee:
confirmed directly in excelize's `ReadZipReader` source that this
spill-or-stay-resident decision only ever applies to two kinds of
part - the shared-strings table and a worksheet's own XML (matched by
an `xl/worksheets/sheet` prefix) - every other part (styles, the
workbook's own `workbook.xml`, `[Content_Types].xml`, ...) is always
read fully into `fileList` regardless of this limit's value, bounded
only by `UnzipSizeLimit`'s own cumulative check. Neither of those two
handled kinds is ever allowed to claim more than this fixed ceiling
(or the caller's own smaller `maxSize`, whichever is lower) resident
in memory at once, regardless of how high `--max-size` itself is set.
The `maxSize <= 0` ("disabled") case is unchanged, as before: it
leaves excelize's own built-in defaults (16 GiB/16 MiB) in place
rather than applying none at all - there's no literal "no limit"
excelize itself recognizes, only "xlq's own configured bound" versus
"excelize's own built-in one."

## Consequences

`internal/xlsx/xlsx.go`'s `unzipXMLSizeLimit` constant changes value
(16 MiB -> 128 MiB); `excelizeOptions`'s own logic is unchanged.
`TestExcelizeOptionsCapsXMLLimitAtMaxSize` is updated to assert
against the new constant value, keeping its original shape (a small
`maxSize` still caps the XML limit down to it; a large one leaves the
fixed default in place, now 128 MiB instead of 16 MiB).
`TestReadRejectsOversizedDecompressedContent` (#51's own adversarial-
file test, exercising `UnzipSizeLimit`'s cumulative check, which this
doesn't change) needs no changes and must still pass. A large,
string-heavy file (the actual motivating case) reads noticeably
faster, now that a realistic shared-strings table fits under the new
cap instead of spilling to disk; an adversarial worksheet or
shared-strings part still can't claim more than 128 MiB (or a smaller
`--max-size`) of process memory at once, preserving the defense-in-
depth layer the first draft of this ADR would have removed.
