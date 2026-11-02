package xlsx

import (
	"archive/zip"
	"regexp"
	"strconv"
	"strings"
)

// sharedStringsXML mirrors xl/sharedStrings.xml's <sst><si>...</si>
// list - the text a shared-string cell's raw numeric <v> indexes
// into, 0-based, in document order.
type sharedStringsXML struct {
	Items []sharedStringItem `xml:"si"`
}

// sharedStringItem mirrors excelize's own xlsxSI.String() (confirmed
// directly against its source, cell.go): an <si> entry's text is its
// own direct <t>, plus each rich-text <r><t>...</t></r> run's text,
// concatenated in document order - explicitly excluding <rPh>
// (phonetic run) and <phoneticPr> elements, which hold reading-aid
// annotations (furigana and similar), not the cell's own value. A
// naive "concatenate every CharData regardless of element" approach
// (the way internal/ods's own paragraphText handles ODF's nested
// inline formatting) would incorrectly fold phonetic annotations
// into the value here, since OOXML - unlike ODF - uses dedicated
// sibling elements for them rather than nesting them inside the same
// run.
type sharedStringItem struct {
	T string            `xml:"t"`
	R []sharedStringRun `xml:"r"`
}

type sharedStringRun struct {
	T string `xml:"t"`
}

// text returns si's resolved value, matching excelize's own
// xlsxSI.String() exactly: direct <t> first, then each run's <t> in
// order, with OOXML's "_xHHHH_" control-character escapes (bstr
// escaping - how Office represents a character XML itself can't
// express even via a numeric character reference, such as a raw
// SOH) resolved back to the literal character.
func (si sharedStringItem) text() string {
	var sb strings.Builder
	sb.WriteString(si.T)
	for _, r := range si.R {
		sb.WriteString(r.T)
	}
	return unescapeBstr(sb.String())
}

// bstrEscape matches excelize's own bstrExp (lib.go) exactly:
// "_x", 4 hex digits, "_".
var bstrEscape = regexp.MustCompile(`_x[0-9a-fA-F]{4}_`)

// unescapeBstr reverses OOXML's bstr escaping, matching excelize's
// own bstrUnmarshal (lib.go) exactly, confirmed directly against its
// source rather than assumed: "_x005F_" is itself an escaped literal
// underscore (so a real "_x0041_"-shaped substring that was never
// meant as an escape can still round-trip), and every other match is
// a Unicode code point's 4 hex digits.
func unescapeBstr(s string) string {
	if !strings.Contains(s, "_x") {
		return s
	}
	return bstrEscape.ReplaceAllStringFunc(s, func(m string) string {
		if m == "_x005F_" {
			return "_"
		}
		v, err := strconv.Unquote(`"\u` + m[2:6] + `"`)
		if err != nil {
			return m
		}
		return v
	})
}

// sharedStrings parses xl/sharedStrings.xml from zr into its
// resolved text, indexed exactly as a shared-string cell's raw <v>
// does (0-based, document order). The part is legitimately optional -
// a workbook with no string cells at all can have no sharedStrings.xml
// in its zip (confirmed directly by building one) - so a missing part
// returns a nil slice, not an error.
//
// maxSize bounds how large the part may decompress to, the same
// --max-size-aware bound every other part this new reader touches
// gets (ADR-0048); maxSize <= 0 disables it. This table can be the
// single largest part of a string-heavy workbook - ADR-0047's own
// investigation measured ~62.6 MiB for a real 105,012-row file - so
// it is never read unbounded.
func sharedStrings(zr *zip.Reader, maxSize int64) ([]string, error) {
	f := findZipEntry(zr, "xl/sharedStrings.xml")
	if f == nil {
		return nil, nil
	}

	var sst sharedStringsXML
	if err := decodeZipFile(f, &sst, maxSize); err != nil {
		return nil, err
	}

	strs := make([]string, len(sst.Items))
	for i, item := range sst.Items {
		strs[i] = item.text()
	}
	return strs, nil
}
