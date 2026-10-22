# 28. Correct ADR-0016's postflight hook claim

## Status

Accepted

## Context

While backfilling the `brunoarueira/homebrew-tap` repo with a cask for
the already-published v0.2.0 release (Homebrew publishing was added to
`.goreleaser.yaml` after that tag, so no real release had gone through
this pipeline yet to catch the bug below), actually installing the
generated cask surfaced a real bug: the quarantine-removal
`hooks.post.install` block used `on_macos do run "/usr/bin/xattr",
args: [...] end`. `on_macos` is a cask DSL helper valid in a cask's
main body, not inside the `postflight do ... end` block GoReleaser
wraps a `hooks.post.install` value in, and plain `postflight` doesn't
expose a `run` method either - only `system_command` does. Verified
directly: `brew install` with that hook failed outright and rolled
back the whole cask (the Caskroom entry removed entirely, not merely a
warning).

ADR-0016 documents this exact `on_macos`/`run` hook as its own
decision, and describes it as "confirmed via a local snapshot build"
(`goreleaser release --skip=publish`). That verification method was
real but insufficient: a snapshot build only confirms GoReleaser can
*render* the cask file without erroring; it never attempts an actual
`brew install`, so it couldn't have caught a hook that renders fine
but fails at install time. The corrected hook (shown in the Decision
section below) was verified more thoroughly this time: a real `brew
install` via a disposable local tap, confirming the binary installs,
runs, and has its `com.apple.quarantine` attribute actually stripped
(`xattr -l` on the installed binary shows no such
entry).

Per `CONTRIBUTING.md`'s own convention, an accepted ADR isn't edited -
a later correction gets a new ADR instead, the way ADR-0019 corrected
ADR-0018 without touching it. This isn't a changed decision (the
intent - strip quarantine on macOS post-install - is unchanged); it
corrects what ADR-0016 says about the hook's own syntax and how that
syntax was verified.

## Decision

Read ADR-0016's quarantine-removal hook example as:

```ruby
if OS.mac?
  system_command "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "#{staged_path}/xlq"]
end
```

not `on_macos do run "/usr/bin/xattr", args: [...] end` as originally
written - the `.goreleaser.yaml` `hooks.post.install` value this
compiles from is corrected to match (a plain bug fix to the actual
pipeline, not itself a design decision needing its own ADR).

Read its "confirmed via a local snapshot build" claim as insufficient
on its own: a snapshot build confirms the cask *renders*, not that it
*installs*. The corrected hook above is verified by an actual `brew
install` (via a disposable local tap, so nothing is pushed anywhere
real), not just a snapshot build - that's the verification standard
this kind of claim needs going forward.

## Consequences

No change to what ADR-0016 actually decided (strip quarantine via a
`postflight` hook, bypass Gatekeeper rather than sign/notarize) or to
any other part of its Context/Decision/Consequences - only this one
code example and the claim about how it was checked. `.goreleaser.yaml`
itself already has the corrected hook, independent of this ADR.
Anyone reading ADR-0016 on its own should also read this one;
`docs/adr/README.md`'s table is updated to point there, the same way
ADR-0018's own row points to ADR-0019.
