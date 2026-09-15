# 4. CLI shape and initial dependencies: Cobra, excelize

## Status

Accepted

## Context

xlq is a new project; before any filter/query language exists, it
still needs a real command to build, test, and release against, and it
needs to actually be able to read a spreadsheet file. Two dependency
choices have to be made to get there, and both have an established,
boring answer in the Go ecosystem:

- **CLI argument parsing.** `mikefarah/yq` - the closest existing
  analog to xlq - is itself built on
  [`spf13/cobra`](https://github.com/spf13/cobra), as are most
  polished multi-command Go CLIs (`gh`, `kubectl`, `hugo`). It gives
  subcommands, `--help` generation, and shell completion for free,
  at the cost of one dependency tree.
- **Spreadsheet I/O.** [`qax-os/excelize`](https://github.com/qax-os/excelize)
  is the de facto standard pure-Go `.xlsx` reader/writer (no cgo, no
  external binary), actively maintained, and already handles the
  OOXML details (shared strings, styles, multiple sheets) xlq would
  otherwise have to reimplement.

The actual jq-style filter language (`xlq '<filter>' file.xlsx`) is a
substantial design problem on its own - grammar, the data model a
spreadsheet maps to, how multiple sheets and mixed cell types behave
under a filter - and doesn't have an answer yet. Blocking the very
first commit on designing it would mean shipping no CI/CD, no
releases, and no repo structure until that's settled.

## Decision

- Root command: `xlq`, built with Cobra, exposing `--version` (wired to
  `internal/version.Version`).
- First real subcommand: `xlq sheets <file>`, which opens a spreadsheet
  with excelize and prints its sheet names. This is deliberately small:
  enough to prove the dependency and the CLI wiring end-to-end, with a
  real, testable behavior, without committing to the filter language's
  design.
- The `xlq '<filter>' <file>` invocation jq/yq users would expect is
  explicitly deferred. It gets its own ADR (or ADRs, likely plural,
  matching how thoth-mesh's wire protocol grew across several) once the
  query language's grammar and data model are actually designed.

## Consequences

The project has a real, buildable, testable CLI from the first commit,
which is what CI/CD and release automation need to exist at all.
Cobra and excelize are both committed dependencies now - swapping
either later is possible but not free, since `NewRootCommand` and
`sheetNames` would need rewriting. The filter language remains fully
open; nothing here constrains its eventual grammar or execution model.
