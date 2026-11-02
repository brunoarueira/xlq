package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"math"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestSharedStringItemPlainText(t *testing.T) {
	var si sharedStringItem
	must(t, xml.Unmarshal([]byte(`<si><t>plain</t></si>`), &si))
	if got := si.text(); got != "plain" {
		t.Errorf("text() = %q, want %q", got, "plain")
	}
}

// TestSharedStringItemRichText confirms a rich-text <si> (multiple
// <r><t>...</t></r> runs, each with its own formatting via <rPr> -
// irrelevant to the value itself) concatenates every run's text in
// order, matching excelize's own xlsxSI.String().
func TestSharedStringItemRichText(t *testing.T) {
	var si sharedStringItem
	must(t, xml.Unmarshal([]byte(`<si><r><rPr><b val="1"></b></rPr><t xml:space="preserve">bold </t></r><r><t>normal</t></r></si>`), &si))
	if got := si.text(); got != "bold normal" {
		t.Errorf("text() = %q, want %q", got, "bold normal")
	}
}

// TestSharedStringItemExcludesPhoneticRun guards the exact reason
// sharedStringItem is a structured type, not a generic "concatenate
// every CharData regardless of element" unmarshaler (the way
// internal/ods's own paragraphText handles ODF's inline formatting):
// confirmed directly against excelize's own xlsxSI.String() source
// that a phonetic guide run (<rPh>, e.g. furigana reading-aid text
// for East Asian scripts) is deliberately excluded from the value -
// a generic approach would incorrectly fold it in.
func TestSharedStringItemExcludesPhoneticRun(t *testing.T) {
	var si sharedStringItem
	must(t, xml.Unmarshal([]byte(`<si><t>本</t><rPh sb="0" eb="1"><t>ホン</t></rPh></si>`), &si))
	if got := si.text(); got != "本" {
		t.Errorf("text() = %q, want %q (not the phonetic guide folded in)", got, "本")
	}
}

