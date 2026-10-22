# 30. Recover from panics and bound reads with a wall-clock timeout

## Status

Accepted

## Context

Issue #50: xlq had zero `recover()` calls anywhere in the codebase, so
any panic while reading a file - from excelize, `internal/ods`, or
`internal/csv` - crashed the whole process with a raw Go stack trace
and no clean error, for every input. excelize's own advisories listed
8 open, ordinary-panic DoS bugs and 2 hang bugs reachable through
xlq's normal read path as of the issue's own research; ADR-0029's
excelize pin resolved 4 of those 8 panics and one of the two hangs as
a side effect, but the other 4 panics, the remaining hang
(`GHSA-jrfj-fhj2-jjvm`, unbounded `spinCount` during decryption), and -
more importantly - any *future*, not-yet-discovered panic in any of
the three readers are all still completely unguarded. Issue #50 was
accidentally auto-closed by ADR-0029's own PR title mentioning both
issue numbers near a closing keyword; this ADR is the actual
remaining-scope work that issue described, done after reopening it.

Issue #50 proposed wrapping `xlsx.Read`, `xlsx.SheetNames`,
`csv.Read`/`ReadTSV`, and `ods.Read` directly. Verified against the
actual code before implementing that literally, and it's **incomplete
for xlsx specifically**: ADR-0018 made `xlsx.Read` lazy - its own doc
comment says so directly ("no sheet's row data is read until something
actually ranges over that Sheet's Rows"), and reading `Read`'s own
body confirms it: the per-sheet `Rows` field is built as an
`iter.Seq2` closure (`lazySheet`) that `Read` constructs and stores,
never calls. `Read` never invokes `f.Rows(name)`, `rows.Next()`,
`rows.Columns()`, or the cell-parsing `readCell` anywhere in its own
synchronous body - all of that only runs later, when something ranges
over a `Sheet`'s `Rows` (`sheet.All()`), which happens inside
`eval.Eval`, not inside `input.Read`. Wrapping `xlsx.Read` alone would
catch a panic during `excelize.OpenFile`/`GetWorkbookProps` (the two
calls `Read` does make directly) but miss everything that happens
later during iteration - most of the 8 advisories' own panic sites,
including row-scanning and `CalcCellValue`. `internal/csv` and
`internal/ods` don't have this problem: both read their entire input
eagerly inside their own `Read` function (confirmed by reading both -
`csv.ReadWithDelimiter` loops `r.Read()` to `io.EOF` before returning;
`ods.Read` calls `convertTable` for every sheet before returning), so
wrapping just their `Read` call is already complete for them.

## Decision

A generic `withRecovery[T any](operation string, timeout time.Duration,
fn func() (T, error)) (T, error)` helper (`internal/cli/recovery.go`)
runs `fn` in its own goroutine, recovering any panic there into a clean
error (`internal error while <operation> (the file may be corrupt or
malformed): <panic value>`) and racing it against `timeout` via
`select`. `operation` ("reading", "evaluating", "writing", "listing
sheets") names what `fn` is actually doing, since the three call sites
below wrap genuinely different operations and a single generic wording
would mislabel, say, a `-i` write as "reading." A goroutine is required
for the timeout half regardless (there's no way to cancel a running
call from outside it), and `recover()` only ever catches a panic in the
goroutine where it occurs, so the two mechanisms share one wrapper
naturally rather than needing separate machinery. If `timeout` fires
first, `fn`'s goroutine is abandoned, not killed (Go has no way to
force that) - acceptable for a short-lived CLI process that reports the
error and exits right after, unlike a long-running service where a
leaked goroutine would accumulate. One concrete consequence of
abandoning rather than killing: if `fn` is `xlsx.WriteCell` and it
times out after creating its own temp file but before renaming it over
the real one, that temp file's cleanup (a `defer` inside the abandoned
goroutine) never runs, since `main` calls `os.Exit` as soon as this
returns an error, which skips every pending `defer` in the process. In
practice this window only opens once `WriteCell`'s own
`excelize.OpenFile` on the real file has already returned successfully
- every real advisory's panic/hang risk is in that parse step, which
runs before any temp file exists - so a genuinely malicious file won't
trigger this; a stray `xlq-tmp-*.xlsx` left next to a real file that's
just slow to save is the realistic worst case. On Unix, where
`os.Rename` within one directory is atomic, the target file itself is
never left corrupted or partially written either way. On Windows,
Go's own `os.Rename` docs state the same-directory case isn't atomic
there - a pre-existing property of `WriteCell`'s rename-based write
(ADR-0014), unrelated to and unchanged by this ADR's timeout, and out
of scope to fix here. Accepted as a known, narrow limitation rather
than engineered around - there's no way to cancel the blocked excelize
call to let it clean up after itself, and any workaround that tries to
sweep the temp file from outside the abandoned goroutine would just
race it.
`timeout <= 0` disables the deadline entirely (waits on the channel
with no `select`), for a user who explicitly wants to wait out a
genuinely huge, legitimate file. A `withRecoveryErr` wraps the same
thing for a call that only returns an error (`xlsx.WriteCell`), to
avoid every such call site naming an unused type parameter.

Does **not** help with a Go fatal error (a stack overflow, an
out-of-memory kill) - `recover()` can't catch those in any goroutine.
ADR-0029 fixed one specific stack-overflow-causing formula pattern
(mutually referencing `ANCHORARRAY` formulas); it did not address this
fatal-error class in general, and nothing in this ADR does either - a
stack overflow from any other cause, or an OOM kill, are just as
uncatchable as before.

