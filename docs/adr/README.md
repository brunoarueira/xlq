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
| [0016](0016-homebrew-publishing.md) | Homebrew package publishing via GoReleaser (supersedes ADR-0006's "not yet", see ADR-0028) |
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
| [0027](0027-group-by.md) | `group_by(f)`: grouping by equality, in first-encountered order |
| [0028](0028-correct-adr-0016-postflight-hook-claim.md) | Correct ADR-0016's postflight hook claim |
| [0029](0029-excelize-pseudo-version-pin.md) | Pin excelize to an unreleased commit, fixing #49 and most of #50 |
| [0030](0030-recover-and-timeout-the-read-path.md) | Recover from panics and bound reads with a wall-clock timeout |
| [0031](0031-memory-ceiling-for-untrusted-files.md) | Memory ceiling for untrusted xlsx/csv files |
| [0032](0032-goreleaser-v2-19-postflight-steps.md) | Upgrade GoReleaser to v2.19.0 for `postflight_steps` |
| [0033](0033-upper-lower-trim.md) | `upper`, `lower`, `trim`: jq-parity string builtins |
| [0034](0034-snake-case-camel-case.md) | `snake_case`, `camel_case`: word-boundary case conversion |
| [0035](0035-replace.md) | `replace(old, new)`: literal substring replacement |
| [0036](0036-headers-builtin.md) | `headers(f)`: renaming `records`' keys |
| [0037](0037-semantic-versioning-policy.md) | Semantic versioning policy, including a pre-1.0 reading |
| [0038](0038-slsa-build-provenance.md) | SLSA Build L3 provenance for release binaries |
| [0039](0039-cosign-signing.md) | Sign release checksums with cosign (keyless/OIDC) |
| [0040](0040-sbom-generation.md) | SBOM generation for release archives |
| [0041](0041-draft-then-publish-release.md) | Create the release as a draft, publish it only after provenance lands |
| [0042](0042-provenance-artifact-not-generator-upload.md) | Attach provenance via its own build artifact, not the generator's upload path |

To add a new one, copy the format of an existing ADR, number it
sequentially, and set its status to `Accepted` once the decision is
final.
