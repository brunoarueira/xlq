// Package cli wires xlq's command-line surface.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

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
			"and the result is printed as JSON.",
		Example: "  xlq '.Sheet1.A1' report.xlsx\n" +
			"  xlq '.Sheet1' report.xlsx",
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
			return runFilter(cmd, args[0], args[1])
		},
	}
	root.SetVersionTemplate("xlq {{.Version}}\n")
	root.AddCommand(newSheetsCommand())
	return root
}

// Execute runs the root command against os.Args.
func Execute() error {
	return NewRootCommand().Execute()
}
