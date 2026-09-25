// Package model defines xlq's internal spreadsheet representation:
// Workbook, Sheet, Row, and Cell. It is independent of any one
// spreadsheet file format - readers (xlsx, csv, ...) convert into it,
// and the query engine operates only on these types. See
// docs/adr/0007-spreadsheet-data-model.md for the design rationale.
package model

import "time"

// CellKind identifies which of a Cell's typed values is meaningful.
type CellKind int

const (
	// Empty marks a cell that is present but holds no value, e.g. a
	// formula that evaluated to blank, or a formatted-but-unset cell.
	Empty CellKind = iota
	String
	Number
	Bool
	Date
)

// String returns a lower-case name for the kind, e.g. "number".
func (k CellKind) String() string {
	switch k {
	case Empty:
		return "empty"
	case String:
		return "string"
	case Number:
		return "number"
	case Bool:
		return "bool"
	case Date:
		return "date"
	default:
		return "unknown"
	}
}

// Cell is a single spreadsheet cell. Kind selects which typed
// accessor below is meaningful. A formula-derived cell has Kind and
// value set to its *computed* result; Formula holds the raw
// expression separately.
type Cell struct {
	// Column is this cell's 0-based column index within its Row.
	Column int
	Kind   CellKind
	// Formula is the raw expression, e.g. "=SUM(A1:A2)". Empty for a
	// cell that isn't formula-derived.
	Formula string

	value any // string | float64 | bool | time.Time, per Kind; nil for Empty
}

// NewEmptyCell returns an empty cell at column 0.
func NewEmptyCell() Cell { return Cell{Kind: Empty} }

// NewStringCell returns a string-valued cell at column 0.
func NewStringCell(v string) Cell { return Cell{Kind: String, value: v} }

// NewNumberCell returns a number-valued cell at column 0.
func NewNumberCell(v float64) Cell { return Cell{Kind: Number, value: v} }

// NewBoolCell returns a bool-valued cell at column 0.
func NewBoolCell(v bool) Cell { return Cell{Kind: Bool, value: v} }

// NewDateCell returns a date-valued cell at column 0.
func NewDateCell(v time.Time) Cell { return Cell{Kind: Date, value: v} }

// At returns a copy of c placed at the given 0-based column index.
func (c Cell) At(column int) Cell {
	c.Column = column
	return c
}

// WithFormula returns a copy of c with its raw formula expression
// set. Kind and the computed value are unchanged - they describe the
// formula's result, not its source.
func (c Cell) WithFormula(expr string) Cell {
	c.Formula = expr
	return c
}

// IsFormula reports whether the cell's value came from a formula.
func (c Cell) IsFormula() bool { return c.Formula != "" }

// StringValue returns the cell's value as a string, and whether Kind
// was actually String.
func (c Cell) StringValue() (string, bool) {
	if c.Kind != String {
		return "", false
	}
	v, ok := c.value.(string)
	return v, ok
}

// NumberValue returns the cell's value as a float64, and whether Kind
// was actually Number.
func (c Cell) NumberValue() (float64, bool) {
	if c.Kind != Number {
		return 0, false
	}
	v, ok := c.value.(float64)
	return v, ok
}

// BoolValue returns the cell's value as a bool, and whether Kind was
// actually Bool.
func (c Cell) BoolValue() (bool, bool) {
	if c.Kind != Bool {
		return false, false
	}
	v, ok := c.value.(bool)
	return v, ok
}

// DateValue returns the cell's value as a time.Time, and whether Kind
// was actually Date.
func (c Cell) DateValue() (time.Time, bool) {
	if c.Kind != Date {
		return time.Time{}, false
	}
	v, ok := c.value.(time.Time)
	return v, ok
}

// Row is one sparse row of a Sheet: only cells with a value are
// present, ordered ascending by Column.
type Row struct {
	// Index is this row's 0-based index within its Sheet.
	Index int
	Cells []Cell
}

// Sheet is one sparse sheet in a Workbook: only rows containing at
// least one cell are present, ordered ascending by Index.
type Sheet struct {
	Name string
	Rows []Row
}

// Dimensions returns the sheet's bounding box as one past the highest
// row and column index present (0, 0 for an empty sheet). Sheet is
// sparse, so this does not imply every cell within the box exists.
func (s Sheet) Dimensions() (rows, cols int) {
	for _, r := range s.Rows {
		if r.Index+1 > rows {
			rows = r.Index + 1
		}
		for _, c := range r.Cells {
			if c.Column+1 > cols {
				cols = c.Column + 1
			}
		}
	}
	return rows, cols
}

// Workbook is the top-level container: an ordered list of Sheets.
type Workbook struct {
	Sheets []Sheet
}
