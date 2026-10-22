package cli

import (
	"fmt"
	"time"
)

// withRecovery runs fn in its own goroutine, converting any panic into
// a clean error instead of crashing the whole process, and enforcing
// timeout as a wall-clock deadline so a hang doesn't block forever
// either (#50). operation names what fn is doing ("reading",
// "evaluating", "writing", "listing sheets", ...) for both the panic
// and timeout error messages - the three call sites wrap genuinely
// different operations, and a one-size-wording like "reading or
// evaluating the file" would mislabel a -i write or a sheets listing
// that never evaluates anything.
//
// A goroutine is required for the timeout half of this - there's no
// way to cancel a running one from the outside - so recover()
// naturally has to live inside that same goroutine, since recover()
// only ever catches a panic in the goroutine where it occurs. If
// timeout fires first, fn's goroutine is abandoned, not killed (Go has
// no way to force that) - acceptable here because the caller is a
// short-lived CLI process that reports the error and exits shortly
// after, not a long-running service where a leaked goroutine would
// accumulate. One consequence of abandoning rather than killing: if
// fn is xlsx.WriteCell and the timeout fires after it has already
// created its temp file but before it renames over the real one, that
// temp file's own cleanup (a defer inside the abandoned goroutine)
// never gets to run, since main calls os.Exit as soon as this returns
// an error - os.Exit skips every pending defer in the process, not
// just the calling goroutine's. In practice this window only opens
// once WriteCell's own excelize.OpenFile on the real file has already
// returned successfully (every real advisory's panic/hang risk is in
// that parse step, which runs before any temp file exists), so a
// genuinely malicious file won't trigger it - a stray xlq-tmp-*.xlsx
// left in the target directory next to a real file that's just slow
// to save is the realistic worst case, never a corrupted or
// partially-written target file (WriteCell only ever renames over it
// after the save and a re-open verification both succeed).
//
// Does not help with a Go fatal error (a stack overflow, an
// out-of-memory kill) - recover() can't catch those in any goroutine.
// ADR-0029 fixed one specific stack-overflow-causing formula pattern
// (mutually referencing ANCHORARRAY formulas), not this fatal-error
// class in general - a stack overflow from any other cause, or an OOM
// kill, remain just as uncatchable as they would be without this.
func withRecovery[T any](operation string, timeout time.Duration, fn func() (T, error)) (T, error) {
	type result struct {
		val T
		err error
	}
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				var zero T
				ch <- result{zero, fmt.Errorf("internal error while %s (the file may be corrupt or malformed): %v", operation, r)}
			}
		}()
		val, err := fn()
		ch <- result{val, err}
	}()
	if timeout <= 0 {
		res := <-ch
		return res.val, res.err
	}
	select {
	case res := <-ch:
		return res.val, res.err
	case <-time.After(timeout):
		var zero T
		return zero, fmt.Errorf("timed out after %s while %s", timeout, operation)
	}
}

// withRecoveryErr is withRecovery for a call that only ever returns an
// error, no value worth naming (xlsx.WriteCell) - avoids every call
// site spelling out an unused type parameter for that case.
func withRecoveryErr(operation string, timeout time.Duration, fn func() error) error {
	_, err := withRecovery(operation, timeout, func() (struct{}, error) {
		return struct{}{}, fn()
	})
	return err
}

// timeoutRemaining returns how much of timeout is left before deadline,
// for a second withRecovery call sharing the same per-file budget a
// first one already spent part of - so two wrapped steps together are
// bounded by one --timeout, not each given their own full timeout from
// its own start. timeout <= 0 (the "disabled" convention) passes
// through unchanged, since there's no deadline to share in that case.
// Otherwise this never returns a non-positive duration even if
// deadline has already passed - withRecovery's own timeout <= 0 means
// "disabled," so handing it an expired budget as-is would wait
// forever instead of timing out immediately; a time.Duration this
// small expires at the next possible instant instead.
func timeoutRemaining(timeout time.Duration, deadline time.Time) time.Duration {
	if timeout <= 0 {
		return timeout
	}
	if d := time.Until(deadline); d > 0 {
		return d
	}
	return time.Nanosecond
}
