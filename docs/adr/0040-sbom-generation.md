# 40. SBOM generation for release archives

## Status

Accepted

## Context

Issue #131 (M10): SLSA provenance (#129, ADR-0038) and a cosign
signature (#130, ADR-0039) cover authenticity and integrity; neither
tells a downstream consumer (a vulnerability scanner, a
security-conscious package maintainer) what's actually *inside* a
release archive. A software bill of materials (SBOM) is the standard,
low-effort companion for that - not part of the SLSA spec itself, but
expected alongside it in practice.

## Decision

### GoReleaser's built-in `sboms:` config, Syft, SPDX JSON - all defaults

```yaml
sboms:
  - artifacts: archive
```

No reason to override any of GoReleaser's own defaults here: Syft (the
most widely adopted SBOM generator, also already used by
`slsa-github-generator`'s own ecosystem) producing SPDX JSON - GoReleaser's
own default output format, and one of the two leading standardized SBOM
exchange formats generally (the other being CycloneDX; Syft itself
supports several more output formats beyond either, including its own
native `syft-json` and `spdx-tag-value`/`cyclonedx-xml` variants, none
of which there's a reason to pick over the GoReleaser default here) -
one SBOM per archive (`archive`, not `binary` - covers what a user
actually downloads).

Verified against the exact pinned GoReleaser version (`v2.19.0`) with a
real local snapshot build, not assumed from the docs: `sboms:` produces
one `<archive-name>.sbom.json` per archive, each a genuine SPDX 2.3
document. More importantly, confirmed directly against GoReleaser's own
source (`internal/artifact/artifact.go`'s `ReleaseUploadableTypes()`),
not inferred from the docs or the local `dist/` output alone, that
`SBOM` is one of the artifact types GoReleaser's release-upload step
uploads automatically - the exact thing issue #131 flagged as "not
confirmed from the docs alone."

### Syft installed via `anchore/sbom-action/download-syft`, with `syft-version` pinned explicitly

Syft isn't bundled with GoReleaser or preinstalled on GitHub-hosted
runners. `anchore/sbom-action/download-syft` (Anchore's own official
sub-action for exactly this) is used rather than a hand-rolled
curl-based install - it's already the maintained, documented way to get
Syft into a CI runner's `PATH` for another tool to shell out to, not
something this repo needs to reinvent.

Confirmed directly from the action's own bundled source
(`dist/index.cjs`, not just its README) before relying on it: its
`getSyftCommand()` unconditionally calls `addPath()` on the resolved
Syft binary's directory, so GoReleaser's own `cmd: syft` default (a
bare command name, expected to resolve via `PATH`) works without any
extra wiring - the README only documents the step's `cmd` *output* for
explicit reference elsewhere, which could easily have been read as "you
must wire this up yourself," so it was worth checking the actual
behavior rather than guessing from that alone.

`syft-version` is pinned explicitly to `v1.55.0` (the version tested
locally), not left at this action version's own default - checked
directly in that same bundled source (a hardcoded `v1.54.0` constant,
confirmed, not assumed) and found to differ from what was tested. The
same lesson ADR-0039 already applied to `cosign-installer`'s own
default (`v3.0.6` there, differing from the `v3.1.4` tested) recurred
here with a different tool - worth generalizing: *any* installer
action's own default is a claim to verify against its actual source for
the pinned version in use, not an assumption to carry over from "I
tested with whatever was on my machine."

## Consequences

Every release from v0.4.1 onward carries one `<archive>.sbom.json`
(SPDX JSON) per platform archive, uploaded to the GitHub Release
alongside the binaries, checksums, cosign bundle, and SLSA provenance.
This is the last independent piece under M10; #132 documents end-to-end
verification (provenance, signature, SBOM together) now that all three
have landed.
