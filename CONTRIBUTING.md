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

Versioning is a single `VERSION` file at the repo root. Bump it in a
PR to `main`; once merged, `.github/workflows/release.yml` tags
`v<version>`, cross-compiles binaries, and publishes the GitHub
Release automatically. See
[ADR-0006](docs/adr/0006-release-automation.md).

## License

xlq is licensed under [MIT](LICENSE). By contributing, you agree your
contribution is licensed under the same terms.
