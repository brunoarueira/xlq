// Package xlsx converts .xlsx files into xlq's internal model.Workbook
// (see internal/model and docs/adr/0007-spreadsheet-data-model.md) using
// excelize, so the rest of xlq never depends on excelize directly.
//
// Known limitation: a formula cell with no cached result yet (the
// workbook has never been opened/recalculated by a spreadsheet
// application, nor by excelize itself) is computed live via
// CalcCellValue, but excelize's GetCellType reports only "this is a
// formula" for such a cell, not its result's actual type. Its Kind is
// then inferred from the computed text alone, so a result that is
// itself numeric-looking text (e.g. `="123"`) or a boolean reads back
// as a Number/String rather than the type it actually is. A workbook
// that has been saved by Excel (or any tool that caches formula
// results) is unaffected, since the cached result's real type is used.
//
// Known limitation: a custom number format that renders any value as
// blank (e.g. ";;;", a common "hide this cell's contents" trick) is
// read correctly via the cell's raw value *except* when it's the last
// (or only) populated cell in its row: excelize's row iterator itself
// trims trailing cells by their formatted, not raw, value, so such a
// cell is dropped before this package ever sees it. The same cell
// followed by any other populated cell in its row is unaffected.
package xlsx

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/brunoarueira/xlq/internal/model"
)

// Read opens the .xlsx file at path and converts it into a model.Workbook.
func Read(path string) (model.Workbook, error) {
	f, err := excelize.OpenFile(path)
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
	// than re-deriving it for every cell.
	dateStyles := make(map[int]bool)

	var wb model.Workbook
	for _, name := range f.GetSheetList() {
		sheet, err := readSheet(f, name, date1904, dateStyles)
		if err != nil {
			return model.Workbook{}, fmt.Errorf("read sheet %q of %q: %w", name, path, err)
		}
		wb.Sheets = append(wb.Sheets, sheet)
	}
	return wb, nil
}

// SheetNames returns the worksheet, chart sheet, and dialog sheet names of
// the .xlsx file at path, in workbook order. Unlike Read, it doesn't parse
// any cell data, so it stays cheap and doesn't fail on a sheet whose cells
// (e.g. an uncomputable formula) Read would choke on.
func SheetNames(path string) ([]string, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return f.GetSheetList(), nil
}

func readSheet(f *excelize.File, name string, date1904 bool, dateStyles map[int]bool) (model.Sheet, error) {
	rows, err := f.Rows(name)
	if err != nil {
		return model.Sheet{}, err
	}
	defer func() { _ = rows.Close() }()

	sheet := model.Sheet{Name: name}
	// excelize's row iterator yields one iteration per row *number*
	// (padding with an empty Columns() result for any row the sheet XML
	// omits entirely, e.g. a wholly blank row 2 between rows 1 and 3),
	// so this 0-based counter tracks the real row index correctly even
	// across gaps.
	for rowIndex := 0; rows.Next(); rowIndex++ {
		cols, err := rows.Columns()
		if err != nil {
			return model.Sheet{}, err
		}

		var cells []model.Cell
		for colIndex, display := range cols {
			cell, ok, err := readCell(f, name, colIndex, rowIndex, display, date1904, dateStyles)
			if err != nil {
				return model.Sheet{}, err
			}
			if ok {
				cells = append(cells, cell)
			}
		}
		if len(cells) > 0 {
			sheet.Rows = append(sheet.Rows, model.Row{Index: rowIndex, Cells: cells})
		}
	}
	if err := rows.Error(); err != nil {
		return model.Sheet{}, err
	}
	return sheet, nil
}

// readCell reads the cell at (colIndex, rowIndex) (both 0-based), given
// display, its already-fetched formatted value from the row iterator. It
// reports ok=false for a cell that is genuinely absent: no value and no
// formula.
func readCell(f *excelize.File, sheet string, colIndex, rowIndex int, display string, date1904 bool, dateStyles map[int]bool) (model.Cell, bool, error) {
	cellRef, err := excelize.CoordinatesToCellName(colIndex+1, rowIndex+1)
	if err != nil {
		return model.Cell{}, false, err
	}

	formula, err := f.GetCellFormula(sheet, cellRef)
	if err != nil {
		return model.Cell{}, false, fmt.Errorf("read formula of %s!%s: %w", sheet, cellRef, err)
	}
	// The stored formula never includes the leading "=" (that's purely a
	// spreadsheet-UI convention); ADR-0007's Formula field does, matching
	// how a formula bar displays it, so add it back.
	if formula != "" && !strings.HasPrefix(formula, "=") {
		formula = "=" + formula
	}

	// display is the *formatted* value; fetch the raw one too, since a
	// custom number format such as ";;;" can render a real value as
	// blank display text, and presence must not be decided by display
	// alone.
	raw, err := f.GetCellValue(sheet, cellRef, excelize.Options{RawCellValue: true})
	if err != nil {
		return model.Cell{}, false, fmt.Errorf("read raw value of %s!%s: %w", sheet, cellRef, err)
	}
	if raw == "" && display == "" && formula == "" {
		return model.Cell{}, false, nil
	}

	var cell model.Cell
	switch {
	case raw != "" || display != "":
		// A real value is already present - either a plain cell, or an
		// already-calculated formula's cached result. In both cases
		// excelize's cell type reliably reflects the value's actual
		// type, so use it.
		cellType, err := f.GetCellType(sheet, cellRef)
		if err != nil {
			return model.Cell{}, false, fmt.Errorf("read type of %s!%s: %w", sheet, cellRef, err)
		}
		if cell, err = cellFromTyped(f, sheet, cellRef, cellType, display, raw, date1904, dateStyles); err != nil {
			return model.Cell{}, false, err
		}
	case formula != "":
		// A formula with no cached result yet: compute it live. See the
		// package doc's note on the resulting type-inference limitation.
		computed, calcErr := f.CalcCellValue(sheet, cellRef)
		switch {
		case calcErr != nil:
			// A formula that evaluates to a spreadsheet error (#DIV/0!,
			// #N/A, ...) - or one CalcCellValue simply can't compute, e.g.
			// an unsupported function - surfaces here as a Go error
			// rather than as display text. Treat it as a String cell
			// holding that message, matching how a cell whose *cached*
			// result is an error type is handled in cellFromTyped, rather
			// than failing the whole read over one bad cell.
			cell = model.NewStringCell(calcErr.Error())
		case computed != "":
			if cell, err = cellFromComputedText(f, sheet, cellRef, computed, date1904, dateStyles); err != nil {
				return model.Cell{}, false, err
			}
		}
		// else: the formula evaluates to blank; cell stays the zero
		// value, which is Empty.
	}
	if formula != "" {
		cell = cell.WithFormula(formula)
	}
	return cell.At(colIndex), true, nil
}

