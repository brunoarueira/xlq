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

### The GoReleaser CLI version in CI is pinned, for a different reason than `brews` would have been

Migrating off `brews` removed the original reason to pin (an
unannounced hard-removal of a soft-deprecated key) - `homebrew_casks`
isn't deprecated, so that specific risk doesn't apply here. But
writing the quarantine-removal hook below surfaced a *second*, more
concrete reason to pin anyway: GoReleaser's `homebrew_casks` hook
schema is already version-sensitive in practice, not just in theory -
a newer, not-yet-released schema renames `hooks.post.install` to
`install_steps` and replaces the Ruby `staged_path` method with a
`{{ .StagedPath }}` GoReleaser template variable, discovered firsthand
while getting this hook to actually validate (see below). `release.yml`
is pinned to `v2.18.3` - the version this hook's exact syntax is
verified against - so an unattended `latest` upgrade can't silently
reintroduce that same breakage on some future release between now and
whenever this pin is next deliberately revisited.

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

### A macOS quarantine-removal hook, since xlq's binaries aren't signed

xlq's binaries aren't signed or notarized (nothing in ADR-0006 does
that). A binary downloaded through `brew install` on macOS still gets
the `com.apple.quarantine` extended attribute, and Gatekeeper refuses
to run a quarantined, unsigned binary at all - not a rough edge, per
GoReleaser's own `homebrew_casks` docs: "Not following this might lead
to your app or binary not running," surfacing to the user as "xlq is
damaged and cannot be opened." A `hooks.post.install` block (compiles
to a `postflight do ... end` in the generated cask) strips it:
`if OS.mac? system_command "/usr/bin/xattr", args: ["-dr",
"com.apple.quarantine", "#{staged_path}/xlq"] end` - `staged_path` is
Homebrew's own Cask DSL method for the install directory inside a
hook, not a GoReleaser template variable (an earlier draft of this
hook used a `{{ .StagedPath }}` GoReleaser template placeholder
instead, following GoReleaser's current docs - that field doesn't
exist in the pinned CLI version, v2.18.3, caught immediately by
`goreleaser check` failing; the docs describe a newer, not-yet-released
schema).

A first version of this hook used `on_macos do run ... end` instead,
confirmed only via a local snapshot build (`goreleaser release
--skip=publish`) - which checks that GoReleaser itself renders the
cask without erroring, not that the result actually installs. It
doesn't: `on_macos` is a cask DSL helper valid in a cask's main body,
not inside a `postflight` block, and plain `postflight` doesn't expose
a `run` method either (only `system_command` does) - `brew install`
with that version failed outright and rolled back the whole cask,
caught only once a real install was actually attempted, backfilling
v0.2.0's cask after the fact (no release had shipped through this
pipeline yet to catch it sooner). Verified the fix the same way: a real
`brew install` of the corrected cask succeeds, the binary runs, and the
quarantine attribute is gone (`xattr -l` on the installed binary shows
no `com.apple.quarantine` entry).

This bypasses Gatekeeper's verification rather than fixing its root
cause (no code signing), a tradeoff explicitly called out by
GoReleaser's own docs as appropriate "only when code signing is not
feasible" - true here: notarization needs a paid Apple Developer
account, which this project doesn't have reason to acquire for a free
CLI tool.

### Explicitly deferred

- A `test` block equivalent to the Formula mechanism's `test do ...
  end` - `homebrew_casks`' schema doesn't document one the way `brews`
  did.

## Consequences

`brew install brunoarueira/tap/xlq` works once a release ships through
the updated workflow, for both macOS and Linux - not natively on
Windows (Homebrew itself doesn't run there outside WSL2), which isn't
a gap this ADR needs to address. No `--cask` flag is needed despite
the tap having only a cask, not a formula, named `xlq`: `brew install
<name>` tries a formula first and falls back to a cask automatically
when none exists (`brew`'s own `Library/Homebrew/cli/named_args.rb`,
`load_formula_or_cask` - verified directly against that source, not
just a doc page), printing `Warning: Treating xlq as a cask.` rather
than erroring. ADR-0006's "No package-manager publishing... yet"
subsection is now out of date; this ADR is what changed it, without
ADR-0006 itself being edited.

Unlike the `brews` path this ADR initially (incorrectly) chose,
`homebrew_casks` itself has no known removal risk to track - it's
GoReleaser's currently-supported, non-deprecated mechanism for exactly
this. The CLI version is still pinned, though, for the schema-drift
reason above: upgrading past `v2.18.3` needs the quarantine hook's
exact syntax re-verified against whatever version comes next, the
same way getting it right against `v2.18.3` itself needed two
corrections.
