package loader

import (
	"context"
	"sync"
	"time"
)

// ProgressFunc is called periodically while a fetch is in flight.
type ProgressFunc func(elapsed time.Duration)

type progressKey struct{}

// progressInterval is the heartbeat period. It is a variable so tests can lower it.
var progressInterval = 5 * time.Second

// WithProgress returns a context that makes the loader report fetch progress to fn.
func WithProgress(ctx context.Context, fn ProgressFunc) context.Context {
	return context.WithValue(ctx, progressKey{}, fn)
}

// progressFrom returns the ProgressFunc carried by ctx, or nil when absent.
func progressFrom(ctx context.Context) ProgressFunc {
	fn, _ := ctx.Value(progressKey{}).(ProgressFunc)
	return fn
}

// startHeartbeat invokes the context's ProgressFunc every progressInterval until
// the returned stop function is called or ctx is done. It does nothing when ctx
// carries no ProgressFunc.
//
// The callback runs on its own goroutine so a slow one never delays the fetch or
// stop(); ticks that arrive while it is still running are dropped. stop() does not
// wait for a callback already in flight, but guarantees none starts afterwards.
func startHeartbeat(ctx context.Context) (stop func()) {
	fn := progressFrom(ctx)
	if fn == nil {
		return func() {}
	}

	var (
		mu      sync.Mutex
		stopped bool
		busy    bool
	)
	done := make(chan struct{})
	started := time.Now()
	ticker := time.NewTicker(progressInterval)

	// begin reserves a callback slot; it fails once stopped or while one is running.
	begin := func() bool {
		mu.Lock()
		defer mu.Unlock()
		if stopped || busy {
			return false
		}
		busy = true
		return true
	}
	// run re-checks stopped immediately before invoking the callback.
	run := func() {
		defer func() {
			recover() // a misbehaving callback must not crash the server
			mu.Lock()
			busy = false
			mu.Unlock()
		}()
		mu.Lock()
		if stopped {
			mu.Unlock()
			return
		}
		mu.Unlock()
		fn(time.Since(started))
	}

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if begin() {
					go run()
				}
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			mu.Lock()
			stopped = true
			mu.Unlock()
			close(done)
		})
	}
}
