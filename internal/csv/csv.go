// Package csv converts a delimiter-separated text file (.csv, .tsv) into
// xlq's internal model.Workbook (see internal/model and
// docs/adr/0007-spreadsheet-data-model.md), treating the whole file as a
// single sheet named "Sheet1" - the same name a fresh .xlsx workbook's
// first sheet gets, so a filter written against one format works
// unchanged against the other.
//
// Unlike xlsx, a delimiter-separated file has no sparse/dense ambiguity:
// every field a row actually declares (via its delimiters) becomes a
// cell, an empty field becomes an Empty cell rather than being omitted,
// and encoding/csv itself enforces that every row has the same field
// count as the first (returning csv.ErrFieldCount otherwise), so a
// row's width never needs to be guessed or reconciled the way a sparse
// xlsx sheet's does.
package csv

import (
	"bufio"
	"bytes"
	stdcsv "encoding/csv"
	"fmt"
	"io"
	"os"

	"github.com/brunoarueira/xlq/internal/model"
)

// sheetName is the fixed name given to a CSV/TSV file's single sheet.
const sheetName = "Sheet1"

// Read opens the CSV file at path (comma-delimited) and converts it into
// a single-sheet model.Workbook.
func Read(path string) (model.Workbook, error) {
	return read(path, ',')
}

// ReadTSV opens the TSV file at path (tab-delimited) and converts it into
// a single-sheet model.Workbook.
func ReadTSV(path string) (model.Workbook, error) {
	return read(path, '\t')
}

func read(path string, delim rune) (model.Workbook, error) {
	f, err := os.Open(path)
	if err != nil {
		return model.Workbook{}, fmt.Errorf("open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	r := stdcsv.NewReader(stripBOM(f))
	r.Comma = delim

	sheet := model.Sheet{Name: sheetName}
	for rowIndex := 0; ; rowIndex++ {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return model.Workbook{}, fmt.Errorf("read %q: %w", path, err)
		}

		cells := make([]model.Cell, len(record))
		for col, field := range record {
			cell := model.NewEmptyCell()
			if field != "" {
				cell = model.NewStringCell(field)
			}
			cells[col] = cell.At(col)
		}
		sheet.Rows = append(sheet.Rows, model.Row{Index: rowIndex, Cells: cells})
	}

	return model.Workbook{Sheets: []model.Sheet{sheet}}, nil
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// stripBOM wraps r so a leading UTF-8 byte-order mark - common in
// Excel-exported CSVs - doesn't end up embedded in the first field of
// the first row.
func stripBOM(r io.Reader) io.Reader {
	br := bufio.NewReader(r)
	if b, err := br.Peek(len(utf8BOM)); err == nil && bytes.Equal(b, utf8BOM) {
		_, _ = br.Discard(len(utf8BOM))
	}
	return br
}
