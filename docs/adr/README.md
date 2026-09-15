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

To add a new one, copy the format of an existing ADR, number it
sequentially, and set its status to `Accepted` once the decision is
final.
