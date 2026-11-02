package xlsx

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

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

// sharedStringEntryOverhead is a conservative estimate of a
// []string element's own fixed memory cost - a string header
// (pointer + length, 16 bytes on a 64-bit system) - independent of
// its content's own byte length. maxSize alone bounds the raw XML
// byte count read to produce the result, but not the result's own
// memory footprint: many thousands of tiny <si> elements (each
// costing only a handful of raw XML bytes, e.g. "<si><t/></si>") can
// still materialize far more total memory than that raw byte count
// once this fixed per-entry cost is multiplied by an attacker-chosen
// element count. Counting it as sharedStrings streams keeps the
// resolved slice's own real memory footprint under maxSize too, not
// just the bytes read to build it.
const sharedStringEntryOverhead = 16

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
// it is never read unbounded. <si> elements are streamed one at a
// time directly into the result (rather than decoding the whole
// document into an intermediate struct first, then copying out of
// it) so sharedStringEntryOverhead's own running budget can reject
// an oversized result as it grows, not only after it's already fully
// materialized.
func sharedStrings(zr *zip.Reader, maxSize int64) ([]string, error) {
	f := findZipEntry(zr, "xl/sharedStrings.xml")
	if f == nil {
		return nil, nil
	}
	if maxSize > 0 && int64(f.UncompressedSize64) > maxSize {
		return nil, fmt.Errorf("xlsx: %s is %d bytes uncompressed, over the %d-byte limit for a v1 read", f.Name, f.UncompressedSize64, maxSize)
	}

	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("xlsx: open %s: %w", f.Name, err)
	}
	defer func() { _ = rc.Close() }()

	var r io.Reader = rc
	var limited *io.LimitedReader
	if maxSize > 0 {
		limited = &io.LimitedReader{R: rc, N: maxSize + 1}
		r = limited
	}

	overLimit := func() bool { return limited != nil && limited.N <= 0 }

	var strs []string
	var materialized int64
	dec := xml.NewDecoder(r)
	for {
		tok, tokErr := dec.Token()
		if tokErr == io.EOF {
			break
		}
		if tokErr != nil {
			if overLimit() {
				return nil, fmt.Errorf("xlsx: %s exceeds the %d-byte limit for a v1 read", f.Name, maxSize)
			}
			return nil, fmt.Errorf("xlsx: parse %s: %w", f.Name, tokErr)
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "si" {
			continue
		}

		var item sharedStringItem
		if err := dec.DecodeElement(&item, &start); err != nil {
			if overLimit() {
				return nil, fmt.Errorf("xlsx: %s exceeds the %d-byte limit for a v1 read", f.Name, maxSize)
			}
			return nil, fmt.Errorf("xlsx: parse %s: %w", f.Name, err)
		}

		text := item.text()
		materialized += int64(len(text)) + sharedStringEntryOverhead
		if maxSize > 0 && materialized > maxSize {
			return nil, fmt.Errorf("xlsx: %s's resolved shared strings exceed the %d-byte limit for a v1 read", f.Name, maxSize)
		}
		strs = append(strs, text)
	}
	if overLimit() {
		return nil, fmt.Errorf("xlsx: %s exceeds the %d-byte limit for a v1 read", f.Name, maxSize)
	}
	return strs, nil
}
