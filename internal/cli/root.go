// Package cli wires xlq's command-line surface.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/brunoarueira/xlq/internal/input"
	"github.com/brunoarueira/xlq/internal/version"
)

// NewRootCommand builds the xlq root command and its subcommands.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "xlq <filter> <file>",
		Short: "xlq is a command-line spreadsheet processor",
		Long: "xlq is a jq/yq-style command-line processor for spreadsheet files.\n\n" +
			"<filter> is evaluated against <file> - see\n" +
			"docs/adr/0008-filter-grammar-v1.md (and the grammar correction in\n" +
			"docs/adr/0009-correct-filter-grammar-ebnf.md) for the v1 grammar -\n" +
			"and the result is printed as JSON.\n\n" +
			"<file>'s format (xlsx, csv, tsv, ods) is picked from its extension;\n" +
			"use --input-format to override that for a misnamed file, and\n" +
			"--delimiter to override a csv/tsv file's field separator.",
		Example: "  xlq '.Sheet1.A1' report.xlsx\n" +
			"  xlq '.Sheet1' report.xlsx\n" +
			"  xlq --compact '.Sheet1' report.xlsx\n" +
			"  xlq -c '.Sheet1' report.xlsx\n" +
			"  xlq -r '.Sheet1.A1' report.xlsx\n" +
			"  xlq --input-format csv '.Sheet1' report.txt\n" +
			"  xlq --delimiter ';' '.Sheet1' report.csv",
		Version: version.Version,
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(2)(cmd, args); err != nil {
				return fmt.Errorf("%w\n\nRun 'xlq --help' for usage", err)
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			compactData, _ := cmd.Flags().GetBool("compact")
			compactJSON, _ := cmd.Flags().GetBool("compact-output")
			rawOutput, _ := cmd.Flags().GetBool("raw-output")
			format, err := inputFormat(cmd)
			if err != nil {
				return err
			}
			delim, err := inputDelimiter(cmd)
			if err != nil {
				return err
			}
			return runFilter(cmd, args[0], args[1], format, delim, outputOptions{
				compactData: compactData,
				compactJSON: compactJSON,
				rawOutput:   rawOutput,
			})
		},
	}
	root.Flags().Bool("compact", false, "drop rows and columns with no values from the result")
	root.Flags().BoolP("compact-output", "c", false, "print JSON on one line, without indentation")
	root.Flags().BoolP("raw-output", "r", false, "print a string result without JSON quotes")
	root.PersistentFlags().String("input-format", "", "input format: xlsx, csv, tsv, or ods (default: detected from the file extension)")
	root.PersistentFlags().String("delimiter", "", "field delimiter override for csv/tsv input, exactly one character (default: comma for csv, tab for tsv)")
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
