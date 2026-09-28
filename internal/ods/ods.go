// Package ods converts an OpenDocument Spreadsheet (.ods) file into xlq's
// internal model.Workbook, using only the standard library
// (archive/zip, encoding/xml) - no third-party dependency. See
// docs/adr/0010-ods-reader-no-dependency.md for why.
package ods

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/brunoarueira/xlq/internal/model"
)

// maxMaterializedCells bounds how many cells this reader will actually
// expand from ODF's own repeat-compression
// (table:number-rows-repeated/table:number-columns-repeated), matching
// the budget internal/eval enforces for dense results. A repeat count on
// genuinely blank content costs nothing regardless of size - it's
// skipped entirely, per model.Sheet's sparse design - so this only
// guards against a malformed or adversarial file claiming an enormous
// repeat count on real content.
const maxMaterializedCells = 10_000_000

// contentXML mirrors just enough of content.xml's structure to read
// cell data. Struct tags intentionally omit namespaces: encoding/xml
// matches elements and attributes by local name when no namespace is
// given, which resolves ODF's conventional table:/office:/text:
// prefixes correctly regardless of exactly which prefix a given
// producer used.
type contentXML struct {
	Body struct {
		Spreadsheet struct {
			Tables []xmlTable `xml:"table"`
		} `xml:"spreadsheet"`
	} `xml:"body"`
}

type xmlTable struct {
	Name string   `xml:"name,attr"`
	Rows []xmlRow `xml:"table-row"`
}

type xmlRow struct {
	Repeat string    `xml:"number-rows-repeated,attr"`
	Cells  []xmlCell `xml:"table-cell"`
}

type xmlCell struct {
	Repeat       string   `xml:"number-columns-repeated,attr"`
	ValueType    string   `xml:"value-type,attr"`
	Value        string   `xml:"value,attr"`
	DateValue    string   `xml:"date-value,attr"`
	TimeValue    string   `xml:"time-value,attr"`
	BooleanValue string   `xml:"boolean-value,attr"`
	Formula      string   `xml:"formula,attr"`
	Paragraphs   []string `xml:"p"`
}

// Read opens the .ods file at path and converts it into a
// model.Workbook.
func Read(path string) (model.Workbook, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return model.Workbook{}, fmt.Errorf("open %q: %w", path, err)
	}
	defer func() { _ = zr.Close() }()

	var content contentXML
	found := false
	for _, f := range zr.File {
		if f.Name != "content.xml" {
			continue
		}
		found = true
		if err := decodeEntry(f, &content); err != nil {
			return model.Workbook{}, fmt.Errorf("%q: %w", path, err)
		}
		break
	}
	if !found {
		return model.Workbook{}, fmt.Errorf("%q: not a valid .ods file (no content.xml)", path)
	}

	b := &budget{}
	var wb model.Workbook
	for _, table := range content.Body.Spreadsheet.Tables {
		sheet, err := convertTable(table, b)
		if err != nil {
			return model.Workbook{}, fmt.Errorf("%q: sheet %q: %w", path, table.Name, err)
		}
		wb.Sheets = append(wb.Sheets, sheet)
	}
	return wb, nil
}

func decodeEntry(f *zip.File, v any) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("open %s: %w", f.Name, err)
	}
	defer func() { _ = rc.Close() }()
	if err := xml.NewDecoder(rc).Decode(v); err != nil {
		return fmt.Errorf("parse %s: %w", f.Name, err)
	}
	return nil
}

// budget tracks how many cells have actually been materialized (as
// opposed to skipped for having no real content) across the whole
// workbook.
type budget struct {
	used int
}

func (b *budget) spend(n int) error {
	b.used += n
	if b.used > maxMaterializedCells {
		return fmt.Errorf("workbook would materialize over %d cells (repeated real content), over the limit for a v1 read", maxMaterializedCells)
	}
	return nil
}

