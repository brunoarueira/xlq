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
	// table, csv, and tsv each select a different, mutually exclusive
	// output mode (output.Table/CSV/TSV) instead of JSON. Also mutually
	// exclusive with compactJSON/rawOutput, which only ever meant
	// something for JSON output - any of these combinations is a CLI
	// error, not a silent override (see validate).
	table bool
	csv   bool
	tsv   bool
}

// validate rejects a combination of output modes that don't compose:
// table/csv/tsv are mutually exclusive with each other and with
// compactJSON/rawOutput, which only ever meant something for JSON.
func (o outputOptions) validate() error {
	modes := 0
	for _, set := range []bool{o.table, o.csv, o.tsv} {
		if set {
			modes++
		}
	}
	if modes > 1 {
		return fmt.Errorf("--table, --csv, and --tsv are mutually exclusive output modes")
	}
	if modes == 1 && (o.compactJSON || o.rawOutput) {
		return fmt.Errorf("--table/--csv/--tsv cannot be combined with --compact-output/--raw-output, which only apply to JSON output")
	}
	return nil
}

// runFilter parses filterSrc, evaluates it against the workbook at path
// (per docs/adr/0008-filter-grammar-v1.md), and prints the result as
// JSON by default, or in whichever mode opts selects instead (a table,
// CSV, or TSV; or for a string result under opts.rawOutput, bare
// text). format and delim pick and configure path's reader
// (internal/input); an empty format detects one from path's extension,
// and delim(0) keeps that format's own default field delimiter.
func runFilter(cmd *cobra.Command, filterSrc, path string, format input.Format, delim rune, opts outputOptions) error {
	if err := opts.validate(); err != nil {
		return err
	}

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

	switch {
	case opts.table:
		_, err := fmt.Fprintln(cmd.OutOrStdout(), output.Table(result))
		return err
	case opts.csv:
		text, err := output.CSV(result)
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(cmd.OutOrStdout(), text)
		return err
	case opts.tsv:
		text, err := output.TSV(result)
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(cmd.OutOrStdout(), text)
		return err
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
