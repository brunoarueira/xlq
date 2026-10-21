# Architecture Decision Records

This directory records the significant architecture decisions made on
xlq, using the format described in
[0001](0001-record-architecture-decisions.md).

| ADR | Title |
| --- | --- |
| [0001](0001-record-architecture-decisions.md) | Record architecture decisions |
| [0002](0002-go-module-layout.md) | Single Go module, `cmd/xlq` entrypoint, `internal` packages |
| [0003](0003-mit-license.md) | MIT license |
| [0004](0004-cli-shape-and-initial-dependencies.md) | CLI shape and initial dependencies: Cobra, excelize |
| [0005](0005-ci-gate-on-every-pr.md) | CI gate on every push and pull request |
| [0006](0006-release-automation.md) | Release automation: `VERSION` file, auto-tag on `main`, GoReleaser |
| [0007](0007-spreadsheet-data-model.md) | Spreadsheet data model: Workbook, Sheet, Row, Cell |
| [0008](0008-filter-grammar-v1.md) | Filter language grammar v1: identity, A1 addressing, pipe |
| [0009](0009-correct-filter-grammar-ebnf.md) | Correct ADR-0008's path grammar |
| [0010](0010-ods-reader-no-dependency.md) | ODS reader: no third-party library, standard library only |
| [0011](0011-header-aware-record-output.md) | Header-aware record output: the `records` filter |
| [0012](0012-range-references.md) | Range references: `A1:C10`, and composing with `records` |
| [0013](0013-csv-tsv-output.md) | CSV/TSV output: flattening rules, header row, delimiter |
| [0014](0014-in-place-editing.md) | In-place `.xlsx` editing: assignment grammar and the write path |
| [0015](0015-config-file-for-default-flags.md) | Config file for default flags: location, format, precedence |
| [0016](0016-homebrew-publishing.md) | Homebrew package publishing via GoReleaser (supersedes ADR-0006's "not yet") |
| [0017](0017-multiple-input-files.md) | Multiple input files in one invocation: map, not slurp |
| [0018](0018-streaming-xlsx-reader.md) | Streaming xlsx reader: lazy `Sheet.Rows` via Go iterators (supersedes ADR-0007/0008/0011/0012's `Dimensions()`, see ADR-0019) |
| [0019](0019-correct-adr-0018-streaming-reader-claims.md) | Correct ADR-0018's streaming-reader claims |
| [0020](0020-array-slicing.md) | Array slicing: jq-style `.[START:END]` |
| [0021](0021-column-letter-access-on-records.md) | Column letter access on records (`.["TOP POSTS"] \| records \| .[0].G`) |
| [0022](0022-extensible-builtins.md) | Extensible builtins: `name` or `name(literal, ...)` |
| [0023](0023-explicit-date-parsing.md) | Explicit date parsing: `todate(format)` |
| [0024](0024-comparison-operators.md) | Comparison operators: `==`, `!=`, `<`, `>`, `<=`, `>=` |
| [0025](0025-map-select-filter-arguments.md) | `map`/`select`: higher-order filters with a filter-expression argument |
| [0026](0026-sum-count-avg.md) | `sum`, `count`, `avg`: zero-argument reduce builtins |

To add a new one, copy the format of an existing ADR, number it
sequentially, and set its status to `Accepted` once the decision is
final.