func convertTable(t xmlTable, b *budget) (model.Sheet, error) {
	sheet := model.Sheet{Name: t.Name}
	rowIndex := 0
	for _, xr := range t.Rows {
		repeat, err := repeatCount(xr.Repeat)
		if err != nil {
			return model.Sheet{}, fmt.Errorf("row: %w", err)
		}

		cells, hasContent, err := convertRow(xr, b)
		if err != nil {
			return model.Sheet{}, err
		}
		if !hasContent {
			rowIndex += repeat
			continue
		}

		if err := b.spend(repeat); err != nil {
			return model.Sheet{}, err
		}
		for i := 0; i < repeat; i++ {
			sheet.Rows = append(sheet.Rows, model.Row{Index: rowIndex, Cells: cells})
			rowIndex++
		}
	}
	return sheet, nil
}

// convertRow converts one <table:table-row>'s cells, reporting whether
// the row has any actual content (a repeated-but-entirely-blank row is
// never materialized, regardless of its repeat count).
func convertRow(xr xmlRow, b *budget) ([]model.Cell, bool, error) {
	var cells []model.Cell
	col := 0
	for _, xc := range xr.Cells {
		repeat, err := repeatCount(xc.Repeat)
		if err != nil {
			return nil, false, fmt.Errorf("cell: %w", err)
		}

		cell, hasContent, err := convertCell(xc)
		if err != nil {
			return nil, false, err
		}
		if !hasContent {
			col += repeat
			continue
		}

		if err := b.spend(repeat); err != nil {
			return nil, false, err
		}
		for i := 0; i < repeat; i++ {
			cells = append(cells, cell.At(col))
			col++
		}
	}
	return cells, len(cells) > 0, nil
}

// convertCell converts one <table:table-cell> (without its position,
// applied later by the caller via Cell.At), reporting whether it has
// any real content (a value or a formula).
func convertCell(xc xmlCell) (model.Cell, bool, error) {
	text := strings.Join(xc.Paragraphs, "\n")

	var cell model.Cell
	var hasValue bool
	switch xc.ValueType {
	case "string":
		cell, hasValue = model.NewStringCell(text), true
	case "float", "percentage", "currency":
		v, err := strconv.ParseFloat(xc.Value, 64)
		if err != nil {
			return model.Cell{}, false, fmt.Errorf("parse numeric cell value %q: %w", xc.Value, err)
		}
		cell, hasValue = model.NewNumberCell(v), true
	case "boolean":
		v, err := strconv.ParseBool(xc.BooleanValue)
		if err != nil {
			return model.Cell{}, false, fmt.Errorf("parse boolean cell value %q: %w", xc.BooleanValue, err)
		}
		cell, hasValue = model.NewBoolCell(v), true
	case "date":
		t, err := parseODFDate(xc.DateValue)
		if err != nil {
			return model.Cell{}, false, err
		}
		cell, hasValue = model.NewDateCell(t), true
	case "time":
		// A duration (e.g. "PT2H30M"), not a point in time - no
		// matching model.CellKind. Kept as its raw ISO-8601 text rather
		// than inventing a numeric mapping; see ADR-0010.
		cell, hasValue = model.NewStringCell(xc.TimeValue), xc.TimeValue != ""
	default:
		// No declared type, or an unrecognized one (a vendor extension,
		// a newer spec version, ...): fall back to whatever text is
		// present rather than failing the whole read over one cell.
		if text == "" {
			cell, hasValue = model.NewEmptyCell(), false
		} else {
			cell, hasValue = model.NewStringCell(text), true
		}
	}

	if xc.Formula != "" {
		return cell.WithFormula(xc.Formula), true, nil
	}
	return cell, hasValue, nil
}

var odfDateLayouts = []string{
	"2006-01-02T15:04:05.999",
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02",
}

func parseODFDate(raw string) (time.Time, error) {
	for _, layout := range odfDateLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("parse date cell value %q: unrecognized format", raw)
}

// repeatCount parses a table:number-*-repeated attribute, defaulting to
// 1 when absent.
func repeatCount(raw string) (int, error) {
	if raw == "" {
		return 1, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid repeat count %q: %w", raw, err)
	}
	if n < 1 {
		return 0, fmt.Errorf("invalid repeat count %d: must be positive", n)
	}
	return n, nil
}
