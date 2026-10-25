# 32. Upgrade GoReleaser to v2.19.0 for `postflight_steps`

## Status

Accepted

## Context

Homebrew deprecated the free-form Ruby `postflight do ... end` cask
hook in favor of a declarative `postflight_steps do ... end` (only
specific helpers like `run`/`on_macos`, no arbitrary Ruby). Every
`brew tap`/`brew install` of xlq started printing:

```
Warning: Calling `postflight` is deprecated! Use `postflight_steps` instead.
```

ADR-0016 pinned GoReleaser to `v2.18.3` and documented the
quarantine-removal hook using exactly the now-deprecated form:
`hooks.post.install` compiling to `postflight do ... end`, with
`#{staged_path}` as Homebrew's own Ruby method. That ADR's own
Consequences section anticipated needing to revisit this: "upgrading
past `v2.18.3` needs the quarantine hook's exact syntax re-verified
against whatever version comes next." GoReleaser v2.19.0 (released
2026-10-22, three days before this ADR) is that next version: it adds
`install_steps`/`uninstall_steps` under `homebrew_casks.hooks.{pre,post}`
specifically to render the new `postflight_steps` form instead.

Per CONTRIBUTING.md, an `Accepted` ADR isn't edited when a later
decision changes course - this one supersedes the relevant parts of
ADR-0016 (the pinned version and the hook's exact syntax) without
touching it, the same way ADR-0028 already corrected a different part
of ADR-0016 without editing it.

## Decision

Bump the pinned GoReleaser version in `.github/workflows/release.yml`
from `v2.18.3` to `v2.19.0`, and change `.goreleaser.yaml`'s
quarantine-removal hook from `hooks.post.install` to
`hooks.post.install_steps`:

```yaml
hooks:
  post:
    install_steps: |
      on_macos do
        run "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "{{ .StagedPath }}/xlq"]
      end
```

Three syntax changes from the actual pre-PR `.goreleaser.yaml` (not
ADR-0016's own hook example, which already showed `on_macos`/`run` -
that was the broken form ADR-0028 replaced with `if OS.mac?`/
`system_command`, the version this PR actually started from), all
load-bearing:

- `on_macos do ... end`, not `if OS.mac? ... end` - `postflight_steps`
  exposes `on_macos` as a step-DSL conditional, not a bare Ruby `if`.
- `run`, not `system_command` - `postflight_steps` only exposes a
  fixed set of declarative helpers, not arbitrary Ruby method calls.
- `{{ .StagedPath }}`, not `#{staged_path}` - this is now a GoReleaser
  template variable, resolved when the cask file is generated, not a
  Homebrew Ruby method resolved at install time. Confirmed via a local
  snapshot build (`goreleaser release --snapshot --clean
  --skip=publish,sign,announce`) that `{{ .StagedPath }}` renders to
  the literal string `{{staged_path}}` in the generated cask - that's
  Homebrew's own steps-DSL placeholder for the staged install
  directory, substituted a second time at install time. This matches
  GoReleaser's own upstream golden test fixture for this exact
  pattern (`internal/pipe/cask/testdata/TestFullPipe/hooks_steps.rb.golden`),
  not just its docs.

Verified further than rendering alone, following ADR-0028's own
precedent that a snapshot build only proves the cask *renders*, not
that it *installs*: did a real `brew install` against a disposable
local tap built from the snapshot output. No deprecation warning,
cask installed cleanly, binary ran.

## Consequences

`brew tap`/`brew install` no longer print the `postflight` deprecation
warning once a release ships through the updated pipeline (v0.3.1+ -
the v0.3.0 cask already live in `brunoarueira/homebrew-tap` still uses
the old syntax until the next release regenerates it).

ADR-0016's Decision section (the `#{staged_path}`/`system_command`
example and the `v2.18.3` pin) and the "CLI version is still pinned"
line in its Consequences section are now out of date; this ADR is what
changed them, the same way ADR-0016 itself left ADR-0006 out of date
without editing it. The pin stays, for the same reason ADR-0016 gave:
GoReleaser's `homebrew_casks` hook schema is version-sensitive in
practice, confirmed twice now (ADR-0028's `on_macos`/`run` bug, and
this `install_steps` rename) - an unattended `latest` upgrade could
reintroduce that kind of breakage on some future release. Upgrading
past `v2.19.0` needs this hook's syntax re-verified again first.
