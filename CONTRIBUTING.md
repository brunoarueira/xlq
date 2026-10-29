# Contributing

## Architecture decisions

A real design decision - something with more than one reasonable
answer, where the reasoning is worth keeping - gets an
[ADR](docs/adr/) before implementation starts:
[Nygard-style](docs/adr/0001-record-architecture-decisions.md),
numbered sequentially, indexed in [`docs/adr/README.md`](docs/adr/README.md).
Once an ADR's status is `Accepted`, it isn't edited - if a later
decision changes course, that's a new ADR that supersedes it, not an
edit to the old one. Not every change needs one: a bug fix or a
straightforward addition that doesn't involve a real design choice
doesn't.

To add one: copy the format of an existing ADR, give it the next
number, and add a row to the index.

## Local development

Before opening a PR, run what CI runs:

```sh
gofmt -l .
go vet ./...
go build ./...
go test ./... -race -cover
golangci-lint run
```

`.github/workflows/ci.yml` runs the same checks on every push and pull
request.

## Releasing

Versioning is a single `VERSION` file at the repo root, following
[semantic versioning](https://semver.org/) - see
[ADR-0037](docs/adr/0037-semantic-versioning-policy.md) for what
actually decides a major/minor/patch bump, including xlq's own pre-1.0
reading of it. Bump `VERSION` in a PR to `main`; once merged,
`.github/workflows/release.yml` tags `v<version>`, cross-compiles
binaries, publishes the GitHub Release, and pushes a Homebrew cask to
[`brunoarueira/homebrew-tap`](https://github.com/brunoarueira/homebrew-tap)
automatically, generates [SLSA](https://slsa.dev/) Build L3 provenance
for the binaries, and signs the checksum file with
[cosign](https://github.com/sigstore/cosign). See
[ADR-0006](docs/adr/0006-release-automation.md) for the core
mechanism, [ADR-0016](docs/adr/0016-homebrew-publishing.md) for the
Homebrew piece, [ADR-0038](docs/adr/0038-slsa-build-provenance.md) for
the provenance piece, and
[ADR-0039](docs/adr/0039-cosign-signing.md) for the signing piece.

The Homebrew step needs a `HOMEBREW_TAP_GITHUB_TOKEN` repository
secret - the default `GITHUB_TOKEN` only has write access to this
repo, not the separate tap repo. One-time setup for whoever has admin
access to both repos, **before the next `VERSION` bump**: create a
fine-grained personal access token scoped to
`brunoarueira/homebrew-tap` with Contents: read and write, then add it
as a secret named `HOMEBREW_TAP_GITHUB_TOKEN` in this repo's settings.
Without it, GoReleaser's `homebrew_casks` step has no way to push to
the tap repo and the release run fails there - whether the
binaries/checksums/GitHub Release from earlier in that same run still
end up published despite that failure isn't confirmed, so don't rely
on a partial success; set the secret up first.

## License

xlq is licensed under [MIT](LICENSE). By contributing, you agree your
contribution is licensed under the same terms.
