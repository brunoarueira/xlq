# 42. Attach provenance via its own build artifact, not the generator's upload path

## Status

Accepted

## Status of ADR-0041

ADR-0041's `release.draft: true` + a final `publish-release` job is
still the right shape - this ADR corrects what runs *inside*
`publish-release`, which ADR-0041 got wrong about the SLSA generator's
own `upload-assets`/`upload-tag-name` inputs.

## Context

v0.4.2 was the first release attempted under ADR-0041's fix, and it
failed too - at a different step than v0.4.1, and in a way that did
real, permanent damage: `slsa-framework/slsa-github-generator`'s own
`upload-assets: true` + `upload-tag-name` combination looks up "the
release for this tag" via GitHub's get-release-by-tag API to decide
where to attach the provenance file. That API **never returns a draft
release** - confirmed directly, not assumed: it didn't find the draft
`tag-and-release` had already created for `v0.4.2`, and
`softprops/action-gh-release` (what the generator uses internally)
responded to "no release found" by creating a **second, new** release
for the same tag - published by that path's own default, so already
immutable by the time the very same step tried to attach the
provenance to it. Confirmed by inspecting the repo's actual releases
during the failure: two separate release objects existed for
`v0.4.2`, one real (the draft, with all of GoReleaser's assets) and one
bogus (empty, already immutable).

Deleting the bogus release didn't recover the tag. GitHub's Immutable
Releases ties the "used by an immutable release" state to the **tag
name itself**, not the specific release object - confirmed the hard
way: after deleting the bogus release and successfully uploading the
real provenance file to the real draft, publishing it failed with `tag_name
was used by an immutable release`. The tag is burned permanently,
confirmed against third-party reports of the same behavior (no official
GitHub doc describes it, but every independent report agrees: deleting
an immutable release does not free its tag for reuse). `v0.4.2` itself
is unrecoverable - its draft release was deleted, and the tag can never
have a release again. The next `VERSION` bump goes straight to v0.4.3.

## Decision

### `binary-provenance` only generates and signs - no `upload-assets`, no `upload-tag-name`

Removed both inputs entirely, rather than looking for a way to make the
generator's own upload path draft-aware (there isn't one - it has no
input for "attach to this specific release ID," only tag-based
lookup). `contents: write` is also removed from this job's own
permissions, since nothing in it touches the release anymore - `actions:
read` and `id-token: write` (to read the workflow path and sign) are
the only permissions actually used now.

### `publish-release` downloads the provenance as a workflow artifact and attaches it itself

The generator still exposes `provenance-name` (confirmed in its own
README's Workflow Outputs table) - the name of a plain GitHub Actions
build artifact it uploads via the ordinary workflow-artifact mechanism,
not a release asset. Any job in the same workflow run can retrieve it
with `actions/download-artifact` and just that name - confirmed
directly, not assumed: downloaded the real artifact from the failed
v0.4.2 run after the fact and verified it was a complete, validly-signed
provenance file.

```yaml
- uses: actions/download-artifact@<sha> # v8.0.2
  with:
    name: ${{ needs.binary-provenance.outputs.provenance-name }}
- run: |
    gh release upload "$TAG" "$PROVENANCE_NAME" --repo "${{ github.repository }}"
    gh release edit "$TAG" --repo "${{ github.repository }}" --draft=false
```

`gh release upload` targets the release by tag name too, but it's being
run from *this* repo's own workflow, with no ambiguity about which
release it means (there's exactly one release per tag once the bogus
one above can never happen again) - unlike the generator's own
`softprops/action-gh-release`-based path, there's no "didn't find one,
so create a new one" fallback behavior to trigger this failure mode a
second time.

## Consequences

Provenance generation stays isolated in the generator's own trusted
job (ADR-0038's whole point); attaching it to the release is this
repo's own responsibility now, using a mechanism (a known build
artifact, downloaded and uploaded explicitly) that doesn't depend on
the generator correctly discovering a draft release it has no way to
find. The next `VERSION` bump (v0.4.3) is both the first real test of
this corrected sequencing and, if it succeeds, the first release with
all five artifacts (binaries, checksums, cosign bundle, SBOMs,
provenance) together - still unblocking #132.

A tag that's ever had any release (even a bogus, accidental one) go
immutable can never be reused for a release again, permanently - worth
remembering as a hard constraint on this pipeline specifically, not
just an incident footnote: any future change that could cause a second,
unintended release object to be created for an in-flight tag (as the
generator's own upload path did here) risks burning that tag the same
way, with no recovery possible.
