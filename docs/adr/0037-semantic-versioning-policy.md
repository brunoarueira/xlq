# 37. Semantic versioning policy, including a pre-1.0 reading

## Status

Accepted

## Context

Issue #135: ADR-0006 picked `VERSION`'s file format ("plain semver, no
`v` prefix") and the tagging mechanism, but never actually committed to
semver's *policy* - what decides whether a given change bumps the
major, minor, or patch number. In practice that decision has already
been made, release by release, entirely in PR/conversation history, not
written down anywhere a contributor (or a future session) could check
it against:

| Bump | Release | What changed |
| --- | --- | --- |
| patch | 0.1.0 -> 0.1.1 -> 0.1.2 | early fixes, no filter-language change |
| minor | 0.1.2 -> 0.2.0 | new functionality |
| minor | 0.2.0 -> 0.3.0 | the public docs site - reasoned explicitly at the time as "minor not major, since everything is additive" |
| patch | 0.3.0 -> 0.3.1 | the Homebrew `postflight` hook fix - pipeline-only, no filter-language change at all |
| minor | 0.3.1 -> 0.4.0 | seven new builtins across four PRs (`upper`, `lower`, `trim`, `snake_case`, `camel_case`, `replace`, `headers`) |

Every one of these bumps already matches ordinary semver *in spirit* -
this ADR is making that explicit and permanent, not changing course.

## Decision

### xlq follows [semantic versioning](https://semver.org/)

`MAJOR.MINOR.PATCH`. Standard meaning once past 1.0.0: `MAJOR` for a
breaking change, `MINOR` for backward-compatible new functionality,
`PATCH` for a backward-compatible fix.

### Pre-1.0 (`0.y.z`): `MAJOR` stays `0` deliberately; `MINOR` and `PATCH` still carry their ordinary meaning

Strict semver (spec item 4) says `0.y.z` is "for initial development...
the public API should not be considered stable" - read literally, that
would mean *anything* can change in a patch release pre-1.0, which
isn't actually how `VERSION` has been bumped here in practice (the
table above already draws the minor/patch line the ordinary way, every
time). xlq's own reading, made explicit here: while `MAJOR` stays
pinned at `0` (no 1.0.0 yet - deliberately out of scope for this ADR,
which is about the bump *rule*, not a 1.0 timeline or readiness
criteria), `MINOR` and `PATCH` are read exactly like they would be
post-1.0:

- **`PATCH`**: a backward-compatible fix with no new functionality - a
  bug fix (correcting behavior that was already wrong, not adding a
  capability that didn't exist before), a docs-only change, a
  release-pipeline-only change (0.3.0 -> 0.3.1's Homebrew hook fix is
  the clearest example so far: it changed nothing about `xlq` itself,
  only how its release artifacts are built). A bug fix necessarily
  changes the behavior a user sees (that's the fix) - "no new
  functionality" is the actual line between this and `MINOR`, not "no
  observable change."
- **`MINOR`**: new, backward-compatible functionality - a new builtin,
  a new flag, a new output mode, a new supported input format. Still
  pre-1.0, so there's no compatibility *promise* being made the way a
  real 1.x `MINOR` bump would carry, but the bump itself signals "look
  here for what's new," which is the whole point of having the number
  at all.
- **A breaking change bumps `MINOR` too, not `PATCH`** - pre-1.0, there
  is no `MAJOR` to bump for it instead, and a breaking change
  self-evidently isn't just a fix. Examples worth naming concretely,
  since "breaking" is otherwise a judgment call: removing or renaming a
  builtin, changing a JSON output shape, changing a CLI flag's
  existing behavior or meaning (not just adding a new one), dropping
  support for a previously-supported platform/`GOOS`. None of these
  has happened yet - this is a definition for when one does, not a
  retroactive reclassification of anything in the table above.

### Out of scope

A 1.0.0 timeline or readiness criteria (what would actually need to be
true for `MAJOR` to leave `0`) - this ADR is only about the bump rule
itself, not when or whether to apply it toward a 1.0 milestone.

## Consequences

Opening a release PR has a concrete rule to check a change against,
instead of needing to re-derive the reasoning each time the way the
table above's entries originally did. `CONTRIBUTING.md`'s Releasing
section now points here.
