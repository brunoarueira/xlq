package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/brunoarueira/xlq/internal/eval"
	"github.com/brunoarueira/xlq/internal/filter"
	"github.com/brunoarueira/xlq/internal/input"
	"github.com/brunoarueira/xlq/internal/model"
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
// fileCount additionally rejects table/csv/tsv for more than one file
// (docs/adr/0017-multiple-input-files.md): both already have a case
// for a plain []any ("a single row or column," one line per element),
// and a multi-file results array is structurally identical to that -
// without this check, --table would silently flatten each file's
// entire result into one mangled cell instead of erroring, the same
// way ADR-0013 already rejects a whole workbook's CSV output rather
// than guess at a flattening. This checks o's fields directly, which
// already hold each flag's effective, config-merged value by the time
// RunE reads them - not just what was explicitly typed - so a config
// table/csv/tsv default (ADR-0015) hits this the same way an explicit
// flag would; --table=false (etc.) is the escape hatch for one
// invocation, via the same Changed-based skip mergeConfigDefaults
// already uses.
func (o outputOptions) validate(fileCount int) error {
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
	if modes == 1 && fileCount > 1 {
		return fmt.Errorf("--table/--csv/--tsv don't support multiple files - no single tabular shape for %d independent results; use JSON, or run xlq once per file", fileCount)
	}
	return nil
}

// runFilter parses filterSrc and evaluates it against the workbook at
// each of paths independently (per docs/adr/0008-filter-grammar-v1.md
// for evaluation, docs/adr/0017-multiple-input-files.md for more than
// one path), printing the result as JSON by default, or in whichever
// mode opts selects instead (a table, CSV, or TSV; or for a string
// result under opts.rawOutput, bare text). format and delim pick and
// configure every path's reader (internal/input) uniformly; an empty
// format detects one from each path's own extension, and delim(0)
// keeps each format's own default field delimiter.
//
// For exactly one path, the result is exactly what it always was -
// unwrapped, with unwrapped errors - matching ADR-0017's promise that
// a single-file invocation is completely unchanged. For more than one,
// the top-level result is a JSON array of each path's own result, in
// argument order, and a read/eval error names which path (by position
// and value) failed.
func runFilter(cmd *cobra.Command, filterSrc string, paths []string, format input.Format, delim rune, timeout time.Duration, maxSize int64, opts outputOptions) error {
	if err := opts.validate(len(paths)); err != nil {
		return err
	}

	expr, err := filter.Parse(filterSrc)
	if err != nil {
		return err
	}

	results := make([]any, len(paths))
	for i, path := range paths {
		// Each wrapped separately, not as one combined step: xlsx
		// reading is lazy (docs/adr/0018-streaming-xlsx-reader.md) -
		// input.Read still opens and parses the workbook eagerly
		// (excelize.OpenFile, GetWorkbookProps), but defers actually
		// decoding any sheet's row data until something ranges over
		// that Sheet's Rows, which happens inside eval.Eval, not inside
		// Read. So most of excelize's own panic/hang-prone work (row
		// scanning, CalcCellValue, ...) happens during the second
		// wrapped call below, not the first. csv/ods read everything
		// eagerly, so this is belt-and-suspenders for them, but it's
		// the only correct place to catch it for xlsx (#50,
		// docs/adr/0030-recover-and-timeout-the-read-path.md).
		//
		// Both calls share one deadline computed once per file, not
		// timeout each restarted from its own start - otherwise a
		// single file could run for close to 2x --timeout (reading
		// right up to the limit, then evaluating for another full
		// timeout's worth) despite --timeout's own documented meaning
		// as one wall-clock budget for the whole file.
		deadline := time.Now().Add(timeout)
		wb, err := withRecovery("reading", timeout, func() (model.Workbook, error) {
			return input.Read(path, format, delim, maxSize)
		})
		if err != nil {
			if len(paths) > 1 {
				return fmt.Errorf("file %d (%s): %w", i+1, path, err)
			}
			return err
		}

		r, err := withRecovery("evaluating", timeoutRemaining(timeout, deadline), func() (any, error) {
			return eval.Eval(expr, wb)
		})
		if err != nil {
			if len(paths) > 1 {
				return fmt.Errorf("file %d (%s): %w", i+1, path, err)
			}
			return err
		}

		if opts.compactData {
			r = output.Compact(r)
		}
		results[i] = r
	}

	result := results[0]
	if len(paths) > 1 {
		result = results
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
