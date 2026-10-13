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
xlq --table '.Sheet1 | records' path/to/book.xlsx  # aligned table, not JSON
xlq --csv '.Sheet1 | records' path/to/book.xlsx  # with a header row
xlq --tsv '.Sheet1' path/to/book.xlsx            # positional, no header
xlq -i '.Sheet1.A1 = "done"' path/to/book.xlsx   # edit a cell in place (.xlsx only)
xlq --version
```

The input format (`xlsx`, `csv`, `tsv`, or `ods`) is picked from the
file's extension; `--input-format` overrides that for a misnamed file
or one with no recognizable extension. `--delimiter` overrides a
csv/tsv file's own default field separator (comma, tab) with any
single character the CSV reader accepts (a double quote, carriage
return, newline, or invalid character aren't valid delimiters) -
useful for a semicolon-separated export from a locale where comma is
the decimal separator. It's an error alongside
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

`--table` prints a human-readable, column-aligned table instead of
JSON - particularly useful piped from `records`, where it renders one
row per record headed by its sorted field names, but it handles every
result shape (a whole sheet, a range, a single cell, the whole
workbook). It's a different output mode entirely, not a JSON
formatting tweak, so it can't be combined with `-c`/`-r`.

`--csv`/`--tsv` (see [ADR-0013](docs/adr/0013-csv-tsv-output.md)) print
comma/tab-separated text instead of JSON, with `encoding/csv`'s own
quoting and escaping. A sheet grid or range flattens positionally,
with no header row; `records` gets a header row (its sorted field
names) plus one row per record - pipe through `records` first when a
header is wanted. The whole-workbook result is a clear error (no
single tabular shape), not a guess. `--table`, `--csv`, and `--tsv`
are mutually exclusive output modes, and each is also mutually
exclusive with `-c`/`-r` (which only ever meant something for JSON) -
but `-c` and `-r` themselves still compose freely with each other.

`-i`/`--in-place` (see
[ADR-0014](docs/adr/0014-in-place-editing.md)) edits a `.xlsx` file
instead of reading it: `<filter>` must be an assignment
(`.Sheet1.A1 = "value"`, a literal string/number/`true`/`false`/`null`,
exactly one cell), not a pipeline. It prints nothing on success,
matching `yq -i`'s own silence - the edit is the result. csv/tsv/ods
have no writer yet, so `-i` only supports xlsx.

## Install

Prebuilt binaries (macOS, Linux, Windows) are attached to each
[release](https://github.com/brunoarueira/xlq/releases).

Or build from source with Go 1.24+:

```sh
go install github.com/brunoarueira/xlq/cmd/xlq@latest
```

## Shell completions

`xlq completion <shell>` generates a completion script for bash, zsh,
fish, or powershell (via Cobra's own built-in generator - run `xlq
completion <shell> --help` for that shell's exact setup instructions).
For example, on bash:

```sh
# load for the current session only
source <(xlq completion bash)

# load for every new session (Linux)
xlq completion bash | sudo tee /etc/bash_completion.d/xlq >/dev/null

# load for every new session (macOS, Homebrew's bash-completion@2)
xlq completion bash > $(brew --prefix)/etc/bash_completion.d/xlq
```

`xlq`'s completion covers subcommands and flags (`xlq sheets`,
`--table`, ...) and may fall back to filename completion for
positional arguments; `<filter>` itself isn't completed, since it
isn't a fixed set of words.

## Config file

A YAML file can set defaults for any flag except `-i`/`--in-place`
(see [ADR-0015](docs/adr/0015-config-file-for-default-flags.md)), so a
flag you'd otherwise pass on every invocation can be set once instead.
By default it's read from `os.UserConfigDir()/xlq/config.yaml` -
`~/.config/xlq/config.yaml` on Linux, `~/Library/Application
Support/xlq/config.yaml` on macOS, `%AppData%\xlq\config.yaml` on
Windows - or from `--config <path>` instead. A missing file at the
default location is fine (the feature is entirely opt-in); an
explicitly-passed `--config` path that doesn't exist is an error.

```yaml
table: true
input-format: csv
```

Keys match each flag's own long name exactly (`compact`,
`compact-output`, `raw-output`, `table`, `csv`, `tsv`, `input-format`,
`delimiter`) - an unrecognized key is a hard error, to catch a typo
rather than silently ignore it. An explicitly-passed flag always wins
over its config value; `-i` additionally suppresses every config value
outright, so a config file never reaches - or breaks - an in-place
edit.

## License

MIT, see [`LICENSE`](LICENSE).
