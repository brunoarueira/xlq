# 31. Memory ceiling for untrusted xlsx/csv files

## Status

Accepted

## Context

Issue #51: `internal/ods` already bounds its own resource use against
a hostile file - `maxContentXMLSize` caps decompressed `content.xml`
at 256 MiB, `maxMaterializedCells` caps the cells a read will ever
materialize at 10,000,000 (ADR-0010). Neither `internal/xlsx` (via
`excelize.OpenFile`, which loads the file into memory with no size
check of our own) nor `internal/csv` (which reads record-by-record via
`encoding/csv`, itself with no field or record size cap) had an
equivalent guard. A single oversized or adversarially-crafted file
could exhaust host memory before xlq ever applies a filter, since
`internal/eval`'s own `maxDenseCells` budget only bounds a query
*result*, not the act of reading a file into `model.Workbook` in the
first place.

The issue's own proposal was "a pre-flight file-size check (a
configurable max)" for all three readers, plus extending
`internal/csv`'s reading with ods's own bounding spirit. Verified
before implementing that literally, and a pre-flight on-disk size
check alone is **not sufficient** for xlsx (or ods, already handled):
xlsx is a zip container, and a small file can decompress to something
huge - exactly ods's own `maxContentXMLSize` is there to guard against
for *its* format. Confirmed this concretely: built a 307 KiB `.xlsx`
(60,000 cells, each the same short string - excelize's own
shared-string table means repeating the *value* costs almost nothing;
the decompressed size comes from the accumulated per-cell XML
structure instead) that decompresses to several megabytes. A pre-flight
check against the 307 KiB on-disk size would wave this straight
through.

excelize itself exposes exactly the right hook: `excelize.Options{
UnzipSizeLimit, UnzipXMLSizeLimit }` passed to `OpenFile`. Verified
directly against the real binary (`go doc`, then an actual `OpenFile`
call against the fixture above): `UnzipSizeLimit: 1<<20` against that
fixture fails cleanly with `"unzip size exceeds the 1048576 bytes
limit"`; leaving both at their zero value opens the file successfully
using excelize's own generous built-in defaults (16 GiB / 16 MiB,
confirmed via its own doc comment and by observation - `0` is excelize's
own "not configured" sentinel, not a hard "reject everything" value);
`UnzipXMLSizeLimit` set above `UnzipSizeLimit` is rejected by
`OpenFile` itself with a clear validation error, so any caller setting
both must keep the XML limit at or below the overall one.

`internal/csv` has a separate, non-decompression risk: it streams from
disk (no compression amplification, so a pre-flight byte check is
already sufficient for raw content size), but accumulates an unbounded
`[]model.Row` - a file within a byte limit but built from an enormous
number of tiny fields could still cost far more heap than its raw size
suggests, from per-cell/per-row struct overhead alone, the same
"many tiny units, not raw bytes" risk `maxMaterializedCells` already
exists to guard against for ods.

## Decision

Three layers, matching what each format actually needs rather than one
uniform mechanism:

