---
layout: page
title: Quickstart
permalink: /quickstart.html
---

This page reflects the latest release. If you're on an older `xlq`
(check with `xlq --version`), something here may not match yet - see
the [releases page](https://github.com/brunoarueira/xlq/releases) for
what changed since.

## Install

```sh
brew install brunoarueira/tap/xlq
# or: go install github.com/brunoarueira/xlq/cmd/xlq@latest
# or download a release binary: https://github.com/brunoarueira/xlq/releases
```

## List the sheets in a file

```sh
xlq sheets report.xlsx
```

## Read a single cell

```sh
xlq '.Sheet1.B2' report.xlsx
```

Prints the cell's value as JSON (a quoted string, a bare number, `true`/
`false`, or `null`). Add `-r` to print a string result without the quotes.

## Read a whole sheet as records

Most real spreadsheets have a header row. `records` turns the sheet into an
array of objects keyed by that header, so you can address a column by name
instead of position:

```sh
xlq '.Sheet1 | records' report.xlsx
```

```json
[
  { "Category": "Shoes", "Revenue": 1200 },
  { "Category": "Shirts", "Revenue": 850 }
]
```

(A genuine numeric cell in `.xlsx` reads back as a bare JSON number. A
CSV/text export often stores the same value as a string instead - see
[`tonumber`](reference.html#conversions) for converting it explicitly.)

## Filter and total a column

```sh
xlq '.Sheet1 | records | select(.Revenue | tonumber > 1000) | map(.Revenue | tonumber) | sum'
```

`tonumber` converts text to a number (common for CSV/export data where
numbers are stored as strings); `select` keeps matching records; `map`
projects a single field; `sum` totals an array. These compose the same way
`jq`'s own builtins do.

## It works on CSV/TSV/ODS too

The same filters run unchanged against any supported format - the input
format is picked from the file's extension:

```sh
xlq '.Sheet1 | records | select(.Revenue | tonumber > 1000)' report.csv
```

## Next

- [Reference](reference.html) - the full filter language, output modes, config file.
- [Examples](examples.html) - real problems, solved in one line.
