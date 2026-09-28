package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/brunoarueira/xlq/internal/eval"
	"github.com/brunoarueira/xlq/internal/filter"
	"github.com/brunoarueira/xlq/internal/xlsx"
)

// runFilter parses filterSrc, evaluates it against the workbook at path
// (per docs/adr/0008-filter-grammar-v1.md), and prints the result as
// indented JSON.
func runFilter(cmd *cobra.Command, filterSrc, path string) error {
	expr, err := filter.Parse(filterSrc)
	if err != nil {
		return err
	}

	wb, err := xlsx.Read(path)
	if err != nil {
		return err
	}

	result, err := eval.Eval(expr, wb)
	if err != nil {
		return err
	}

	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("encode result as JSON: %w", err)
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
	return err
}
