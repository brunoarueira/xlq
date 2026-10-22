# 29. Pin excelize to an unreleased commit, fixing #49 and most of #50

## Status

Accepted

## Context

Issue #49: a crafted workbook with two mutually-referencing
`ANCHORARRAY` array formulas crashes xlq unrecoverably - excelize's
`CalcCellValue` re-enters itself for `ANCHORARRAY`, building a fresh
calculation context each time, so the circular-reference guard (which
lives inside one context) never engages and the recursion never
terminates, ending in a Go fatal error (`fatal error: stack overflow`,
not a panic - `recover()` can't catch it). [GHSA-wp2g-vpjj-g53r
(CVE-2026-107216)](https://github.com/xuri/excelize/security/advisories/GHSA-wp2g-vpjj-g53r),
unpatched in any tagged release through our pin (v2.11.0) as of #52's
own research. The issue itself weighed three remediations, none of
which is what this ADR decides: accept the risk and wait for a
release; stop calling `CalcCellValue` for uncached formulas entirely
(dropping a documented, working feature); or isolate formula
evaluation in a subprocess (meaningfully more engineering for a
one-shot CLI tool).

Issue #49 also claims the two malicious cells "don't need to be on the
sheet... the user's filter targets... because `Read()` processes every
sheet up front regardless of what's queried." Verified directly before
deciding anything here, and that claim is now **outdated**: since
ADR-0018 (already merged when #49 was filed, but apparently not
accounted for in its own analysis), each sheet's rows are read lazily,
on first access to that specific `Sheet.Rows` - not eagerly by
`Read()`. Built an actual malicious workbook (two sheets: `Sheet1`
with ordinary data, `Sheet2` with the circular `ANCHORARRAY` pair) and
confirmed: `xlq '.Sheet1.A1' evil.xlsx` and `xlq 'sheets' evil.xlsx`
both return cleanly, never touching `Sheet2` at all. The crash is real
and still reachable, but only when the query actually reads from the
poisoned sheet - either directly (`xlq '.Sheet2.A1' evil.xlsx`, which
does crash, confirmed with the real fatal-error stack trace matching
the advisory exactly) or indirectly via a whole-workbook dump (`xlq
'.' evil.xlsx`, which touches every sheet and also crashes, confirmed).
Narrower than the issue's own blanket claim, but still a real,
unauthenticated DoS for the sheets actually queried.

**The decision-changing discovery**: `govulncheck`'s own output names
the exact unreleased commit where each advisory was fixed - not just
"fixed in 2.11.1" (a tag that doesn't exist), but a real pseudo-version
like `v2.11.1-0.20260911060113-ea12859e43c6`. Go's module system can
depend on any commit directly via `go get module@commit-sha`, which
resolves to exactly this kind of pseudo-version - not a hypothetical,
something `go get` actually does today. Tried it, verified
thoroughly, before deciding to rely on it:

- Pinned to `v2.11.1-0.20260911060113-ea12859e43c6` (the commit
  `govulncheck` named for the `ANCHORARRAY` fix specifically): the
  real malicious workbook from above no longer crashes -
  `.Sheet2.A1` returns `null` cleanly.
- Four separate advisories each had their own "fixed in" pseudo-version
  commit, at different dates (Sep 11, 12, 15, 30). Since excelize's
  commits on its own default branch are cumulative, tried the
  *latest* of the four (`v2.11.1-0.20260930021559-01a9ff32fb3c`,
  Sep 30) instead of just the `ANCHORARRAY`-specific one, to get
  whichever of the others it also happens to include.
- Result: `govulncheck` goes from 4 reachable vulnerabilities to
  **0** (re-verified directly, not just trusted the summary line).
  The real malicious workbook's whole-workbook dump (`xlq '.'
  evil.xlsx`), which crashed before, now returns clean JSON with
  `Sheet2`'s formula cells correctly resolving to `null`. All 591
  existing tests still pass, unmodified.
- The four resolved advisories: `GHSA-wp2g-vpjj-g53r` (`ANCHORARRAY`,
  this issue), `GHSA-jw42-f3rr-4cc3` (unbounded row number hangs
  `Rows.Columns`/`Rows.Next` - one of #50's two hang-class bugs),
  `GHSA-2j4c-ffch-9f23` (`Decrypt`, 9 panic sites), and
  `GHSA-x2q3-8cjh-766f` (`extractPart`'s unbounded CFB buffer). All
  four were in #50's own table/#52's tracking as reachable-but-
  unpatched; none of #50's *other* four listed panic advisories
  showed as reachable from xlq's own call graph either before or
  after this pin (matching the uncertainty #50 itself already noted
  for those). #50's second hang-class bug (`GHSA-jrfj-fhj2-jjvm`,
  unbounded `spinCount` in agile decryption) isn't resolved by this
  pin and was never flagged as reachable by `govulncheck` for xlq
  specifically either, before or after - out of scope for this ADR,
  unaffected either way.

## Decision

Pin `github.com/xuri/excelize/v2` to the pseudo-version
`v2.11.1-0.20260930021559-01a9ff32fb3c` via `go get
github.com/xuri/excelize/v2@01a9ff32fb3c` - a real, specific commit
from excelize's own default branch, not a tagged release. This is a
deliberate, temporary exception to depending only on tagged releases:
the fix is real and verified; waiting for excelize to cut a release
that happens to include it has no fixed timeline, and four concretely
reachable DoS vulnerabilities (one of them this ADR's own subject)
sitting unpatched in the meantime is a worse trade-off than depending
on an untagged commit whose content has been directly verified to fix
exactly what it claims to, with zero regressions in xlq's own test
suite.

This is explicitly a **temporary** pin, not a standing practice:
revisit it the moment excelize ships any tagged release `>=
v2.11.1-0.20260930021559-01a9ff32fb3c` (checked via `go list -m
-versions github.com/xuri/excelize/v2` - already run as part of #52's
own standing watch, no new process needed) and switch to that tagged
version instead, keeping the same verification this ADR used (rebuild,
full test suite, `govulncheck`, re-run the malicious-workbook
regression test added alongside this ADR) rather than trusting the
version bump blindly.

Transitive dependencies `go get` pulled in alongside this pin
(`github.com/richardlehane/mscfb`, `github.com/xuri/efp`,
`golang.org/x/crypto`, `golang.org/x/net`, `golang.org/x/text`) are
accepted as-is, not pinned to anything more specific - ordinary `go.sum`
version resolution, the same as any other dependency bump.

#50's own remaining scope (wrapping the read path with `recover()`,
plus a wall-clock timeout) stays separately valuable after this pin,
not made moot by it: `recover()` guards against *any* future, not-yet-
discovered panic in excelize/`internal/ods`/`internal/csv` alike, not
just the four specific advisories this pin happens to fix, and a
timeout is the only mitigation that helps at all for a hang (this pin
fixes one hang-class bug specifically, but a wall-clock timeout would
also catch a hang from a cause nobody's found yet). #51 (memory
ceiling) is entirely unaffected by any of this.

## Consequences

- `go.mod`/`go.sum`: `github.com/xuri/excelize/v2` pinned to the
  pseudo-version above; its own transitive dependencies bumped
  accordingly by `go get`.
- A new regression test (constructing the real malicious workbook via
  excelize's own write API, matching this ADR's own verification)
  guards against ever regressing back to a version where this crashes.
- Closes #49 entirely (the specific crash this issue describes no
  longer reproduces, verified with a real malicious file, not just a
  CVE number). Narrows #50's own remaining scope (4 of its 8 listed
  panic/hang advisories are now resolved) but doesn't close it - the
  `recover()`/timeout work there is still its own, separate ADR and
  implementation.
- #52's own standing watch gains one concrete, actionable item: revisit
  this pin once a real tagged release ships that includes (or
  supersedes) this commit.
