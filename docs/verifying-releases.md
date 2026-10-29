---
layout: page
title: Verifying releases
permalink: /verifying-releases.html
---

Every release from [v0.4.3](https://github.com/brunoarueira/xlq/releases/tag/v0.4.3)
onward attaches, alongside the binaries themselves:

- `checksums.txt` - a SHA-256 digest of every archive
- `checksums.txt.sigstore.json` - a [cosign](https://github.com/sigstore/cosign)
  signature over `checksums.txt`, using GitHub Actions' own identity
  (no key for anyone to leak or rotate)
- `multiple.intoto.jsonl` - [SLSA](https://slsa.dev/) Build L3 provenance,
  attesting these exact artifacts were built by this exact GitHub Actions
  workflow run, from this exact source commit
- one `<archive>.sbom.json` per archive - a software bill of materials
  (SPDX JSON, via [Syft](https://github.com/anchore/syft)), listing
  every dependency actually built into that binary

None of this replaces the others - each checks something different, and
every command below was run against a real downloaded release before
being written down here.

## Checksum

The baseline check: confirms a download wasn't corrupted or swapped for
something else, nothing more.

```sh
# Linux (GNU coreutils)
sha256sum --check checksums.txt

# macOS
shasum -a 256 -c checksums.txt
```

## Signature

Confirms `checksums.txt` - and so, transitively, every archive it lists -
was signed by this project's own release workflow, not tampered with
after the fact. Needs [cosign](https://github.com/sigstore/cosign)
installed.

```sh
cosign verify-blob \
  --certificate-identity "https://github.com/brunoarueira/xlq/.github/workflows/release.yml@refs/heads/main" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  --bundle checksums.txt.sigstore.json \
  checksums.txt
```

`--certificate-identity` is the exact workflow path and ref that signed
it - this project's own `release.yml`, triggered on `main` (not a tag
push; see [ADR-0006](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0006-release-automation.md)
for why). A signature from anything else is rejected, not silently
accepted.

## Provenance

Confirms a specific binary came from this exact workflow run and source
commit - not just *a* signed release, but *this* one, independently of
the signature above. Needs
[`slsa-verifier`](https://github.com/slsa-framework/slsa-verifier)
installed.

```sh
slsa-verifier verify-artifact \
  --provenance-path multiple.intoto.jsonl \
  --source-uri github.com/brunoarueira/xlq \
  --source-branch main \
  xlq_<version>_<os>_<arch>.tar.gz
```

`--source-branch main`, not `--source-tag <version>` - confirmed
directly: `--source-tag` fails here ("invalid ref: unexpected ref
type"), since this pipeline's provenance records the build as coming
from a push to `main`, not a tag-triggered run (the same reason
behind `--certificate-identity`'s `refs/heads/main` above). A
tampered or substituted archive fails clearly instead of passing:

```sh
$ slsa-verifier verify-artifact --provenance-path multiple.intoto.jsonl \
    --source-uri github.com/brunoarueira/xlq tampered.tar.gz
FAILED: SLSA verification failed: expected hash '...' not found:
artifact hash does not match provenance subject
```

## Software bill of materials

Not a trust check by itself - lists what's actually inside a given
archive, for a vulnerability scanner or a security review to act on:

```sh
jq '.packages[] | .name' xlq_<version>_<os>_<arch>.tar.gz.sbom.json
```