Three call sites get wrapped, each independently (not combined into
one call, since each wrapped call already fully protects its own
panic/hang risk regardless of what else is wrapped):

- `runFilter` (`internal/cli/filter.go`): `input.Read` and
  `eval.Eval`, **separately** - exactly because of the lazy-xlsx
  finding above. Wrapping only the combination as a single call would
  have worked too, but wrapping them separately matches the issue's
  own "wrap the read path" framing as closely as the real architecture
  allows, and keeps each wrapped region's own panic attributable to
  "reading" vs "evaluating" rather than blurring the two. The two calls
  share one deadline computed once per file (`timeoutRemaining`), not
  `--timeout` restarted from its own start for each - otherwise a
  single file could run for close to twice `--timeout` (reading right
  up to the limit, then evaluating for another full timeout's worth),
  despite the flag's own documented meaning as one wall-clock budget
  for the whole file.
- `runInPlace` (`internal/cli/inplace.go`): `xlsx.WriteCell`, which
  opens and re-saves the file - the same excelize internals a
  read-only filter's own open/parse step risks.
- The `sheets` command (`internal/cli/sheets.go`): `input.SheetNames`.
  `xlsx.SheetNames`'s own doc comment says it "doesn't parse any cell
  data," but it still calls `excelize.OpenFile`, which alone reaches
  some of the panic-class advisories (a malformed encrypted workbook's
  CFB/OLE parsing, for one) - not fully exempt just because it skips
  row scanning.

A new `--timeout` duration flag (root's `PersistentFlags`, inherited by
`sheets` too) controls the deadline, defaulting to 2 minutes -
deliberately generous: its job is catching a genuine hang (an infinite
loop, an adversarial file), not rushing a legitimately large one, and
there's no size cap yet (#51) that would make a slow-but-honest file
impossible in the first place. `0` (or any non-positive value)
disables it.

Verified directly, not just unit-tested in isolation:

- A dedicated test (`TestWithRecoveryCatchesPanicDuringLazyEvalIteration`)
  builds a real `model.Workbook` whose `Sheet.Rows` iterator panics on
  iteration and drives it through the real `eval.Eval` - not a
  synthetic callback that panics immediately, which would prove nothing
  beyond what `recover()`'s own documented semantics already guarantee
  regardless of call depth. This would catch a regression that removed
  or misplaced the second `withRecovery` call, which a same-depth
  synthetic panic could not.
- A timeout test confirms the call returns before the slow call's own
  completion signal fires, checked via a channel the slow call closes
  after sleeping rather than an absolute elapsed-time threshold - the
  latter can flake on a CI runner that merely pauses the test process
  for a moment, independent of whether the timeout logic itself is
  correct.
- A zero-timeout test confirms the "0 disables it" convention actually
  waits out a slow-but-successful call rather than imposing some
  implicit floor.
- A `timeoutRemaining` test confirms its three cases directly: disabled
  stays disabled, a future deadline yields roughly the time left, and
  an already-past deadline (reading consumed the whole shared budget)
  yields a small positive duration rather than 0 or negative, which
  `withRecovery` would otherwise misread as "disabled" instead of
  "expired."
- Ran the real CLI end to end: normal reads/edits/`sheets` listing are
  completely unaffected; an absurdly short `--timeout 1ns` against a
  trivial file correctly produces the clean timeout message and a
  non-zero exit, not a hang or a crash; `--help` shows the new flag
  with its default.
- The full existing test suite (598 tests after this ADR's own 6 new
  ones) passes unmodified - no behavior change for the success path,
  confirming this is purely additive safety, not a regression risk.

No real, currently-reachable panic exists to reproduce end to end
right now (ADR-0029 already fixed the ones that were) - a good
problem to have, and the honest reason this ADR's own verification
leans on a synthetic panic deep in a simulated call chain rather than
a real malicious file the way ADR-0029's did.

## Consequences

- `internal/cli/recovery.go`: new, `withRecovery`/`withRecoveryErr`.
- `internal/cli/root.go`: new `--timeout` flag, `defaultReadTimeout`
  constant, `readTimeout` helper.
- `runFilter`/`runInPlace` both gain a `timeout time.Duration`
  parameter (not re-read from `cmd` internally) and wrap their own
  excelize/csv/ods-touching calls.
- Not extended to ADR-0015's config-file defaulting - `--timeout` is
  flag-only for now; adding it to the config file is straightforward
  follow-up if ever wanted, not bundled in here to keep this change
  narrowly scoped to #50's own ask.
- This closes #50. The 4 excelize panic advisories `govulncheck` can't
  prove reachable, and the remaining `spinCount` hang, aren't
  individually *patched* the way ADR-0029's dependency bump patched
  four others - but unlike a dependency patch, `recover()`/timeout
  aren't advisory-specific: they catch *any* panic or hang in the
  wrapped calls, regardless of which bug (known, unreachable-per-
  govulncheck, or not yet discovered) causes it. Concretely: all 4
  remaining advisories are plain panics with no other side effect, so
  `recover()` fully neutralizes them as a DoS risk if they ever did
  trigger; the `spinCount` hang happens inside `excelize.OpenFile`,
  which is itself inside the synchronous part of `input.Read` that
  this ADR's timeout already wraps, so it's bounded too. That leaves
  #51 (memory ceiling) as the one still-open, genuinely separate
  concern this doesn't touch.
