// Package xlsx converts .xlsx files into xlq's internal model.Workbook
// (see internal/model and docs/adr/0007-spreadsheet-data-model.md) using
// excelize, so the rest of xlq never depends on excelize directly.
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

func readSheet(f *excelize.File, name string, date1904 bool, dateStyles map[int]bool) (model.Sheet, error) {
	rows, err := f.Rows(name)
	if err != nil {
		return model.Sheet{}, err
	}
	defer func() { _ = rows.Close() }()

	sheet := model.Sheet{Name: name}
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
	if display == "" && formula != "" {
		// A formula's cached result is only present once the workbook has
		// been opened and recalculated by a spreadsheet application (or by
		// excelize itself); compute it directly rather than reporting the
		// cell as blank.
		if display, err = f.CalcCellValue(sheet, cellRef); err != nil {
			return model.Cell{}, false, fmt.Errorf("calculate %s!%s: %w", sheet, cellRef, err)
		}
	}
	if display == "" && formula == "" {
		return model.Cell{}, false, nil
	}

	cell := model.NewEmptyCell()
	if display != "" {
		if formula != "" {
			// GetCellType on a formula cell reports that it's a formula,
			// not the type of its result (and does so even once the
			// result has been calculated), so infer the result's Kind
			// from its computed text and style instead.
			cell, err = cellFromText(f, sheet, cellRef, display, date1904, dateStyles)
		} else {
			var cellType excelize.CellType
			if cellType, err = f.GetCellType(sheet, cellRef); err == nil {
				cell, err = cellFromTyped(f, sheet, cellRef, cellType, display, date1904, dateStyles)
			}
		}
		if err != nil {
			return model.Cell{}, false, err
		}
	}
	if formula != "" {
		cell = cell.WithFormula(formula)
	}
	return cell.At(colIndex), true, nil
}

// cellFromTyped converts a non-formula cell using its excelize CellType.
func cellFromTyped(f *excelize.File, sheet, cellRef string, cellType excelize.CellType, display string, date1904 bool, dateStyles map[int]bool) (model.Cell, error) {
	switch cellType {
	case excelize.CellTypeBool:
		return model.NewBoolCell(display == "1" || strings.EqualFold(display, "TRUE")), nil
	case excelize.CellTypeSharedString, excelize.CellTypeInlineString, excelize.CellTypeError:
		return model.NewStringCell(display), nil
	default:
		// CellTypeUnset (a plain stored number) or CellTypeDate (the rare
		// ISO-8601-native date type): both are backed by a number, so
		// fall through to the same value/style heuristic used for
		// formula results.
		return cellFromText(f, sheet, cellRef, display, date1904, dateStyles)
	}
}

// cellFromText infers a Kind for a cell purely from its computed display
// text and its style's number format: a date-styled number becomes a Date,
// a parseable number becomes a Number, a literal "TRUE"/"FALSE" becomes a
// Bool, and anything else is kept as a String.
func cellFromText(f *excelize.File, sheet, cellRef, display string, date1904 bool, dateStyles map[int]bool) (model.Cell, error) {
	isDate, err := isDateStyled(f, sheet, cellRef, dateStyles)
	if err != nil {
		return model.Cell{}, err
	}
	if isDate {
		raw, err := f.GetCellValue(sheet, cellRef, excelize.Options{RawCellValue: true})
		if err != nil {
			return model.Cell{}, fmt.Errorf("read raw value of %s!%s: %w", sheet, cellRef, err)
		}
		if serial, err := strconv.ParseFloat(raw, 64); err == nil {
			if t, err := excelize.ExcelDateToTime(serial, date1904); err == nil {
				return model.NewDateCell(t), nil
			}
		}
		// Styled as a date but not actually a parseable serial number;
		// fall back to the displayed text rather than lose the cell.
		return model.NewStringCell(display), nil
	}

	if num, err := strconv.ParseFloat(display, 64); err == nil {
		return model.NewNumberCell(num), nil
	}
	if display == "TRUE" || display == "FALSE" {
		return model.NewBoolCell(display == "TRUE"), nil
	}
	return model.NewStringCell(display), nil
}

// builtinDateNumFmtIDs are the built-in (ECMA-376) number format IDs that
// render a number as a date and/or time.
var builtinDateNumFmtIDs = map[int]bool{
	14: true, 15: true, 16: true, 17: true, 18: true, 19: true, 20: true,
	21: true, 22: true, 45: true, 46: true, 47: true,
}

// dateTimeToken matches a date/time format-code letter (y, m, d, h, s),
// used to classify a custom number format as date-like once quoted
// literals and bracketed color/locale segments have been stripped out.
var dateTimeToken = regexp.MustCompile(`(?i)[ymdhs]`)

var (
	quotedLiteral    = regexp.MustCompile(`"[^"]*"`)
	bracketedLiteral = regexp.MustCompile(`\[[^\]]*\]`)
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
		code := bracketedLiteral.ReplaceAllString(*style.CustomNumFmt, "")
		code = quotedLiteral.ReplaceAllString(code, "")
		isDate = dateTimeToken.MatchString(code)
	} else {
		isDate = builtinDateNumFmtIDs[style.NumFmt]
	}
	cache[styleID] = isDate
	return isDate, nil
}
