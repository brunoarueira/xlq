// Package input dispatches a file path to the right reader - xlsx,
// csv, tsv, or ods - based on its extension by default, or an
// explicit Format override, so the rest of xlq never needs to know
// which format it's reading.
package input

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/brunoarueira/xlq/internal/csv"
	"github.com/brunoarueira/xlq/internal/model"
	"github.com/brunoarueira/xlq/internal/ods"
	"github.com/brunoarueira/xlq/internal/xlsx"
)

// Format identifies one of xlq's supported input formats.
type Format string

const (
	XLSX Format = "xlsx"
	CSV  Format = "csv"
	TSV  Format = "tsv"
	ODS  Format = "ods"
)

var formatsByName = map[string]Format{
	"xlsx": XLSX,
	"csv":  CSV,
	"tsv":  TSV,
	"ods":  ODS,
}

// ParseFormat validates and normalizes an explicit format name (as a
// user would type it, e.g. to an --input-format flag), case-
// insensitive.
func ParseFormat(s string) (Format, error) {
	if f, ok := formatsByName[strings.ToLower(s)]; ok {
		return f, nil
	}
	return "", fmt.Errorf("input: %q is not a supported input format (xlsx, csv, tsv, ods)", s)
}

// detect picks a Format from path's extension, case-insensitive.
func detect(path string) (Format, error) {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if f, ok := formatsByName[ext]; ok {
		return f, nil
	}
	return "", fmt.Errorf("input: cannot determine the input format from %q's extension; use --input-format to specify one explicitly (xlsx, csv, tsv, ods)", path)
}

// resolve returns format unchanged if it's already set, otherwise
// detects one from path's extension.
func resolve(path string, format Format) (Format, error) {
	if format != "" {
		return format, nil
	}
	return detect(path)
}

// ResolveFormat is resolve, exported for a caller that needs to know
// the resolved format before deciding whether an operation even
// supports it - --in-place (ADR-0014), which only ever supports xlsx,
// regardless of whether that came from an explicit --input-format or
// was detected from path's extension.
func ResolveFormat(path string, format Format) (Format, error) {
	return resolve(path, format)
}

// Read reads path into a model.Workbook using format, or auto-
// detecting one from path's extension if format is empty. delim
// overrides the field delimiter for csv/tsv input (0 keeps each
// format's own default - a comma for csv, a tab for tsv); a non-zero
// delim for any other format is an error, since xlsx/ods have no
// delimiter concept to override at all.
func Read(path string, format Format, delim rune) (model.Workbook, error) {
	format, err := resolve(path, format)
	if err != nil {
		return model.Workbook{}, err
	}
	if err := validateDelimiter(format, delim); err != nil {
		return model.Workbook{}, err
	}
	switch format {
	case XLSX:
		return xlsx.Read(path)
	case CSV:
		return csv.ReadWithDelimiter(path, delimOrDefault(delim, ','))
	case TSV:
		return csv.ReadWithDelimiter(path, delimOrDefault(delim, '\t'))
	case ODS:
		return ods.Read(path)
	default:
		return model.Workbook{}, fmt.Errorf("input: %q is not a supported input format (xlsx, csv, tsv, ods)", format)
	}
}

// SheetNames returns path's sheet names using format, or auto-
// detecting one from path's extension if format is empty. delim is
// validated the same way Read's is, even though csv/tsv's sheet name
// doesn't actually depend on it - passing --delimiter alongside xlsx
// or ods should still be a clear error here, not silently accepted
// just because this particular operation happens not to need it.
//
// xlsx has its own lightweight fast path (xlsx.SheetNames, which skips
// parsing any cell data); csv/tsv always have exactly one sheet,
// SheetName - known without parsing the file, but its existence is
// still checked, so a missing csv/tsv file errors here the same way a
// missing xlsx or ods one does; ods has no fast path of its own yet,
// so this reads the whole file just to list its sheets.
func SheetNames(path string, format Format, delim rune) ([]string, error) {
	format, err := resolve(path, format)
	if err != nil {
		return nil, err
	}
	if err := validateDelimiter(format, delim); err != nil {
		return nil, err
	}
	switch format {
	case XLSX:
		return xlsx.SheetNames(path)
	case CSV, TSV:
		if err := checkReadableFile(path); err != nil {
			return nil, err
		}
		return []string{csv.SheetName}, nil
	case ODS:
		wb, err := ods.Read(path)
		if err != nil {
			return nil, err
		}
		names := make([]string, len(wb.Sheets))
		for i, s := range wb.Sheets {
			names[i] = s.Name
		}
		return names, nil
	default:
		return nil, fmt.Errorf("input: %q is not a supported input format (xlsx, csv, tsv, ods)", format)
	}
}

// validateDelimiter rejects a non-zero delim for any format that has
// no delimiter concept at all (xlsx, ods) - a clear, deliberate error
// rather than silently ignoring an override the user explicitly asked
// for - and rejects a delim encoding/csv itself would refuse (a double
// quote, carriage return, newline, or invalid rune), with a clear
// xlq-level message instead of leaving that to surface later as
// encoding/csv's own unlabeled "invalid field or comment delimiter".
//
// That second check matters for SheetNames as much as Read even
// though its csv/tsv fast path never actually parses the file (so an
// invalid delim would otherwise go uncaught there, while the exact
// same delim would correctly fail once a filter actually reads the
// file) - both call this same function, so both reject it the same
// way, consistently, rather than one silently accepting what the
// other would error on.
func validateDelimiter(format Format, delim rune) error {
	if delim == 0 {
		return nil
	}
	if format != CSV && format != TSV {
		return fmt.Errorf("input: --delimiter only applies to csv/tsv input, not %s", format)
	}
	if delim == '"' || delim == '\r' || delim == '\n' || delim == utf8.RuneError {
		return fmt.Errorf("input: %q is not a valid --delimiter character", delim)
	}
	return nil
}

// delimOrDefault returns delim unless it's unset (0), in which case it
// returns def.
func delimOrDefault(delim, def rune) rune {
	if delim == 0 {
		return def
	}
	return delim
}

// checkReadableFile confirms path exists, is a regular file, and is
// actually openable. Used by SheetNames' csv/tsv fast path, which
// otherwise never opens the file at all, so it would fail consistently
// with the xlsx/ods branches (which do open/parse) and with actually
// reading the same file - a directory, for instance, exists and stats
// successfully, but csv.Read would fail trying to read from one.
//
// The regular-file check must come from os.Stat, before anything ever
// opens path - os.Open on a FIFO blocks until a writer connects on the
// other end, so checking regularity only *after* opening (via the
// returned file's own Stat) would hang on a FIFO instead of rejecting
// it. os.Stat itself never blocks like this; it only reads directory
// metadata.
func checkReadableFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("input: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("input: %q is not a regular file", path)
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("input: %w", err)
	}
	return f.Close()
}
