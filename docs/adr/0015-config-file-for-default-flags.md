# 15. Config file for default flags

## Status

Accepted

## Context

Issue #16 asks for a config file so a user doesn't have to repeat the
same flags (`--table`, `--input-format csv`, ...) on every invocation.
Its own example flag, `--output-format`, is a stale leftover from an
earlier, since-abandoned design (#12, superseded by #32's discrete
`--csv`/`--tsv`/`--table` flags) - noted on the issue itself, and
confirmed against the actual flag set in `internal/cli/root.go` today:

- Boolean: `--compact`, `--compact-output`/`-c`, `--raw-output`/`-r`,
  `--table`, `--csv`, `--tsv`.
- String: `--input-format`, `--delimiter`.
- `-i`/`--in-place` also exists, but is excluded from this design -
  see Scope below.

xlq has had no persistent state of any kind until now: every
invocation is fully determined by its CLI arguments. A config file
introduces a second source of defaults, read from disk, that CLI
flags must take precedence over - the first time xlq's behavior
depends on anything besides what's typed on the command line.

## Decision

### Scope: every output/input flag except `-i`

Every flag above except `-i`/`--in-place` becomes config-defaultable.
`-i` is deliberately excluded: it changes xlq from read-only to
destructive (ADR-0014), and a config file silently turning every
invocation into an edit - or worse, an edit a user didn't realize was
active because it came from a forgotten config file - is exactly the
kind of surprise a destructive flag should never be subject to. `-i`
stays something you always type explicitly.

### File location: `os.UserConfigDir()/xlq/config.yaml`, with `--config` to override

Go's `os.UserConfigDir()` already resolves the right per-OS
convention - `$XDG_CONFIG_HOME` or `~/.config` on Linux, `~/Library/
Application Support` on macOS, `%AppData%` on Windows - with no new
dependency; it's exactly what the issue's own illustrative path
(`~/.config/xlq/config.yaml`) means, generalized across every
platform xlq ships binaries for (ADR-0006).

A new `--config <path>` persistent flag overrides that default
location. The two cases behave differently on a missing file,
matching how explicit-vs-defaulted is already handled for
`--delimiter` (`internal/cli/root.go`'s `inputDelimiter`, via
`cmd.Flags().Changed`): the **default** location missing is not an
error - most users will never create this file, and the whole feature
is opt-in - but an **explicitly passed** `--config` path that doesn't
exist is, since the user pointed at it on purpose.

### Format: YAML via `gopkg.in/yaml.v3`, flat keys matching flag names

`gopkg.in/yaml.v3` is the de facto standard pure-Go YAML library -
its own `go.mod` lists one indirect dependency, `gopkg.in/check.v1`,
but that's `yaml.v3`'s own test-only dependency: `go mod tidy` against
a module that imports `yaml.v3` never adds it to that module's own
`go.mod`, and no code from it is ever compiled into anything that
merely imports `yaml.v3` (confirmed empirically). Matching ADR-0004's
own reasoning for Cobra and excelize: a boring,
widely-used, already-trusted choice over hand-rolling a parser or
reaching for a heavier config framework. `spf13/viper` (Cobra's usual
companion) was considered and rejected: it pulls in a much larger
dependency tree (multiple format parsers, a filesystem abstraction,
env var binding, live-reload watching) for a feature that's really
just "read a handful of named values and use them as flag defaults" -
exactly viper's own job, but at a cost disproportionate to what xlq
needs here.

The file is a flat mapping, keyed by each flag's own long name
exactly as `--help` shows it - `compact`, `compact-output`,
`raw-output`, `table`, `csv`, `tsv`, `input-format`, `delimiter` - not
a short alias, and not nested under any subcommand: every one of
these flags already means the same thing everywhere it appears
(`input-format`/`delimiter` are root `PersistentFlags`, inherited by
`sheets` too). An unrecognized key is a hard error (`yaml.v3`'s
`Decoder.KnownFields(true)`) - silently ignoring a typo'd key (e.g.
`delimeter:`) would be a footgun a strict decoder catches for free.

Example:

```yaml
table: true
input-format: csv
```

### Precedence: an explicit flag always wins; config values are applied as if they'd been typed

A new `internal/config` package loads and parses the file into a
struct with one plain `bool`/`string` field per key (no pointers
needed: every one of these flags' own built-in zero value - `false`,
`""` - is also "not set," so a config file that omits a key or
explicitly repeats its zero value behave identically, which is the
behavior this feature actually needs; see Explicitly deferred for a
case this doesn't cover).

Merging happens in a `PersistentPreRunE` on the root command - the one
place that runs before *any* command's own `RunE` (`sheets` defines no
`PersistentPreRunE` of its own, so Cobra runs the root's), without
needing every `RunE` to know about config at all. For each config key
whose value is non-zero: if the current command actually defines a
flag by that name (`cmd.Flags().Lookup(name)`, since `sheets` has no
`--table`) and the user didn't already pass it explicitly
(`!cmd.Flags().Changed(name)`), set it via `cmd.Flags().Set(name,
...)` - the standard pflag idiom for "apply a default from somewhere
other than the flag's own registration," and exactly what reading the
flag normally (`GetBool`/`GetString`) sees from then on. Every
existing `RunE` (`runFilter`, `runInPlace`, `sheets`) is completely
unchanged - from where they sit, a config-sourced value is
indistinguishable from one the user actually typed.

This also means `outputOptions.validate()` (`internal/cli/filter.go`)
doesn't need a config-aware variant: by the time it runs, config has
already been folded into the same flags it already reads, so a config
file that sets `csv: true` as a default *and* an invocation that
explicitly passes `--table` hits the same "mutually exclusive output
modes" error an invocation passing both flags directly would.

### Explicitly deferred

- Environment variable overrides (a third precedence tier between the
  CLI and the config file, as some CLIs have). Not asked for by the
  issue; config-file-only keeps the precedence model to two tiers
  instead of three.
- Distinguishing "a config key explicitly set to its zero value" from
  "a config key omitted entirely" (e.g. `delimiter: ""` meaning
  "force the format's own default even if some *other*, lower-
  precedence source would have set a delimiter") - there is no other
  source for these flags to disagree with yet, so this distinction has
  no observable effect today. If env var overrides (above) are ever
  added, this would need revisiting with pointer fields instead of
  plain ones.
- A project-local config (e.g. `./.xlq.yaml`, checked in next to the
  files a filter runs against) in addition to the user-global one.
- Any `xlq config` subcommand to read/write/validate the file instead
  of editing it by hand.
- Config for anything other than a flag default - there's no other
  kind of setting to configure yet.

## Consequences

A user who runs `xlq --table '.Sheet1' report.xlsx` every day can
instead write `table: true` once to
`~/.config/xlq/config.yaml` (or wherever `os.UserConfigDir()` resolves
on their OS) and drop `--table` from the command entirely; explicitly
passing `--csv` on some other invocation still overrides it, the same
as if no config file existed at all. `-i` is never defaultable, by
design.

A new dependency (`gopkg.in/yaml.v3`) and a new package
(`internal/config`) are added. Every existing `RunE` and
`outputOptions.validate()` stay as they are; the new
`PersistentPreRunE` is root's only direct touch point, and it does
nothing at all for a user who has no config file - the default,
most common case has zero behavioral change.
