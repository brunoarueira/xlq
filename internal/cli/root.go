// Package cli wires xlq's command-line surface.
package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/brunoarueira/xlq/internal/input"
	"github.com/brunoarueira/xlq/internal/version"
)

// defaultReadTimeout is generous on purpose: its job is catching a
// genuine hang (an infinite loop, an adversarial file), not rushing a
// legitimately large one.
const defaultReadTimeout = 2 * time.Minute

// defaultMaxFileSize bounds how large a file any reader will attempt,
// and (for xlsx specifically) how large excelize will let a workbook
// decompress to in memory (docs/adr/0031-memory-ceiling-for-untrusted-files.md).
// 256 MiB matches internal/ods's own existing, independently-chosen
// maxContentXMLSize, for one consistent "this is clearly not a
// legitimate v1-sized file" ceiling across every format rather than a
// different magic number per reader.
const defaultMaxFileSize = 256 << 20

// adrURL is the base of a stable link to an ADR's own file, for any
// text that ships inside the compiled binary (--help, flag
// descriptions) rather than being read from a checkout or GitHub's own
// repo browser. A local "docs/adr/NNNN-....md" path - which every
// internal Go doc comment and every Markdown file in this repo still
// uses, deliberately, since those are read from a checkout or
// GitHub's own rendering of this repo, where a relative link already
// resolves correctly - is dead for anyone who installed xlq via `go
// install`, a release tarball, or Homebrew: none of those ship a
// docs/ directory alongside the binary at all. main, not a tagged
// version, is safe to link because an ADR's own content never changes
// after it's Accepted (CONTRIBUTING.md's own convention - a later
// decision is a new ADR, not an edit to an old one), so any version of
// xlq and this link always agree on what a given ADR number says.
const adrURL = "https://github.com/brunoarueira/xlq/blob/main/docs/adr/"

// NewRootCommand builds the xlq root command and its subcommands.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "xlq <filter> <file>...",
		Short: "xlq is a command-line spreadsheet processor",
		Long: "xlq is a jq/yq-style command-line processor for spreadsheet files.\n\n" +
			"<filter> is evaluated against <file> - see\n" +
			adrURL + "0008-filter-grammar-v1.md\n" +
			"- and the grammar correction in\n" +
			adrURL + "0009-correct-filter-grammar-ebnf.md\n" +
			"- for the v1 grammar. The result is printed as JSON, unless\n" +
			"--table, --csv, or --tsv asks for something else instead.\n\n" +
			"<file>'s format (xlsx, csv, tsv, ods) is picked from its extension;\n" +
			"use --input-format to override that for a misnamed file, and\n" +
			"--delimiter to override a csv/tsv file's field separator.\n\n" +
			"More than one <file> maps the same <filter> over each one\n" +
			"independently, collecting the results into a JSON array in file\n" +
			"order - useful for diffing two exports; --table/--csv/--tsv and\n" +
			"-i don't support more than one file - see\n" +
			adrURL + "0017-multiple-input-files.md\n\n" +
			"-i/--in-place edits <file> instead of reading it: <filter> must be\n" +
			"an assignment (\"path = value\"), and only .xlsx is supported - see\n" +
			adrURL + "0014-in-place-editing.md\n\n" +
			"A config file (default: os.UserConfigDir()/xlq/config.yaml, override\n" +
			"with --config) can set defaults for any flag above except -i - see\n" +
			adrURL + "0015-config-file-for-default-flags.md",
		Example: "  xlq '.Sheet1.A1' report.xlsx\n" +
			"  xlq '.Sheet1' report.xlsx\n" +
			"  xlq --compact '.Sheet1' report.xlsx\n" +
			"  xlq -c '.Sheet1' report.xlsx\n" +
			"  xlq -r '.Sheet1.A1' report.xlsx\n" +
			"  xlq --table '.Sheet1' report.xlsx\n" +
			"  xlq --csv '.Sheet1 | records' report.xlsx\n" +
			"  xlq --input-format csv '.Sheet1' report.txt\n" +
			"  xlq --delimiter ';' '.Sheet1' report.csv\n" +
			"  xlq '.Sheet1.A1' old.xlsx new.xlsx\n" +
			`  xlq -i '.Sheet1.A1 = "done"' report.xlsx` + "\n" +
			"  xlq --config ~/my-xlq.yaml '.Sheet1' report.xlsx",
		Version: version.Version,
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.MinimumNArgs(2)(cmd, args); err != nil {
				return fmt.Errorf("%w\n\nRun 'xlq --help' for usage", err)
			}
			return nil
		},
		SilenceUsage:      true,
		SilenceErrors:     true,
		PersistentPreRunE: mergeConfigDefaults,
		RunE: func(cmd *cobra.Command, args []string) error {
			compactData, _ := cmd.Flags().GetBool("compact")
			compactJSON, _ := cmd.Flags().GetBool("compact-output")
			rawOutput, _ := cmd.Flags().GetBool("raw-output")
			table, _ := cmd.Flags().GetBool("table")
			csvOut, _ := cmd.Flags().GetBool("csv")
			tsvOut, _ := cmd.Flags().GetBool("tsv")
			inPlace, _ := cmd.Flags().GetBool("in-place")
			format, err := inputFormat(cmd)
			if err != nil {
				return err
			}
			delim, err := inputDelimiter(cmd)
			if err != nil {
				return err
			}
			timeout := readTimeout(cmd)
			maxSize := maxFileSize(cmd)
			if inPlace {
				if compactData || compactJSON || rawOutput || table || csvOut || tsvOut {
					return fmt.Errorf("--in-place cannot be combined with --compact/--compact-output/--raw-output/--table/--csv/--tsv - an in-place edit prints nothing")
				}
				if len(args) != 2 {
					return fmt.Errorf("--in-place requires exactly one file, got %d", len(args)-1)
				}
				return runInPlace(args[0], args[1], format, delim, timeout, maxSize)
			}
			return runFilter(cmd, args[0], args[1:], format, delim, timeout, maxSize, outputOptions{
				compactData: compactData,
				compactJSON: compactJSON,
				rawOutput:   rawOutput,
				table:       table,
				csv:         csvOut,
				tsv:         tsvOut,
			})
		},
	}
	root.Flags().Bool("compact", false, "drop rows and columns with no values from the result")
	root.Flags().BoolP("compact-output", "c", false, "print JSON on one line, without indentation")
	root.Flags().BoolP("raw-output", "r", false, "print a string result without JSON quotes")
	root.Flags().Bool("table", false, "print a human-readable, column-aligned table instead of JSON")
	root.Flags().Bool("csv", false, "print comma-separated values instead of JSON (see "+adrURL+"0013-csv-tsv-output.md for how a result flattens)")
	root.Flags().Bool("tsv", false, "print tab-separated values instead of JSON (see "+adrURL+"0013-csv-tsv-output.md for how a result flattens)")
	root.Flags().BoolP("in-place", "i", false, `edit the .xlsx file in place: <filter> must be an assignment, e.g. '.Sheet1.A1 = "value"' (see `+adrURL+`0014-in-place-editing.md); prints nothing on success`)
	root.PersistentFlags().String("input-format", "", "input format: xlsx, csv, tsv, or ods (default: detected from the file extension)")
	root.PersistentFlags().String("delimiter", "", "field delimiter override for csv/tsv input, exactly one character (default: comma for csv, tab for tsv)")
	root.PersistentFlags().Duration("timeout", defaultReadTimeout, "wall-clock limit for reading and evaluating a file, e.g. 30s or 2m - a hang in a corrupt or adversarial file can't block past this (see "+adrURL+"0030-recover-and-timeout-the-read-path.md); 0 disables it")
	root.PersistentFlags().Int64("max-size", defaultMaxFileSize, "maximum file size in bytes xlq will attempt to read, and (for xlsx) the most excelize will let a workbook decompress to in memory - default 268435456 (256 MiB); 0 disables it (see "+adrURL+"0031-memory-ceiling-for-untrusted-files.md)")
	root.PersistentFlags().String("config", "", "config file path (default: os.UserConfigDir()/xlq/config.yaml; see "+adrURL+"0015-config-file-for-default-flags.md for which flags it can default and how)")
	root.SetVersionTemplate("xlq {{.Version}}\n")
	root.AddCommand(newSheetsCommand())
	return root
}

