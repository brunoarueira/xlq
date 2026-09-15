# 5. CI gate on every push and pull request

## Status

Accepted

## Context

Go's toolchain makes a small, fast, deterministic CI job cheap:
`gofmt`, `go vet`, and `go test` all ship in the standard toolchain and
need no extra installation, and `golangci-lint` is the community
standard for everything beyond `go vet` (unused code, style,
common bug patterns) bundled as a single binary/action.

## Decision

`.github/workflows/ci.yml` runs on every push to any branch and every
pull request, with two jobs:

- **`test`**: `gofmt -l` (fails if anything is unformatted), `go vet
  ./...`, `go build ./...`, `go test ./... -race -cover`.
- **`lint`**: `golangci-lint run`, via the official action, using its
  default rule set to start.

Both jobs pin third-party actions to a commit SHA (with the version as
a trailing comment), not a floating tag, and request only
`permissions: contents: read` - CI never needs to write to the repo.
Concurrency is scoped per-ref so a new push cancels a superseded run
instead of queuing behind it.

## Consequences

Every PR is gated on formatting, vetting, building, testing, and
linting before it can merge, matching the bar `CONTRIBUTING.md` asks
contributors to check locally. As the module grows (more packages,
build tags for platform-specific code), these jobs may need to widen
(matrix builds across OSes, for instance) - that's an amendment to this
ADR's decision worth its own ADR if it becomes non-trivial, not a
silent workflow edit.
