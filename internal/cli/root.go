// Package cli wires xlq's command-line surface.
package cli

import (
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
			"docs/adr/0008-filter-grammar-v1.md for the v1 grammar - and the\n" +
			"result is printed as JSON.",
		Example: "  xlq '.Sheet1.A1' report.xlsx\n" +
			"  xlq '.Sheet1' report.xlsx",
		Version:       version.Version,
		Args:          cobra.ExactArgs(2),
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