1. **A pre-flight on-disk size check**, shared across every format via
   a new `checkFileSize` in `internal/input` (`Read` and `SheetNames`,
   the two dispatch points every format funnels through), plus
   `xlsx.WriteCell` checking for itself (`-i` has no shared dispatcher
   to do it on its behalf). Rejects a file outright before any reader
   opens it at all. Configurable via a new `--max-size` flag (bytes,
   default 268435456 / 256 MiB - matching ods's own existing
   `maxContentXMLSize` for one consistent "not a legitimate v1-sized
   file" ceiling rather than a different magic number per format), `0`
   disables it (same convention `--timeout` already uses, ADR-0030).
2. **excelize's own `UnzipSizeLimit`/`UnzipXMLSizeLimit`**, xlsx-only,
   via a new `excelizeOptions(maxSize)` helper in `internal/xlsx`
   passed to every `excelize.OpenFile` call (`Read`, `SheetNames`,
   `lazySheet`'s per-sheet reopen, and both of `WriteCell`'s calls).
   Reuses the same `--max-size` value as the overall unzip cap, rather
   than introducing a second, separately-configured number for one
   underlying concern. `UnzipXMLSizeLimit` is capped at `min(16 MiB,
   maxSize)` - a fixed ceiling independent of how large `--max-size`
   itself is, so even a large permitted workbook keeps its *peak*
   memory down by spilling any one worksheet's XML over that size to a
   temp file rather than holding every worksheet in memory at once
   regardless of the overall limit.
3. **A cell-count cap for csv**, a new `maxMaterializedCells = 10_000_000`
   package variable in `internal/csv` (duplicated from ods's own value,
   not shared - this package has no dependency on `internal/ods` - for
   one consistent number, same spirit as #50's own duplicated
   `cellRefPattern`/`columnIndex` precedent in `internal/xlsx`; a
   variable rather than a constant only so a test can lower it
   temporarily instead of writing a genuinely 10-million-cell fixture).
   Fixed at its default, not tied to `--max-size`: this guards a
   different axis (cell count, not byte size) that a byte-oriented flag
   has no natural value for, matching ods's own choice to keep its two
   layers independent of each
   other.

`maxSize <= 0` disables layer 2 (falls through to excelize's own
defaults, not to "no limit at all" literally - there's no sentinel
excelize itself recognizes for that) and the *size comparison* half of
layer 1. Layer 1's other half - rejecting a non-regular file (FIFO,
device, socket) outright - is unconditional even then: `0` means "no
byte ceiling," not "block indefinitely on a pipe is fine." This matters
concretely because `os.Stat` reports a FIFO's size as `0` regardless of
how much data a writer eventually feeds it - without the unconditional
check, a size comparison alone would always pass for one (`0` is never
`> maxSize`), and the actual reader would then block in `os.Open`
waiting for a writer, or stream an unbounded amount once one connects,
the exact risk `--max-size` exists to bound in the first place.
`xlsx.WriteCell`'s own pre-flight check mirrors this exactly, not just
`checkFileSize` - a cubic-dev-ai review caught a first version of
`WriteCell` that only compared `Size()`, with no regular-file check at
all, meaning `-i` against a FIFO bypassed this guard entirely even
though the read path's own `checkFileSize` already had it. Layer 3
(csv's cell-count cap) is unconditional outright,
matching ods's own `maxMaterializedCells` having no disable path
either - but layer 3 has its own gap worth calling out: checking
`totalCells` only after `encoding/csv.Reader.Read` returns means a
*single* record with millions of fields has already paid that whole
allocation cost before the check ever runs (confirmed directly: a
`Reader` with `FieldsPerRecord` set to reject nearly everything still
blocked reading an unbounded single record rather than failing as
soon as the field count was exceeded - `encoding/csv` has no hook to
interrupt a record mid-parse). A new `fieldCountLimitedReader` wraps
the source reader, counting delimiter bytes *outside any quoted
field* as they stream in and failing as soon as the running total
exceeds the cap - before `encoding/csv` can finish parsing whatever
record those delimiters belong to, not after. "Outside any quoted
field" matters concretely: a first version counted every delimiter
byte unconditionally, which a cubic-dev-ai review caught rejecting
valid CSV whose one field happens to hold a long comma-separated blob
as though it were that many separate fields - `"a,b,c,d,e",x,y` is 2
real fields, not 6. The reader tracks RFC 4180 quoting (a field
starting with `"` runs until the next `"` not immediately followed by
another `"`, which is a literal escaped quote) well enough to avoid
that specific false positive, without needing to fully replicate
`encoding/csv`'s own parsing - it's deliberately forgiving rather than
a strict validator, so a delimiter is only ever excluded from the
count when it's unambiguously inside an open, unescaped quote; nothing
ambiguous is ever silently under-counted into passing. Scoped to a
single-byte delimiter (every delimiter this project's own
documentation and tests ever specify is - comma, tab, semicolon) to
avoid a general multi-byte streaming matcher for a combination that
has never come up; `totalCells`'s own
cross-record accumulation still catches "many small records" either
way, regardless of delimiter width.

Verified directly, not just unit-tested in isolation:

- `TestReadRejectsOversizedDecompressedContent` and its `SheetNames`
  counterpart build the real small-on-disk/large-decompressed fixture
  described above and confirm `Read`/`SheetNames` reject it under a
  low `--max-size`, mentioning excelize's own unzip-limit error -
  proving the mechanism catches what a pre-flight check alone
  provably can't (`TestReadMaxSizeZeroDisablesDecompressionLimit`
  confirms the same fixture succeeds with it disabled).
