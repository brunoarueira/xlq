package xlsx

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/brunoarueira/xlq/internal/model"
)

// ADR-0046: catches a regression in the per-row/cell read path as it
// changes, against a fixture large and mixed-type enough to make that
// cost clearly measurable - the same shape (row/column count, cell
// type mix) ADR-0046's own profiling used, so a number from this
// benchmark is directly comparable to the numbers recorded there. Run
// with:
//
//	go test ./internal/xlsx/... -run=^$ -bench=. -benchmem
//
// Unlike internal/eval's own large-workbook benchmarks (genSheet,
// eval_bench_test.go), which build a model.Sheet directly in memory to
// isolate the evaluator's own cost from any one format's I/O, this
// benchmark's whole point is that I/O cost - excelize.OpenFile,
// streaming the worksheet XML, and this package's own per-cell
// handling - so it needs a real .xlsx file on disk, not an in-memory
// model.Workbook.
const (
	benchRows = 105_012
	benchCols = 14
)

// buildLargeFixture writes a synthetic .xlsx with benchRows rows and
// benchCols columns, cycling through string/number/date/bool values
// column by column - the same mixed-type shape as the real workbook
// that motivated ADR-0046 - and returns its path. Building it (SaveAs
// included) takes real wall-clock time, so callers exclude it from
// the timed portion of a benchmark via b.ResetTimer.
func buildLargeFixture(b *testing.B) string {
	b.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	sheet := "Data"
	if _, err := f.NewSheet(sheet); err != nil {
		b.Fatalf("NewSheet: %v", err)
	}
	if err := f.DeleteSheet("Sheet1"); err != nil {
		b.Fatalf("DeleteSheet: %v", err)
	}

	now := time.Now()
	for row := 1; row <= benchRows; row++ {
		for col := 1; col <= benchCols; col++ {
			cell, err := excelize.CoordinatesToCellName(col, row)
			if err != nil {
				b.Fatalf("CoordinatesToCellName: %v", err)
			}
			var value any
			switch col % 4 {
			case 0:
				value = row * col
			case 1:
				value = fmt.Sprintf("value-%d-%d", row, col)
			case 2:
				value = now.AddDate(0, 0, -(row % 1000))
			default:
				value = row%2 == 0
			}
			if err := f.SetCellValue(sheet, cell, value); err != nil {
				b.Fatalf("SetCellValue: %v", err)
			}
		}
	}

	path := filepath.Join(b.TempDir(), "bench.xlsx")
	if err := f.SaveAs(path); err != nil {
		b.Fatalf("SaveAs: %v", err)
	}
	return path
}

// BenchmarkReadLargeSheet is the internal/xlsx layer's own cost in
// isolation, not the full "xlq '.Data' large.xlsx" CLI path: xlsx.Read
// (lazy - cheap on its own, ADR-0018), then draining the one sheet's
// rows via model.CollectRows - not eval.Eval's own sheetGrid, which
// does more on top (converting each cell to its Go-native value,
// widening every row to the sheet's full column count, enforcing the
// maxDenseCells budget). ADR-0046's own profiling found this reader
// layer dominates the full CLI path's total time regardless (eval and
// JSON encoding added no measurable difference there), which is what
// justifies isolating it here rather than benchmarking through
// eval.Eval for a literally-identical number.
func BenchmarkReadLargeSheet(b *testing.B) {
	path := buildLargeFixture(b)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		wb, err := Read(path, 0)
		if err != nil {
			b.Fatalf("Read: %v", err)
		}
		rows, err := model.CollectRows(wb.Sheets[0])
		if err != nil {
			b.Fatalf("CollectRows: %v", err)
		}
		if len(rows) != benchRows {
			b.Fatalf("got %d rows, want %d", len(rows), benchRows)
		}
	}
}
