//go:build unix

package input

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestSheetNamesCSVRejectsFIFOWithoutHanging guards against the exact
// regression a cubic-dev-ai review caught in an earlier fix: opening a
// FIFO blocks until a writer connects on the other end, so the
// regular-file check in checkReadableFile must come from os.Stat,
// before path is ever opened - checking regularity only after opening
// it would hang here instead of rejecting it. There's no writer on the
// other end of this FIFO, so a hang would block until the test's own
// timeout kills the whole run, not just fail this one test.
func TestSheetNamesCSVRejectsFIFOWithoutHanging(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pipe.csv")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("Mkfifo: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := SheetNames(path, "", 0, 0)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("SheetNames on a FIFO named *.csv: want error, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SheetNames on a FIFO named *.csv did not return within 2s - it's hanging in os.Open")
	}
}

// TestReadRejectsFIFOWithoutHanging guards the gap a cubic-dev-ai
// review caught in #51's own checkFileSize: os.Stat reports a FIFO's
// size as 0 regardless of how much data a writer eventually feeds it,
// so checkFileSize's size comparison alone would always pass for one -
// even with maxSize set - and csv.ReadWithDelimiter would then block
// in os.Open waiting for a writer, or stream an unbounded amount once
// one connects, exactly what maxSize exists to bound in the first
// place. checkFileSize's regular-file check (unconditional, even with
// maxSize disabled - 0 only means "no byte ceiling," not "a pipe is
// fine to block on") must come from os.Stat, same reasoning as
// checkReadableFile's own - checking regularity only after opening
// would hang here instead of rejecting it.
func TestReadRejectsFIFOWithoutHanging(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pipe.csv")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("Mkfifo: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := Read(path, "", 0, 1<<20)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("Read on a FIFO named *.csv: want error, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read on a FIFO named *.csv did not return within 2s - it's hanging")
	}
}
