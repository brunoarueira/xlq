# 39. Sign release checksums with cosign (keyless/OIDC)

## Status

Accepted

## Context

Issue #130 (M10): SLSA provenance (#129, ADR-0038) attests a release
binary came from this repo's workflow; it doesn't let a user verify a
downloaded file against a cryptographic signature directly - the more
familiar, independently-useful verification primitive, and the one
most package-manager/security-scanner tooling actually checks for.

[cosign](https://github.com/sigstore/cosign)'s keyless signing (GitHub
Actions' own OIDC identity as the signing credential, via Sigstore's
Fulcio/Rekor) was chosen over a traditional key pair for the same
reason this repo already avoids managing secrets where it can: no key
to generate, store as a repository secret, or rotate - the signing
credential is the workflow run's own, already-ephemeral identity.

## Decision

### GoReleaser's `signs:` config, signing the checksum file only - not every archive individually

```yaml
signs:
  - cmd: cosign
    signature: "${artifact}.sigstore.json"
    args:
      - "sign-blob"
      - "--bundle=${signature}"
      - "${artifact}"
      - "--yes"
    artifacts: checksum
```

Signing `checksums.txt` (one signature) rather than each of the five
per-platform archives individually is sufficient: the checksum file
already commits to every archive's own SHA-256 digest, so one verified
signature on it transitively vouches for all of them - the same
one-signature-covers-many-files shape SLSA's own provenance file
already has (ADR-0038).

Verified directly against the pinned GoReleaser version (`v2.19.0`,
the same local binary CI uses) and cosign `v3.1.4`: `goreleaser check`
validates this config, and a full local snapshot release - with
`--key`/a local disposable key pair swapped in for the keyless flow,
since that needs a real GitHub Actions OIDC context no local run can
provide - actually invoked `cosign sign-blob` correctly, produced a
real `checksums.txt.sigstore.json` bundle, and `cosign verify-blob`
confirmed it. This project has been burned twice before by trusting a
GoReleaser config snippet against the wrong schema version without
running it (ADR-0016, ADR-0028) - not repeating that here.

### `sigstore/cosign-installer`, pinned by commit SHA like every other action in this repo

cosign isn't bundled with GoReleaser or preinstalled on GitHub-hosted
runners, so a step installs it before the GoReleaser step runs.
Unlike `slsa-github-generator` (ADR-0038, which *requires* tag-pinning
for its own verifier compatibility), `cosign-installer` has no such
constraint - pinned by commit SHA (`@<sha> # v4.1.2`), the normal
convention here.

`cosign-release` is pinned explicitly to `v3.1.4`, not left at this
installer version's own default. The README's "the used Cosign version
only changes when cosign-installer is upgraded" reads like that default
alone would already give a fixed, known version once the installer
itself is SHA-pinned - checked directly against `cosign-installer`
`v4.1.2`'s own `action.yml` rather than assumed from that one sentence,
and its hardcoded default is `v3.0.6`, not the `v3.1.4` this signing
config was actually verified against locally. Pinning `cosign-release`
explicitly closes that gap between "a fixed version" and "the specific
version this was tested with" - the same distinction this repo has
been burned by before with GoReleaser's own pinned-CLI-version schema
drift (ADR-0016, ADR-0028, ADR-0032).

### `id-token: write`, scoped to the `tag-and-release` job specifically

Needed for cosign to request a GitHub Actions OIDC token for the
keyless flow. Added as that job's own `permissions:` block (`contents:
write`, already needed for the tag push and release creation, plus
`id-token: write`) rather than widening the workflow-level
`permissions:` block - least privilege, and consistent with
`binary-provenance` (ADR-0038) already scoping its own `id-token:
write` to just itself rather than relying on a shared, broader grant.

## Consequences

Every release from v0.4.1 onward carries a `checksums.txt.sigstore.json`
alongside the checksum file itself, verifiable with `cosign verify-blob
--bundle checksums.txt.sigstore.json checksums.txt` plus the expected
certificate identity/issuer (the exact identity string depends on this
repo's workflow path and ref, confirmed once a real signed release
exists to test against). #131 (SBOMs) is the last independent piece
under M10; #132 documents end-to-end verification (provenance,
signature, SBOM together) once all three have landed.
