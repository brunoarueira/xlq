# 2. Single Go module, `cmd/xlq` entrypoint, `internal` packages

## Status

Accepted

## Context

xlq starts as one binary with no plans to ship a reusable library API
yet. Go projects that expect this shape conventionally split
`cmd/<binary>` (the `main` package, kept thin) from the packages that
do the actual work, and mark anything not meant for external import as
`internal/` so the Go toolchain enforces that boundary.

## Decision

- One Go module at the repo root: `github.com/brunoarueira/xlq`.
- `cmd/xlq/main.go` is the entrypoint: it does nothing but call into
  `internal/cli` and translate its error into an exit code.
- Everything else lives under `internal/`, one package per concern
  (`internal/cli` for command wiring today; a future query engine,
  spreadsheet readers, etc. get their own packages as they're added).

No `pkg/` directory, and nothing importable from outside the module,
until there's an actual reason to expose a public Go API - that's a
decision to revisit with its own ADR if it comes up, not a default to
build in speculatively.

## Consequences

Adding a second binary later (say, a language-server-style long-running
process) is just another `cmd/` directory; it doesn't force a restructure.
Keeping everything under `internal/` for now costs nothing and avoids
committing to a public API surface before xlq's own shape (the filter
language, in particular) has settled.
