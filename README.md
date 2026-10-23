# xlq

`xlq` is a command-line spreadsheet processor in the spirit of
[`jq`](https://jqlang.org/) and [`yq`](https://mikefarah.gitbook.io/yq/):
a single static binary for slicing, filtering, and transforming
spreadsheet data (`.xlsx`, `.csv`, `.tsv`, `.ods`) from the shell.

**[Full documentation, a quickstart, and examples →](https://brunoarueira.github.io/xlq/)**

See [`docs/adr/`](docs/adr/) for the design decisions made so far, and
[`CONTRIBUTING.md`](CONTRIBUTING.md) for how the project is run.

## Status

Early, but the core loop works:

```sh
xlq sheets path/to/book.xlsx           # list sheet names
xlq '.Sheet1.B2' path/to/book.xlsx     # a single cell
xlq '.Sheet1 | records | select(.Likes > 100)' path/to/book.xlsx  # filter rows by a header-keyed field
xlq '.Sheet1 | records | map(.Likes) | sum' path/to/book.xlsx     # total a column
xlq --table '.Sheet1 | records' path/to/book.xlsx                 # aligned table, not JSON
xlq -i '.Sheet1.A1 = "done"' path/to/book.xlsx                    # edit a cell in place (.xlsx only)
```

Arithmetic and formula introspection aren't implemented yet. See the
[Reference](https://brunoarueira.github.io/xlq/reference.html) for
everything the filter language actually supports, and
[Examples](https://brunoarueira.github.io/xlq/examples.html) for real
problems it solves.

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

## License

MIT, see [`LICENSE`](LICENSE).
