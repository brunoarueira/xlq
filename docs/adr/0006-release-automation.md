# 6. Release automation: `VERSION` file, auto-tag on `main`, GoReleaser

## Status

Accepted

## Context

Releases need to happen without a manual "build six binaries by hand
and upload them" step, and a version bump landing on `main` should be
the one, unambiguous trigger for cutting a release - mirroring the
Cargo-workspace equivalent in
[thoth-mesh's ADR-0032](https://github.com/brunoarueira/thoth-mesh/blob/main/docs/adr/0032-git-tags-and-github-releases.md):
read the version out of a manifest, tag it if that tag doesn't exist
yet, and let the tag drive the actual release build.

Go has no `Cargo.toml`/`version =` field to read, so xlq needs its own
single source of truth for the current version. Cross-compiling Go
binaries for multiple `GOOS`/`GOARCH` targets, archiving them,
generating checksums, and creating the GitHub Release with all of that
attached is exactly what [GoReleaser](https://goreleaser.com/) exists
to do, rather than hand-rolling a matrix build and upload steps.

## Decision

### A `VERSION` file at the repo root is the single source of truth

Plain semver, no `v` prefix (e.g. `0.1.0`). Bumping it is a normal PR.

### `release-tag.yml`: tag `v<version>` on push to `main`, if it doesn't already exist

Same shape as thoth-mesh's release workflow:

1. Read `VERSION`.
2. Check whether `refs/tags/v<version>` already exists on `origin`.
3. If not, create and push an annotated tag `v<version>`.

This is idempotent by construction - reruns, pushes that don't touch
`VERSION`, and `main` advancing without a bump are all no-ops - so it's
safe to run unconditionally on every push to `main`, with no need to
diff commit ranges or reason about squash-merges. Needs
`permissions: contents: write` to push the tag; every other workflow in
this repo stays read-only.

### `release-build.yml`: GoReleaser on tag push, builds the release

Triggered on `push: tags: ["v*"]`, separate from `release-tag.yml`, so
"decide a release is happening" and "build it" are two workflows with
one job each:

1. Install Go, run `goreleaser release` via the official action.
2. `.goreleaser.yaml` targets `darwin/amd64`, `darwin/arm64`,
   `linux/amd64`, `linux/arm64`, and `windows/amd64` - darwin because
   that's the author's daily driver and the explicit requirement,
   linux/windows because they're free once the build matrix exists and
   cost nothing to keep supporting.
3. GoReleaser injects the version into the binary via
   `-ldflags -X .../internal/version.Version={{.Version}}`, creates
   archives + checksums, and creates the GitHub Release itself
   (`--generate-notes`-equivalent changelog from commits since the
   last tag) - no separate `gh release create` step.

### No package-manager publishing (Homebrew tap, etc.) yet

GoReleaser can push a Homebrew formula, a Scoop manifest, and more, but
none of that has a concrete need yet. Adding one is a config change to
`.goreleaser.yaml` when it's actually wanted, not a reason to hold up
this ADR.

## Consequences

Merging a `VERSION` bump to `main` is the only manual step in cutting a
release; everything after that - tagging, cross-compiling, archiving,
publishing - is automatic. The two-workflow split means a GoReleaser
config mistake only wastes a tag, not blocks the tagging logic, and
vice versa. If `VERSION` and the tag ever drift (e.g. someone tags
manually), `release-tag.yml`'s tag-existence check is what keeps it
idempotent rather than double-releasing.
