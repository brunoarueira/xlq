package xlsx

import (
	"testing"
)

func TestStreamWorksheetRowsBasic(t *testing.T) {
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetData>
<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1"><v>42.5</v></c></row>
<row r="3"><c r="C3"><v>9</v></c></row>
</sheetData>
</worksheet>`
	zr := buildRawZip(t, "xl/worksheets/sheet1.xml", []byte(xmlContent))
	f := findZipEntry(zr, "xl/worksheets/sheet1.xml")
	if f == nil {
		t.Fatal("entry not found")
	}

	type gotRow struct {
		rowIndex int
		cols     []int
	}
	var rows []gotRow
	streamWorksheetRows(f, 0, func(rowIndex int, cells []resolvedCell, err error) bool {
		if err != nil {
			t.Fatalf("streamWorksheetRows: %v", err)
		}
		var cols []int
		for _, c := range cells {
			cols = append(cols, c.col)
		}
		rows = append(rows, gotRow{rowIndex, cols})
		return true
	})

	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].rowIndex != 0 || len(rows[0].cols) != 2 || rows[0].cols[0] != 0 || rows[0].cols[1] != 1 {
		t.Errorf("rows[0] = %+v, want rowIndex=0, cols=[0,1]", rows[0])
	}
	if rows[1].rowIndex != 2 || len(rows[1].cols) != 1 || rows[1].cols[0] != 2 {
		t.Errorf("rows[1] = %+v, want rowIndex=2, cols=[2]", rows[1])
	}
}

// TestStreamWorksheetRowsSequentialColumnFallback guards the one
// subtlety excelize's own rowXMLHandler handles that a naive reader
// might not: a <c> with no r attribute at all falls back to the
// column immediately after the previous cell's own, mirrored here
// from its source rather than assumed.
func TestStreamWorksheetRowsSequentialColumnFallback(t *testing.T) {
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetData>
<row r="1"><c><v>1</v></c><c><v>2</v></c><c><v>3</v></c></row>
</sheetData>
</worksheet>`
	zr := buildRawZip(t, "xl/worksheets/sheet1.xml", []byte(xmlContent))
	f := findZipEntry(zr, "xl/worksheets/sheet1.xml")

	var cols []int
	streamWorksheetRows(f, 0, func(rowIndex int, cells []resolvedCell, err error) bool {
		if err != nil {
			t.Fatalf("streamWorksheetRows: %v", err)
		}
		for _, c := range cells {
			cols = append(cols, c.col)
		}
		return true
	})
	want := []int{0, 1, 2}
	if len(cols) != len(want) {
		t.Fatalf("cols = %v, want %v", cols, want)
	}
	for i := range want {
		if cols[i] != want[i] {
			t.Errorf("cols[%d] = %d, want %d", i, cols[i], want[i])
		}
	}
}

func TestStreamWorksheetRowsCaptureStyleTypeFormula(t *testing.T) {
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetData>
<row r="1"><c r="A1" s="5" t="str"><f>A2</f><v>hi</v></c></row>
</sheetData>
</worksheet>`
	zr := buildRawZip(t, "xl/worksheets/sheet1.xml", []byte(xmlContent))
	f := findZipEntry(zr, "xl/worksheets/sheet1.xml")

	var got resolvedCell
	streamWorksheetRows(f, 0, func(rowIndex int, cells []resolvedCell, err error) bool {
		if err != nil {
			t.Fatalf("streamWorksheetRows: %v", err)
		}
		if len(cells) != 1 {
			t.Fatalf("len(cells) = %d, want 1", len(cells))
		}
		got = cells[0]
		return true
	})
	if got.styleID != 5 {
		t.Errorf("styleID = %d, want 5", got.styleID)
	}
	if got.t != "str" {
		t.Errorf("t = %q, want %q", got.t, "str")
	}
	if !got.hasF || got.formulaText != "A2" {
		t.Errorf("hasF=%v formulaText=%q, want true, %q", got.hasF, got.formulaText, "A2")
	}
	if !got.hasV || got.v != "hi" {
		t.Errorf("hasV=%v v=%q, want true, %q", got.hasV, got.v, "hi")
	}
}

func TestStreamWorksheetRowsInlineString(t *testing.T) {
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetData>
<row r="1"><c r="A1" t="inlineStr"><is><t>plain</t></is></c></row>
</sheetData>
</worksheet>`
	zr := buildRawZip(t, "xl/worksheets/sheet1.xml", []byte(xmlContent))
	f := findZipEntry(zr, "xl/worksheets/sheet1.xml")

	var got resolvedCell
	streamWorksheetRows(f, 0, func(rowIndex int, cells []resolvedCell, err error) bool {
		if err != nil {
			t.Fatalf("streamWorksheetRows: %v", err)
		}
		got = cells[0]
		return true
	})
	if got.is == nil {
		t.Fatal("is = nil, want non-nil")
	}
	if text := got.is.text(); text != "plain" {
		t.Errorf("is.text() = %q, want %q", text, "plain")
	}
}

