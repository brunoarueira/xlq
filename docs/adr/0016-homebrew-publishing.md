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

### `brews`, not GoReleaser's `homebrew_casks`

GoReleaser soft-deprecated `brews` (its longstanding Homebrew Formula
publisher) in favor of `homebrew_casks`, now recommending Casks
generally for "binary distribution," not just GUI `.app` bundles.
Casks are a macOS-only Homebrew extension, though - Linuxbrew doesn't
support them at all - while xlq ships Linux binaries (ADR-0006) that a
Homebrew Formula installs there exactly as well as on macOS. `brews`
was kept deliberately for this reason: a Cask-based tap would silently
drop Linux support for `brew install`, where a Formula doesn't.
`brews` still works as of the pinned GoReleaser version - confirmed
locally: `goreleaser check` and `goreleaser release --snapshot
--clean --skip=publish` both only warn about the deprecated key, they
don't fail on it.

### The GoReleaser CLI version in CI is pinned, not `latest`

`release.yml`'s GoReleaser step previously used `version: latest`.
Since `brews` is explicitly marked for eventual removal, an
unannounced future GoReleaser release could drop it entirely, silently
breaking the very next tag's release run with no warning beforehand.
Pinned instead to the exact version confirmed to still support it
(`v2.18.3`, the actual latest release as of this decision - verified
via `gh api repos/goreleaser/goreleaser/releases/latest`), so
upgrading past it - and re-verifying `brews` still works, or migrating
to whatever replaces it - becomes a deliberate, reviewed step instead
of something that happens automatically the next time a release runs.

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

### Formula layout

`directory: Formula` (Homebrew's own convention for where a tap's
formula files live), `install`/`test` blocks matching every other
single-binary Homebrew formula (`bin.install "xlq"`, `system
"#{bin}/xlq --version"`), `license: "MIT"` matching ADR-0003.

## Consequences

`brew install brunoarueira/tap/xlq` works once a release ships through
the updated workflow, for both macOS and Linux - not Windows, since
Homebrew doesn't run there at all, which isn't a gap this ADR needs to
address. ADR-0006's "No package-manager publishing... yet" subsection
is now out of date; this ADR is what changed it, without ADR-0006
itself being edited.

`brews` being soft-deprecated means this isn't an indefinitely stable
choice: if GoReleaser hard-removes it in some future release,
upgrading past the pin here needs a fresh look - migrate to whatever
replaces it for cross-platform CLI distribution, or re-evaluate Casks
if Linuxbrew ever gains support - tracked as a reason to revisit this
decision later, not a problem to solve now.
