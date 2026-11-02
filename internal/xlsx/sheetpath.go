package xlsx

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strings"
)

// workbookXML mirrors just enough of xl/workbook.xml's structure to
// read each sheet's name and relationship ID, in the same order
// GetSheetList() (excelize's own, still used by Read's own upfront
// OpenFile call, ADR-0048) returns them. Struct tags intentionally
// omit namespaces, matching internal/ods's own convention
// (docs/adr/0010): encoding/xml matches an element or attribute by
// local name when no namespace is given, so "r:id" (the relationship-
// ID attribute's own namespaced form) is matched via its local name
// "id" regardless of which namespace prefix a given producer used.
type workbookXML struct {
	Sheets []workbookSheet `xml:"sheets>sheet"`
}

type workbookSheet struct {
	Name string `xml:"name,attr"`
	RID  string `xml:"id,attr"`
}

// workbookRelsXML mirrors xl/_rels/workbook.xml.rels: the mapping
// from a relationship ID (what a <sheet> element's own r:id attribute
// names) to the actual zip entry path, relative to xl/, that
// relationship ID resolves to.
type workbookRelsXML struct {
	Relationships []workbookRelationship `xml:"Relationship"`
}

type workbookRelationship struct {
	ID     string `xml:"Id,attr"`
	Target string `xml:"Target,attr"`
}

// sheetXMLPath resolves sheetName to its worksheet XML's path inside
// zr (e.g. "xl/worksheets/sheet2.xml"), via xl/workbook.xml's own
// <sheet name="..." r:id="..."/> list and xl/_rels/workbook.xml.rels'
// own relationship-ID-to-Target mapping - never a naming convention
// (sheet1.xml matching the first sheet, and so on). Confirmed
// directly that convention doesn't hold in practice: a workbook whose
// sheets were reordered, renamed, or had an earlier one deleted can
// have its two remaining sheets named sheet2.xml and sheet3.xml, with
// no sheet1.xml in the zip at all - this always resolves through the
// real relationship, never guesses from position. maxSize (#51,
// docs/adr/0031-memory-ceiling-for-untrusted-files.md) bounds how
// large workbook.xml and the rels part are each allowed to decompress
// to, the same way every other part this new reader touches is
// bounded (ADR-0048/ADR-0047) - <= 0 disables it.
//
// sheetName is matched exactly, not case-insensitively: unlike a
// user-supplied filter reference (eval.findSheet's own case-
// insensitive fallback for that), every caller of this function
// already has an exact name from excelize's own GetSheetList()
// (Read's upfront OpenFile call, kept per ADR-0048) - this never
// needs to guess which of several case-variant names a filter meant.
func sheetXMLPath(zr *zip.Reader, sheetName string, maxSize int64) (string, error) {
	var wb workbookXML
	if err := decodeZipEntry(zr, "xl/workbook.xml", &wb, maxSize); err != nil {
		return "", err
	}

	var rID string
	found := false
	for _, s := range wb.Sheets {
		if s.Name == sheetName {
			rID = s.RID
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("xlsx: sheet %q not found in xl/workbook.xml", sheetName)
	}

	var rels workbookRelsXML
	if err := decodeZipEntry(zr, "xl/_rels/workbook.xml.rels", &rels, maxSize); err != nil {
		return "", err
	}
	for _, r := range rels.Relationships {
		if r.ID == rID {
			return resolveRelationshipTarget(r.Target), nil
		}
	}
	return "", fmt.Errorf("xlsx: sheet %q's relationship %q not found in xl/_rels/workbook.xml.rels", sheetName, rID)
}

// resolveRelationshipTarget resolves target - an OPC relative
// reference, found in xl/_rels/workbook.xml.rels, not a literal
// suffix of the source part's own path - against xl/workbook.xml,
// the part that contains that rels file. Confirmed directly against
// real excelize-generated output that the simple, common case
// ("worksheets/sheet2.xml") resolves to exactly what naive string
// concatenation would also produce, but a spec-valid target isn't
// always that simple: a package-absolute target (a leading "/", e.g.
// "/xl/worksheets/sheet1.xml") is relative to the zip root, not to
// xl/, and a target using ".." segments must have those resolved, not
// left as literal path components a zip entry's own name would never
// actually contain.
func resolveRelationshipTarget(target string) string {
	if abs, ok := strings.CutPrefix(target, "/"); ok {
		return abs
	}
	return path.Join("xl", target)
}

// decodeZipEntry finds the zip entry named name within zr and decodes
// it as XML into v, or returns a clear error if the entry is missing,
// isn't valid XML, or exceeds maxSize decompressed. maxSize <= 0
// disables the size check - the file's own declared UncompressedSize
// can't be trusted on its own (it's just a header value), so the
// actual bytes read are capped too, the same two-layer check
// internal/ods's own decodeEntryWithLimit already uses for ODS.
func decodeZipEntry(zr *zip.Reader, name string, v any, maxSize int64) error {
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		if maxSize > 0 && int64(f.UncompressedSize64) > maxSize {
			return fmt.Errorf("xlsx: %s is %d bytes uncompressed, over the %d-byte limit for a v1 read", name, f.UncompressedSize64, maxSize)
		}

		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("xlsx: open %s: %w", name, err)
		}
		defer func() { _ = rc.Close() }()

		var r io.Reader = rc
		var limited *io.LimitedReader
		if maxSize > 0 {
			limited = &io.LimitedReader{R: rc, N: maxSize + 1}
			r = limited
		}
		if err := xml.NewDecoder(r).Decode(v); err != nil {
			return fmt.Errorf("xlsx: parse %s: %w", name, err)
		}
		if limited != nil && limited.N <= 0 {
			return fmt.Errorf("xlsx: %s exceeds the %d-byte limit for a v1 read", name, maxSize)
		}
		return nil
	}
	return fmt.Errorf("xlsx: %s not found in archive", name)
}
