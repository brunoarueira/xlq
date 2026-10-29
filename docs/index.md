---
layout: home
title: xlq
---

`xlq` is a command-line spreadsheet processor in the spirit of
[`jq`](https://jqlang.org/) and [`yq`](https://mikefarah.gitbook.io/yq/): a
single static binary for slicing, filtering, and transforming spreadsheet
data (`.xlsx`, `.csv`, `.tsv`, `.ods`) from the shell - no Excel, no
throwaway script, no macro.

![xlq fixing a "SUM comes out as 0" export, in one command](assets/img/hero.gif)

## Why

Spreadsheet exports break in the same handful of ways, over and over:
numbers stored as text so `SUM` comes out `0`, a semicolon-delimited CSV
that opens as one giant column, a date column that's ambiguous between
`M/D/YYYY` and `D/M/YYYY`, a field with an embedded comma that breaks naive
`awk` parsing. `xlq` handles the file correctly by construction and lets
you say what you actually want:

```sh
# numbers imported as text - SUM comes out 0 in Excel until you fix it by hand
xlq '.Sheet1 | records | map(.Likes | tonumber) | sum' export.xlsx

# a field has a literal comma in it - awk -F',' would silently misalign every
# column after it; xlq's CSV reader is quote-aware
xlq '.Sheet1 | records | select(.Revenue | tonumber > 10000)' report.csv

# a semicolon-delimited export from a European-locale system
xlq --delimiter ';' '.Sheet1 | records' export.csv
```

See [Examples](examples.html) for more real problems `xlq` solves, or jump
straight to the [Quickstart](quickstart.html).

## Install

On macOS or Linux, via Homebrew:

```sh
brew install brunoarueira/tap/xlq
```

Prebuilt binaries (macOS, Linux, Windows) are attached to each
[release](https://github.com/brunoarueira/xlq/releases), alongside a
checksum, a signature, SLSA provenance, and an SBOM for each - see
[Verifying releases](verifying-releases.html) to actually check them.
Or build from source with Go 1.26.10+:

```sh
go install github.com/brunoarueira/xlq/cmd/xlq@latest
```

## Status

Early, but the core loop works - reading, filtering, and a handful of
builtins (`records`, `headers`, `map`, `select`, `sum`/`count`/`avg`,
`group_by`, `tonumber`, `todate`, `upper`/`lower`/`trim`,
`snake_case`/`camel_case`, `replace`) cover a lot of real-world
spreadsheet wrangling already. General
arithmetic between cells and formula introspection aren't implemented
yet. See the [Reference](reference.html) for exactly what's
supported today, and [`docs/adr/`](https://github.com/brunoarueira/xlq/tree/main/docs/adr)
for the design decisions behind it.
