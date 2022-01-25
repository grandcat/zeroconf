package zeroconf

import (
	"context"
	"runtime/debug"
	"strings"
	"time"

	"github.com/edaniels/golog"
)

func parseSubtypes(service string) (string, []string) {
	subtypes := strings.Split(service, ",")
	return subtypes[0], subtypes[1:]
}

// trimDot is used to trim the dots from the start or end of a string
func trimDot(s string) string {
	return strings.Trim(s, ".")
}

// panicCapturingGo spawns a goroutine to run the given function and captures
// any panic that occurs and logs it.
func panicCapturingGo(f func()) {
	panicCapturingGoWithCallback(f, nil)
}

const waitDur = 3 * time.Second

// panicCapturingGoWithCallback spawns a goroutine to run the given function and captures
// any panic that occurs, logs it, and calls the given callback. The callback can be
// used for restart functionality.
func panicCapturingGoWithCallback(f func(), callback func(err interface{})) {
	go func() {
		defer func() {
			if err := recover(); err != nil {
				debug.PrintStack()
				golog.Global.Errorw("panic while running function", "error", err)
				if callback == nil {
					return
				}
				golog.Global.Infow("waiting a bit to call callback", "wait", waitDur.String())
				time.Sleep(waitDur)
				callback(err)
			}
		}()
		f()
	}()
}

// managedGo keeps the given function alive in the background until
// it terminates normally.
func managedGo(f func(), onComplete func()) {
	panicCapturingGoWithCallback(func() {
		defer func() {
			if err := recover(); err == nil && onComplete != nil {
				onComplete()
			} else if err != nil {
				// re-panic
				panic(err)
			}
		}()
		f()
	}, func(_ interface{}) {
		managedGo(f, onComplete)
	})
}

// selectContextOrWait either terminates because the given context is done
// or the given duration elapses. It returns true if the duration elapsed.
func selectContextOrWait(ctx context.Context, dur time.Duration) bool {
	timer := time.NewTimer(dur)
	defer timer.Stop()
	return selectContextOrWaitChan(ctx, timer.C)
}

// selectContextOrWaitChan either terminates because the given context is done
// or the given time channel is received on. It returns true if the channel
// was received on.
func selectContextOrWaitChan(ctx context.Context, c <-chan time.Time) bool {
	select {
	case <-ctx.Done():
		return false
	default:
	}
	select {
	case <-ctx.Done():
		return false
	case <-c:
	}
	return true
}
