package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/brunoarueira/xlq/internal/eval"
	"github.com/brunoarueira/xlq/internal/filter"
	"github.com/brunoarueira/xlq/internal/output"
	"github.com/brunoarueira/xlq/internal/xlsx"
)

// runFilter parses filterSrc, evaluates it against the workbook at path
// (per docs/adr/0008-filter-grammar-v1.md), and prints the result as
// indented JSON. If compact is set, rows and columns with no values are
// dropped from the result first (output.Compact); the dense shapes
// ADR-0008 defines are otherwise unaffected.
func runFilter(cmd *cobra.Command, filterSrc, path string, compact bool) error {
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

	if compact {
		result = output.Compact(result)
	}

	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("encode result as JSON: %w", err)
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
	return err
}
