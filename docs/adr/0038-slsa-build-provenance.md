# 38. SLSA Build L3 provenance for release binaries

## Status

Accepted

## Context

Issue #129 (M10): a user (or an automated consumer, e.g. a package
manager) downloading an `xlq` release binary has no way to verify it
was actually built by this repo's own GitHub Actions workflow, from
the exact source commit it claims, rather than tampered with in
between or substituted entirely.

GoReleaser has no native provenance generation (confirmed: its own
[blog post on this exact
topic](https://goreleaser.com/blog/slsa-generation-for-your-artifacts/)
recommends a separate job calling
[`slsa-framework/slsa-github-generator`](https://github.com/slsa-framework/slsa-github-generator)'s
reusable workflow after GoReleaser runs, not a GoReleaser config
option). That project's own `builder_go_slsa3.yml` - a dedicated Go
builder that performs the build itself inside the trusted workflow, a
stronger hermetic-build guarantee than wrapping an existing build step
- was considered and rejected: adopting it would mean giving up
GoReleaser's own build/archive/checksum/Homebrew-cask pipeline
entirely, which the generic-generator approach doesn't require.

## Decision

### A second job, `binary-provenance`, calling the generic generator after GoReleaser runs

`.github/workflows/release.yml`'s existing `tag-and-release` job gains
one more step after the GoReleaser step: base64-encode
`dist/checksums.txt` (GoReleaser's own checksum output, already in the
exact `sha256sum`-compatible format - `<hex digest>  <filename>` per
line - the generator's `base64-subjects` input requires) and expose it
as a job output, `hashes`. A new `binary-provenance` job, gated on that
output being non-empty (empty on a no-op run, where the tag already
existed and GoReleaser never ran - the same idempotent-by-construction
shape ADR-0006 already established), calls
`generator_generic_slsa3.yml` with it, `upload-assets: true` attaching
the resulting `*.intoto.jsonl` provenance file to the GitHub Release.

### `upload-tag-name` is required here, not optional

The generator's own integration examples for most ecosystems - its
GoReleaser one included - show `upload-assets: true` alone, no
`upload-tag-name`. Those all assume the whole calling workflow is
*triggered by* the tag push itself, so `github.ref` already is
`refs/tags/vX.Y.Z` by the time the provenance job runs, and the
generator can infer which release to attach to from that. This
workflow deliberately isn't triggered that way (ADR-0006: it creates
and pushes the tag itself, inside a run triggered by a push to `main`,
specifically because GitHub doesn't fire a second workflow run for a
tag pushed with `GITHUB_TOKEN`) - so `github.ref` here is always
`refs/heads/main`, never the tag, for the entire run including this
job. `upload-assets: true` alone would have silently uploaded nothing -
caught in review, confirmed against the generator's own README, which
documents exactly `upload-assets` + `upload-tag-name` together for a
non-tag-triggered caller. Fixed by exposing the tag `tag-and-release`
already computes (`steps.version.outputs.tag`) as a second job output
and passing it through.

The checksum file's name is read directly, not discovered by parsing
`goreleaser-action`'s own `artifacts` JSON output with `jq` (an
alternative the GoReleaser blog post itself demonstrates) - `.goreleaser.
yaml`'s `checksum.name_template` already pins it to exactly
`checksums.txt`, so there's nothing to discover.

### Pinned by the exact `@vX.Y.Z` tag, not a commit SHA - a deliberate exception to this repo's usual pinning convention

Every other `uses:` line in this repo's workflows pins to a full commit
SHA with a version comment (`actions/checkout@3d3c42e...# v7.0.1`,
etc.). `slsa-github-generator`'s reusable workflow is pinned to
`@v2.1.0` instead, confirmed against that project's own README before
writing this, not assumed: it **requires** being referenced by its
exact semantic-version tag - a shorter tag like `@v2.1` or `@v2` fails
the build outright, and pinning by commit SHA isn't supported at all -
because `slsa-verifier` (the tool that later checks a provenance file
against the workflow that produced it) validates the generator's own
ref as part of that check, and can only do so against a recognized
`@vX.Y.Z` tag, not an arbitrary SHA. The project's own README says this
plainly: it's "contrary to the GitHub best practice for third-party
actions... but intentional due to limits in GitHub Actions" (tracked
upstream as
[slsa-verifier#12](https://github.com/slsa-framework/slsa-verifier/issues/12)).
This is the one place in this repo where tag-pinning is the correct
choice, not an inconsistency to fix later.

## Consequences

Every release from v0.4.1 onward (the current v0.4.0 predates this)
carries a `*.intoto.jsonl` provenance file, verifiable with
`slsa-verifier` against the exact workflow run and source commit that
produced it. #130 (cosign signing) and #131 (SBOM generation) are
separate, independent pieces under the same M10 milestone; #132
documents how a user actually runs the verification once all three
have landed.
