# 16. Homebrew package publishing via GoReleaser

## Status

Accepted

## Context

ADR-0006 explicitly deferred this: "No package-manager publishing
(Homebrew tap, etc.) yet... Adding one is a config change to
`.goreleaser.yaml` when it's actually wanted, not a reason to hold up
this ADR." Issue #17 asked for exactly that, once a tap repo existed
(`brunoarueira/homebrew-tap`, created and set up as its own
prerequisite). This ADR records the decision now that it's actually
been made and implemented - superseding ADR-0006's "not yet" stance
for this one piece, without editing that ADR: per CONTRIBUTING.md's
own convention, an `Accepted` ADR isn't edited when a later decision
changes course, the way ADR-0009 corrected ADR-0008 without touching
it.

## Decision

### `homebrew_casks`, not GoReleaser's older `brews`

GoReleaser soft-deprecated `brews` (its longstanding Homebrew Formula
publisher) in favor of `homebrew_casks`. An earlier version of this
decision kept `brews` anyway, on the belief that Casks only work on
macOS - that belief was wrong, caught in review, and is worth
recording precisely since it reverses the actual decision: Homebrew
Casks support Linux too, via `on_macos`/`on_linux` blocks inside the
same cask, and GoReleaser already generates exactly that split
automatically from whichever `goos` targets `builds`/`archives`
produce - confirmed locally (`goreleaser release --snapshot --clean
--skip=publish`), the generated `Casks/xlq.rb` has both an `on_macos`
block (arm/intel) and an `on_linux` block (arm/intel), covering every
archive this project actually builds. `homebrew_casks` is also not
deprecated at all - `brews` was the one soft-deprecation here, not a
choice between two equally-supported options - so there's no future
removal risk to plan around either.

### No GoReleaser CLI version pin needed

Since `homebrew_casks` isn't deprecated, there's no analog to `brews`'
removal risk to hedge against by pinning the GoReleaser CLI version in
`release.yml` - it stays on `version: latest`, as it already was
before this change.

### A separate token, not the default `GITHUB_TOKEN`

GoReleaser needs write access to `brunoarueira/homebrew-tap` - a
different repository from the one the release workflow runs in - and
the default `GITHUB_TOKEN` a workflow gets is scoped only to the repo
running it. A fine-grained personal access token scoped to just the
tap repo (Contents: read and write), stored as a
`HOMEBREW_TAP_GITHUB_TOKEN` repository secret on `xlq`, is what
GoReleaser's `repository.token` field uses instead. Creating and
wiring up that token is manual, one-time setup outside any PR's reach
- documented in `CONTRIBUTING.md`'s Releasing section as an
operational step, not decided here as a design choice.

### Cask layout

`binaries: [xlq]` (the plural form - `binary:`, singular, is itself
separately deprecated as of GoReleaser 2.12.6), `name`/`homepage`/
`description` matching `.goreleaser.yaml`'s existing `release` block
conventions, no `directory:` override (`Casks/`, the default, is
already Homebrew's own convention for a tap's cask files - unlike a
Formula, which needed an explicit `directory: Formula`).

### Explicitly deferred

- A macOS quarantine-removal post-install hook, which GoReleaser's own
  `homebrew_casks` docs mention as sometimes needed for an unsigned
  binary. xlq's binaries aren't signed or notarized (nothing in
  ADR-0006 does that), so this may eventually matter for a smooth
  first-run experience on macOS - worth revisiting if it turns out to
  be needed in practice, not decided here.
- A `test` block equivalent to the Formula mechanism's `test do ...
  end` - `homebrew_casks`' schema doesn't document one the way `brews`
  did.

## Consequences

`brew install brunoarueira/tap/xlq` works once a release ships through
the updated workflow, for both macOS and Linux - not natively on
Windows (Homebrew itself doesn't run there outside WSL2), which isn't
a gap this ADR needs to address. ADR-0006's "No package-manager
publishing... yet" subsection is now out of date; this ADR is what
changed it, without ADR-0006 itself being edited.

Unlike the `brews` path this ADR initially (incorrectly) chose, this
one has no known future-removal risk to track - `homebrew_casks` is
GoReleaser's currently-supported, non-deprecated mechanism for exactly
this.
