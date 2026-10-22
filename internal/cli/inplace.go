package cli

import (
	"fmt"
	"time"

	"github.com/brunoarueira/xlq/internal/filter"
	"github.com/brunoarueira/xlq/internal/input"
	"github.com/brunoarueira/xlq/internal/xlsx"
)

// runInPlace parses filterSrc as an assignment (ADR-0014) and applies
// it to the .xlsx file at path, overwriting it. format and delim are
// resolved the same way a read-only filter's are, except -i only ever
// supports xlsx: format must resolve to it (whether detected or
// explicit), and delim (a csv/tsv-only concept) must be unset. On
// success, nothing is printed - matching yq -i's own silence. timeout
// and panic recovery (#50, docs/adr/0030-recover-and-timeout-the-read-path.md)
// wrap xlsx.WriteCell, which opens and re-saves the file - the same
// excelize internals a read-only filter's own open/parse step risks.
func runInPlace(filterSrc, path string, format input.Format, delim rune, timeout time.Duration) error {
	resolved, err := input.ResolveFormat(path, format)
	if err != nil {
		return err
	}
	if resolved != input.XLSX {
		return fmt.Errorf("--in-place only supports .xlsx files, not %s", resolved)
	}
	if delim != 0 {
		return fmt.Errorf("--in-place: --delimiter has no meaning for .xlsx")
	}

	expr, err := filter.Parse(filterSrc)
	if err != nil {
		return err
	}
	assign, ok := expr.(filter.Assign)
	if !ok {
		return fmt.Errorf(`--in-place requires an assignment filter, e.g. '.Sheet1.A1 = "value"'`)
	}

	sheet, cellRef, err := xlsx.ResolveTarget(assign.Target)
	if err != nil {
		return err
	}
	return withRecoveryErr("writing", timeout, func() error {
		return xlsx.WriteCell(path, sheet, cellRef, assign.Value)
	})
}
