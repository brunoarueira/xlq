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

### One workflow, `release.yml`, tags *and* builds in the same job

Triggered on push to `main` (scoped to `paths: VERSION`), idempotent by
construction the same way thoth-mesh's is:

1. Read `VERSION`.
2. Check whether `refs/tags/v<version>` already exists on `origin`. If
   it does, this push didn't bump the version (or already got
   released) - stop, no-op.
3. Otherwise: create and push the tag `v<version>`, then run
   `goreleaser release` in that same job.

Reruns, pushes that don't touch `VERSION`, and `main` advancing without
a bump are all no-ops, so it's safe to run unconditionally. Needs
`permissions: contents: write` to push the tag and create the release;
`ci.yml` stays read-only.

Tagging and building are deliberately **one workflow run, not two**.
The natural-looking split - a `release-tag.yml` that pushes the tag,
and a separate `release-build.yml` triggered by `push: tags: ["v*"]` -
doesn't work: GitHub does not fire new workflow runs for pushes made
with the default `GITHUB_TOKEN` (an anti-recursion safeguard), so a tag
pushed by a workflow using `GITHUB_TOKEN` never triggers a
tag-triggered workflow. This was tried, shipped, and silently no-opped
(the tag existed with no release attached) before being caught and
merged into a single job. thoth-mesh's ADR-0032 already does tag +
release in one job for the same reason, restated here because it's the
part of that design most tempting to "clean up" into two workflows.

### GoReleaser builds `darwin`, `linux`, and `windows`, then creates the release itself

`.goreleaser.yaml` targets `darwin/amd64`, `darwin/arm64`,
`linux/amd64`, `linux/arm64`, and `windows/amd64` - darwin because
that's the author's daily driver and the explicit requirement,
linux/windows because they're free once the build matrix exists (pure
Go, `CGO_ENABLED=0`, no per-OS runner needed) and cost nothing to keep
supporting. GoReleaser injects the version into the binary via
`-ldflags -X .../internal/version.Version={{.Version}}`, creates
archives + checksums, and creates the GitHub Release itself (changelog
generated from commits since the last tag) - no separate
`gh release create` step.

### No package-manager publishing (Homebrew tap, etc.) yet

GoReleaser can push a Homebrew formula, a Scoop manifest, and more, but
none of that has a concrete need yet. Adding one is a config change to
`.goreleaser.yaml` when it's actually wanted, not a reason to hold up
this ADR.

## Consequences

Merging a `VERSION` bump to `main` is the only manual step in cutting a
release; everything after that - tagging, cross-compiling, archiving,
publishing - is automatic. Doing it in one job means a GoReleaser
failure leaves a pushed tag with no release attached - a rerun of the
same workflow run (or a re-push) is safe, since the tag-existence check
is what makes this idempotent, not a boundary between two workflows.
If `VERSION` and the tag ever drift (e.g. someone tags manually), that
same check is what keeps this a no-op rather than double-releasing.
