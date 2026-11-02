// Package xlsx converts .xlsx files into xlq's internal model.Workbook
// (see internal/model and docs/adr/0007-spreadsheet-data-model.md).
//
// Row/cell data is read via this package's own direct worksheet XML
// reader (worksheet.go, sheetpath.go, sharedstrings.go -
// docs/adr/0048-custom-xlsx-worksheet-read-path.md), not excelize's
// Rows/Columns: excelize stays in use only for what that new reader
// doesn't do - Read's own upfront OpenFile (the sheet list, workbook
// properties, and size-limit enforcement, ADR-0031/ADR-0047),
// CalcCellValue (an uncached formula's live result), GetCellFormula
// (a shared formula's own relative-reference reconstruction, see
// readCell), and GetStyle (resolving a style ID to its definition,
// for isDateStyled) - so the rest of xlq still never depends on
// excelize directly.
//
// Read itself returns as soon as it knows the sheet list: no sheet's
// row data is read until something actually ranges over that Sheet's
// Rows, at which point it opens its own fresh copy of the file for
// that one pass (docs/adr/0018-streaming-xlsx-reader.md).
//
// Known limitation: a formula cell with no cached result yet (the
// workbook has never been opened/recalculated by a spreadsheet
// application, nor by excelize itself) is computed live via
// CalcCellValue, but this package's own cell-type classification only
// knows "this is a formula" for such a cell, not its result's actual
// type. Its Kind is then inferred from the computed text alone, so a
// result that is itself numeric-looking text (e.g. `="123"`) or a
// boolean reads back as a Number/String rather than the type it
// actually is. A workbook that has been saved by Excel (or any tool
// that caches formula results) is unaffected, since the cached
// result's real type is used.
package xlsx

import (
	"archive/zip"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/brunoarueira/xlq/internal/model"
)

// unzipXMLSizeLimit caps how large a single XML part (a worksheet's
// own, or the shared-strings table) excelize will hold resident in its
// own process memory before spilling it to a temporary file instead -
// a deliberately separate, lower ceiling than maxSize itself (ADR-0047
// corrects an earlier, unmerged draft of that ADR that removed this
// cap entirely: UnzipSizeLimit's own cumulative check, independent of
// this constant, already bounds the *total* ever decompressed across
// every part, but doesn't bound how much of that total a single part
// can claim resident in memory at once - an adversarial file crafted
// to inflate just one part, most easily the shared-strings table,
// could otherwise legitimately claim the *entire* maxSize budget in
// one contiguous allocation, a real loss of defense-in-depth even
// though the cumulative total stays within what the user already
// accepted). 128 MiB, not the original 16 MiB: confirmed directly
// against a representative large (105,012-row, string-heavy) fixture
// that its real shared-strings table needs ~62.6 MiB decompressed -
// comfortably under this with headroom, while still meaningfully
// below the 256 MiB default maxSize a single part could otherwise
// claim outright.
const unzipXMLSizeLimit = 128 << 20

// excelizeOptions returns the excelize.Options that bound how large a
// workbook excelize will let itself decompress to in memory, from
// maxSize (#51, docs/adr/0031-memory-ceiling-for-untrusted-files.md).
// maxSize <= 0 returns the zero Options, which leaves excelize's own
// generous built-in defaults (16 GiB/16 MiB) in place rather than
// imposing no limit at all literally - there's no "unset" sentinel
// excelize itself recognizes other than its own defaults.
func excelizeOptions(maxSize int64) excelize.Options {
	if maxSize <= 0 {
		return excelize.Options{}
	}
	xmlLimit := int64(unzipXMLSizeLimit)
	if xmlLimit > maxSize {
		xmlLimit = maxSize
	}
	return excelize.Options{UnzipSizeLimit: maxSize, UnzipXMLSizeLimit: xmlLimit}
}

