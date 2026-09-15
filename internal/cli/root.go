// Package cli wires xlq's command-line surface.
package cli

import (
	"github.com/spf13/cobra"

	"github.com/brunoarueira/xlq/internal/version"
)

// NewRootCommand builds the xlq root command and its subcommands.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "xlq",
		Short: "xlq is a command-line spreadsheet processor",
		Long: "xlq is a jq/yq-style command-line processor for spreadsheet files.\n\n" +
			"The filter/query language isn't implemented yet - see\n" +
			"docs/adr/0004-cli-shape-and-initial-dependencies.md. Today, xlq only\n" +
			"exposes the \"sheets\" subcommand.",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate("xlq {{.Version}}\n")
	root.AddCommand(newSheetsCommand())
	return root
}

// Execute runs the root command against os.Args.
func Execute() error {
	return NewRootCommand().Execute()
}