// TestStreamWorksheetRowsSkipsEmptyRow guards the same skip-if-empty
// convention the old excelize-Rows-based loop relied on: a row with
// no cells carrying a value or formula (here, none at all) isn't
// yielded.
func TestStreamWorksheetRowsSkipsEmptyRow(t *testing.T) {
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetData>
<row r="1"></row>
<row r="2"><c r="A2"><v>1</v></c></row>
</sheetData>
</worksheet>`
	zr := buildRawZip(t, "xl/worksheets/sheet1.xml", []byte(xmlContent))
	f := findZipEntry(zr, "xl/worksheets/sheet1.xml")

	var rowIndices []int
	streamWorksheetRows(f, 0, func(rowIndex int, cells []resolvedCell, err error) bool {
		if err != nil {
			t.Fatalf("streamWorksheetRows: %v", err)
		}
		rowIndices = append(rowIndices, rowIndex)
		return true
	})
	if len(rowIndices) != 1 || rowIndices[0] != 1 {
		t.Errorf("rowIndices = %v, want [1]", rowIndices)
	}
}

// TestStreamWorksheetRowsIgnoresNestedRowAndCell guards the same
// depth-tracking concern #168's own TestSharedStringsIgnoresNestedSI
// does: a <row>- or <c>-named element nested somewhere other than a
// direct child of <sheetData>/<row> must not be misread as real
// sheet data. <extLst> stands in for a genuine OOXML extensibility
// point that can legitimately appear as a sheetData sibling.
func TestStreamWorksheetRowsIgnoresNestedRowAndCell(t *testing.T) {
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<extLst><ext><row r="99"><c r="Z99"><v>fake</v></c></row></ext></extLst>
<sheetData>
<row r="1"><c r="A1"><v>real</v></c></row>
</sheetData>
</worksheet>`
	zr := buildRawZip(t, "xl/worksheets/sheet1.xml", []byte(xmlContent))
	f := findZipEntry(zr, "xl/worksheets/sheet1.xml")

	var rowIndices []int
	streamWorksheetRows(f, 0, func(rowIndex int, cells []resolvedCell, err error) bool {
		if err != nil {
			t.Fatalf("streamWorksheetRows: %v", err)
		}
		rowIndices = append(rowIndices, rowIndex)
		return true
	})
	if len(rowIndices) != 1 || rowIndices[0] != 0 {
		t.Errorf("rowIndices = %v, want [0] (the fake nested row/cell must not count)", rowIndices)
	}
}

func TestStreamWorksheetRowsStopsEarly(t *testing.T) {
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetData>
<row r="1"><c r="A1"><v>1</v></c></row>
<row r="2"><c r="A2"><v>2</v></c></row>
<row r="3"><c r="A3"><v>3</v></c></row>
</sheetData>
</worksheet>`
	zr := buildRawZip(t, "xl/worksheets/sheet1.xml", []byte(xmlContent))
	f := findZipEntry(zr, "xl/worksheets/sheet1.xml")

	var rowIndices []int
	streamWorksheetRows(f, 0, func(rowIndex int, cells []resolvedCell, err error) bool {
		rowIndices = append(rowIndices, rowIndex)
		return len(rowIndices) < 2 // stop after the second row
	})
	if len(rowIndices) != 2 {
		t.Errorf("rowIndices = %v, want 2 entries (stopped early)", rowIndices)
	}
}

func TestStreamWorksheetRowsRejectsOversizedPart(t *testing.T) {
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetData>
<row r="1"><c r="A1"><v>1</v></c></row>
</sheetData>
</worksheet>`
	zr := buildRawZip(t, "xl/worksheets/sheet1.xml", []byte(xmlContent))
	f := findZipEntry(zr, "xl/worksheets/sheet1.xml")

	var gotErr error
	streamWorksheetRows(f, 1, func(rowIndex int, cells []resolvedCell, err error) bool {
		gotErr = err
		return false
	})
	if gotErr == nil {
		t.Fatal("streamWorksheetRows with maxSize=1: want an error, got nil")
	}
}
