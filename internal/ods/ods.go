// Package ods converts an OpenDocument Spreadsheet (.ods) file into xlq's
// internal model.Workbook, using only the standard library
// (archive/zip, encoding/xml) - no third-party dependency. See
// docs/adr/0010-ods-reader-no-dependency.md for why.
package ods

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/brunoarueira/xlq/internal/model"
)

// maxMaterializedCells serves two distinct guards that happen to share
// the same threshold:
//
//   - budget.spend bounds how many cells this reader will actually
//     expand from ODF's own repeat-compression
//     (table:number-rows-repeated/table:number-columns-repeated) into
//     real model.Cell/model.Row values, matching the budget
//     internal/eval enforces for dense results (see its maxDenseCells
//     constant). A repeat count on genuinely blank content is skipped
//     entirely rather than counted here, per model.Sheet's sparse
//     design - this only guards a malformed or adversarial file
//     claiming an enormous repeat count on real content.
//   - repeatCount rejects any repeat count over this same threshold
//     outright, blank or not: even a skipped blank repeat still
//     advances a row/column index by that count, and an unbounded
//     value read straight from the file could overflow that
//     arithmetic. A blank run past this limit is thus reported as an
//     error rather than silently skipped - the one case where "skipped
//     regardless of size" doesn't hold.
const maxMaterializedCells = 10_000_000

// maxContentXMLSize bounds how large a decompressed content.xml this
// reader will parse. Without this, the per-cell budget above wouldn't
// help at all against a file that writes out a huge number of cells
// literally (no repeat-compression involved) or that decompresses to
// far more than its on-disk size suggests: the whole document is
// unmarshaled into memory before any cell is even looked at. 256 MiB
// comfortably covers a legitimate .ods far larger than xlq targets in
// v1 (see M6, deferred, for real large-file support).
const maxContentXMLSize = 256 << 20 // 256 MiB

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

// xmlTable's rows aren't always direct <table:table> children: ODF
// allows them to be nested inside <table:table-header-rows>,
// <table:table-rows>, or (recursively) <table:table-row-group>
// wrapper elements instead - a real, common feature (e.g. LibreOffice's
// frozen/repeated print header rows). UnmarshalXML flattens all of
// these into a single, document-ordered list of rows, since
// model.Sheet has no concept of "header" or "grouped" rows to preserve
// that distinction with.
type xmlTable struct {
	Name string
	Rows []xmlRow
}

func (t *xmlTable) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for _, attr := range start.Attr {
		if attr.Name.Local == "name" {
			t.Name = attr.Value
		}
	}
	rows, err := decodeRows(d, start.Name)
	if err != nil {
		return err
	}
	t.Rows = rows
	return nil
}

// decodeRows decodes the children of a container element (its start
// tag already consumed by the caller) up to and including its matching
// end tag, flattening table-row elements found directly or within
// nested table-header-rows/table-rows/table-row-group wrappers, in
// document order. Any other child (table-column definitions, ...) is
// skipped.
func decodeRows(d *xml.Decoder, containerName xml.Name) ([]xmlRow, error) {
	var rows []xmlRow
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "table-row":
				var row xmlRow
				if err := d.DecodeElement(&row, &t); err != nil {
					return nil, err
				}
				rows = append(rows, row)
			case "table-header-rows", "table-rows", "table-row-group":
				nested, err := decodeRows(d, t.Name)
				if err != nil {
					return nil, err
				}
				rows = append(rows, nested...)
			default:
				if err := d.Skip(); err != nil {
					return nil, err
				}
			}
		case xml.EndElement:
			if t.Name == containerName {
				return rows, nil
			}
		}
	}
}

// xmlRow's cells include both <table:table-cell> (real cells) and
// <table:covered-table-cell> (a position covered by a preceding merged
// cell's column span - has no value of its own). UnmarshalXML keeps
// both in document order so column indices advance correctly across a
// merge; a covered cell is always treated as blank.
type xmlRow struct {
	Repeat string
	Cells  []xmlCell
}

func (r *xmlRow) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for _, attr := range start.Attr {
		if attr.Name.Local == "number-rows-repeated" {
			r.Repeat = attr.Value
		}
	}

	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "table-cell":
				var cell xmlCell
				if err := d.DecodeElement(&cell, &t); err != nil {
					return err
				}
				r.Cells = append(r.Cells, cell)
			case "covered-table-cell":
				var covered xmlCell
				if err := d.DecodeElement(&covered, &t); err != nil {
					return err
				}
				// A covered cell has no value of its own regardless of
				// what attributes it happens to carry; keep only its
				// repeat count so column advancement stays correct.
				r.Cells = append(r.Cells, xmlCell{Repeat: covered.Repeat})
			default:
				if err := d.Skip(); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if t.Name == start.Name {
				return nil
			}
		}
	}
}

