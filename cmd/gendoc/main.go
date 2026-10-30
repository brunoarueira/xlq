// Command gendoc generates xlq's man page from its own Cobra command
// tree (docs/adr/0044-generated-man-pages-and-docs-site-help-links.md).
// It's a release-build-time tool, not something end users run or that
// ships in the release binary itself - .goreleaser.yaml invokes it as
// a before.hooks step, and the result is bundled into the release
// archive via archives.files.
package main

import (
	"fmt"
	"os"
	"regexp"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"github.com/spf13/pflag"

	"github.com/brunoarueira/xlq/internal/cli"
	"github.com/brunoarueira/xlq/internal/version"
)

// placeholderPattern matches xlq's own "<filter>"/"<file>" convention
// for a placeholder argument, used throughout internal/cli/root.go's
// Use/Long/Example/flag-usage text.
var placeholderPattern = regexp.MustCompile(`<[^<>\s]+>`)

// escapeForMan wraps each placeholder in a markdown code span before
// cobra/doc's GenManTree runs the text through its markdown-to-roff
// pipeline (github.com/cpuguy83/go-md2man). Confirmed directly against
// that pipeline: a bare "<filter>" is treated as unrecognized inline
// HTML and silently dropped, while a code-span-wrapped "`<filter>`"
// survives, rendered bold. Plain `xlq --help` output never runs
// through this - it prints root.go's Long/Example fields completely
// unprocessed - so this only ever touches the throwaway command tree
// built for man-page generation, never root.go's own strings.
func escapeForMan(s string) string {
	return placeholderPattern.ReplaceAllString(s, "`$0`")
}

// escapeCommandForMan applies escapeForMan to cmd and every descendant
// command's Use/Short/Long/Example and every flag's usage string.
// seen dedupes a flag visited through both Flags() and
// PersistentFlags(), which can be the same *pflag.Flag - escaping it
// twice would double-wrap it in backticks.
func escapeCommandForMan(cmd *cobra.Command, seen map[*pflag.Flag]bool) {
	cmd.Use = escapeForMan(cmd.Use)
	cmd.Short = escapeForMan(cmd.Short)
	cmd.Long = escapeForMan(cmd.Long)
	cmd.Example = escapeForMan(cmd.Example)
	visit := func(f *pflag.Flag) {
		if seen[f] {
			return
		}
		seen[f] = true
		f.Usage = escapeForMan(f.Usage)
	}
	cmd.Flags().VisitAll(visit)
	cmd.PersistentFlags().VisitAll(visit)
	for _, sub := range cmd.Commands() {
		escapeCommandForMan(sub, seen)
	}
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: gendoc <output-dir>")
		os.Exit(1)
	}
	dir := os.Args[1]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	root := cli.NewRootCommand()
	// GenManTree reads Version off the command itself for the man
	// page's own header - set here since Execute() (cmd/xlq/main.go)
	// never calls this path, so it's otherwise left at its zero value.
	root.Version = version.Version
	escapeCommandForMan(root, map[*pflag.Flag]bool{})

	header := &doc.GenManHeader{
		Title:   "XLQ",
		Section: "1",
		Source:  "xlq " + version.Version,
		Manual:  "xlq Manual",
	}
	if err := doc.GenManTree(root, header, dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