// Execute runs the root command against os.Args.
func Execute() error {
	return NewRootCommand().Execute()
}

// inputFormat reads cmd's --input-format flag (inherited from the
// root command's PersistentFlags by every subcommand) and validates
// it, if set. An unset flag resolves to input.Format("") - input.Read
// and input.SheetNames both treat that as "detect from the file
// extension instead."
func inputFormat(cmd *cobra.Command) (input.Format, error) {
	s, _ := cmd.Flags().GetString("input-format")
	if s == "" {
		return "", nil
	}
	return input.ParseFormat(s)
}

// inputDelimiter reads cmd's --delimiter flag (inherited from the root
// command's PersistentFlags, same as --input-format) and validates it
// decodes to exactly one rune, if set. An unset flag resolves to
// rune(0) - input.Read and input.SheetNames both treat that as "use
// the resolved format's own default delimiter," and reject a non-zero
// one for a format that has no delimiter concept at all.
//
// Whether the flag was set is read from Changed, not from comparing
// its value to "" - --delimiter "" is a flag the user did set, just to
// an empty string, and that's exactly zero characters, not the "I
// didn't pass this" case; it must fail the one-character check below
// like any other wrong-length value, not silently fall back to the
// format's default.
func inputDelimiter(cmd *cobra.Command) (rune, error) {
	if !cmd.Flags().Changed("delimiter") {
		return 0, nil
	}
	s, _ := cmd.Flags().GetString("delimiter")
	runes := []rune(s)
	if len(runes) != 1 {
		return 0, fmt.Errorf("--delimiter must be exactly one character, got %q", s)
	}
	return runes[0], nil
}

// readTimeout reads cmd's --timeout flag (inherited from the root
// command's PersistentFlags, same as --input-format/--delimiter). A
// negative value (not just 0) also disables it, same as withRecovery's
// own convention - Cobra's Duration type parses "-1s" fine, and
// rejecting it here would just be one more way to say "no timeout"
// that this already handles correctly by accident otherwise.
func readTimeout(cmd *cobra.Command) time.Duration {
	d, _ := cmd.Flags().GetDuration("timeout")
	return d
}

// maxFileSize reads cmd's --max-size flag (inherited from the root
// command's PersistentFlags, same as --timeout). A negative value also
// disables it, same convention as readTimeout.
func maxFileSize(cmd *cobra.Command) int64 {
	n, _ := cmd.Flags().GetInt64("max-size")
	return n
}