type xmlCell struct {
	Repeat       string          `xml:"number-columns-repeated,attr"`
	ValueType    string          `xml:"value-type,attr"`
	Value        string          `xml:"value,attr"`
	DateValue    string          `xml:"date-value,attr"`
	TimeValue    string          `xml:"time-value,attr"`
	BooleanValue string          `xml:"boolean-value,attr"`
	Formula      string          `xml:"formula,attr"`
	Paragraphs   []paragraphText `xml:"p"`
}

// paragraphText concatenates all character data within a <text:p>
// element, including that of nested inline formatting elements like
// <text:span>. A plain string field would only capture text directly
// inside <text:p>, silently dropping any text wrapped in such
// formatting.
type paragraphText string

func (p *paragraphText) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var sb strings.Builder
	depth := 0
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.CharData:
			sb.Write(t)
		case xml.StartElement:
			depth++
		case xml.EndElement:
			if depth == 0 {
				*p = paragraphText(sb.String())
				return nil
			}
			depth--
		}
	}
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
	return decodeEntryWithLimit(f, v, maxContentXMLSize)
}

// decodeEntryWithLimit is decodeEntry with the size limit as a
// parameter, so tests can exercise the limit logic itself without
// needing to generate a gigantic fixture file.
func decodeEntryWithLimit(f *zip.File, v any, maxSize int64) error {
	if int64(f.UncompressedSize64) > maxSize {
		return fmt.Errorf("%s is %d bytes uncompressed, over the %d-byte limit for a v1 read", f.Name, f.UncompressedSize64, maxSize)
	}

	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("open %s: %w", f.Name, err)
	}
	defer func() { _ = rc.Close() }()

	// A zip's own declared UncompressedSize64 can't be trusted on its
	// own - it's just a header value - so also cap the actual bytes
	// read, in case it understates the truth.
	limited := &io.LimitedReader{R: rc, N: maxSize + 1}
	if err := xml.NewDecoder(limited).Decode(v); err != nil {
		return fmt.Errorf("parse %s: %w", f.Name, err)
	}
	if limited.N <= 0 {
		return fmt.Errorf("%s exceeds the %d-byte limit for a v1 read", f.Name, maxSize)
	}
	return nil
}

// budget tracks how many cells have actually been materialized (as
// opposed to skipped for having no real content) across the whole
// workbook. used and spend's n are int64, not int: convertTable's
// repeat*len(cells) cost is itself a multiplication of two values each
// individually capped at maxMaterializedCells, and that product alone
// can exceed a 32-bit int's range even though neither factor does.
type budget struct {
	used int64
}

// spend records n more materialized cells, checked against the
// remaining budget *before* adding - not after - so a crafted n close
// to int64's limit can't overflow the running total and defeat the
// check.
func (b *budget) spend(n int64) error {
	if n < 0 || n > maxMaterializedCells-b.used {
		return fmt.Errorf("workbook would materialize over %d cells (repeated real content), over the limit for a v1 read", maxMaterializedCells)
	}
	b.used += n
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

		// A repeated row duplicates every one of its cells at each
		// repeat, not just itself: the true cost is repeat*len(cells),
		// not repeat. Both factors are individually capped at
		// maxMaterializedCells, but their product isn't - compute it in
		// int64 rather than risk overflowing a 32-bit int.
		if err := b.spend(int64(repeat) * int64(len(cells))); err != nil {
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
// never materialized, regardless of its repeat count). The budget is
// not spent here for a cell's own repeat count alone - convertTable
// charges the full repeat(row)*len(cells) cost once the row's total
// shape is known.
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

		if err := b.spend(int64(repeat)); err != nil {
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
	parts := make([]string, len(xc.Paragraphs))
	for i, p := range xc.Paragraphs {
		parts[i] = string(p)
	}
	text := strings.Join(parts, "\n")

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
		return cell.WithFormula(normalizeFormula(xc.Formula)), true, nil
	}
	return cell, hasValue, nil
}

// normalizeFormula strips ODF's "of:" OpenFormula-dialect marker (a
// stored formula is typically "of:=EXPR") so an ODS-derived Formula has
// the same leading-"=" shape as the xlsx reader's, per ADR-0007's
// stated convention (a formula bar display, not the raw stored text).
// Cell references are left in ODF's own "[.A1]"-style bracket notation,
// not translated to plain A1 - a narrower, documented gap; see
// ADR-0010.
func normalizeFormula(raw string) string {
	raw = strings.TrimPrefix(raw, "of:")
	if raw != "" && !strings.HasPrefix(raw, "=") {
		raw = "=" + raw
	}
	return raw
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
// 1 when absent. The result is capped at maxMaterializedCells - even a
// repeat on blank content, which is otherwise skipped at no cost, still
// advances a row/column index by that count, and an uncapped value read
// straight from the file could overflow that arithmetic. A blank run
// past this cap is therefore reported as an error here rather than
// silently skipped further up the call chain.
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
	if n > maxMaterializedCells {
		return 0, fmt.Errorf("repeat count %d exceeds the %d limit for a v1 read", n, maxMaterializedCells)
	}
	return n, nil
}
