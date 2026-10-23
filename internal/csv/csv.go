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
	"unicode/utf8"

	"github.com/brunoarueira/xlq/internal/model"
)

// SheetName is the fixed name given to a CSV/TSV file's single sheet.
const SheetName = "Sheet1"

// maxMaterializedCells bounds how many cells a read will ever
// materialize into memory, matching internal/ods's own
// maxMaterializedCells value (duplicated, not shared, since this
// package has no dependency on internal/ods) - one consistent "this is
// clearly not a legitimate v1-sized file" ceiling across formats (#51,
// docs/adr/0031-memory-ceiling-for-untrusted-files.md). Unlike xlsx/ods,
// csv has no compression step that could make a small on-disk file
// expand far past its own size, so --max-size's pre-flight byte check
// (internal/input's checkFileSize) already bounds raw content size for
// csv - this exists for the separate risk that same check can't catch:
// a file within the byte limit but built from an enormous number of
// tiny fields/rows, where each materialized model.Cell/model.Row's own
// struct overhead (far more than the 1-2 bytes such a field holds)
// dominates actual memory use.
// A var, not a const, so a test can lower it temporarily rather than
// writing a genuinely 10-million-cell fixture to exercise this path.
var maxMaterializedCells = 10_000_000

// Read opens the CSV file at path (comma-delimited) and converts it into
// a single-sheet model.Workbook.
func Read(path string) (model.Workbook, error) {
	return ReadWithDelimiter(path, ',')
}

// ReadTSV opens the TSV file at path (tab-delimited) and converts it into
// a single-sheet model.Workbook.
func ReadTSV(path string) (model.Workbook, error) {
	return ReadWithDelimiter(path, '\t')
}

