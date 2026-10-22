package cli

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWithRecoverySuccess(t *testing.T) {
	got, err := withRecovery(time.Second, func() (int, error) {
		return 42, nil
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got != 42 {
		t.Errorf("got = %d, want 42", got)
	}
}

func TestWithRecoveryOrdinaryError(t *testing.T) {
	wantErr := errors.New("ordinary error")
	_, err := withRecovery(time.Second, func() (int, error) {
		return 0, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

func TestWithRecoveryCatchesPanic(t *testing.T) {
	_, err := withRecovery(time.Second, func() (int, error) {
		panic("synthetic panic")
	})
	if err == nil {
		t.Fatal("err = nil, want an error converted from the panic")
	}
	if !strings.Contains(err.Error(), "synthetic panic") {
		t.Errorf("err = %q, want it to mention the panic value", err)
	}
	if !strings.Contains(err.Error(), "may be corrupt or malformed") {
		t.Errorf("err = %q, want the clean, user-facing wording", err)
	}
}

// TestWithRecoveryCatchesPanicNotJustInTheOutermostCall guards the
// actual reason this wraps more than one call site: ADR-0018 made
// xlsx.Read lazy, so a panic from excelize's own row-scanning/formula
// internals happens later, while something ranges over Sheet.Rows -
// inside eval.Eval, not inside input.Read. Verifies withRecovery
// catches a panic regardless of which call it's wrapping, not just one
// specific shape of call.
func TestWithRecoveryCatchesPanicNotJustInTheOutermostCall(t *testing.T) {
	simulateLazyIteration := func() (string, error) {
		// Stands in for eval.Eval driving a lazy Sheet.Rows iterator
		// that panics partway through, the same shape of risk a
		// corrupt xlsx file's row data poses - not anything wrong
		// inside input.Read itself, which already returned cleanly by
		// this point in the real call sequence.
		panic("panic from deep inside iteration, not from Read")
	}
	_, err := withRecovery(time.Second, simulateLazyIteration)
	if err == nil {
		t.Fatal("err = nil, want the panic from the wrapped call to be caught")
	}
}

func TestWithRecoveryTimeout(t *testing.T) {
	start := time.Now()
	_, err := withRecovery(50*time.Millisecond, func() (int, error) {
		time.Sleep(time.Second)
		return 1, nil
	})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("err = nil, want a timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("err = %q, want it to mention timing out", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("elapsed = %s, want it to return promptly at the timeout, not wait for the slow call", elapsed)
	}
}

// TestWithRecoveryZeroTimeoutDisablesIt guards the "0 means no
// timeout" convention: a legitimately huge file shouldn't be forced to
// finish within some default window if the user explicitly disables
// it.
func TestWithRecoveryZeroTimeoutDisablesIt(t *testing.T) {
	got, err := withRecovery(0, func() (int, error) {
		time.Sleep(100 * time.Millisecond)
		return 7, nil
	})
	if err != nil {
		t.Fatalf("err = %v, want nil - timeout 0 should mean wait indefinitely", err)
	}
	if got != 7 {
		t.Errorf("got = %d, want 7", got)
	}
}
