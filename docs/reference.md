---
layout: page
title: Reference
permalink: /reference.html
---

The full filter language, output modes, and configuration. This page
reflects the latest release - if you're on an older `xlq` (check with
`xlq --version`), something here may not match yet; see the
[releases page](https://github.com/brunoarueira/xlq/releases) for what
changed since. For design rationale behind any of this, the linked ADR
has it -
[`docs/adr/`](https://github.com/brunoarueira/xlq/tree/main/docs/adr) is the
complete decision history.

`xlq '<filter>' file.xlsx` parses `<filter>`
([ADR-0008](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0008-filter-grammar-v1.md)),
evaluates it against the file, and prints the result as JSON.

## Addressing

Filters address a sheet by name (`.Sheet1`, case-insensitive), and from
there a cell by its A1 reference (`.Sheet1.A1`), a whole column
(`.Sheet1.B`), a whole row (`.Sheet1[5]`, 1-based), or a rectangular range
(`.Sheet1["A3:C9"]`). Arithmetic and formula introspection aren't
implemented yet.

```sh
xlq sheets path/to/book.xlsx           # list sheet names
xlq '.Sheet1.B2' path/to/book.xlsx     # a single cell
xlq '.Sheet1' path/to/book.xlsx        # a whole sheet, as a 2D array
xlq '.Sheet1 | .B2' path/to/book.xlsx  # same cell, piped
```

A sheet name is a plain identifier (`.Sheet1`), so one containing a space
or other non-identifier character needs the bracket-string form instead:

```sh
xlq '.["TOP POSTS"] | records | .[0].G' path/to/book.xlsx
```

### Ranges

A range ([ADR-0012](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0012-range-references.md))
selects a rectangular block of a sheet and behaves exactly like a whole
sheet from then on - it's still just a sheet, only smaller. That means
every address inside it is **relative to the range**, not the original
sheet: `.Sheet1["C3:E9"].A1` is the range's own top-left cell, i.e. the
original sheet's `C3`, not its actual `A1`. This is what makes a sheet with
two tables side by side addressable at all - narrow a range to one table's
rows *and* columns before piping into `records`, so the other table's
columns were never part of the slice to begin with.

## `records`

([ADR-0011](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0011-header-aware-record-output.md))
turns a sheet into an array of row objects keyed by its row-1 header, so a
column can be addressed by name instead of position - useful for the common
case of a sheet shaped like a CSV table.

```sh
xlq '.Sheet1 | records | .[0].SomeHeader' path/to/book.xlsx
```

Two things worth knowing before relying on it: the records array is
0-indexed, jq-style (`.[0]` is the first record), unlike a sheet's own
1-based row numbers above; and a record's JSON keys print alphabetically,
not in the sheet's original column order, since that's how Go's JSON
encoder orders any object's keys. It also assumes row 1 actually is a
header row; piping a sheet without one into `records` will treat its first
data row as headers instead.

### `headers(f)`

([ADR-0036](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0036-headers-builtin.md))
applies a filter - typically one of the [string builtins](#string-builtins)
below - to every header name, either on its own (to preview the result) or
nested inside `records` (to actually rename the resulting keys):

```sh
xlq '.Sheet1 | headers(snake_case)' path/to/book.xlsx
# -> ["account", "priority_order"]

xlq '.Sheet1 | records(headers(snake_case))' path/to/book.xlsx
# -> [{"account": "acme", "priority_order": "1"}]
```

`records(f)` alone - any `f` other than `headers(...)` - is an error:
`records`'s only valid argument shape is `headers(...)`, checked at eval
time (the parser accepts any filter expression there, the same way it
does for `map`/`select`'s own argument). An unnamed column's own letter
fallback (`records`' own behavior, above) is transformed too, same as a
real header name. Unlike `records` itself, standalone `headers(f)`
tolerates two columns transforming to the same name (its result is a
plain array, not a set of object keys) - `records(headers(f))` still
errors on that collision, the same "duplicate header" error plain
`records` already has for two identically-named columns.

## Conversions

### `tonumber`

([ADR-0022](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0022-extensible-builtins.md)
for the builtin mechanism it's built on) converts a string to a number, for
exports (LinkedIn's among them) that store numbers as text:

```sh
xlq '.DISCOVERY.B2 | tonumber' path/to/book.xlsx  # "3048" -> 3048
```

An already-numeric value passes through unchanged; anything that can't be
parsed as a number - including `null` and a boolean, which don't get the
"propagates as null" treatment field/index access has - is an error, not a
silent `null`, matching jq's own `tonumber`. Only a plain decimal number is
accepted (an optional minus sign - no leading `+` - digits, an optional
fraction, an optional
exponent) - `"NaN"`, `"Inf"`/`"Infinity"`, and a hex float like `"0x1p2"`
are all rejected too, even though Go's own number parser would otherwise
accept them, since none of those is a number a spreadsheet cell's text
would ever legitimately mean. Leading/trailing whitespace is trimmed before
parsing (a deliberate difference from jq, for exactly the kind of
incidental copy-paste noise this conversion exists to look past).

### `todate(format)`

([ADR-0023](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0023-explicit-date-parsing.md))
converts a string to an RFC3339 date, given an explicit format - a date
like `7/8/2026` is genuinely ambiguous (US month/day/year vs. everyone
else's day/month/year: July 8th or August 7th?), so, matching `tonumber`'s
own "never guess" rule, `todate` always needs to be told which:

```sh
xlq '.Sheet1.B2 | todate("M/D/YYYY")' path/to/book.xlsx  # "7/28/2026" -> an RFC3339 date
```

`.Sheet1.B2 | todate("M/D/YYYY")` and `.Sheet1.B2 | todate("D/M/YYYY")` read
the same text as two different, equally valid dates. Format tokens are
Excel/spreadsheet-style, not code syntax: `YYYY`/`YY` (year, 4 or 2 digits -
a 2-digit year pivots 00-68 to 2000-2068 and 69-99 to 1969-1999), `MM`/`M`
(month, zero-padded or not), `DD`/`D` (day, same). Anything else in the
format - `/`, `-`, a space, even a word - is matched as literal, never
reinterpreted (`todate("D January M YYYY")` treats `"January"` as an
arbitrary literal separator, the same as `/` would be, not a month name). A
word that happens to start with a token letter, like `"March"` or the
all-caps `"MAY"` (an Oracle-style `"DD-MON-YYYY"` separator), is still
literal as long as what follows that letter isn't itself the start of
another token - so `"D of M, March YYYY"` still reads the real month from
`M`, not from `"March"`, while `"MDDYYYY"` (`M` immediately followed by
`DD`, which *is* a token) needs no separator at all between the two. Either
way, a month name supplies no month value by itself - a format that relies
on one alone, with no separate numeric month token, still errors for
missing the month, the same as any other incomplete format.

A format must include exactly one year, one month, and one day token -
`todate("M/D")` (no year) and `todate("M/D/YYYY/D")` (two day tokens) are
both errors, not silently accepted with a guessed or overwritten value -
and the input must actually describe a real calendar date:
`todate("M/D/YYYY")` on `"2/30/2026"` errors, since February never has 30
days. The input must already be a string; `null`, a number, and a boolean
are all errors, with no "propagates as null" exception, same as
`tonumber`. Leading/trailing whitespace is trimmed before parsing, matching
`tonumber`'s own deviation from jq, for the same reason: incidental
copy-paste noise, not a meaningful part of the date text.

## String builtins

### `upper` / `lower` / `trim`

([ADR-0033](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0033-upper-lower-trim.md))
`upper`, `lower`, and `trim` are zero-argument string transforms, useful for
normalizing a field before writing it to a database or calling an API that
expects a specific casing, or for cleaning up messy header/column text:

```sh
xlq '.Sheet1.B2 | lower | trim' path/to/book.xlsx         # a "Status" column cell: "  ACTIVE " -> "active"
xlq '.Sheet1 | records | map(.Status | lower)' path/to/book.xlsx
```

Like every builtin, `upper`/`lower`/`trim` are pipeline steps or a
`map`/`select`/`group_by` filter-expression argument (as above) - never a
whole filter's own first segment, which always starts from a path
(`.Sheet1...`). They also parse as a comparison's right-hand side, the same
grammar position every builtin has (ADR-0022); that position evaluates
against **the comparison's own current input**, not the left operand's
result, so it isn't a natural fit here either, the same caveat `sum`/
`count`/`avg` already carry.

`upper`/`lower` are Unicode-aware (Go's `strings.ToUpper`/`ToLower`), not
ASCII-only like jq's `ascii_upcase`/`ascii_downcase` - a cell holding `café`
becomes `CAFÉ`. `trim` matches jq's own `trim` exactly: it strips only
leading and trailing whitespace, leaving internal runs alone - a cell
holding `"  a   b  "` becomes `"a   b"`, not `"a b"`. All three require a
string input and error on anything else, including `null` - no "propagates
as null" exception, the same rule `tonumber`/`todate` already follow.

### `snake_case` / `camel_case`

([ADR-0034](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0034-snake-case-camel-case.md))
convert between word-boundary casing styles - useful for the same header-
and field-cleanup cases as `upper`/`lower`/`trim`, when the source and
target casing conventions actually differ:

```sh
xlq '.Sheet1.A1 | snake_case' path/to/book.xlsx  # "PriorityOrder" -> "priority_order"
xlq '.Sheet1.A1 | camel_case' path/to/book.xlsx  # "priority_order" -> "priorityOrder"
```

Both split the input into words on any non-letter-non-digit separator
(space, `-`, `_`, ...) and on two further boundaries within a run of
letters/digits: before an uppercase letter directly following a lowercase
letter or digit (`priorityOrder` -> `priority`/`Order`, `q3Report` ->
`q3`/`Report`), and before the last letter of an all-uppercase run when
it's immediately followed by a lowercase letter (`HTTPServer` ->
`HTTP`/`Server` - without this rule, an acronym followed by a word
wouldn't split at all). A standalone acronym with nothing lowercase after
it (`URL`) stays one word. `snake_case` lowercases every word and joins
with `_`; `camel_case` lowercases every word and capitalizes all but the
first, with no separator - so `camel_case`'s first word is always fully
lowercase, even for a single all-uppercase input (`URL` -> `url`, not
`uRL`). Both are Unicode-aware, matching `upper`/`lower`, and both error
on a non-string input, including `null`, the same rule every string
builtin here follows.

### `replace(old, new)`

([ADR-0035](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0035-replace.md))
replaces every occurrence of a literal substring with another - a plain
substring swap, not a regex:

```sh
xlq '.Sheet1.A1 | replace(",", "")' path/to/book.xlsx          # "1,234,567" -> "1234567"
xlq '.Sheet1.A1 | replace(",", "") | tonumber' path/to/book.xlsx
```

Every occurrence is replaced, not just the first (matching Go's own
`strings.ReplaceAll`, not JavaScript's plain `.replace`, which only
touches the first). `old` must be a non-empty string - an empty `old`
would otherwise insert `new` between every character of the input,
which is never useful, so it's rejected as an error instead. Both
arguments are literal strings, the same `name(arg, ...)` shape
`todate(format)` already has - not a filter-expression argument like
`map`/`select`'s. Errors on a non-string current input, including
`null`, the same rule every string builtin here follows.

Like every builtin, `replace` is a pipeline step or a `map`/`select`/
`group_by` filter-expression argument - never a whole filter's own
first segment - and it also parses as a comparison's right-hand side,
the same grammar position every builtin has (ADR-0022); that position
evaluates against **the comparison's own current input**, not the left
operand's result, so it isn't a natural fit here either, the same
caveat `upper`/`lower`/`trim` and `sum`/`count`/`avg` already carry.

This closes out #124's three-builtin batch (`upper`/`lower`/`trim`,
`snake_case`/`camel_case`, `replace`). See [`headers(f)`](#headersf)
above for applying one of these to `records`' own keys.

## Comparisons

([ADR-0024](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0024-comparison-operators.md))
`==`, `!=`, `<`, `>`, `<=`, `>=` work the way you'd expect:

```sh
xlq '.Sheet1.A1 == .Sheet1.B1' path/to/book.xlsx
xlq '.Sheet1 | records | .[0].Likes > 100' path/to/book.xlsx
xlq '.Sheet1.A1 == "done"' path/to/book.xlsx
```

`==`/`!=` never error on a type mismatch - a number and a string are just
"not equal," matching jq exactly - though comparing a whole sheet or
workbook is still a clear error, never a (meaningless) `true` or `false`,
the same as for ordering below. Ordering deliberately does **not** match
jq: comparing two different types (other than with `null`) is an error,
not a guessed ranking - `tonumber`/`todate` already refuse to guess a
cell's type from its text, and silently deciding "a string always outranks
a number" would be the same kind of guess. This includes two values of the
*same* type when neither is a number or a string - `true > false` is also
an error, not `true`, since this project defines no ordering for booleans
at all. `null` compared with any scalar via an ordering operator is
`false`, never an error - unlike equality's `null == null`, which is
`true` - so a sparse column's missing value just quietly fails a threshold
check instead of erroring out; comparing a whole sheet or workbook is
still an error even against `null`, the same as every other comparison
involving one.

## `map` / `select`

([ADR-0025](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0025-map-select-filter-arguments.md))
take a filter expression - not a literal, unlike every other builtin's
arguments - and run it once per array element, with `.` rebound to that
element:

```sh
xlq '.Sheet1 | records | map(.Likes)' path/to/book.xlsx           # extract one field across every record
xlq '.Sheet1 | records | select(.Likes > 100)' path/to/book.xlsx  # keep only matching records
```

Both iterate and collect/filter an array directly, by themselves - unlike
jq's own `select`, which needs the separate `.[] | select(...)` idiom,
since this project has no `.[]` operator. `map`'s result is always a new
array - its filter is free to transform each element into something else
entirely - while `select` keeps each passing element exactly as it was.
`select`'s filter must evaluate to an actual boolean; a non-boolean result
(even a jq-style "truthy" value like `0` or `""`) is an error, not a
guessed coercion. Both require an actual array (`records`'s output, or a
plain row/column array) - a scalar, a single record, or `null` is an
error. For `map`, that matches jq's own behavior (`jq -n 'null | map(.)'`
also errors, "Cannot iterate over null"); for `select`, it's this
project's own deliberate design, not a jq match - jq's `select` isn't
array-iterating at all (see above), so it has no equivalent "requires an
array" rule to match in the first place.

## `sum` / `count` / `avg`

([ADR-0026](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0026-sum-count-avg.md))
are zero-argument reduce builtins - no filter-expression argument, unlike
`map`/`select`; project a field first with `map`:

```sh
xlq '.Sheet1 | records | map(.Likes) | sum' path/to/book.xlsx
```

the same composition jq's own `map(.field) | add` idiom uses. Like every
builtin, any of the three also parses as a comparison's right-hand side,
the same grammar position `tonumber`/`todate` already have - it just
isn't a particularly natural fit for `sum`/`count`/`avg` specifically,
since that position evaluates against **the comparison's own current
input** (the whole filter's starting value), not the left operand's
result - the left side is usually already the array these builtins need,
so a right-hand `sum` doesn't operate on it the way it might look like it
would. `count`
accepts `records`'s output or a plain array and returns its element count,
including `null` elements - an array-only, jq-compatible subset of jq's
own polymorphic `length`, which also defines a length for a string, a
number, and an object. `sum` requires a plain array of numbers (not
`records`'s output directly - summing a record has no single defined
field); a `null` element contributes `0`, the same jq-inspired behavior
jq's own `add` has for numbers - jq's `add` is itself polymorphic too
(string/array concatenation, object merging), none of which `sum` here
attempts, deliberately: guessing "the user must have meant concatenation"
from an array's element type would be exactly the kind of cross-type guess
this project otherwise refuses to make; any other non-number element is a
clear error. An array with no non-`null` numbers at all (empty, or every
element `null`) sums to `null`, not `0`, matching jq's own numeric `add`
for both cases - so "no data" and "a genuine zero total" stay
distinguishable. `avg` has no jq equivalent - this project's own addition -
and deliberately disagrees with `sum` on `null`: excluded from both the
total and the count used to divide, not treated as a zero contribution,
since averaging a sparse column should reflect the values actually
present.

## `group_by(filter)`

([ADR-0027](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0027-group-by.md))
groups an array's elements by the filter's per-element result:

```sh
xlq '.Sheet1 | records | group_by(.Category)' path/to/book.xlsx
xlq '.Sheet1 | records | group_by(.Category) | map(count)' path/to/book.xlsx          # size each group
xlq '.Sheet1 | records | group_by(.Category) | map(map(.Likes) | sum)' path/to/book.xlsx  # total each group
```

Returns an array of arrays, one per distinct `Category` value, each
holding the matching records in their original order. Unlike jq's own
`group_by`, the groups come out in the order their key was **first
encountered**, not sorted by key - sorting would mean adopting an ordering
over group keys, and this project's own ordering is deliberately
restricted (number/string only) specifically to avoid guessing a ranking
across types the way jq's own total ordering does. Grouping by equality
instead sidesteps that question entirely, and keeps `true`/`false`/`null`
available as group keys, not just numbers and strings. A record, an
array, a sheet, or a workbook can't be used as a group key - project a
scalar field first, the same restriction `sum`/`avg` already have.
`--csv`/`--table` render each group as a single JSON-text row, not a
nested table - there's no single obvious flat tabular form for a grouped
result, so JSON remains the more useful way to consume one.

## Indexing & slicing

Once a value is an ordinary array - a `records` result, or a whole
column/row (`.Sheet1.B`, `.Sheet1[5]`) - it can be indexed (`.[0]`) or
sliced (`.[1:10]`, jq-style: 0-based, end-exclusive, a negative bound
counts from the end, and an out-of-range bound clamps rather than
erroring) the same way a `jq` array would.

`[START:END]` can't be applied *directly* to a sheet or a range -
`.Sheet1[5:10]` is a clear error, not "rows 5 through 9" - but a range
composes with slicing just fine once it's piped into something
array-shaped:

```sh
xlq '.Sheet1["A3:C9"] | records | .[0:5]' path/to/book.xlsx
```

A range picks a rectangular block of a *sheet* by cell address, 1-based,
before the sheet is ever turned into an array of anything; `[START:END]`
slices an *already-produced array* by position, 0-based, same as
`records`' own `.[N]` convention. See
[ADR-0020](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0020-array-slicing.md)
for why the two don't mix: `.Sheet1[5]` already means "spreadsheet row 5",
so `.Sheet1[5:10]` meaning "array positions 5 through 9" on that same
sheet value would silently disagree with it - slice an array you've
already produced instead, e.g. `.Sheet1 | records | .[0:10]` for the top
10 rows.

## Output modes

```sh
xlq --compact '.Sheet1' path/to/book.xlsx  # drop empty rows/columns
xlq -c '.Sheet1' path/to/book.xlsx         # compact JSON, one line
xlq -r '.Sheet1.A1' path/to/book.xlsx      # a string result, unquoted
xlq --table '.Sheet1 | records' path/to/book.xlsx  # aligned table, not JSON
xlq --csv '.Sheet1 | records' path/to/book.xlsx    # with a header row
xlq --tsv '.Sheet1' path/to/book.xlsx              # positional, no header
```

`--table` prints a human-readable, column-aligned table instead of JSON -
particularly useful piped from `records`, where it renders one row per
record headed by its sorted field names, but it handles every result shape
(a whole sheet, a range, a single cell, the whole workbook). It's a
different output mode entirely, not a JSON formatting tweak, so it can't
be combined with `-c`/`-r`.

`--csv`/`--tsv` ([ADR-0013](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0013-csv-tsv-output.md))
print comma/tab-separated text instead of JSON, with `encoding/csv`'s own
quoting and escaping. A sheet grid or range flattens positionally, with no
header row; `records` gets a header row (its sorted field names) plus one
row per record - pipe through `records` first when a header is wanted.
The whole-workbook result is a clear error (no single tabular shape), not
a guess. `--table`, `--csv`, and `--tsv` are mutually exclusive output
modes, and each is also mutually exclusive with `-c`/`-r` (which only ever
meant something for JSON) - but `-c` and `-r` themselves still compose
freely with each other.

## In-place editing (`-i`)

([ADR-0014](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0014-in-place-editing.md))
edits a `.xlsx` file instead of reading it: `<filter>` must be an
assignment (`.Sheet1.A1 = "value"`, a literal
string/number/`true`/`false`/`null`, exactly one cell), not a pipeline.

```sh
xlq -i '.Sheet1.A1 = "done"' path/to/book.xlsx
```

It prints nothing on success, matching `yq -i`'s own silence - the edit is
the result. csv/tsv/ods have no writer yet, so `-i` only supports xlsx.

## Multiple input files

([ADR-0017](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0017-multiple-input-files.md))
passing more than one `<file>` maps the *same* `<filter>` over each file
independently - not a diff, just each file's own result placed at that
file's own index - collecting them into a JSON array in file order:

```sh
xlq '.Sheet1.A1' old.xlsx new.xlsx   # same filter on each file, results as an array
xlq '.Sheet1 | records' old.xlsx new.xlsx
# [ [ {old file's records...} ], [ {new file's records...} ] ]
```

A single file's result is completely unaffected by this - still printed
bare, never wrapped in a one-element array. `--table`/`--csv`/`--tsv` and
`-i` don't support more than one file (no single tabular shape to flatten
N results into, and an in-place edit of more than one file is out of
scope) - this includes a config-file default (`table: true`, etc.): it
hits the same rejection a typed `--table` would, since there's no way to
tell them apart by the time this check runs, so `--table=false` (or
`--csv=false`/`--tsv=false`) is how to get plain JSON for one multi-file
invocation despite that default. A read/parse error names which file (by
position and path) failed.

`--input-format`/`--delimiter` (typed or from a config default) apply to
*every* file uniformly, not per-file - mixing formats across files (e.g.
diffing an `.xlsx` against a `.csv`) only auto-detects correctly when
neither resolves to a value, letting each file's own extension decide its
format as usual.

## Input format & delimiter

The input format (`xlsx`, `csv`, `tsv`, or `ods`) is picked from the
file's extension; `--input-format` overrides that for a misnamed file or
one with no recognizable extension. `--delimiter` overrides a csv/tsv
file's own default field separator (comma, tab) with any single character
the CSV reader accepts (a double quote, carriage return, newline, or
invalid character aren't valid delimiters) - useful for a
semicolon-separated export from a locale where comma is the decimal
separator. It's an error alongside an xlsx or ods input, which have no
delimiter concept to override.

```sh
xlq --input-format csv '.Sheet1' path/to/export.txt  # misnamed file
xlq --delimiter ';' '.Sheet1' path/to/export.csv     # non-comma csv
```

## Resource limits

`--timeout` (default 2 minutes) bounds reading and evaluating a file as a
wall-clock limit, so a hang on a corrupt or adversarial file can't block
forever; `0` disables it. `--max-size` (default 256 MiB) bounds how large
a file `xlq` will attempt, including how large an `.xlsx` is allowed to
decompress to in memory; `0` disables it. Both exist to make `xlq` safe
against untrusted input (a CI step, a wrapper service processing
user-uploaded files) - see
[ADR-0030](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0030-recover-and-timeout-the-read-path.md)
and
[ADR-0031](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0031-memory-ceiling-for-untrusted-files.md).

## Shell completions

`xlq completion <shell>` generates a completion script for bash, zsh,
fish, or powershell (via Cobra's own built-in generator - run `xlq
completion <shell> --help` for that shell's exact setup instructions). For
example, on bash:

```sh
# load for the current session only
source <(xlq completion bash)

# load for every new session (Linux)
xlq completion bash | sudo tee /etc/bash_completion.d/xlq >/dev/null

# load for every new session (macOS, Homebrew's bash-completion@2)
xlq completion bash > $(brew --prefix)/etc/bash_completion.d/xlq
```

`xlq`'s completion covers subcommands and flags (`xlq sheets`, `--table`,
...) and may fall back to filename completion for positional arguments;
`<filter>` itself isn't completed, since it isn't a fixed set of words.

## Config file

([ADR-0015](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0015-config-file-for-default-flags.md))
a YAML file can set defaults for the eight flags listed below - every
input/output flag that existed when this mechanism was built, though not
`--timeout`/`--max-size` (added later, in
[ADR-0030](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0030-recover-and-timeout-the-read-path.md)/[ADR-0031](https://github.com/brunoarueira/xlq/blob/main/docs/adr/0031-memory-ceiling-for-untrusted-files.md),
neither of which extended the config schema) or, of course,
`-i`/`--in-place`, which can never be config-defaulted at all - so a flag
you'd otherwise pass on every invocation can be set once instead. By
default it's read from `os.UserConfigDir()/xlq/config.yaml` -
`$XDG_CONFIG_HOME/xlq/config.yaml` (or `~/.config/xlq/config.yaml` if
`$XDG_CONFIG_HOME` is unset) on Linux, `~/Library/Application
Support/xlq/config.yaml` on macOS, `%AppData%\xlq\config.yaml` on Windows -
or from `--config <path>` instead. A missing file at the default location
is fine (the feature is entirely opt-in); an explicitly-passed `--config`
path that doesn't exist is an error.

```yaml
table: true
input-format: csv
```

Keys match each flag's own long name exactly (`compact`, `compact-output`,
`raw-output`, `table`, `csv`, `tsv`, `input-format`, `delimiter`) - an
unrecognized key is a hard error, to catch a typo rather than silently
ignore it. An explicitly-passed flag always wins over its config value.
`compact-output`, `raw-output`, `table`, `csv`, and `tsv` don't override
one at a time, though: since at most one of `table`/`csv`/`tsv` can ever
be active together with either JSON option, an explicitly-active flag
among these five suppresses config's values for *all five*, not just the
one with the same name - an explicit `--csv` fully overrides a configured
`table: true`, not just leaves it unset alongside `--csv` in a way that
would fail. `-i` additionally suppresses every one of config's *values*
outright, so none of them can reach - or break - an in-place edit. The
file itself is still loaded and must be valid either way: a malformed
file, or one with an unrecognized key, is an error regardless of `-i`.