// Read opens the .xlsx file at path and converts it into a
// model.Workbook. maxSize bounds how large excelize will let the
// workbook decompress to in memory; <= 0 disables that bound (#51).
func Read(path string, maxSize int64) (model.Workbook, error) {
	f, err := excelize.OpenFile(path, excelizeOptions(maxSize))
	if err != nil {
		return model.Workbook{}, fmt.Errorf("open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	props, err := f.GetWorkbookProps()
	if err != nil {
		return model.Workbook{}, fmt.Errorf("read workbook properties of %q: %w", path, err)
	}
	date1904 := props.Date1904 != nil && *props.Date1904

	// Cell styles are shared workbook-wide, so cache the (usually small)
	// set of "is this style a date format" answers by style ID rather
	// than re-deriving it for every cell. Shared across every sheet's
	// own lazy pass below (captured by reference, keyed by style ID
	// rather than by any one open *excelize.File) since there's no
	// concurrent sheet access anywhere in this project to guard against.
	dateStyles := make(map[int]bool)

	var wb model.Workbook
	for _, name := range f.GetSheetList() {
		wb.Sheets = append(wb.Sheets, lazySheet(path, name, date1904, dateStyles, maxSize))
	}
	return wb, nil
}

// SheetNames returns the worksheet, chart sheet, and dialog sheet names of
// the .xlsx file at path, in workbook order. Unlike Read, it doesn't parse
// any cell data, so it stays cheap and doesn't fail on a sheet whose cells
// (e.g. an uncomputable formula) Read would choke on. maxSize is the same
// bound Read applies (#51) - SheetNames still calls excelize.OpenFile,
// which alone risks the same decompression-based memory blowup even
// though it never reads a row.
func SheetNames(path string, maxSize int64) ([]string, error) {
	f, err := excelize.OpenFile(path, excelizeOptions(maxSize))
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return f.GetSheetList(), nil
}

// lazySheet returns a Sheet whose Rows never reads anything until
// actually ranged over, and opens its own fresh *excelize.File and a
// fresh *zip.ReadCloser - both closed again once that one pass
// finishes - every time it is: there's no way to keep Read's own
// already-open file around for later use, since Read returns (and
// so, today, already closed its file) before any lazy consumer ever
// gets a chance to range over Rows. See
// docs/adr/0018-streaming-xlsx-reader.md for why this, rather than
// giving model.Workbook an explicit open/close lifetime of its own,
// was chosen, and for the trade-off it accepts: Read's own upfront
// open (for the sheet list and workbook properties) plus one more
// pair of opens per sheet actually ranged over. maxSize (#51) is
// applied to both opens the same way Read applies it to its own.
//
// ADR-0048: row/cell data itself is read via streamWorksheetRows -
// xlq's own direct worksheet XML reader - not excelize's Rows/
// Columns, eliminating the per-cell GetCellFormula/GetCellType tax
// #164 found dominates large-file reads. The *excelize.File stays
// open alongside the *zip.Reader only for what this package still
// delegates to it: CalcCellValue (an uncached formula's live result),
// GetCellFormula (a shared formula's own, genuinely complex relative-
// reference reconstruction - see readCell), and GetStyle (resolving a
// style ID to its definition, for isDateStyled).
func lazySheet(path, name string, date1904 bool, dateStyles map[int]bool, maxSize int64) model.Sheet {
	return model.Sheet{
		Name: name,
		Rows: func(yield func(model.Row, error) bool) {
			f, err := excelize.OpenFile(path, excelizeOptions(maxSize))
			if err != nil {
				yield(model.Row{}, fmt.Errorf("open %q: %w", path, err))
				return
			}
			defer func() { _ = f.Close() }()

			zr, err := zip.OpenReader(path)
			if err != nil {
				yield(model.Row{}, fmt.Errorf("open %q: %w", path, err))
				return
			}
			defer func() { _ = zr.Close() }()

			sheetPath, err := sheetXMLPath(&zr.Reader, name, maxSize)
			if err != nil {
				yield(model.Row{}, fmt.Errorf("resolve sheet %q of %q: %w", name, path, err))
				return
			}
			strs, err := sharedStrings(&zr.Reader, maxSize)
			if err != nil {
				yield(model.Row{}, fmt.Errorf("read shared strings of %q: %w", path, err))
				return
			}
			sheetFile := findZipEntry(&zr.Reader, sheetPath)
			if sheetFile == nil {
				yield(model.Row{}, fmt.Errorf("sheet %q of %q: worksheet part %q not found in archive", name, path, sheetPath))
				return
			}

			streamWorksheetRows(sheetFile, maxSize, func(rowIndex int, rawCells []resolvedCell, rowErr error) bool {
				if rowErr != nil {
					return yield(model.Row{}, fmt.Errorf("read sheet %q of %q: %w", name, path, rowErr))
				}
				var cells []model.Cell
				for _, rc := range rawCells {
					cell, ok, err := readCell(f, name, rowIndex, rc, strs, date1904, dateStyles)
					if err != nil {
						return yield(model.Row{}, fmt.Errorf("%q: %w", path, err))
					}
					if ok {
						cells = append(cells, cell)
					}
				}
				if len(cells) == 0 {
					return true
				}
				return yield(model.Row{Index: rowIndex, Cells: cells}, nil)
			})
		},
	}
}

// resolveCellValue returns rc's resolved raw text value: strs[v] for
// a shared string (t="s" - excelize's own getValueFrom, confirmed
// directly in its source, is a pass-through for every other type
// once raw mode is in effect, which this reader always is by
// construction), rc.is's own resolved text for an inline string
// (t="inlineStr" - xlsxIS and xlsxSI are the identical type in
// excelize's own source, so reusing sharedStringItem.text() here is
// exact, not approximate), or rc.v unchanged for everything else.
func resolveCellValue(rc resolvedCell, strs []string) (string, error) {
	switch rc.t {
	case "s":
		if !rc.hasV || rc.v == "" {
			return "", nil
		}
		idx, err := strconv.Atoi(strings.TrimSpace(rc.v))
		if err != nil {
			return "", fmt.Errorf("shared string index %q: %w", rc.v, err)
		}
		if idx < 0 || idx >= len(strs) {
			return "", fmt.Errorf("shared string index %d out of range (0-%d)", idx, len(strs)-1)
		}
		return strs[idx], nil
	case "inlineStr":
		if rc.is != nil {
			return rc.is.text(), nil
		}
		return rc.v, nil
	default:
		return rc.v, nil
	}
}

// readCell converts rc (rowIndex is 0-based) into a model.Cell. It
// reports ok=false for a cell that is genuinely absent: no value and
// no formula.
//
// cellRef (the "A1"-style reference excelize's own API needs) is
// computed only when rc actually has a formula - the one case that
// still needs excelize (GetCellFormula for a shared formula,
// CalcCellValue for a live one) - rather than for every cell
// unconditionally: confirmed directly via profiling that computing
// it for every cell, including the overwhelming majority with no
// formula at all, was a meaningful share of this reader's own
// allocations at this call volume.
func readCell(f *excelize.File, sheet string, rowIndex int, rc resolvedCell, strs []string, date1904 bool, dateStyles map[int]bool) (model.Cell, bool, error) {
	raw, err := resolveCellValue(rc, strs)
	if err != nil {
		cellRef, _ := excelize.CoordinatesToCellName(rc.col+1, rowIndex+1)
		return model.Cell{}, false, fmt.Errorf("resolve value of %s!%s: %w", sheet, cellRef, err)
	}

	var formula, cellRef string
	if rc.hasF {
		if cellRef, err = excelize.CoordinatesToCellName(rc.col+1, rowIndex+1); err != nil {
			return model.Cell{}, false, err
		}
		if rc.formulaType == "shared" {
			// A shared formula's own relative-reference reconstruction
			// for anything but the group's master cell is genuinely
			// complex (excelize's own getSharedFormula/
			// convertSharedFormula/shiftCell, confirmed directly in its
			// source) - and confirmed directly that excelize resolves
			// *every* cell in a shared-formula group this way,
			// including the master itself, not only member cells with
			// empty <f> content. Deferring to excelize here, rather
			// than risk a silently wrong reconstruction, is the same
			// boundary ADR-0048 already draws around CalcCellValue.
			if formula, err = f.GetCellFormula(sheet, cellRef); err != nil {
				return model.Cell{}, false, fmt.Errorf("read shared formula of %s!%s: %w", sheet, cellRef, err)
			}
		} else {
			formula = rc.formulaText
		}
		// The stored formula never includes the leading "=" (that's
		// purely a spreadsheet-UI convention); ADR-0007's Formula field
		// does, matching how a formula bar displays it, so add it back.
		if formula != "" && !strings.HasPrefix(formula, "=") {
			formula = "=" + formula
		}
	}

	if raw == "" && formula == "" {
		return model.Cell{}, false, nil
	}

	var cell model.Cell
	switch {
	case raw != "":
		// A real value is already present - either a plain cell, or an
		// already-calculated formula's cached result. In both cases the
		// cell's own raw type attribute reliably reflects the value's
		// actual type, so use it.
		if cell, err = cellFromRawType(f, rc.t, raw, rc.styleID, date1904, dateStyles); err != nil {
			return model.Cell{}, false, err
		}
	case formula != "":
		// A formula with no cached result yet: compute it live. See the
		// package doc's note on the resulting type-inference limitation.
		// RawCellValue bypasses number formatting on the computed result
		// too (e.g. a date-styled formula's result comes back as its
		// numeric serial, "46290", not "09-25-26"), matching how a plain
		// cell's raw value is used elsewhere in this file.
		computed, calcErr := f.CalcCellValue(sheet, cellRef, excelize.Options{RawCellValue: true})
		switch {
		case calcErr != nil:
			// A formula that evaluates to a spreadsheet error (#DIV/0!,
			// #N/A, ...) - or one CalcCellValue simply can't compute, e.g.
			// an unsupported function - surfaces here as a Go error
			// rather than as display text. Treat it as a String cell
			// holding that message, matching how a cell whose *cached*
			// result is an error type is handled in cellFromRawType, rather
			// than failing the whole read over one bad cell.
			cell = model.NewStringCell(calcErr.Error())
		case computed != "":
			if cell, err = cellFromComputedText(f, rc.styleID, computed, date1904, dateStyles); err != nil {
				return model.Cell{}, false, err
			}
		}
		// else: the formula evaluates to blank; cell stays the zero
		// value, which is Empty.
	}
	if formula != "" {
		cell = cell.WithFormula(formula)
	}
	return cell.At(rc.col), true, nil
}

// cellFromRawType converts a cell whose own raw t attribute reliably
// describes its current value: either a plain (non-formula) cell, or
// a formula whose cached result is already present. t is classified
// via the same fixed table excelize's own cellTypes uses (confirmed
// directly in its source): "b"/"d"/"n"/"e"/"s"/"str"/"inlineStr", any
// other value (including "", absent) defaulting to a plain number -
// excelize's own map lookup falls back to its zero value, CellTypeUnset,
// for exactly the same reason.
func cellFromRawType(f *excelize.File, t, raw string, styleID int, date1904 bool, dateStyles map[int]bool) (model.Cell, error) {
	switch t {
	case "b":
		return model.NewBoolCell(raw == "1" || strings.EqualFold(raw, "TRUE")), nil
	case "s", "str", "e", "inlineStr":
		// "s" (shared string) and "inlineStr" are already resolved to
		// their text by resolveCellValue; "str" specifically means "a
		// formula's cached result is text" (not merely "has a
		// formula"); "e" is a cached spreadsheet error, e.g. "#DIV/0!".
		// All four are a plain String cell.
		return model.NewStringCell(raw), nil
	case "d":
		// The rare ISO-8601-native date type: unlike a plain date-styled
		// number, its raw value is date text (e.g. "2026-09-25T00:00:00"),
		// not a numeric serial.
		if parsed, ok := parseISODate(raw); ok {
			return model.NewDateCell(parsed), nil
		}
		// Not a recognized ISO layout variant; fall back to the raw text
		// rather than lose the cell.
		return model.NewStringCell(raw), nil
	default:
		// "n", "", or any unrecognized value: a plain stored number, or
		// a cached formula result that's a number. A date is
		// distinguished from a plain number by the cell's style, not by
		// its type attribute.
		return numericOrDateCell(f, styleID, raw, date1904, dateStyles)
	}
}

// isoDateLayouts are the layouts excelize's own writer uses for the rare
// ISO-8601-native date cell type (OOXML t="d"), tried in order.
var isoDateLayouts = []string{
	"2006-01-02T15:04:05.999",
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02",
}

func parseISODate(text string) (time.Time, bool) {
	for _, layout := range isoDateLayouts {
		if t, err := time.Parse(layout, text); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// numericOrDateCell converts a cell already known to be backed by a raw
// numeric value (a plain number, or a cached formula result): a formatted
// number (a currency style, a thousands separator, a percentage, ...)
// still parses since raw is never run through excelize's own display
// formatting (ADR-0046).
func numericOrDateCell(f *excelize.File, styleID int, raw string, date1904 bool, dateStyles map[int]bool) (model.Cell, error) {
	isDate, err := isDateStyled(f, styleID, dateStyles)
	if err != nil {
		return model.Cell{}, err
	}
	if isDate {
		if serial, err := strconv.ParseFloat(raw, 64); err == nil {
			if t, err := excelize.ExcelDateToTime(serial, date1904); err == nil {
				return model.NewDateCell(t), nil
			}
		}
		// Styled as a date but not actually a parseable serial number;
		// fall back to the raw text rather than lose the cell.
		return model.NewStringCell(raw), nil
	}
	if num, err := strconv.ParseFloat(raw, 64); err == nil {
		return model.NewNumberCell(num), nil
	}
	return model.NewStringCell(raw), nil
}

// cellFromComputedText infers a Kind purely from a formula's freshly
// computed text (already fetched with RawCellValue, so a numeric or date
// result is its unformatted serial, not display text), for the case where
// the formula has no cached result and so no reliable type information is
// available (see the package doc). It's the same numeric/date logic as
// numericOrDateCell, plus recognizing a literal "TRUE"/"FALSE" result as a
// Bool - CellTypeBool can't be trusted here the way it can for a cell with
// a real cached value.
func cellFromComputedText(f *excelize.File, styleID int, text string, date1904 bool, dateStyles map[int]bool) (model.Cell, error) {
	cell, err := numericOrDateCell(f, styleID, text, date1904, dateStyles)
	if err != nil {
		return model.Cell{}, err
	}
	if cell.Kind == model.String && (text == "TRUE" || text == "FALSE") {
		return model.NewBoolCell(text == "TRUE"), nil
	}
	return cell, nil
}

// builtinDateNumFmtIDs are the built-in (ECMA-376) number format IDs that
// render a number as a date and/or time, including the locale-dependent
// ones (27-36, 50-58) used by non-US-locale Excel and other writers.
var builtinDateNumFmtIDs = map[int]bool{
	14: true, 15: true, 16: true, 17: true, 18: true, 19: true, 20: true,
	21: true, 22: true, 45: true, 46: true, 47: true,
}

func init() {
	for id := 27; id <= 36; id++ {
		builtinDateNumFmtIDs[id] = true
	}
	for id := 50; id <= 58; id++ {
		builtinDateNumFmtIDs[id] = true
	}
}

// dateTimeToken matches a date/time format-code letter (y, m, d, h, s),
// used to classify a custom number format as date-like once quoted
// literals, bracketed color/locale segments, and backslash-escaped
// literal characters have been stripped out.
var dateTimeToken = regexp.MustCompile(`(?i)[ymdhs]`)

var (
	quotedLiteral    = regexp.MustCompile(`"[^"]*"`)
	bracketedLiteral = regexp.MustCompile(`\[[^\]]*\]`)
	escapedLiteral   = regexp.MustCompile(`\\.`)
)

// isDateStyled reports whether styleID has a date/time number format,
// per its built-in format ID or custom format code. styleID comes
// from the cell's own s attribute, captured directly by
// streamWorksheetRows - unlike the excelize-Rows-based reader this
// replaces, this no longer calls GetCellStyle: confirmed directly in
// excelize's own source that it also calls workSheetReader, the same
// per-worksheet materialization cost GetCellFormula/GetCellType have
// (ADR-0048) - keeping it would have undermined the whole point of
// this change, since it would have remained the one caller still
// forcing that materialization to happen at all. GetStyle(styleID)
// itself stays on excelize: confirmed independent, resolving via
// stylesReader, workbook-wide, not workSheetReader.
func isDateStyled(f *excelize.File, styleID int, cache map[int]bool) (bool, error) {
	if isDate, ok := cache[styleID]; ok {
		return isDate, nil
	}

	style, err := f.GetStyle(styleID)
	if err != nil {
		return false, fmt.Errorf("read style %d: %w", styleID, err)
	}

	var isDate bool
	if style.CustomNumFmt != nil {
		code := quotedLiteral.ReplaceAllString(*style.CustomNumFmt, "")
		code = bracketedLiteral.ReplaceAllString(code, "")
		code = escapedLiteral.ReplaceAllString(code, "")
		isDate = dateTimeToken.MatchString(code)
	} else {
		isDate = builtinDateNumFmtIDs[style.NumFmt]
	}
	cache[styleID] = isDate
	return isDate, nil
}
