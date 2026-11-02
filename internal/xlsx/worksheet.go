package xlsx

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"

	"github.com/xuri/excelize/v2"
)

// resolvedCell is a <c> element's own attributes and children, with
// its column index already resolved to 0-based - from its own r
// attribute ("C3"), or sequentially following the previous cell in
// its row when r is absent, mirroring excelize's own rowXMLHandler
// (confirmed directly in its source: it increments a running column
// counter for every <c> seen, then overrides it from r only when r
// is present).
type resolvedCell struct {
	col     int
	styleID int
	t       string // the cell's own raw "t" type attribute, "" if absent
	v       string
	hasV    bool
	// formulaType is the <f> element's own "t" attribute (e.g.
	// "shared", "array"); "" means a normal (or absent) formula.
	formulaType string
	formulaText string
	hasF        bool
	is          *sharedStringItem // non-nil if an <is> (inline string) child was present
}

// cellXMLAttrs is decodeCellXML's own raw result, before row-level
// column resolution (decodeRowXML's job - a cell can't resolve its
// own column without knowing the previous cell's, when r is absent).
type cellXMLAttrs struct {
	r           string
	styleID     int
	t           string
	v           string
	hasV        bool
	formulaType string
	formulaText string
	hasF        bool
	is          *sharedStringItem
}

// decodeCellXML decodes a <c> element (its own StartElement already
// consumed, start) into its raw attributes and children - mirroring
// excelize's own unexported cellXMLAttrHandler/cellXMLHandler,
// confirmed directly in its source, just never exposed.
func decodeCellXML(dec *xml.Decoder, start xml.StartElement) (cellXMLAttrs, error) {
	var c cellXMLAttrs
	for _, attr := range start.Attr {
		switch attr.Name.Local {
		case "r":
			c.r = attr.Value
		case "s":
			if n, err := strconv.Atoi(attr.Value); err == nil {
				c.styleID = n
			}
		case "t":
			c.t = attr.Value
		}
	}

	for {
		tok, err := dec.Token()
		if err != nil {
			return c, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "v":
				if err := dec.DecodeElement(&c.v, &t); err != nil {
					return c, err
				}
				c.hasV = true
			case "f":
				for _, attr := range t.Attr {
					if attr.Name.Local == "t" {
						c.formulaType = attr.Value
					}
				}
				if err := dec.DecodeElement(&c.formulaText, &t); err != nil {
					return c, err
				}
				c.hasF = true
			case "is":
				var is sharedStringItem
				if err := dec.DecodeElement(&is, &t); err != nil {
					return c, err
				}
				c.is = &is
			default:
				if err := dec.Skip(); err != nil {
					return c, err
				}
			}
		case xml.EndElement:
			return c, nil
		}
	}
}

// decodeRowXML decodes a <row> element (its own StartElement already
// consumed, start) into its 0-based row index (from its own r
// attribute, -1 if absent) and its cells, in document order, with
// each cell's column already resolved.
func decodeRowXML(dec *xml.Decoder, start xml.StartElement) (rowIndex int, cells []resolvedCell, err error) {
	rowIndex = -1
	for _, attr := range start.Attr {
		if attr.Name.Local == "r" {
			if n, err := strconv.Atoi(attr.Value); err == nil {
				rowIndex = n - 1
			}
		}
	}

	nextCol := 0 // 1-based running column, mirrors excelize's own rowIterator.cellCol
	for {
		tok, tokErr := dec.Token()
		if tokErr != nil {
			return rowIndex, cells, tokErr
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "c" {
				if err := dec.Skip(); err != nil {
					return rowIndex, cells, err
				}
				continue
			}
			nextCol++
			raw, cellErr := decodeCellXML(dec, t)
			if cellErr != nil {
				return rowIndex, cells, cellErr
			}
			col := nextCol
			if raw.r != "" {
				if c, _, err := excelize.CellNameToCoordinates(raw.r); err == nil {
					col = c
				}
			}
			nextCol = col
			cells = append(cells, resolvedCell{
				col: col - 1, styleID: raw.styleID, t: raw.t,
				v: raw.v, hasV: raw.hasV,
				formulaType: raw.formulaType, formulaText: raw.formulaText, hasF: raw.hasF,
				is: raw.is,
			})
		case xml.EndElement:
			return rowIndex, cells, nil
		}
	}
}

