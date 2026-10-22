package cli

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/brunoarueira/xlq/internal/eval"
	"github.com/brunoarueira/xlq/internal/filter"
	"github.com/brunoarueira/xlq/internal/model"
)

func TestWithRecoverySuccess(t *testing.T) {
	got, err := withRecovery("testing", time.Second, func() (int, error) {
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
	_, err := withRecovery("testing", time.Second, func() (int, error) {
		return 0, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

func TestWithRecoveryCatchesPanic(t *testing.T) {
	_, err := withRecovery("testing", time.Second, func() (int, error) {
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
	if !strings.Contains(err.Error(), "testing") {
		t.Errorf("err = %q, want it to mention the operation label", err)
	}
}

// TestWithRecoveryCatchesPanicDuringLazyEvalIteration guards the
// actual reason runFilter wraps input.Read and eval.Eval separately:
// ADR-0018 made xlsx.Read lazy, so a panic from excelize's own
// row-scanning/formula internals happens later, inside eval.Eval's own
// call stack as it ranges over a Sheet's Rows - not inside input.Read,
// which already returned cleanly by then. Rather than a synthetic
// callback that panics immediately (indistinguishable from
// TestWithRecoveryCatchesPanic and provable by recover()'s documented
// semantics alone, regardless of call depth), this builds a real
// model.Workbook whose Sheet.Rows iterator panics on iteration and
// drives it through the real eval.Eval, the same way runFilter's
// second withRecovery call does - so a regression that removed that
// second wrapper, or wrapped the wrong thing, would actually be
// caught here.
func TestWithRecoveryCatchesPanicDuringLazyEvalIteration(t *testing.T) {
	wb := model.Workbook{
		Sheets: []model.Sheet{
			{
				Name: "Sheet1",
				Rows: func(func(model.Row, error) bool) {
					panic("panic from deep inside a lazy Sheet.Rows iterator, not from input.Read")
				},
			},
		},
	}
	expr, err := filter.Parse(".Sheet1")
	if err != nil {
		t.Fatalf("filter.Parse: %v", err)
	}
	_, err = withRecovery("testing", time.Second, func() (any, error) {
		return eval.Eval(expr, wb)
	})
	if err == nil {
		t.Fatal("err = nil, want the panic from eval.Eval's own iteration to be caught")
	}
}

// TestWithRecoveryTimeout asserts withRecovery returns at the timeout
// without waiting for the slow call to actually finish on its own - via
// a channel the slow call closes only after its own sleep, checked
// with a non-blocking select right after withRecovery returns, not via
// an absolute elapsed-time threshold. A wall-clock bound like "under
// 500ms" can fail on a CI runner that merely pauses the test process
// for a moment, even when the timeout logic itself is working
// correctly; racing against the slow call's own completion signal
// instead makes this immune to that.
func TestWithRecoveryTimeout(t *testing.T) {
	slowCallDone := make(chan struct{})
	_, err := withRecovery("testing", 50*time.Millisecond, func() (int, error) {
		time.Sleep(2 * time.Second)
		close(slowCallDone)
		return 1, nil
	})
	if err == nil {
		t.Fatal("err = nil, want a timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("err = %q, want it to mention timing out", err)
	}
	if !strings.Contains(err.Error(), "testing") {
		t.Errorf("err = %q, want it to mention the operation label", err)
	}
	select {
	case <-slowCallDone:
		t.Error("the slow call had already finished by the time withRecovery returned - timeout didn't return early")
	default:
	}
}

// TestWithRecoveryZeroTimeoutDisablesIt guards the "0 means no
// timeout" convention: a legitimately huge file shouldn't be forced to
// finish within some default window if the user explicitly disables
// it.
func TestWithRecoveryZeroTimeoutDisablesIt(t *testing.T) {
	got, err := withRecovery("testing", 0, func() (int, error) {
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

// TestTimeoutRemaining guards the three cases that actually matter for
// sharing one deadline across runFilter's two withRecovery calls: a
// disabled timeout stays disabled regardless of deadline, a deadline
// still comfortably in the future yields roughly that much time, and a
// deadline that has already passed - the realistic "reading used up
// nearly the whole budget" case - yields a small positive duration
// rather than 0 or negative, which withRecovery would otherwise
// misread as "disabled" instead of "expired."
func TestTimeoutRemaining(t *testing.T) {
	if got := timeoutRemaining(0, time.Now().Add(time.Hour)); got != 0 {
		t.Errorf("disabled timeout: got %s, want 0 unchanged", got)
	}
	future := time.Now().Add(time.Minute)
	if got := timeoutRemaining(time.Minute, future); got <= 0 || got > time.Minute {
		t.Errorf("future deadline: got %s, want a positive duration no more than a minute", got)
	}
	past := time.Now().Add(-time.Hour)
	if got := timeoutRemaining(time.Minute, past); got <= 0 {
		t.Errorf("expired deadline: got %s, want a small positive duration, not 0 or negative (withRecovery reads <=0 as disabled)", got)
	}
}
