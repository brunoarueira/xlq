package cli

import (
	"fmt"
	"time"
)

// withRecovery runs fn in its own goroutine, converting any panic into
// a clean error instead of crashing the whole process, and enforcing
// timeout as a wall-clock deadline so a hang doesn't block forever
// either (#50). A goroutine is required for the timeout half of this -
// there's no way to cancel a running one from the outside - so
// recover() naturally has to live inside that same goroutine, since
// recover() only ever catches a panic in the goroutine where it
// occurs. If timeout fires first, fn's goroutine is abandoned, not
// killed (Go has no way to force that) - acceptable here because the
// caller is a short-lived CLI process that reports the error and exits
// shortly after, not a long-running service where a leaked goroutine
// would accumulate.
//
// Does not help with a Go fatal error (a stack overflow, an
// out-of-memory kill) - recover() can't catch those in any goroutine;
// see ADR-0029 for that separate class of problem.
func withRecovery[T any](timeout time.Duration, fn func() (T, error)) (T, error) {
	type result struct {
		val T
		err error
	}
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				var zero T
				ch <- result{zero, fmt.Errorf("internal error while parsing (the file may be corrupt or malformed): %v", r)}
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
		return zero, fmt.Errorf("timed out after %s reading or evaluating the file", timeout)
	}
}

// withRecoveryErr is withRecovery for a call that only ever returns an
// error, no value worth naming (xlsx.WriteCell) - avoids every call
// site spelling out an unused type parameter for that case.
func withRecoveryErr(timeout time.Duration, fn func() error) error {
	_, err := withRecovery(timeout, func() (struct{}, error) {
		return struct{}{}, fn()
	})
	return err
}
