package eval

import (
	"fmt"
	"testing"

	"github.com/brunoarueira/xlq/internal/filter"
	"github.com/brunoarueira/xlq/internal/model"
)

// Issue #19 (M6): catch performance regressions in the filter
// evaluator as it grows, against a generated workbook large enough to
// make a full scan's cost clearly measurable and an early-exit
// operation's cost visibly track target position rather than sheet
// size. Run with:
//
//	go test ./internal/eval/... -run=^$ -bench=. -benchmem
//
// genSheet builds the data in memory via model.NewSheet rather than
// writing and reading an actual file, so these benchmarks isolate the
// evaluator's own cost (this package's scope) from any one format's
// I/O (internal/xlsx, internal/csv, internal/ods each have their own
// concerns - ADR-0018 already covers internal/xlsx's).

// largeRows/largeCols size the workbook every benchmark below shares:
// ~1,000,000 cells is large enough that a full scan (sheetGrid,
// records, wholeColumn/wholeRow) is clearly measurable and comfortably
// under maxDenseCells, yet small enough to build once per benchmark
// run without that setup cost dominating the measurement itself.
const (
	largeRows = 100_000
	largeCols = 10
)

// genSheet builds a synthetic, dense sheet: every row has a string in
// column 0 and a number in every other column - except row 0, which
// only has its column-0 string. sheetRecords (ADR-0011) treats row 0
// as the header row, so this gives records a real header name to find
// in column 0 and, for every other column, a genuinely absent header
// cell - the actual fallback-to-letters case, not a numeric header
// value (which headerString renders as its own JSON text, not a
// letter fallback, confirmed by a cubic-dev-ai review round on this
// same PR).
func genSheet(name string, rows, cols int) model.Sheet {
	rs := make([]model.Row, rows)
	for r := 0; r < rows; r++ {
		cells := []model.Cell{model.NewStringCell(fmt.Sprintf("row-%d", r)).At(0)}
		if r > 0 {
			for c := 1; c < cols; c++ {
				cells = append(cells, model.NewNumberCell(float64(r*cols+c)).At(c))
			}
		}
		rs[r] = model.Row{Index: r, Cells: cells}
	}
	return model.NewSheet(name, rs)
}

func largeWorkbook() model.Workbook {
	return model.Workbook{Sheets: []model.Sheet{genSheet("Sheet1", largeRows, largeCols)}}
}

// benchmarkEval parses input once, builds wb once, then measures only
// repeated Eval calls - wb's own construction (and filter.Parse's
// negligible cost) must not count against the evaluator's own time.
func benchmarkEval(b *testing.B, wb model.Workbook, input string) {
	b.Helper()
	expr, err := filter.Parse(input)
	if err != nil {
		b.Fatalf("filter.Parse(%q): %v", input, err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Eval(expr, wb); err != nil {
			b.Fatalf("Eval(%q): %v", input, err)
		}
	}
}

// BenchmarkCellScalarNearTop and BenchmarkCellScalarNearBottom use the
// same, large sheet to demonstrate cellScalar's early exit (ADR-0018):
// a lookup near the bottom costs roughly as much as one near the top
// scaled by its own row position, not by re-scanning rows already
// known to be past the target in vain - it should not cost anywhere
// near largeRows times as much as the near-top case.
func BenchmarkCellScalarNearTop(b *testing.B) {
	benchmarkEval(b, largeWorkbook(), ".Sheet1.A1")
}

func BenchmarkCellScalarNearBottom(b *testing.B) {
	benchmarkEval(b, largeWorkbook(), fmt.Sprintf(".Sheet1.A%d", largeRows))
}

// BenchmarkRangeNearTop and BenchmarkRangeNearBottom are sheetRange's
// own early-exit case (ADR-0018): a small range's cost should track
// where it sits, not the sheet's overall size.
func BenchmarkRangeNearTop(b *testing.B) {
	benchmarkEval(b, largeWorkbook(), `.Sheet1["A1:J10"]`)
}

func BenchmarkRangeNearBottom(b *testing.B) {
	bottom := largeRows
	top := bottom - 9
	benchmarkEval(b, largeWorkbook(), fmt.Sprintf(`.Sheet1["A%d:J%d"]`, top, bottom))
}

// BenchmarkWholeColumn and BenchmarkWholeRow are the two single-pass,
// no-early-exit operations (ADR-0018): every row must still be seen,
// but memory stays bounded to the current row's own cells rather than
// a fully-materialized sheet.
func BenchmarkWholeColumn(b *testing.B) {
	benchmarkEval(b, largeWorkbook(), ".Sheet1.B")
}

func BenchmarkWholeRow(b *testing.B) {
	benchmarkEval(b, largeWorkbook(), fmt.Sprintf(".Sheet1[%d]", largeRows/2))
}

// BenchmarkSheetGrid converts the whole sheet to a dense 2D array -
// the most expensive single-sheet operation, bounded by maxDenseCells.
func BenchmarkSheetGrid(b *testing.B) {
	benchmarkEval(b, largeWorkbook(), ".Sheet1")
}

// BenchmarkRecords is records' (ADR-0011) own exception to the
// "discard a row's cells immediately" memory profile (ADR-0019):
// every data row is buffered until the final column count is known,
// which this benchmark's allocation count should reflect.
func BenchmarkRecords(b *testing.B) {
	benchmarkEval(b, largeWorkbook(), ".Sheet1 | records")
}

// BenchmarkIdentityWholeWorkbook exercises toJSON's own whole-workbook
// path (the cumulative cross-sheet maxDenseCells budget, ADR-0018/
// PR #24), via the identity filter against the same large sheet.
func BenchmarkIdentityWholeWorkbook(b *testing.B) {
	benchmarkEval(b, largeWorkbook(), ".")
}
