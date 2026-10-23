//go:build unix

package xlsx

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestWriteCellRejectsFIFOWithoutHanging guards the gap a cubic-dev-ai
// review caught: -i has no shared dispatcher (internal/input) to
// apply the #51 FIFO guard on its behalf, so WriteCell must reject a
// non-regular file itself, the same way internal/input.checkFileSize
// does for the read path. os.Stat reports a FIFO's size as 0
// regardless of how much data a writer eventually feeds it, so a
// size-only check would always pass for one even with maxSize set,
// and excelize.OpenFile would then block waiting for a writer. Run
// with a 2-second deadline so a regression hangs this one test
// instead of the whole run.
func TestWriteCellRejectsFIFOWithoutHanging(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pipe.xlsx")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("Mkfifo: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- WriteCell(path, "Sheet1", "A1", "x", 1<<20)
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("WriteCell on a FIFO: want error, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WriteCell on a FIFO did not return within 2s - it's hanging")
	}
}