- `TestExcelizeOptionsCapsXMLLimitAtMaxSize` guards the one subtlety
  that would otherwise produce a confusing validation error from
  excelize itself if gotten wrong: the XML limit must never exceed
  `maxSize`.
- Ran the real CLI end to end against a real 307 KiB-on-disk,
  multi-megabyte-decompressed `.xlsx`: default `--max-size` succeeds;
  `--max-size 1048576` fails cleanly (not a hang, not a crash) for the
  main filter path, `sheets`, and `-i` alike; `--max-size 0` disables
  it; `--help` shows the new flag with its default.
- `TestReadRejectsOverMaxMaterializedCells` guards csv's cell-count cap
  by temporarily lowering the constant (a `var`, not a `const`, for
  exactly this) rather than writing a genuinely 10-million-cell
  fixture; `TestReadWithinMaxMaterializedCellsStillSucceeds` confirms
  the boundary itself isn't off by one.
- A plain `stat` on a real FIFO confirmed it does report size `0`
  before relying on that to explain the bug.
- A scratch program confirmed `encoding/csv.Reader` has no hook to
  interrupt a record mid-parse (a `Reader` with `FieldsPerRecord` set
  to reject nearly everything still blocked indefinitely reading an
  artificially unbounded single record, rather than failing as soon as
  the field count was exceeded), before settling on
  `fieldCountLimitedReader` as the fix.
- `TestFieldCountLimitedReaderIgnoresDelimitersInQuotedFields` and
  `TestFieldCountLimitedReaderHandlesEscapedQuotes` guard the quoting
  fix directly: a field with 4 literal commas inside quotes, and a
  field with a doubled (escaped) quote, both read successfully at a
  cap far below their own raw comma count.
  `TestFieldCountLimitedReaderStillCountsDelimitersAfterQuotedField`
  confirms the reader correctly resumes counting once a quoted field
  actually closes, rather than getting stuck thinking it's still
  inside one. `TestReadWithManyDelimitersInsideOneQuotedFieldStillSucceeds`
  repeats the same guard through the real `Read` entry point, not just
  the wrapper's own unit tests.
- `TestReadRejectsFIFOWithoutHanging` (for `Read`) and
  `TestWriteCellRejectsFIFOWithoutHanging` (for `-i`'s own `WriteCell`)
  build a real FIFO via `syscall.Mkfifo` and confirm both reject it
  promptly rather than hanging, each run with a 2-second deadline so a
  regression fails the one test instead of blocking the whole run.
- The full existing test suite (616 tests after this ADR's own 18 new
  ones) passes unmodified with every call site's signature change -
  every existing call passes `maxSize=0`, preserving prior behavior
  for every *regular* file exactly. The one deliberate exception: a
  FIFO/device/socket is now always rejected, even at `maxSize=0` - see
  the non-regular-file reasoning above. No existing test exercised a
  non-regular file against `Read` or `WriteCell` before this (the one
  pre-existing FIFO test, `TestSheetNamesCSVRejectsFIFOWithoutHanging`,
  covers `SheetNames`'s own already-separate `checkReadableFile` path,
  unchanged by this ADR), so this exception doesn't contradict "passes
  unmodified" above - it's new coverage, not a changed assertion.

## Consequences

- `internal/input.Read`/`SheetNames`, `xlsx.Read`/`SheetNames`/`WriteCell`
  all gain a `maxSize int64` parameter; `runFilter`/`runInPlace` (both
  already carrying `timeout` since ADR-0030) gain `maxSize` alongside
  it, read from the new `--max-size` flag in `root.go`.
- Not extended to ADR-0015's config-file defaulting, matching
  ADR-0030's own `--timeout` precedent there - flag-only for now.
- `internal/ods` is unchanged - its own existing bounds already cover
  what this ADR adds for xlsx/csv, and were the model for both layer 1's
  default value and layer 3's constant.
- Doesn't address #18-adjacent genuine large-file *performance* (a
  legitimate multi-hundred-MB file is still slow to process end to
  end, just no longer able to exhaust memory outright) - this is about
  bounding an untrusted file's worst case, not making a trusted large
  one fast.
