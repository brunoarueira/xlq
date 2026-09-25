package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/brunoarueira/xlq/internal/xlsx"
)

func newSheetsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "sheets <file>",
		Short: "List the sheet names in a spreadsheet file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			names, err := sheetNames(args[0])
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

func sheetNames(path string) ([]string, error) {
	wb, err := xlsx.Read(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(wb.Sheets))
	for i, sheet := range wb.Sheets {
		names[i] = sheet.Name
	}
	return names, nil
}
