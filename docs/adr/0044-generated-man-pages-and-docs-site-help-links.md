# 44. Generated man pages, and `--help` links to the docs site instead of raw ADRs

## Status

Accepted

## Context

Every long explanation in `xlq --help` - the filter grammar, multiple
input files, in-place editing, the config file, `--csv`/`--tsv`
flattening, `--timeout`/`--max-size` - links straight to a raw ADR file
on GitHub (`internal/cli/root.go`'s `adrURL`). An ADR is a
Nygard-style Context/Decision/Consequences record written for someone
deciding or reviewing architecture; it was never written to explain a
flag to someone who just typed `--help`. `docs/reference.md` already
shows the better pattern for the same ground - prose and a runnable
example first, with the ADR link kept only as a secondary "design
rationale" reference - but `--help` has only terse, cobra-style inline
prose and no runnable example, with the ADR link standing in for the
detailed explanation.

Pointing `--help` at `reference.html`'s existing, friendlier sections
instead seems like the obvious fix, but it trades away something
`adrURL`'s own doc comment calls out deliberately: an ADR's content is
frozen forever once `Accepted` (`CONTRIBUTING.md`'s own convention - a
later decision is a new ADR, never an edit), so a link baked into any
xlq binary ever built remains correct for that binary, forever.
`reference.md` tracks `main`, not any particular release - a heading
could be reworded or a section reorganized, silently breaking an old
binary's anchor (no 404; the browser just lands at the top of the
page), or, worse, still resolve but now describe behavior that's
since changed underneath an already-shipped binary.

Full versioned documentation (a `reference.html` snapshot per release,
a version switcher) would close that gap, but it's disproportionate
to xlq's actual rate of change: pre-1.0, mostly additive (new
builtins), with ADR-0037's semver policy already requiring a version
bump for anything that would actually contradict existing docs.
Projects that do version their full docs this heavily (Kubernetes,
Terraform) do it because their behavior genuinely differs release to
release in ways that matter; xlq isn't there, and standing up that
machinery now would add a new release-pipeline surface purely to hedge
a risk that's currently narrow and mostly cosmetic.

What comparable Go CLIs actually do instead: keep `--help` terse, and
either point at one living docs site with no per-release versioning
(`jq`, `yq` - the two projects xlq is explicitly modeled on), or
generate a man page straight from the command tree at build time via
Cobra's own `cobra/doc` package (`GenManTree`) - `gh` does this. A
generated man page can't drift from the binary it ships with: it's
produced from that exact build's flag strings, not hand-maintained
prose written once and left to rot.

## Decision

Two independent changes, not one:

1. **`--help` and every flag description in `internal/cli/root.go`
   lead with a link to the docs site** (`reference.html`/
   `quickstart.html`, using their existing anchors -
   `#addressing`, `#output-modes`, `#in-place-editing--i`,
   `#multiple-input-files`, `#input-format--delimiter`,
   `#resource-limits`, `#config-file`) **instead of an ADR file**, via
   a new `docsURL` constant alongside the existing `adrURL`. The ADR
   link is kept, parenthetically, after the docs-site link wherever it
   appears today - never removed, since it's the only link in this
   list that's actually guaranteed to still say the same thing for
   whichever version of xlq is asking. `docs/reference.md` and
   `docs/quickstart.md` each gain a one-line note that they reflect
   the latest release, so if the two ever do disagree for an older
   binary, that's visible instead of silent.

2. **xlq ships a generated man page.** A small generator (invoked from
   `.goreleaser.yaml` as a `before.hooks` step, not a hidden
   subcommand shipped in the binary itself - it only needs to run once
   per release build) calls `cobra/doc`'s `GenManTree` against
   `NewRootCommand()`, and the result is added to `archives.files` so
   it's bundled into the release archive alongside the binary. This
   is the version-safe reference `adrURL` was standing in for: whoever
   downloads a given release's archive gets a man page generated from
   that exact build's command tree, not a prose description that can
   fall out of sync with it.

Explicitly out of scope, left as known gaps rather than silently
unsolved:

- **Homebrew.** `homebrew_casks` (ADR-0016) has no Formula-style
  `man1.install` stanza - casks don't manage `MANPATH`. Making
  `brew install`'s man page show up under `man xlq` would mean either
  reopening ADR-0016's formula-vs-cask tradeoff or adding another
  cask `postflight`/`install_steps` hook, the exact surface that
  already broke once this project (the deprecated `postflight do`
  block fixed by ADR-0028). Not worth doing alongside this change.
- **`go install` users get no man page at all** - that install path
  only ever fetches Go source, never extra release-archive files.

## Consequences

- `internal/cli/root.go`: new `docsURL` constant; every `adrURL`-only
  explanation in `Long`, flag descriptions, and `Example` gets a
  docs-site link in front of it, with the ADR link kept after.
- `internal/cli/filter_test.go`'s
  `TestHelpNeverReferencesABareLocalDocsPath` needs updating to expect
  both URL forms present in `--help`'s output, not just `adrURL`.
- A new generator plus a `.goreleaser.yaml` `before.hooks` entry and
  `archives.files` addition; the release pipeline gains one more
  build-time step, but not another *published* artifact pipeline stage
  (unlike ADR-0038/0039/0040's provenance/signing/SBOM jobs, this runs
  before the archive is built, not after - no new place for GitHub's
  immutable-release behavior to bite).
- `docs/reference.md`/`docs/quickstart.md` gain a short "reflects the
  latest release" note.
- Anyone on an old, already-installed xlq still gets a link that
  resolves to *something* real from every `--help` invocation ever
  shipped - the docs site (current, and now labeled as such) and the
  ADR (frozen, and exactly what that version's design decision said) -
  never a dead reference, consistent with this same test's existing
  intent.
