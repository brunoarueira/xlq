# 41. Create the release as a draft, publish it only after provenance lands

## Status

Accepted

## Context

v0.4.1 - the first real release to exercise SLSA provenance (#129,
ADR-0038), cosign signing (#130, ADR-0039), and SBOM generation (#131,
ADR-0040) together - failed at the `binary-provenance` job with `Cannot
upload assets to an immutable release.` GoReleaser's own run (building,
signing, generating SBOMs, creating and publishing the GitHub Release)
succeeded completely; the separate `binary-provenance` job then tried
to attach `multiple.intoto.jsonl` to that same release afterward, and
GitHub rejected it outright.

The cause is GitHub's [Immutable
Releases](https://github.com/orgs/community/discussions/178351) (now
generally available): once a release is published, its assets can
never be added to, replaced, or removed again, full stop - the only
documented way to attach an asset generated *after* the release was
created is to not have published it yet. This repo's pipeline
inherently needs that: SLSA provenance has to attest to artifacts that
already exist (GoReleaser's own output), and the generator that
produces it runs as a genuinely separate, later workflow job -
deliberately so, per ADR-0038's own reasoning (isolating provenance
generation from the build is the whole point of the trust model, not
something to collapse back into one job just to dodge this).

v0.4.1 itself is now stuck exactly as GoReleaser left it - published,
immutable, missing only the provenance file - permanently; there is no
way to retroactively attach it. Not worth a corrective release on its
own; the next real `VERSION` bump carries the fix and produces the
first release with all four artifacts (binaries, checksums, cosign
bundle, SBOMs, and provenance) together.

## Decision

### `.goreleaser.yaml`: `release.draft: true`

GoReleaser creates the release but leaves it unpublished. Every other
GoReleaser-produced asset (archives, checksums, the cosign bundle, the
SBOMs) still gets attached during that same run, since a draft release
accepts uploads exactly like any other - immutability only begins at
publish.

### A new `publish-release` job, after `binary-provenance` succeeds

```yaml
publish-release:
  needs: [tag-and-release, binary-provenance]
  if: needs.tag-and-release.outputs.hashes != ''
  permissions:
    contents: write
  steps:
    - run: gh release edit "$TAG" --repo "${{ github.repository }}" --draft=false
```

Publishing is a separate, final job rather than a step appended to
`binary-provenance` - that job is a call to a reusable workflow
(`generator_generic_slsa3.yml`), not a sequence of steps this repo
controls, so there's nowhere to append a step to it even if that were
desirable. Gated on `binary-provenance` actually succeeding (the normal
`needs:` dependency semantics - a job doesn't run if one of its `needs`
failed), so a provenance failure leaves the release as an incomplete,
unpublished draft rather than a published-but-missing-a-security-artifact
release - the same "fail loudly, don't silently ship incomplete"
instinct the rest of this pipeline already has.

### Known gap: a stuck draft can't currently be resumed by re-running the workflow

If `binary-provenance` fails for an unrelated, transient reason after
GoReleaser has already created the draft, a `workflow_dispatch` retry
won't help: the "tag already exists" check (ADR-0006) treats any
existing tag as "already released," skipping straight past
tag-creation, GoReleaser, and everything else - it has no way to detect
"a draft exists for this tag but was never published" and resume from
there. The manual fallback - `gh release edit <tag> --draft=false`, or
re-running just the `binary-provenance` job via the Actions UI if the
draft and its other assets are still intact - is accepted for now
rather than building automatic resume logic for what should be a rare
failure mode (the provenance job succeeded on its only real run before
this fix). Worth a proper fix if it actually recurs.

### Accepted tradeoff: a brief window where the release exists but isn't publicly downloadable

Between GoReleaser creating the draft and `publish-release` completing,
anyone trying to download a release asset (including the Homebrew cask
GoReleaser pushes to the tap repo in that same run, which references
this release's eventual public URLs) would find the release
not-yet-visible. This window is as long as `binary-provenance` takes to
run - not instant, but not the kind of delay anyone is likely to hit in
practice. Strictly better than the alternative this ADR fixes (the
pipeline failing outright, publishing nothing additional at all).

## Consequences

The next real release (the one that ships this fix) is the first to
actually test the draft-then-publish sequencing end to end, and the
first with all four of binaries, checksums, cosign signature, SBOMs,
and SLSA provenance together - also unblocking #132 (verification
docs), which needs a complete real release to write accurate commands
against.
