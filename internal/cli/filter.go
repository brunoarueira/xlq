package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/brunoarueira/xlq/internal/eval"
	"github.com/brunoarueira/xlq/internal/filter"
	"github.com/brunoarueira/xlq/internal/input"
	"github.com/brunoarueira/xlq/internal/output"
)

// outputOptions controls how runFilter prints a result, independent of
// filter evaluation itself.
type outputOptions struct {
	// compactData drops rows and columns with no values from the result
	// (output.Compact) before printing. Unrelated to compactJSON below,
	// despite the similar name - this reshapes the data, not the JSON
	// formatting.
	compactData bool
	// compactJSON prints JSON on one line, jq's -c/--compact-output.
	compactJSON bool
	// rawOutput prints a string result bare, without JSON quoting,
	// jq's -r/--raw-output. Any other result type is unaffected.
	rawOutput bool
}

// runFilter parses filterSrc, evaluates it against the workbook at path
// (per docs/adr/0008-filter-grammar-v1.md), and prints the result as
// JSON (or, for a string result under opts.rawOutput, bare text).
// format and delim pick and configure path's reader (internal/input);
// an empty format detects one from path's extension, and delim(0)
// keeps that format's own default field delimiter.
func runFilter(cmd *cobra.Command, filterSrc, path string, format input.Format, delim rune, opts outputOptions) error {
	expr, err := filter.Parse(filterSrc)
	if err != nil {
		return err
	}

	wb, err := input.Read(path, format, delim)
	if err != nil {
		return err
	}

	result, err := eval.Eval(expr, wb)
	if err != nil {
		return err
	}

	if opts.compactData {
		result = output.Compact(result)
	}

	if opts.rawOutput {
		if s, ok := result.(string); ok {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), s)
			return err
		}
	}

	var encoded []byte
	if opts.compactJSON {
		encoded, err = json.Marshal(result)
	} else {
		encoded, err = json.MarshalIndent(result, "", "  ")
	}
	if err != nil {
		return fmt.Errorf("encode result as JSON: %w", err)
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
	return err
}
