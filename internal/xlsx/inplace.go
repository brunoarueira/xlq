package xlsx

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/brunoarueira/xlq/internal/filter"
)

// cellRefPattern matches a single A1-style cell reference, e.g. "A1" -
// the same shape eval.go's own cellReference regex recognizes, kept as
// a separate copy here since this package has no dependency on
// internal/eval (and shouldn't gain one just for this).
var cellRefPattern = regexp.MustCompile(`^[A-Za-z]+[0-9]+$`)

var errBadTarget = fmt.Errorf(`xlsx: --in-place target must be a sheet and a single cell reference, e.g. .Sheet1.A1 - not a pipe, a whole row/column, a range, or a records step`)

// ResolveTarget resolves an Assign's Target (docs/adr/0014-in-place-editing.md)
// to a (sheet, cell) pair. Target must be exactly Field{Field{Identity,
// sheetName}, cellRef} - a sheet name followed by one A1 cell
// reference - the only shape that identifies one real, writable cell
// unambiguously. Anything else (a pipe, a whole row/column, a range, a
// records step, ...) is rejected here with a clear error, before ever
// reaching excelize.
func ResolveTarget(target filter.Expr) (sheet, cellRef string, err error) {
	outer, ok := target.(filter.Field)
	if !ok {
		return "", "", errBadTarget
	}
	inner, ok := outer.Base.(filter.Field)
	if !ok {
		return "", "", errBadTarget
	}
	if _, ok := inner.Base.(filter.Identity); !ok {
		return "", "", errBadTarget
	}
	if !cellRefPattern.MatchString(outer.Name) {
		return "", "", errBadTarget
	}
	return inner.Name, outer.Name, nil
}

// WriteCell opens the .xlsx file at path, sets sheet!cellRef to value
// (a string, float64, bool, or nil - Assign.Value's own possible
// types), and overwrites path with the result. See
// docs/adr/0014-in-place-editing.md: the whole file stays in
// excelize's own in-memory representation throughout, so everything
// SetCellValue doesn't touch - formatting, other cells, charts, merged
// ranges, anything else OOXML holds - survives untouched; nothing is
// ever reduced to model.Workbook and reconstructed from it.
//
// sheet is matched the same way the read path's eval.findSheet does:
// an exact match wins, otherwise the first case-insensitive match -
// so "xlq -i '.sheet1.A1 = ...'" and the read-only ".sheet1.A1" agree
// on which sheet "sheet1" means on a workbook whose sheet is actually
// named "Sheet1".
//
// The write goes to a temporary file in the same directory as path
// (so the final rename is same-filesystem), named with a literal
// ".xlsx" extension (excelize's SaveAs rejects any other extension),
// which is then re-opened as a sanity check, chmod'd to match path's
// own permissions (os.CreateTemp always creates 0600, and the rename
// below replaces the destination wholesale, so without this a
// successful edit would silently narrow e.g. an originally 0644 file
// to 0600), and only then renamed over path. If anything fails before
// that rename - the edit itself, the save, the re-open check, or the
// chmod - path is never touched.
func WriteCell(path, sheet, cellRef string, value any) error {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return fmt.Errorf("xlsx: open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	resolvedSheet, ok := findSheetName(f, sheet)
	if !ok {
		return fmt.Errorf("xlsx: sheet %q not found in %q", sheet, path)
	}

	if err := f.SetCellValue(resolvedSheet, cellRef, value); err != nil {
		return fmt.Errorf("xlsx: set %s!%s: %w", resolvedSheet, cellRef, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("xlsx: stat %q: %w", path, err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "xlq-tmp-*.xlsx")
	if err != nil {
		return fmt.Errorf("xlsx: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := f.SaveAs(tmpPath); err != nil {
		return fmt.Errorf("xlsx: save: %w", err)
	}

	verify, err := excelize.OpenFile(tmpPath)
	if err != nil {
		return fmt.Errorf("xlsx: verify written file: %w", err)
	}
	_ = verify.Close()

	if err := os.Chmod(tmpPath, info.Mode()); err != nil {
		return fmt.Errorf("xlsx: preserve file permissions: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("xlsx: replace %q: %w", path, err)
	}
	removeTemp = false
	return nil
}

// findSheetName resolves name against f's own sheet list - an exact
// match wins, otherwise the first case-insensitive match - matching
// internal/eval's findSheet semantics for the read path.
func findSheetName(f *excelize.File, name string) (string, bool) {
	list := f.GetSheetList()
	for _, s := range list {
		if s == name {
			return s, true
		}
	}
	for _, s := range list {
		if strings.EqualFold(s, name) {
			return s, true
		}
	}
	return "", false
}