// streamWorksheetRows streams f's (a worksheet's own xl/worksheets/
// sheetN.xml) <sheetData> row-by-row, cell-by-cell, via encoding/xml
// token walking - mirroring excelize's own unexported rowXMLHandler/
// cellXMLHandler, confirmed directly in its source: they already do
// exactly this internally, just never expose it. yield is called
// once per <row> that has at least one cell (an empty row, or one
// whose only cells have neither a value nor a formula, is skipped,
// matching the row-iterator convention model.Sheet.Rows itself
// already uses) - returning false from yield stops iteration early
// without error. A genuine read error (an XML parse failure, or
// maxSize exceeded) is reported through yield exactly once, with a
// zero rowIndex and nil cells.
//
// depth tracking (not "any <row> or <c>, regardless of nesting")
// guards the same concern #168's own sharedStrings fix does for
// <si>: a <row>-or-<c>-named element nested somewhere other than a
// direct child of <sheetData>/<row> - not a shape real producers are
// known to emit, but one a crafted file could still contain and
// still be well-formed XML - must not be misread as real sheet data.
//
// maxSize bounds how large this part may decompress to, re-applied
// here independently (the same --max-size-aware bound #168's own
// sharedStrings already applies to its part): Read's upfront OpenFile
// check doesn't protect this later, separate archive/zip read.
func streamWorksheetRows(f *zip.File, maxSize int64, yield func(rowIndex int, cells []resolvedCell, err error) bool) {
	fail := func(err error) bool {
		return yield(0, nil, err)
	}

	if maxSize > 0 && int64(f.UncompressedSize64) > maxSize {
		fail(fmt.Errorf("xlsx: %s is %d bytes uncompressed, over the %d-byte limit for a v1 read", f.Name, f.UncompressedSize64, maxSize))
		return
	}
	rc, err := f.Open()
	if err != nil {
		fail(fmt.Errorf("xlsx: open %s: %w", f.Name, err))
		return
	}
	defer func() { _ = rc.Close() }()

	var r io.Reader = rc
	var limited *io.LimitedReader
	if maxSize > 0 {
		limited = &io.LimitedReader{R: rc, N: limitedReaderN(maxSize)}
		r = limited
	}
	overLimit := func() bool { return limited != nil && limited.N <= 0 }
	limitOrErr := func(err error) error {
		if overLimit() {
			return fmt.Errorf("xlsx: %s exceeds the %d-byte limit for a v1 read", f.Name, maxSize)
		}
		return fmt.Errorf("xlsx: parse %s: %w", f.Name, err)
	}

	dec := xml.NewDecoder(r)
	depth := 0
	inSheetData := false
	sheetDataDepth := -1
	for {
		tok, tokErr := dec.Token()
		if tokErr == io.EOF {
			break
		}
		if tokErr != nil {
			fail(limitOrErr(tokErr))
			return
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch {
			case !inSheetData && depth == 1 && t.Name.Local == "sheetData":
				inSheetData = true
				sheetDataDepth = depth
				depth++
			case inSheetData && depth == sheetDataDepth+1 && t.Name.Local == "row":
				rowIndex, cells, rowErr := decodeRowXML(dec, t)
				if rowErr != nil {
					fail(limitOrErr(rowErr))
					return
				}
				if len(cells) > 0 && !yield(rowIndex, cells, nil) {
					return
				}
			default:
				depth++
			}
		case xml.EndElement:
			depth--
			if inSheetData && depth <= sheetDataDepth {
				inSheetData = false
			}
		}
	}
	if overLimit() {
		fail(fmt.Errorf("xlsx: %s exceeds the %d-byte limit for a v1 read", f.Name, maxSize))
	}
}
