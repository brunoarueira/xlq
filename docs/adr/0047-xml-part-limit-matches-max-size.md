# 47. excelize's per-part XML limit matches --max-size, not a separate fixed cap

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
this accounted for ~58-88% of total time. Raising the constant to
256 MiB directly, as a test, cut total time by a further ~25-30% on
top of ADR-0046's own improvement, and the disk-I/O signature
disappeared from the profile entirely.

The real question this raises is whether raising it is actually safe:
`unzipXMLSizeLimit` exists at all because of #51
(`docs/adr/0031-memory-ceiling-for-untrusted-files.md`) - bounding
memory against an adversarial file that decompresses far past its
on-disk size. Read excelize's own `ReadZipReader` source directly
(not assumed) to settle this precisely: it accumulates `unzipSize`
across *every* part in the zip as it reads each one's header, and
calls `checkFileSize` - which compares that running total against
`UnzipSizeLimit` - on every single part, unconditionally, before ever
deciding whether that specific part gets spilled to a temp file or
not. `UnzipXMLSizeLimit` only controls that second, later decision -
once a part has already passed the cumulative `UnzipSizeLimit` check,
should *this one* additionally stay resident in `ReadZipReader`'s own
in-memory `fileList` map, or be spilled to a temp file anyway even
though it already fit within the permitted total.

That means `UnzipXMLSizeLimit` was never a second, independent
security boundary - the actual adversarial-file protection is
entirely `UnzipSizeLimit`'s cumulative check, which this doesn't
touch at all. `unzipXMLSizeLimit`'s own fixed 16 MiB value was simply
*more conservative, per part, than the total budget the user already
explicitly chose* via `--max-size` (default 256 MiB) - every
worksheet's own XML, and the shared-strings table, each individually
capped at 16 MiB even when the user's own chosen ceiling for the
*whole file* was 16x that. Raising a per-part limit to match a total
the user already accepted doesn't let an adversarial file use any
more memory than it already could.

## Decision

`excelizeOptions` sets `UnzipXMLSizeLimit` equal to `UnzipSizeLimit`
(both `maxSize`) instead of a separate, lower, fixed constant - no
single part is restricted below the overall ceiling `--max-size`
already grants the whole file. The `maxSize <= 0` ("disabled") case is
unchanged: it already opts out of resource bounds entirely, and
excelize's own built-in defaults (16 GiB/16 MiB) apply as before -
revisiting that path is a separate, smaller concern, out of scope
here, since the default, bounded case (what essentially every real
invocation hits, including the one that motivated this) is what
actually needed fixing.

## Consequences

`internal/xlsx/xlsx.go` loses the separate `unzipXMLSizeLimit`
constant; `excelizeOptions` becomes a straight pass-through of
`maxSize` to both fields. `TestExcelizeOptionsCapsXMLLimitAtMaxSize`
is rewritten to assert the new behavior (XML limit always equals
`maxSize`, not a fixed cap below it for a large `maxSize`).
`TestReadRejectsOversizedDecompressedContent` (#51's own adversarial-
file test, exercising `UnzipSizeLimit`'s cumulative check, which this
doesn't change) needs no changes and must still pass. A large,
string-heavy file (the actual motivating case) reads noticeably
faster; peak memory for a file within `--max-size` doesn't change in
the worst case, since that worst case was already `UnzipSizeLimit`-
bounded independently of this constant.