// cellFromTyped converts a cell whose excelize CellType reliably describes
// its current value: either a plain (non-formula) cell, or a formula whose
// cached result is already present.
func cellFromTyped(f *excelize.File, sheet, cellRef string, cellType excelize.CellType, display, raw string, date1904 bool, dateStyles map[int]bool) (model.Cell, error) {
	switch cellType {
	case excelize.CellTypeBool:
		return model.NewBoolCell(raw == "1" || strings.EqualFold(raw, "TRUE")), nil
	case excelize.CellTypeSharedString, excelize.CellTypeInlineString, excelize.CellTypeError:
		return model.NewStringCell(display), nil
	case excelize.CellTypeFormula:
		// This specifically means "a formula's cached result is text"
		// (OOXML's t="str"), not merely "this cell has a formula".
		return model.NewStringCell(display), nil
	default:
		// CellTypeUnset (a plain stored number, or a cached formula
		// result that's a number) or CellTypeDate (the rare
		// ISO-8601-native type): both are backed by a number, so a date
		// is distinguished from a plain number by the cell's style, not
		// by cellType.
		return numericOrDateCell(f, sheet, cellRef, raw, display, date1904, dateStyles)
	}
}

// numericOrDateCell converts a cell already known to be backed by a raw
// numeric value (a plain number, or a cached formula result), reading raw
// rather than the formatted display text so that a formatted number (a
// currency style, a thousands separator, a percentage, ...) still parses.
func numericOrDateCell(f *excelize.File, sheet, cellRef, raw, display string, date1904 bool, dateStyles map[int]bool) (model.Cell, error) {
	isDate, err := isDateStyled(f, sheet, cellRef, dateStyles)
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
		// fall back to the displayed text rather than lose the cell.
		return model.NewStringCell(display), nil
	}
	if num, err := strconv.ParseFloat(raw, 64); err == nil {
		return model.NewNumberCell(num), nil
	}
	return model.NewStringCell(display), nil
}

// cellFromComputedText infers a Kind purely from a formula's freshly
// computed text, for the case where the formula has no cached result and
// so no reliable type information is available (see the package doc). A
// date-styled result becomes a Date, a parseable number becomes a Number,
// a literal "TRUE"/"FALSE" becomes a Bool, and anything else stays a
// String.
func cellFromComputedText(f *excelize.File, sheet, cellRef, text string, date1904 bool, dateStyles map[int]bool) (model.Cell, error) {
	isDate, err := isDateStyled(f, sheet, cellRef, dateStyles)
	if err != nil {
		return model.Cell{}, err
	}
	if isDate {
		if raw, err := f.GetCellValue(sheet, cellRef, excelize.Options{RawCellValue: true}); err == nil && raw != "" {
			if serial, err := strconv.ParseFloat(raw, 64); err == nil {
				if t, err := excelize.ExcelDateToTime(serial, date1904); err == nil {
					return model.NewDateCell(t), nil
				}
			}
		}
		return model.NewStringCell(text), nil
	}
	if num, err := strconv.ParseFloat(text, 64); err == nil {
		return model.NewNumberCell(num), nil
	}
	if text == "TRUE" || text == "FALSE" {
		return model.NewBoolCell(text == "TRUE"), nil
	}
	return model.NewStringCell(text), nil
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

// isDateStyled reports whether the cell at cellRef has a date/time number
// format, per its style's built-in format ID or custom format code.
func isDateStyled(f *excelize.File, sheet, cellRef string, cache map[int]bool) (bool, error) {
	styleID, err := f.GetCellStyle(sheet, cellRef)
	if err != nil {
		return false, fmt.Errorf("read style of %s!%s: %w", sheet, cellRef, err)
	}
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
