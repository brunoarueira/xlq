# xlq

`xlq` is a command-line spreadsheet processor in the spirit of
[`jq`](https://jqlang.org/) and [`yq`](https://mikefarah.gitbook.io/yq/):
a single static binary for slicing, filtering, and transforming
spreadsheet data (`.xlsx`, `.csv`, `.tsv`, `.ods`) from the shell.

See [`docs/adr/`](docs/adr/) for the design decisions made so far, and
[`CONTRIBUTING.md`](CONTRIBUTING.md) for how the project is run.

## Status

Early, but the core loop works. `xlq '<filter>' file.xlsx` parses
`<filter>` (see [ADR-0008](docs/adr/0008-filter-grammar-v1.md) for the
v1 grammar), evaluates it against the file, and prints the result as
JSON:

```sh
xlq sheets path/to/book.xlsx   # list sheet names
xlq '.Sheet1.B2' path/to/book.xlsx     # a single cell
xlq '.Sheet1' path/to/book.xlsx        # a whole sheet, as a 2D array
xlq '.Sheet1 | .B2' path/to/book.xlsx  # same cell, piped
xlq --compact '.Sheet1' path/to/book.xlsx  # drop empty rows/columns
xlq -c '.Sheet1' path/to/book.xlsx         # compact JSON, one line
xlq -r '.Sheet1.A1' path/to/book.xlsx      # a string result, unquoted
xlq '.Sheet1 | records | .[0].SomeHeader' path/to/book.xlsx  # by header name
xlq '.Sheet1["A3:C9"]' path/to/book.xlsx  # a rectangular range
xlq '.Sheet1' path/to/book.csv            # same filters work on csv, tsv, ods
xlq --input-format csv '.Sheet1' path/to/export.txt  # misnamed file
xlq --delimiter ';' '.Sheet1' path/to/export.csv  # non-comma csv
xlq --version
```

The input format (`xlsx`, `csv`, `tsv`, or `ods`) is picked from the
file's extension; `--input-format` overrides that for a misnamed file
or one with no recognizable extension. `--delimiter` overrides a
csv/tsv file's own default field separator (comma, tab) with any
single character - useful for a semicolon-separated export from a
locale where comma is the decimal separator. It's an error alongside
an xlsx or ods input, which have no delimiter concept to override.

Filters can address a sheet by name (`.Sheet1`, case-insensitive), and
from there a cell by its A1 reference (`.Sheet1.A1`), a whole column
(`.Sheet1.B`), a whole row (`.Sheet1[5]`, 1-based), or a rectangular
range (`.Sheet1["A3:C9"]`, see below); arithmetic, `map`/`select`, and
formula introspection aren't implemented yet.

`records` (see [ADR-0011](docs/adr/0011-header-aware-record-output.md))
turns a sheet into an array of row objects keyed by its row-1 header,
so a column can be addressed by name instead of position - useful for
the common case of a sheet shaped like a CSV table. Two things worth
knowing before relying on it: the records array is 0-indexed, jq-style
(`.[0]` is the first record), unlike a sheet's own 1-based row numbers
above; and a record's JSON keys print alphabetically, not in the
sheet's original column order, since that's how Go's JSON encoder
orders any object's keys. It also assumes row 1 actually is a header
row; piping a sheet without one into `records` will treat its first
data row as headers instead.

A range (`.Sheet1["A3:C9"]`, see
[ADR-0012](docs/adr/0012-range-references.md)) selects a rectangular
block of a sheet and behaves exactly like a whole sheet from then on -
it's still just a sheet, only smaller. That means every address inside
it is **relative to the range**, not the original sheet: `.Sheet1
["C3:E9"].A1` is the range's own top-left cell, i.e. the original
sheet's `C3`, not its actual `A1`. This is what makes a sheet with two
tables side by side addressable at all - narrow a range to one
table's rows *and* columns before piping into `records`, so the other
table's columns were never part of the slice to begin with.

## Install

Prebuilt binaries (macOS, Linux, Windows) are attached to each
[release](https://github.com/brunoarueira/xlq/releases).

Or build from source with Go 1.24+:

```sh
go install github.com/brunoarueira/xlq/cmd/xlq@latest
```

## License

MIT, see [`LICENSE`](LICENSE).
