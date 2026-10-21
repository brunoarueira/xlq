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
xlq '.Sheet1 | records | .[0:10]' path/to/book.xlsx  # the first 10 records
xlq '.Sheet1.B2 | tonumber' path/to/book.xlsx  # "3048" -> 3048
xlq '.Sheet1.B2 | todate("M/D/YYYY")' path/to/book.xlsx  # "7/28/2026" -> an RFC3339 date
xlq '.Sheet1.B2 | tonumber > 100' path/to/book.xlsx  # a comparison, true or false
xlq '.Sheet1 | records | select(.Likes > 100)' path/to/book.xlsx  # keep matching records
xlq '.Sheet1' path/to/book.csv            # same filters work on csv, tsv, ods
xlq --input-format csv '.Sheet1' path/to/export.txt  # misnamed file
xlq --delimiter ';' '.Sheet1' path/to/export.csv  # non-comma csv
xlq --table '.Sheet1 | records' path/to/book.xlsx  # aligned table, not JSON
xlq --csv '.Sheet1 | records' path/to/book.xlsx  # with a header row
xlq --tsv '.Sheet1' path/to/book.xlsx            # positional, no header
xlq '.Sheet1.A1' old.xlsx new.xlsx   # same filter on each file, results as an array
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

`tonumber` (#37, see [ADR-0022](docs/adr/0022-extensible-builtins.md)
for the builtin mechanism it's built on) converts a string to a
number, for exports (LinkedIn's among them) that store numbers as
text: `.DISCOVERY.B2 | tonumber` turns `"3048"` into `3048`. An
already-numeric value passes through unchanged; anything that can't be
parsed as a number - including `null` and a boolean, which don't get
the "propagates as null" treatment field/index access has - is an
error, not a silent `null`, matching jq's own `tonumber`. Only a plain
decimal number is accepted (an optional sign, digits, an optional
fraction, an optional exponent) - `"NaN"`, `"Inf"`/`"Infinity"`, and a
hex float like `"0x1p2"` are all rejected too, even though Go's own
number parser would otherwise accept them, since none of those is a
number a spreadsheet cell's text would ever legitimately mean.
Leading/trailing whitespace is trimmed before parsing (a deliberate
difference from jq, for exactly the kind of incidental copy-paste
noise this conversion exists to look past).

`todate(format)` (#42, see
[ADR-0023](docs/adr/0023-explicit-date-parsing.md)) converts a string
to an RFC3339 date, given an explicit format - a date like `7/8/2026`
is genuinely ambiguous (US month/day/year vs. everyone else's
day/month/year: July 8th or August 7th?), so, matching `tonumber`'s own
"never guess" rule, `todate` always needs to be told which:
`.Sheet1.B2 | todate("M/D/YYYY")` and `.Sheet1.B2 | todate("D/M/YYYY")`
read the same text as two different, equally valid dates. Format
tokens are Excel/spreadsheet-style, not code syntax: `YYYY`/`YY` (year,
4 or 2 digits - a 2-digit year pivots 00-68 to 2000-2068 and 69-99 to
1969-1999), `MM`/`M` (month, zero-padded or not), `DD`/`D` (day, same).
Anything else in the format - `/`, `-`, a space, even a word - is
matched as literal, never reinterpreted (`todate("D January M YYYY")`
treats `"January"` as an arbitrary literal separator, the same as `/`
would be, not a month name). A word that happens to start with a token
letter, like `"March"` or the all-caps `"MAY"` (an Oracle-style
`"DD-MON-YYYY"` separator), is still literal as long as what follows
that letter isn't itself the start of another token - so
`"D of M, March YYYY"` still reads the real month from `M`, not from
`"March"`, while `"MDDYYYY"` (`M` immediately followed by `DD`, which
*is* a token) needs no separator at all between the two. Either way, a
month name supplies no month value by itself - a format that relies on
one alone, with no separate numeric month token, still errors for
missing the month, the same as any other incomplete format.

A format must include exactly one year, one month, and one day token -
`todate("M/D")` (no year) and `todate("M/D/YYYY/D")` (two day tokens)
are both errors, not silently accepted with a guessed or overwritten
value - and the input must actually describe a real calendar date:
`todate("M/D/YYYY")` on `"2/30/2026"` errors, since February never has
30 days. The input must already be a string; `null`, a number, and a
boolean are all errors, with no "propagates as null" exception, same
as `tonumber`. Leading/trailing whitespace is trimmed before parsing,
matching `tonumber`'s own deviation from jq, for the same reason:
incidental copy-paste noise, not a meaningful part of the date text.

Comparisons (`==`, `!=`, `<`, `>`, `<=`, `>=`, see
[ADR-0024](docs/adr/0024-comparison-operators.md)) work the way you'd
expect: `.Sheet1.A1 == .Sheet1.B1`, `.Sheet1 | records | .[0].Likes >
100`, or against a literal (`.Sheet1.A1 == "done"`). `==`/`!=` never
error on a type mismatch - a number and a string are just "not equal,"
matching jq exactly - though comparing a whole sheet or workbook is
still a clear error, never a (meaningless) `true` or `false`, the same
as for ordering below. Ordering deliberately does **not** match jq:
comparing two different types (other than with `null`) is an error,
not a guessed ranking - `tonumber`/`todate` already refuse to guess a
cell's type from its text, and silently deciding "a string always
outranks a number" would be the same kind of guess. This includes two
values of the *same* type when neither is a number or a string -
`true > false` is also an error, not `true`, since this project
defines no ordering for booleans at all. `null` compared with anything
via an ordering operator is `false`, never an error - unlike equality's
`null == null`, which is `true` - so a sparse column's missing value
just quietly fails a threshold check instead of erroring out.

`map(filter)` and `select(filter)` (see
[ADR-0025](docs/adr/0025-map-select-filter-arguments.md)) take a
filter expression - not a literal, unlike every other builtin's
arguments - and run it once per array element, with `.` rebound to
that element: `.Sheet1 | records | map(.Likes)` extracts one field
across every record; `.Sheet1 | records | select(.Likes > 100)` keeps
only the records clearing a threshold, unchanged. Both iterate and
collect/filter an array directly, by themselves - unlike jq's own
`select`, which needs the separate `.[] | select(...)` idiom, since
this project has no `.[]` operator. `map`'s result is always a new
array - its filter is free to transform each element into something
else entirely - while `select` keeps each passing element exactly as
it was. `select`'s filter must evaluate to an actual boolean; a
non-boolean result (even a jq-style "truthy" value like `0` or `""`)
is an error, not a guessed coercion. Both require an actual array
(`records`'s output, or a plain row/column array) - a scalar, a single
record, or `null` is an error, matching jq's own `map`/`select`
behavior on those.

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

Once a value is an ordinary array - a `records` result, or a whole
column/row (`.Sheet1.B`, `.Sheet1[5]`) - it can be indexed (`.[0]`) or
sliced (`.[1:10]`, jq-style: 0-based, end-exclusive, a negative bound
counts from the end, and an out-of-range bound clamps rather than
erroring) the same way a `jq` array would. `[START:END]` can't be
applied *directly* to a sheet or a range - `.Sheet1[5:10]` is a clear
error, not "rows 5 through 9" - but a range composes with slicing just
fine once it's piped into something array-shaped, e.g.
`.Sheet1["A3:C9"] | records | .[0:5]`: a range picks a rectangular
block of a *sheet* by cell address, 1-based, before the sheet is ever
turned into an array of anything; `[START:END]` slices an
*already-produced array* by position, 0-based, same as `records`' own
`.[N]` convention. See
[ADR-0020](docs/adr/0020-array-slicing.md) for why the two don't mix:
`.Sheet1[5]` already means "spreadsheet row 5", so `.Sheet1[5:10]`
meaning "array positions 5 through 9" on that same sheet value would
silently disagree with it - slice an array you've already produced
instead, e.g. `.Sheet1 | records | .[0:10]` for the top 10 rows.

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

Passing more than one `<file>` (see
[ADR-0017](docs/adr/0017-multiple-input-files.md)) maps the *same*
`<filter>` over each file independently - not a diff, just each file's
own result placed at that file's own index - collecting them into a
JSON array in file order:

```sh
xlq '.Sheet1 | records' old.xlsx new.xlsx
# [ [ {old file's records...} ], [ {new file's records...} ] ]
```

A single file's result is completely unaffected by this - still
printed bare, never wrapped in a one-element array. `--table`/`--csv`/
`--tsv` and `-i` don't support more than one file (no single tabular
shape to flatten N results into, and an in-place edit of more than one
file is out of scope) - this includes a config-file default
(`table: true`, etc., see below): it hits the same rejection a typed
`--table` would, since there's no way to tell them apart by the time
this check runs, so `--table=false` (or `--csv=false`/`--tsv=false`)
is how to get plain JSON for one multi-file invocation despite that
default. A read/parse error names which file (by position and path)
failed.

`--input-format`/`--delimiter` (typed or from a config default) apply
to *every* file uniformly, not per-file - mixing formats across files
(e.g. diffing an `.xlsx` against a `.csv`) only auto-detects correctly
when neither resolves to a value, letting each file's own extension
decide its format as usual. A config `input-format` default forces
every file to that one *resolved* format regardless of its own
extension - a file that isn't actually that format might get silently
misread, or might fail outright with a read/parse error, depending on
its actual content. A config `delimiter` default fails the moment any
file *resolves* to `xlsx`/`ods` (whether from its own extension or
from an `input-format` override putting it there), since those have no
delimiter concept to override - pairing a delimiter default with an
`input-format` override of `csv`/`tsv` avoids that conflict entirely,
since nothing resolves to `xlsx`/`ods` either way. `--input-format ""`
clears a config default for one invocation the same way `--table=false`
does; `--delimiter ""` has no equivalent (an explicit empty delimiter is
already its own error, not a reset) - a persistent `delimiter` default
and mixing in an `.xlsx`/`.ods` file are simply incompatible short of
editing the config file.

## Install

On macOS or Linux, via Homebrew:

```sh
brew install brunoarueira/tap/xlq
```

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

A YAML file can set defaults for the eight input/output flags below -
every flag xlq has except `-i`/`--in-place`, which can never be
config-defaulted at all (see
[ADR-0015](docs/adr/0015-config-file-for-default-flags.md)) - so a
flag you'd otherwise pass on every invocation can be set once instead.
By default it's read from `os.UserConfigDir()/xlq/config.yaml` -
`$XDG_CONFIG_HOME/xlq/config.yaml` (or `~/.config/xlq/config.yaml` if
`$XDG_CONFIG_HOME` is unset) on Linux, `~/Library/Application
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
over its config value. `compact-output`, `raw-output`, `table`, `csv`,
and `tsv` don't override one at a time, though: since at most one of
`table`/`csv`/`tsv` can ever be active together with either JSON
option, an explicitly-active flag among these five suppresses config's
values for *all five*, not just the one with the same name - an
explicit `--csv` fully overrides a configured `table: true`, not just
leaves it unset alongside `--csv` in a way that would fail. `-i`
additionally suppresses every one of config's *values* outright, so
none of them can reach - or break - an in-place edit. The file itself
is still loaded and must be valid either way: a malformed file, or one
with an unrecognized key, is an error regardless of `-i`.

## License

MIT, see [`LICENSE`](LICENSE).
