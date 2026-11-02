package xlsx

import (
	"archive/zip"
	"encoding/xml"
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
