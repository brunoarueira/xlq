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
			// SheetNames doesn't parse cell data (xlsx.SheetNames's own
			// doc comment), but it still calls excelize.OpenFile, which
			// alone is enough to reach some of the panic-class advisories
			// #50/docs/adr/0030-recover-and-timeout-the-read-path.md
			// track (e.g. a malformed encrypted workbook's CFB/OLE
			// parsing) - wrapped for the same reason runFilter wraps
			// input.Read.
			names, err := withRecovery("listing sheets", readTimeout(cmd), func() ([]string, error) {
				return input.SheetNames(args[0], format, delim, maxFileSize(cmd))
			})
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