func TestUnescapeBstr(t *testing.T) {
	tests := []struct{ in, want string }{
		// No escape present - returned unchanged (the common case).
		{"plain text", "plain text"},
		// A real bstr escape: ADR-0031-style control character OOXML
		// can't express even via a numeric character reference.
		{"before_x0007_after", "before\aafter"},
		// "_x005F_" is itself the escaped form of a literal
		// underscore - guards against a real "_x0041_"-shaped
		// substring round-tripping as if it were an escape.
		{"literal_x005F_x0041_underscore", "literal_x0041_underscore"},
	}
	for _, tt := range tests {
		if got := unescapeBstr(tt.in); got != tt.want {
			t.Errorf("unescapeBstr(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// openZipReader is openZip (sheetpath_test.go) returning just the
// *zip.Reader, for tests that don't need the *zip.ReadCloser itself.
func openZipReader(t *testing.T, path string) *zip.Reader {
	t.Helper()
	return &openZip(t, path).Reader
}

// TestSharedStringsEndToEnd confirms sharedStrings resolves a real,
// excelize-written workbook's shared-strings table correctly end to
// end: plain text, a duplicate value correctly reusing one shared
// index rather than appearing twice, rich text, and whitespace
// preservation (xml:space="preserve" - confirmed directly that
// Go's own XML decoder already preserves CharData as written
// regardless of this attribute, so no extra handling is needed, only
// no accidental trimming).
func TestSharedStringsEndToEnd(t *testing.T) {
	path := build(t, func(f *excelize.File) {
		must(t, f.SetCellValue("Sheet1", "A1", "plain"))
		must(t, f.SetCellValue("Sheet1", "A2", "  leading and trailing  "))
		must(t, f.SetCellValue("Sheet1", "A3", "plain")) // reuses A1's index
		must(t, f.SetCellRichText("Sheet1", "A4", []excelize.RichTextRun{
			{Text: "bold "}, {Text: "normal"},
		}))
	})
	zr := openZipReader(t, path)

	strs, err := sharedStrings(zr, 0)
	if err != nil {
		t.Fatalf("sharedStrings: %v", err)
	}
	if len(strs) != 3 {
		t.Fatalf("len(strs) = %d, want 3 (plain, spaced, rich - A3 reuses index 0)", len(strs))
	}
	if strs[0] != "plain" {
		t.Errorf("strs[0] = %q, want %q", strs[0], "plain")
	}
	if strs[1] != "  leading and trailing  " {
		t.Errorf("strs[1] = %q, want %q", strs[1], "  leading and trailing  ")
	}
	if strs[2] != "bold normal" {
		t.Errorf("strs[2] = %q, want %q", strs[2], "bold normal")
	}
}

// TestSharedStringsMissingPartIsNotAnError guards a legitimately
// optional part: confirmed directly that a workbook with no string
// cells at all has no xl/sharedStrings.xml entry in its zip.
func TestSharedStringsMissingPartIsNotAnError(t *testing.T) {
	path := build(t, func(f *excelize.File) {
		must(t, f.SetCellValue("Sheet1", "A1", 42))
	})
	zr := openZipReader(t, path)

	strs, err := sharedStrings(zr, 0)
	if err != nil {
		t.Fatalf("sharedStrings on a numbers-only workbook: %v", err)
	}
	if strs != nil {
		t.Errorf("strs = %v, want nil", strs)
	}
}

// TestSharedStringsRejectsOversizedPart confirms sharedStrings
// actually threads maxSize through to decodeZipFile, rather than
// reading the part unbounded - decodeZipFile's own test suite
// (sheetpath_test.go) already covers the two-layer check itself in
// depth.
func TestSharedStringsRejectsOversizedPart(t *testing.T) {
	path := build(t, func(f *excelize.File) {
		must(t, f.SetCellValue("Sheet1", "A1", "plain"))
	})
	zr := openZipReader(t, path)

	if _, err := sharedStrings(zr, 1); err == nil {
		t.Fatal("sharedStrings with maxSize=1: want an error, got nil")
	}
}

// TestSharedStringsRejectsManyTinyEntries guards the materialized-
// memory budget sharedStringEntryOverhead enforces: a workbook's
// declared and actual raw XML byte count can stay well within
// maxSize while its *resolved* []string still costs far more real
// memory than that, once a large enough element count's own fixed
// per-string overhead is added up - a minimal "<si></si>" entry
// costs only 9 raw bytes but a full 16-byte string-header slot in
// the result, so enough of them blow the materialized budget long
// before the raw byte count would.
func TestSharedStringsRejectsManyTinyEntries(t *testing.T) {
	var xmlBuf bytes.Buffer
	xmlBuf.WriteString(`<?xml version="1.0" encoding="UTF-8"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	const n = 100
	for range n {
		xmlBuf.WriteString(`<si></si>`)
	}
	xmlBuf.WriteString(`</sst>`)

	const maxSize = 1200
	if int64(xmlBuf.Len()) >= maxSize {
		t.Fatalf("test fixture itself is %d bytes, not under maxSize=%d - adjust n", xmlBuf.Len(), maxSize)
	}
	if n*sharedStringEntryOverhead <= maxSize {
		t.Fatalf("n*overhead = %d, want it over maxSize=%d - adjust n", n*sharedStringEntryOverhead, maxSize)
	}

	zr := buildRawZip(t, "xl/sharedStrings.xml", xmlBuf.Bytes())

	_, err := sharedStrings(zr, maxSize)
	if err == nil {
		t.Fatalf("sharedStrings with %d tiny entries (raw bytes %d < maxSize %d, but n*overhead %d > maxSize): want an error, got nil", n, xmlBuf.Len(), maxSize, n*sharedStringEntryOverhead)
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Errorf("error = %q, want it to mention the limit", err)
	}
}

// TestSharedStringsIgnoresNestedSI guards sharedStrings' own depth
// tracking: a <si>-named element isn't only ever a direct child of
// the root in a maliciously crafted (if still well-formed) file - an
// <extLst> extension block (a genuine OOXML extensibility point that
// can legitimately appear as a sibling of real <si> entries) wrapping
// one here stands in for that. Without tracking depth, a naive "any
// <si> anywhere" match would treat the nested one as its own entry,
// shifting index 0 onto the wrong text.
func TestSharedStringsIgnoresNestedSI(t *testing.T) {
	xmlContent := []byte(`<?xml version="1.0" encoding="UTF-8"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><extLst><ext><si>nested, not a real entry</si></ext></extLst><si><t>real</t></si></sst>`)
	zr := buildRawZip(t, "xl/sharedStrings.xml", xmlContent)

	strs, err := sharedStrings(zr, 0)
	if err != nil {
		t.Fatalf("sharedStrings: %v", err)
	}
	if len(strs) != 1 {
		t.Fatalf("len(strs) = %d, want 1 (the nested <si> must not count as its own entry)", len(strs))
	}
	if strs[0] != "real" {
		t.Errorf("strs[0] = %q, want %q", strs[0], "real")
	}
}

// TestLimitedReaderNNoOverflow guards limitedReaderN against the
// one input where a plain maxSize+1 silently wraps negative:
// math.MaxInt64, the largest representable value, where there's no
// larger one to ask for anyway.
func TestLimitedReaderNNoOverflow(t *testing.T) {
	if got := limitedReaderN(math.MaxInt64); got <= 0 {
		t.Errorf("limitedReaderN(MaxInt64) = %d, want a positive value (not an overflow)", got)
	}
	if got := limitedReaderN(100); got != 101 {
		t.Errorf("limitedReaderN(100) = %d, want 101", got)
	}
}

// TestSharedStringsMaxInt64DoesNotOverflow is the literal reported
// bug, end to end: maxSize == math.MaxInt64 must still read a tiny,
// valid part successfully, not reject it as oversized.
func TestSharedStringsMaxInt64DoesNotOverflow(t *testing.T) {
	path := build(t, func(f *excelize.File) {
		must(t, f.SetCellValue("Sheet1", "A1", "plain"))
	})
	zr := openZipReader(t, path)

	strs, err := sharedStrings(zr, math.MaxInt64)
	if err != nil {
		t.Fatalf("sharedStrings with maxSize=math.MaxInt64: %v", err)
	}
	if len(strs) != 1 || strs[0] != "plain" {
		t.Errorf("strs = %v, want [\"plain\"]", strs)
	}
}

// buildRawZip builds a single-entry, in-memory zip (Store method, no
// declared-size lies) and returns it as a *zip.Reader.
func buildRawZip(t *testing.T, name string, content []byte) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatalf("zip Create: %v", err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	must(t, zw.Close())

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	return zr
}
