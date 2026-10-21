# AGENTS.md

Orientation for an AI coding agent working in this repo. For anything
not covered here, see [`CONTRIBUTING.md`](CONTRIBUTING.md) and
[`docs/adr/`](docs/adr/) - this file points at those rather than
duplicating them.

## What this is

`xlq` is a command-line spreadsheet processor (`.xlsx`, `.csv`, `.tsv`,
`.ods`) with a `jq`-style filter language. See
[`README.md`](README.md) for the user-facing overview and
[ADR-0008](docs/adr/0008-filter-grammar-v1.md) for the filter grammar.

## Repo layout

One Go module, `cmd/xlq` as the thin entrypoint, everything else under
`internal/` - one package per concern (`internal/filter` for
lexing/parsing, `internal/eval` for evaluating a parsed filter,
`internal/xlsx`/`internal/csv`/`internal/ods` for the readers, and so
on). No `pkg/`, nothing importable from outside the module. See
[ADR-0002](docs/adr/0002-go-module-layout.md).

## Before opening a PR

Run what CI runs:

```sh
gofmt -l .
go vet ./...
go build ./...
go test ./... -race -cover
golangci-lint run
```

`.github/workflows/ci.yml` is the source of truth if this ever drifts.

## Architecture decisions

A real design decision - more than one reasonable answer, reasoning
worth keeping - gets an ADR before implementation starts, per
[`CONTRIBUTING.md`](CONTRIBUTING.md#architecture-decisions). A bug fix
or a straightforward addition with no real design choice doesn't need
one. Once an ADR is `Accepted`, it isn't edited - a later decision that
changes course gets a new, superseding ADR instead (e.g.
[ADR-0019](docs/adr/0019-correct-adr-0018-streaming-reader-claims.md)
correcting [ADR-0018](docs/adr/0018-streaming-xlsx-reader.md)).

## Releasing

A `VERSION` file at the repo root, bumped in a PR to `main`; merging
that PR tags, builds, and publishes automatically. See
[ADR-0006](docs/adr/0006-release-automation.md),
[ADR-0016](docs/adr/0016-homebrew-publishing.md), and
[`CONTRIBUTING.md`](CONTRIBUTING.md#releasing) for the one-time
Homebrew tap setup this depends on.
