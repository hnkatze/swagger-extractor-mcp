package loader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hnkatze/swagger-mcp-go/internal/config"
	"github.com/hnkatze/swagger-mcp-go/internal/types"
)

// setProgressInterval lowers the heartbeat period for the duration of a test.
func setProgressInterval(t *testing.T, d time.Duration) {
	t.Helper()
	old := progressInterval
	progressInterval = d
	t.Cleanup(func() { progressInterval = old })
}

func slowSpecServer(delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(testSpec))
	}))
}

func plainLoader() *Loader {
	return New(config.Config{
		CacheTTL:      5 * time.Minute,
		MaxCacheSize:  10,
		MaxSpecSize:   20 * 1024 * 1024,
		FetchTimeout:  5 * time.Second,
		DefaultFormat: "json",
	})
}

// callRecorder records the start time of every progress callback.
type callRecorder struct {
	mu    sync.Mutex
	times []time.Time
}

func (r *callRecorder) fn(delay time.Duration) ProgressFunc {
	return func(time.Duration) {
		r.mu.Lock()
		r.times = append(r.times, time.Now())
		r.mu.Unlock()
		time.Sleep(delay)
	}
}

func (r *callRecorder) snapshot() []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Time(nil), r.times...)
}

func TestProgress_FullFetchHeartbeats(t *testing.T) {
	setProgressInterval(t, 20*time.Millisecond)
	srv := slowSpecServer(200 * time.Millisecond)
	defer srv.Close()

	rec := &callRecorder{}
	ctx := WithProgress(context.Background(), rec.fn(0))
	if _, _, err := plainLoader().LoadFromURL(ctx, srv.URL+"/spec.json"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	n := len(rec.snapshot())
	t.Logf("full fetch heartbeats: %d", n)
	if n < 2 {
		t.Fatalf("expected at least 2 heartbeats, got %d", n)
	}
}

func TestProgress_ConditionalFetchHeartbeats(t *testing.T) {
	setProgressInterval(t, 20*time.Millisecond)
	l, dir := testLoaderWithDisk(t)
	_ = dir
	srv := slowSpecServer(200 * time.Millisecond)
	defer srv.Close()

	url := srv.URL + "/spec.json"
	seedDiskCache(t, l.diskCache, normalizeURL(url), []byte(testSpec), types.DiskCacheMeta{
		FetchedAt: time.Now().Add(-2 * time.Hour),
		ETag:      `"old"`,
	})

	rec := &callRecorder{}
	ctx := WithProgress(context.Background(), rec.fn(0))
	if _, _, err := l.LoadFromURL(ctx, url); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	n := len(rec.snapshot())
	t.Logf("conditional fetch heartbeats: %d", n)
	if n < 2 {
		t.Fatalf("expected at least 2 heartbeats, got %d", n)
	}
}

func TestProgress_NoCallbackNoPanic(t *testing.T) {
	setProgressInterval(t, 5*time.Millisecond)
	srv := slowSpecServer(30 * time.Millisecond)
	defer srv.Close()

	if progressFrom(context.Background()) != nil {
		t.Fatal("expected nil ProgressFunc on a bare context")
	}
	doc, _, err := plainLoader().LoadFromURL(context.Background(), srv.URL+"/spec.json")
	if err != nil || doc == nil {
		t.Fatalf("expected successful load, got doc=%v err=%v", doc, err)
	}
}

func TestProgress_NotCalledAfterReturn(t *testing.T) {
	setProgressInterval(t, 10*time.Millisecond)
	srv := slowSpecServer(100 * time.Millisecond)
	defer srv.Close()

	rec := &callRecorder{}
	ctx := WithProgress(context.Background(), rec.fn(0))
	if _, _, err := plainLoader().LoadFromURL(ctx, srv.URL+"/spec.json"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	returned := time.Now()
	before := len(rec.snapshot())

	time.Sleep(10 * progressInterval)
	after := rec.snapshot()
	t.Logf("calls at return: %d, after waiting: %d", before, len(after))
	if len(after) != before {
		t.Fatalf("callback invoked after loader returned: %d -> %d", before, len(after))
	}
	for _, ts := range after {
		if ts.After(returned) {
			t.Fatalf("callback started %v after loader returned", ts.Sub(returned))
		}
	}
}

func TestProgress_SlowCallbackDoesNotSlowFetch(t *testing.T) {
	setProgressInterval(t, 10*time.Millisecond)
	srv := slowSpecServer(150 * time.Millisecond)
	defer srv.Close()

	start := time.Now()
	if _, _, err := plainLoader().LoadFromURL(context.Background(), srv.URL+"/spec.json"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	baseline := time.Since(start)

	rec := &callRecorder{}
	ctx := WithProgress(context.Background(), rec.fn(2*time.Second))
	start = time.Now()
	if _, _, err := plainLoader().LoadFromURL(ctx, srv.URL+"/spec.json"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	withSlow := time.Since(start)
	t.Logf("baseline=%v withSlowCallback=%v calls=%d", baseline, withSlow, len(rec.snapshot()))

	if withSlow > baseline+100*time.Millisecond {
		t.Fatalf("slow callback extended fetch: baseline=%v with=%v", baseline, withSlow)
	}
	if n := len(rec.snapshot()); n != 1 {
		t.Fatalf("expected ticks to be dropped while callback is busy (1 call), got %d", n)
	}
}

func TestProgress_StopsOnContextCancel(t *testing.T) {
	setProgressInterval(t, 10*time.Millisecond)
	srv := slowSpecServer(500 * time.Millisecond)
	defer srv.Close()

	rec := &callRecorder{}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	ctx = WithProgress(ctx, rec.fn(0))

	_, _, err := plainLoader().LoadFromURL(ctx, srv.URL+"/spec.json")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if te, ok := err.(*types.ToolError); !ok || te.Code != types.ErrNetworkError {
		t.Fatalf("expected ErrNetworkError, got %v", err)
	}
	returned := time.Now()
	time.Sleep(100 * time.Millisecond)
	for _, ts := range rec.snapshot() {
		if ts.After(returned) {
			t.Fatal("callback invoked after cancelled fetch returned")
		}
	}
}
