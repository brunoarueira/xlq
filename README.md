# xlq

`xlq` is a command-line spreadsheet processor in the spirit of
[`jq`](https://jqlang.org/) and [`yq`](https://mikefarah.gitbook.io/yq/):
a single static binary for slicing, filtering, and transforming
spreadsheet data (`.xlsx` to start) from the shell.

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
xlq --version
```

Filters can address a sheet by name (`.Sheet1`, case-insensitive), and
from there a cell by its A1 reference (`.Sheet1.A1`), a whole column
(`.Sheet1.B`), or a whole row (`.Sheet1[5]`, 1-based); arithmetic,
`map`/`select`, and formula introspection aren't implemented yet.

`records` (see [ADR-0011](docs/adr/0011-header-aware-record-output.md))
turns a sheet into an array of row objects keyed by its row-1 header,
so a column can be addressed by name instead of position - useful for
the common case of a sheet shaped like a CSV table. It assumes row 1
actually is a header row; piping a sheet without one into `records`
will treat its first data row as headers instead.

## Install

Prebuilt binaries (macOS, Linux, Windows) are attached to each
[release](https://github.com/brunoarueira/xlq/releases).

Or build from source with Go 1.24+:

```sh
go install github.com/brunoarueira/xlq/cmd/xlq@latest
```

## License

MIT, see [`LICENSE`](LICENSE).