// ReadWithDelimiter opens the delimiter-separated file at path, using
// delim as the field separator instead of Read/ReadTSV's fixed comma
// or tab - for a file that doesn't use either, e.g. a semicolon-
// separated export from a locale where comma is the decimal separator.
// An invalid delim (a newline, the quote character, or anything
// encoding/csv itself rejects) surfaces as an error from the first
// Read call, not from this function - encoding/csv validates it
// lazily, and this package doesn't duplicate that check.
func ReadWithDelimiter(path string, delim rune) (model.Workbook, error) {
	f, err := os.Open(path)
	if err != nil {
		return model.Workbook{}, fmt.Errorf("open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	src := stripBOM(f)
	// encoding/csv.Reader.Read fully parses one record before ever
	// checking anything about it - confirmed directly (a Reader whose
	// FieldsPerRecord was set to reject nearly everything still blocked
	// reading an unbounded single record rather than failing as soon as
	// the field count was exceeded) - so a single record with millions
	// of fields would already have paid that whole allocation cost
	// before totalCells's own post-hoc check below ever runs. Wrapping
	// the source reader to count delim occurrences as bytes stream in,
	// not after a record completes, closes that gap - restricted to a
	// single-byte delim (every delimiter this project's own
	// documentation and tests ever specify is: comma, tab, semicolon)
	// to avoid a general multi-byte streaming matcher for a
	// combination (multi-byte delim exercising this specific gap) that
	// has never come up; totalCells's own cross-record accumulation
	// below still catches "many small records" regardless of delim
	// width either way.
	if utf8.RuneLen(delim) == 1 {
		src = newFieldCountLimitedReader(src, byte(delim), maxMaterializedCells)
	}
	r := stdcsv.NewReader(src)
	r.Comma = delim

	var rows []model.Row
	totalCells := 0
	for rowIndex := 0; ; rowIndex++ {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return model.Workbook{}, fmt.Errorf("read %q: %w", path, err)
		}

		totalCells += len(record)
		if totalCells > maxMaterializedCells {
			return model.Workbook{}, fmt.Errorf("read %q: workbook would materialize over %d cells, over the limit for a v1 read", path, maxMaterializedCells)
		}

		cells := make([]model.Cell, len(record))
		for col, field := range record {
			cell := model.NewEmptyCell()
			if field != "" {
				cell = model.NewStringCell(field)
			}
			cells[col] = cell.At(col)
		}
		rows = append(rows, model.Row{Index: rowIndex, Cells: cells})
	}

	return model.Workbook{Sheets: []model.Sheet{model.NewSheet(SheetName, rows)}}, nil
}

// fieldCountLimitedReader wraps r, counting delim bytes outside any
// quoted field as they stream through Read and failing as soon as the
// running total exceeds max - before encoding/csv.Reader can finish
// parsing whatever record those delimiters belong to, not after (#51,
// docs/adr/0031-memory-ceiling-for-untrusted-files.md). The count is
// never reset between records, so it also naturally bounds the total
// across the whole file, the same thing ReadWithDelimiter's own
// totalCells separately tracks post-hoc - the two overlap rather than
// one subsuming the other, since this wrapper only exists for a
// single-byte delim.
//
// Tracks RFC 4180 quoting (a field starting with '"' runs until the
// next '"' not immediately followed by another '"', which is a
// literal escaped quote rather than the field's end) well enough to
// never count a delim that's actually inside a quoted field as a real
// field separator - a cubic-dev-ai review caught a first version that
// counted every delim byte unconditionally, rejecting valid CSV with a
// large quoted value (e.g. one field holding a long comma-separated
// blob) as though it were that many separate fields. It's forgiving
// rather than a strict validator: anything that isn't unambiguously
// "this delim is inside an open, unescaped quote" is counted, so a
// malformed file is never under-counted into silently passing - this
// only needs to avoid the specific false positive above, not fully
// replicate encoding/csv's own parsing.
type fieldCountLimitedReader struct {
	r     io.Reader
	delim byte
	count int
	max   int
	// atFieldStart is true at the very start of input and right after
	// every delim or '\n' - the only positions where a '"' begins a
	// quoted field rather than appearing as ordinary field content.
	atFieldStart bool
	inQuotes     bool
	// afterQuoteInQuotes is true right after a '"' seen while
	// inQuotes - the next byte decides whether that was a doubled,
	// escaped quote (another '"': stay inQuotes) or the field's real
	// closing quote (anything else: leave inQuotes, then process this
	// byte normally, since it's the first byte after the field ends).
	afterQuoteInQuotes bool
	// tripped sticks once set, so every call after the limit is first
	// exceeded keeps failing the same way - not just the one call where
	// it was first detected - regardless of how encoding/csv's own
	// buffered reading retries after an (n>0, err!=nil) return, or
	// whether a later chunk happens to contain no further delim bytes
	// of its own to re-trip on.
	tripped error
}

func newFieldCountLimitedReader(r io.Reader, delim byte, max int) *fieldCountLimitedReader {
	return &fieldCountLimitedReader{r: r, delim: delim, max: max, atFieldStart: true}
}

func (f *fieldCountLimitedReader) Read(p []byte) (int, error) {
	if f.tripped != nil {
		return 0, f.tripped
	}
	n, err := f.r.Read(p)
	for _, b := range p[:n] {
		if f.afterQuoteInQuotes {
			f.afterQuoteInQuotes = false
			if b == '"' {
				// A doubled quote: still inQuotes, this byte is
				// ordinary escaped content, not a new field start.
				continue
			}
			f.inQuotes = false
			// Fall through: b is the first byte after the field's
			// real closing quote, processed normally below (it's
			// usually delim or '\n', but anything else - malformed,
			// unquoted trailing content - is just field content).
		}
		if f.inQuotes {
			if b == '"' {
				f.afterQuoteInQuotes = true
			}
			continue
		}
		if f.atFieldStart && b == '"' {
			f.inQuotes = true
			f.atFieldStart = false
			continue
		}
		if b == f.delim {
			f.count++
			f.atFieldStart = true
			if f.count > f.max {
				f.tripped = fmt.Errorf("a single record contains over %d delimiter-separated fields, over the limit for a v1 read", f.max)
				return n, f.tripped
			}
			continue
		}
		if b == '\n' {
			f.atFieldStart = true
			continue
		}
		f.atFieldStart = false
	}
	return n, err
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
