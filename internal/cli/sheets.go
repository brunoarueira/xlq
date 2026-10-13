package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/brunoarueira/xlq/internal/input"
)

func newSheetsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "sheets <file>",
		Short: "List the sheet names in a spreadsheet file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := inputFormat(cmd)
			if err != nil {
				return err
			}
			delim, err := inputDelimiter(cmd)
			if err != nil {
				return err
			}
			names, err := input.SheetNames(args[0], format, delim)
			if err != nil {
				return err
			}
			for _, name := range names {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), name); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
