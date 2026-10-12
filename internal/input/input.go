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

// Read reads path into a model.Workbook using format, or auto-
// detecting one from path's extension if format is empty.
func Read(path string, format Format) (model.Workbook, error) {
	format, err := resolve(path, format)
	if err != nil {
		return model.Workbook{}, err
	}
	switch format {
	case XLSX:
		return xlsx.Read(path)
	case CSV:
		return csv.Read(path)
	case TSV:
		return csv.ReadTSV(path)
	case ODS:
		return ods.Read(path)
	default:
		return model.Workbook{}, fmt.Errorf("input: %q is not a supported input format (xlsx, csv, tsv, ods)", format)
	}
}

// SheetNames returns path's sheet names using format, or auto-
// detecting one from path's extension if format is empty. xlsx has
// its own lightweight fast path (xlsx.SheetNames, which skips parsing
// any cell data); csv/tsv always have exactly one sheet, SheetName -
// known without parsing the file, but its existence is still checked,
// so a missing csv/tsv file errors here the same way a missing xlsx
// or ods one does; ods has no fast path of its own yet, so this reads
// the whole file just to list its sheets.
func SheetNames(path string, format Format) ([]string, error) {
	format, err := resolve(path, format)
	if err != nil {
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

// checkReadableFile confirms path exists, is actually openable, and is
// a regular file - not a directory, which os.Stat alone wouldn't catch
// (a directory exists and stats successfully, but csv.Read would fail
// trying to read from one), and not a permission-denied file (stat
// typically only needs search permission on the parent directory, not
// read permission on the file itself; opening it does). Used by
// SheetNames' csv/tsv fast path, which otherwise never opens the file
// at all, so it would fail consistently with the xlsx/ods branches
// (which do open/parse) and with actually reading the same file.
func checkReadableFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("input: %w", err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("input: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("input: %q is not a regular file", path)
	}
	return nil
}
