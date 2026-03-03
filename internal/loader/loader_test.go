package loader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/hnkatze/swagger-mcp-go/internal/config"
	"github.com/hnkatze/swagger-mcp-go/internal/types"
)

func TestNormalizeURL_Basic(t *testing.T) {
	input := "HTTPS://API.Example.COM/v1/"
	want := "https://api.example.com/v1"
	got := normalizeURL(input)
	if got != want {
		t.Errorf("normalizeURL(%q) = %q, want %q", input, got, want)
	}
}

func TestNormalizeURL_PreservesPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "preserves path casing",
			input: "https://api.example.com/V2/Users",
			want:  "https://api.example.com/V2/Users",
		},
		{
			name:  "trims trailing slash",
			input: "https://api.example.com/v1/",
			want:  "https://api.example.com/v1",
		},
		{
			name:  "keeps root path",
			input: "https://api.example.com/",
			want:  "https://api.example.com/",
		},
		{
			name:  "trims multiple trailing slashes",
			input: "https://api.example.com/v1///",
			want:  "https://api.example.com/v1",
		},
		{
			name:  "preserves query string",
			input: "https://api.example.com/v1?format=json",
			want:  "https://api.example.com/v1?format=json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeURL(tt.input)
			if got != tt.want {
				t.Errorf("normalizeURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeURL_InvalidURL(t *testing.T) {
	// url.Parse fails on URLs containing ASCII control characters like \x01.
	// In that case, normalizeURL should return the raw input as-is.
	input := "https://example.com/\x01bad"
	got := normalizeURL(input)
	if got != input {
		t.Errorf("normalizeURL(%q) = %q, want input returned as-is", input, got)
	}
}

// loadPetstoreData is a test helper that reads the petstore.json fixture.
func loadPetstoreData(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/petstore.json")
	if err != nil {
		t.Fatalf("failed to read petstore.json: %v", err)
	}
	return data
}

func TestBuildSummary_Petstore(t *testing.T) {
	data := loadPetstoreData(t)

	loader := New(config.Default())
	doc, err := loader.LoadFromData(data)
	if err != nil {
		t.Fatalf("failed to parse petstore.json: %v", err)
	}

	summary := buildSummary(doc)

	if summary.Title != "Petstore" {
		t.Errorf("expected title %q, got %q", "Petstore", summary.Title)
	}
	if summary.Version != "1.0.0" {
		t.Errorf("expected version %q, got %q", "1.0.0", summary.Version)
	}
	if summary.EndpointCount != 5 {
		t.Errorf("expected 5 endpoints, got %d", summary.EndpointCount)
	}
	// The petstore.json fixture has no top-level "tags" array,
	// so buildSummary counts 0 from doc.Tags. Tags are only referenced
	// inside operations, not declared at the top level.
	if summary.TagCount != 0 {
		t.Errorf("expected 0 top-level tags (none declared in fixture), got %d", summary.TagCount)
	}
	if summary.SchemaCount != 4 {
		t.Errorf("expected 4 schemas, got %d", summary.SchemaCount)
	}
	if summary.SpecVersion != "3.0.3" {
		t.Errorf("expected spec version %q, got %q", "3.0.3", summary.SpecVersion)
	}
	if summary.BaseURL != "https://petstore.example.com/v1" {
		t.Errorf("expected base URL %q, got %q", "https://petstore.example.com/v1", summary.BaseURL)
	}
}

func TestBuildSummary_EmptyDoc(t *testing.T) {
	doc := &openapi3.T{}

	summary := buildSummary(doc)

	if summary.Title != "" {
		t.Errorf("expected empty title, got %q", summary.Title)
	}
	if summary.Version != "" {
		t.Errorf("expected empty version, got %q", summary.Version)
	}
	if summary.EndpointCount != 0 {
		t.Errorf("expected 0 endpoints, got %d", summary.EndpointCount)
	}
	if summary.TagCount != 0 {
		t.Errorf("expected 0 tags, got %d", summary.TagCount)
	}
	if summary.SchemaCount != 0 {
		t.Errorf("expected 0 schemas, got %d", summary.SchemaCount)
	}
}

func TestLoadFromData_ValidJSON(t *testing.T) {
	data := loadPetstoreData(t)

	loader := New(config.Default())
	doc, err := loader.LoadFromData(data)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if doc == nil {
		t.Fatal("expected non-nil doc")
	}
	if doc.Info == nil {
		t.Fatal("expected non-nil doc.Info")
	}
	if doc.Info.Title != "Petstore" {
		t.Errorf("expected title %q, got %q", "Petstore", doc.Info.Title)
	}
}

func TestLoadFromData_InvalidJSON(t *testing.T) {
	loader := New(config.Default())
	_, err := loader.LoadFromData([]byte(`this is not json or yaml at all`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}

	toolErr, ok := err.(*types.ToolError)
	if !ok {
		t.Fatalf("expected *types.ToolError, got %T", err)
	}
	if toolErr.Code != types.ErrParseError {
		t.Errorf("expected error code %q, got %q", types.ErrParseError, toolErr.Code)
	}
}

func TestLoadFromURL_InvalidURL(t *testing.T) {
	loader := New(config.Default())
	_, _, err := loader.LoadFromURL(context.Background(), "not-a-url")
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}

	toolErr, ok := err.(*types.ToolError)
	if !ok {
		t.Fatalf("expected *types.ToolError, got %T", err)
	}
	if toolErr.Code != types.ErrInvalidURL {
		t.Errorf("expected error code %q, got %q", types.ErrInvalidURL, toolErr.Code)
	}
}

// --- L2 disk cache integration tests ---

// Minimal valid OpenAPI 3.0.3 spec for tests.
const testSpec = `{"openapi":"3.0.3","info":{"title":"Test","version":"1.0"},"paths":{}}`

// testLoaderWithDisk creates a Loader with disk cache enabled and returns it
// along with the temp directory. The disk cache TTL is set to 1 hour so entries
// are "fresh" by default; tests that need stale entries override meta.FetchedAt.
func testLoaderWithDisk(t *testing.T) (*Loader, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{
		CacheTTL:         5 * time.Minute,
		MaxCacheSize:     10,
		MaxSpecSize:      20 * 1024 * 1024,
		FetchTimeout:     5 * time.Second,
		DefaultFormat:    "json",
		CacheDir:         dir,
		DiskCacheTTL:     1 * time.Hour,
		ConditionalFetch: true,
		MaxDiskEntries:   10,
	}
	return New(cfg), dir
}

// seedDiskCache writes a spec entry to disk cache with the given metadata.
func seedDiskCache(t *testing.T, dc *DiskCache, url string, specData []byte, meta types.DiskCacheMeta) {
	t.Helper()
	dc.Set(url, specData, meta)
}

func TestLoadFromURL_L2Hit_Fresh(t *testing.T) {
	l, _ := testLoaderWithDisk(t)

	// Track HTTP requests
	var requestCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(testSpec))
	}))
	defer srv.Close()

	normalized := normalizeURL(srv.URL + "/spec.json")

	// Seed L2 with fresh data (FetchedAt = now, within 1h TTL)
	meta := types.DiskCacheMeta{
		URL:         normalized,
		Fingerprint: fingerprint([]byte(testSpec)),
		FetchedAt:   time.Now(),
		SpecSize:    int64(len(testSpec)),
		Summary: types.SpecSummary{
			Title:       "Test",
			Version:     "1.0",
			SpecVersion: "3.0.3",
		},
	}
	seedDiskCache(t, l.diskCache, normalized, []byte(testSpec), meta)

	// Call LoadFromURL — should hit L2, NOT make HTTP request
	doc, summary, err := l.LoadFromURL(context.Background(), srv.URL+"/spec.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc == nil {
		t.Fatal("expected non-nil doc")
	}
	if summary == nil {
		t.Fatal("expected non-nil summary")
	}
	if summary.Title != "Test" {
		t.Errorf("expected title %q, got %q", "Test", summary.Title)
	}
	if atomic.LoadInt32(&requestCount) != 0 {
		t.Errorf("expected 0 HTTP requests (L2 hit), got %d", requestCount)
	}
}

func TestLoadFromURL_L2Hit_Stale_304(t *testing.T) {
	l, _ := testLoaderWithDisk(t)

	var requestCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)

		// Verify conditional headers are sent
		if r.Header.Get("If-None-Match") != `"abc123"` {
			t.Errorf("expected If-None-Match %q, got %q", `"abc123"`, r.Header.Get("If-None-Match"))
		}
		if r.Header.Get("If-Modified-Since") != "Mon, 01 Jan 2024 00:00:00 GMT" {
			t.Errorf("expected If-Modified-Since %q, got %q", "Mon, 01 Jan 2024 00:00:00 GMT", r.Header.Get("If-Modified-Since"))
		}

		// Return 304 Not Modified
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	normalized := normalizeURL(srv.URL + "/spec.json")

	// Seed L2 with STALE data (FetchedAt = 2 hours ago, beyond 1h TTL)
	meta := types.DiskCacheMeta{
		URL:          normalized,
		Fingerprint:  fingerprint([]byte(testSpec)),
		FetchedAt:    time.Now().Add(-2 * time.Hour),
		SpecSize:     int64(len(testSpec)),
		ETag:         `"abc123"`,
		LastModified: "Mon, 01 Jan 2024 00:00:00 GMT",
		Summary: types.SpecSummary{
			Title:       "Test",
			Version:     "1.0",
			SpecVersion: "3.0.3",
		},
	}
	seedDiskCache(t, l.diskCache, normalized, []byte(testSpec), meta)

	doc, summary, err := l.LoadFromURL(context.Background(), srv.URL+"/spec.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc == nil {
		t.Fatal("expected non-nil doc")
	}
	if summary.Title != "Test" {
		t.Errorf("expected title %q, got %q", "Test", summary.Title)
	}
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Errorf("expected 1 HTTP request (conditional), got %d", requestCount)
	}

	// Verify FetchedAt was refreshed in disk cache
	_, updatedMeta, ok := l.diskCache.Get(normalized)
	if !ok {
		t.Fatal("expected disk cache entry after 304")
	}
	if time.Since(updatedMeta.FetchedAt) > 5*time.Second {
		t.Error("expected FetchedAt to be refreshed to ~now after 304")
	}
}

func TestLoadFromURL_L2Hit_Stale_200(t *testing.T) {
	l, _ := testLoaderWithDisk(t)

	const updatedSpec = `{"openapi":"3.0.3","info":{"title":"Updated","version":"2.0"},"paths":{}}`

	var requestCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"new-etag"`)
		w.Header().Set("Last-Modified", "Tue, 02 Jan 2024 00:00:00 GMT")
		w.Write([]byte(updatedSpec))
	}))
	defer srv.Close()

	normalized := normalizeURL(srv.URL + "/spec.json")

	// Seed L2 with STALE data
	meta := types.DiskCacheMeta{
		URL:         normalized,
		Fingerprint: fingerprint([]byte(testSpec)),
		FetchedAt:   time.Now().Add(-2 * time.Hour),
		SpecSize:    int64(len(testSpec)),
		ETag:        `"old-etag"`,
		Summary: types.SpecSummary{
			Title:       "Test",
			Version:     "1.0",
			SpecVersion: "3.0.3",
		},
	}
	seedDiskCache(t, l.diskCache, normalized, []byte(testSpec), meta)

	doc, summary, err := l.LoadFromURL(context.Background(), srv.URL+"/spec.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc == nil {
		t.Fatal("expected non-nil doc")
	}
	if summary.Title != "Updated" {
		t.Errorf("expected title %q, got %q", "Updated", summary.Title)
	}
	if summary.Version != "2.0" {
		t.Errorf("expected version %q, got %q", "2.0", summary.Version)
	}
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Errorf("expected 1 HTTP request, got %d", requestCount)
	}

	// Verify disk cache was updated with new data
	_, updatedMeta, ok := l.diskCache.Get(normalized)
	if !ok {
		t.Fatal("expected disk cache entry after 200")
	}
	if updatedMeta.ETag != `"new-etag"` {
		t.Errorf("expected ETag %q, got %q", `"new-etag"`, updatedMeta.ETag)
	}
	if updatedMeta.LastModified != "Tue, 02 Jan 2024 00:00:00 GMT" {
		t.Errorf("expected LastModified %q, got %q", "Tue, 02 Jan 2024 00:00:00 GMT", updatedMeta.LastModified)
	}
	if updatedMeta.Summary.Title != "Updated" {
		t.Errorf("expected disk meta summary title %q, got %q", "Updated", updatedMeta.Summary.Title)
	}
}

func TestLoadFromURL_L2Miss_StoresInDisk(t *testing.T) {
	l, _ := testLoaderWithDisk(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"fresh-etag"`)
		w.Header().Set("Last-Modified", "Wed, 03 Jan 2024 00:00:00 GMT")
		w.Write([]byte(testSpec))
	}))
	defer srv.Close()

	normalized := normalizeURL(srv.URL + "/spec.json")

	// Verify disk cache is empty
	_, _, ok := l.diskCache.Get(normalized)
	if ok {
		t.Fatal("expected disk cache to be empty initially")
	}

	// Full fetch
	doc, summary, err := l.LoadFromURL(context.Background(), srv.URL+"/spec.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc == nil {
		t.Fatal("expected non-nil doc")
	}
	if summary.Title != "Test" {
		t.Errorf("expected title %q, got %q", "Test", summary.Title)
	}

	// Verify disk cache was populated
	diskData, diskMeta, ok := l.diskCache.Get(normalized)
	if !ok {
		t.Fatal("expected disk cache entry after full fetch")
	}
	if string(diskData) != testSpec {
		t.Errorf("expected disk data to match spec")
	}
	if diskMeta.ETag != `"fresh-etag"` {
		t.Errorf("expected ETag %q, got %q", `"fresh-etag"`, diskMeta.ETag)
	}
	if diskMeta.LastModified != "Wed, 03 Jan 2024 00:00:00 GMT" {
		t.Errorf("expected LastModified %q, got %q", "Wed, 03 Jan 2024 00:00:00 GMT", diskMeta.LastModified)
	}
	if diskMeta.Fingerprint == "" {
		t.Error("expected non-empty fingerprint")
	}
	if diskMeta.Summary.Title != "Test" {
		t.Errorf("expected disk meta summary title %q, got %q", "Test", diskMeta.Summary.Title)
	}
}

func TestLoadFromURL_NetworkError_DiskFallback(t *testing.T) {
	l, _ := testLoaderWithDisk(t)

	// Server that always closes connections immediately
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("server does not support hijacking")
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	normalized := normalizeURL(srv.URL + "/spec.json")

	// Seed L2 with STALE data
	meta := types.DiskCacheMeta{
		URL:         normalized,
		Fingerprint: fingerprint([]byte(testSpec)),
		FetchedAt:   time.Now().Add(-2 * time.Hour),
		SpecSize:    int64(len(testSpec)),
		ETag:        `"stale-etag"`,
		Summary: types.SpecSummary{
			Title:       "Test",
			Version:     "1.0",
			SpecVersion: "3.0.3",
		},
	}
	seedDiskCache(t, l.diskCache, normalized, []byte(testSpec), meta)

	// Should fall back to stale disk data due to network error
	doc, summary, err := l.LoadFromURL(context.Background(), srv.URL+"/spec.json")
	if err != nil {
		t.Fatalf("expected fallback to disk data, got error: %v", err)
	}
	if doc == nil {
		t.Fatal("expected non-nil doc from fallback")
	}
	if summary.Title != "Test" {
		t.Errorf("expected title %q from fallback, got %q", "Test", summary.Title)
	}
}

func TestLoadFromURL_ConditionalFetch_Disabled(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		CacheTTL:         5 * time.Minute,
		MaxCacheSize:     10,
		MaxSpecSize:      20 * 1024 * 1024,
		FetchTimeout:     5 * time.Second,
		DefaultFormat:    "json",
		CacheDir:         dir,
		DiskCacheTTL:     1 * time.Hour,
		ConditionalFetch: false, // Disabled!
		MaxDiskEntries:   10,
	}
	l := New(cfg)

	var requestCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)

		// Should NOT have conditional headers since ConditionalFetch is disabled
		if r.Header.Get("If-None-Match") != "" {
			t.Error("expected no If-None-Match header when ConditionalFetch is disabled")
		}
		if r.Header.Get("If-Modified-Since") != "" {
			t.Error("expected no If-Modified-Since header when ConditionalFetch is disabled")
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(testSpec))
	}))
	defer srv.Close()

	normalized := normalizeURL(srv.URL + "/spec.json")

	// Seed L2 with STALE data
	meta := types.DiskCacheMeta{
		URL:         normalized,
		Fingerprint: fingerprint([]byte(testSpec)),
		FetchedAt:   time.Now().Add(-2 * time.Hour),
		SpecSize:    int64(len(testSpec)),
		ETag:        `"some-etag"`,
		Summary: types.SpecSummary{
			Title:       "Test",
			Version:     "1.0",
			SpecVersion: "3.0.3",
		},
	}
	seedDiskCache(t, l.diskCache, normalized, []byte(testSpec), meta)

	// Should skip conditional fetch and do full fetch
	doc, summary, err := l.LoadFromURL(context.Background(), srv.URL+"/spec.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc == nil {
		t.Fatal("expected non-nil doc")
	}
	if summary.Title != "Test" {
		t.Errorf("expected title %q, got %q", "Test", summary.Title)
	}
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Errorf("expected 1 HTTP request (full fetch), got %d", requestCount)
	}
}

func TestLoadFromURL_FullFetch_NoDiskCache(t *testing.T) {
	// Loader without disk cache (backward compatibility)
	cfg := config.Config{
		CacheTTL:      5 * time.Minute,
		MaxCacheSize:  10,
		MaxSpecSize:   20 * 1024 * 1024,
		FetchTimeout:  5 * time.Second,
		DefaultFormat: "json",
		CacheDir:      "", // No disk cache
	}
	l := New(cfg)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(testSpec))
	}))
	defer srv.Close()

	doc, summary, err := l.LoadFromURL(context.Background(), srv.URL+"/spec.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc == nil {
		t.Fatal("expected non-nil doc")
	}
	if summary.Title != "Test" {
		t.Errorf("expected title %q, got %q", "Test", summary.Title)
	}
	if l.diskCache != nil {
		t.Error("expected nil diskCache when CacheDir is empty")
	}
}

func TestLoadFromURL_ConditionalFetch_4xx_NoFallback(t *testing.T) {
	l, _ := testLoaderWithDisk(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	normalized := normalizeURL(srv.URL + "/spec.json")

	// Seed L2 with STALE data
	meta := types.DiskCacheMeta{
		URL:         normalized,
		Fingerprint: fingerprint([]byte(testSpec)),
		FetchedAt:   time.Now().Add(-2 * time.Hour),
		SpecSize:    int64(len(testSpec)),
		ETag:        `"some-etag"`,
		Summary: types.SpecSummary{
			Title:       "Test",
			Version:     "1.0",
			SpecVersion: "3.0.3",
		},
	}
	seedDiskCache(t, l.diskCache, normalized, []byte(testSpec), meta)

	// HTTP 4xx should NOT use stale data — should return error
	_, _, err := l.LoadFromURL(context.Background(), srv.URL+"/spec.json")
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}

	toolErr, ok := err.(*types.ToolError)
	if !ok {
		t.Fatalf("expected *types.ToolError, got %T", err)
	}
	if toolErr.Code != types.ErrHTTPError {
		t.Errorf("expected error code %q, got %q", types.ErrHTTPError, toolErr.Code)
	}
}

func TestLoadFromURL_L1Hit_SkipsL2(t *testing.T) {
	l, _ := testLoaderWithDisk(t)

	var requestCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(testSpec))
	}))
	defer srv.Close()

	// First call — populates L1 and L2
	_, _, err := l.LoadFromURL(context.Background(), srv.URL+"/spec.json")
	if err != nil {
		t.Fatalf("unexpected error on first call: %v", err)
	}
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Fatalf("expected 1 HTTP request on first call, got %d", requestCount)
	}

	// Second call — should hit L1, no HTTP request
	doc, summary, err := l.LoadFromURL(context.Background(), srv.URL+"/spec.json")
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if doc == nil || summary == nil {
		t.Fatal("expected non-nil doc and summary from L1 hit")
	}
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Errorf("expected no additional HTTP requests (L1 hit), got %d total", requestCount)
	}
}

// --- SpecStatus tests ---

func TestSpecStatus_None(t *testing.T) {
	l, _ := testLoaderWithDisk(t)

	st := l.SpecStatus("https://example.com/spec.json")
	if st.Source != "none" {
		t.Errorf("expected source %q, got %q", "none", st.Source)
	}
	if st.Cached {
		t.Error("should not be cached")
	}
	if !st.DiskCacheEnabled {
		t.Error("disk cache should be enabled")
	}
	if st.DiskStats == nil {
		t.Error("expected non-nil DiskStats when disk cache is enabled")
	}
	if st.Summary != nil {
		t.Error("expected nil summary when not cached")
	}
}

func TestSpecStatus_Memory(t *testing.T) {
	l, _ := testLoaderWithDisk(t)

	// Serve a valid spec via httptest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(testSpec))
	}))
	defer srv.Close()

	// Load once to populate L1 memory cache
	_, _, err := l.LoadFromURL(context.Background(), srv.URL+"/spec.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	st := l.SpecStatus(srv.URL + "/spec.json")
	if st.Source != "memory" {
		t.Errorf("expected source %q, got %q", "memory", st.Source)
	}
	if !st.Cached {
		t.Error("expected cached=true")
	}
	if st.Summary == nil {
		t.Error("expected non-nil summary")
	}
	if st.Summary != nil && st.Summary.Title != "Test" {
		t.Errorf("expected summary title %q, got %q", "Test", st.Summary.Title)
	}
	// Disk meta should also be present (full fetch stores in both L1 and L2)
	if st.Fingerprint == "" {
		t.Error("expected non-empty fingerprint from disk meta")
	}
	if st.FetchedAt == nil {
		t.Error("expected non-nil FetchedAt from disk meta")
	}
}

func TestSpecStatus_Disk(t *testing.T) {
	l, _ := testLoaderWithDisk(t)

	normalized := "https://example.com/disk-only.json"

	// Seed L2 directly, bypassing L1
	meta := types.DiskCacheMeta{
		URL:          normalized,
		Fingerprint:  "sha256:abc123",
		FetchedAt:    time.Now().Add(-10 * time.Minute),
		SpecSize:     int64(len(testSpec)),
		ETag:         `"disk-etag"`,
		LastModified: "Mon, 01 Jan 2024 00:00:00 GMT",
		Summary: types.SpecSummary{
			Title:       "Disk Only",
			Version:     "1.0",
			SpecVersion: "3.0.3",
		},
	}
	seedDiskCache(t, l.diskCache, normalized, []byte(testSpec), meta)

	st := l.SpecStatus(normalized)
	if st.Source != "disk" {
		t.Errorf("expected source %q, got %q", "disk", st.Source)
	}
	if !st.Cached {
		t.Error("expected cached=true")
	}
	if st.Fingerprint != "sha256:abc123" {
		t.Errorf("expected fingerprint %q, got %q", "sha256:abc123", st.Fingerprint)
	}
	if st.ETag != `"disk-etag"` {
		t.Errorf("expected ETag %q, got %q", `"disk-etag"`, st.ETag)
	}
	if st.LastModified != "Mon, 01 Jan 2024 00:00:00 GMT" {
		t.Errorf("expected LastModified %q, got %q", "Mon, 01 Jan 2024 00:00:00 GMT", st.LastModified)
	}
	if st.Summary == nil {
		t.Fatal("expected non-nil summary")
	}
	if st.Summary.Title != "Disk Only" {
		t.Errorf("expected summary title %q, got %q", "Disk Only", st.Summary.Title)
	}
	if st.FetchedAt == nil {
		t.Error("expected non-nil FetchedAt")
	}
	if st.AgeSeconds < 600 {
		t.Errorf("expected AgeSeconds >= 600 (10 min ago), got %d", st.AgeSeconds)
	}
	if !st.DiskCacheEnabled {
		t.Error("expected DiskCacheEnabled=true")
	}
}

func TestSpecStatus_NoDiskCache(t *testing.T) {
	cfg := config.Config{
		CacheTTL:      5 * time.Minute,
		MaxCacheSize:  10,
		MaxSpecSize:   20 * 1024 * 1024,
		FetchTimeout:  5 * time.Second,
		DefaultFormat: "json",
		CacheDir:      "", // No disk cache
	}
	l := New(cfg)

	st := l.SpecStatus("https://example.com/spec.json")
	if st.DiskCacheEnabled {
		t.Error("disk cache should be disabled")
	}
	if st.Source != "none" {
		t.Errorf("expected source %q, got %q", "none", st.Source)
	}
	if st.Cached {
		t.Error("expected cached=false")
	}
	if st.DiskStats != nil {
		t.Error("expected nil DiskStats when disk cache is disabled")
	}
}

func TestStoreInDisk_NilDiskCache(t *testing.T) {
	// Loader without disk cache — storeInDisk should be a no-op
	cfg := config.Config{
		CacheTTL:      5 * time.Minute,
		MaxCacheSize:  10,
		MaxSpecSize:   20 * 1024 * 1024,
		FetchTimeout:  5 * time.Second,
		DefaultFormat: "json",
		CacheDir:      "",
	}
	l := New(cfg)

	// Should not panic
	l.storeInDisk("https://example.com/spec.json", []byte(testSpec), types.SpecSummary{}, nil)
}

func TestStoreInDisk_CapturesHeaders(t *testing.T) {
	l, _ := testLoaderWithDisk(t)

	resp := &http.Response{
		Header: http.Header{},
	}
	resp.Header.Set("ETag", `"test-etag"`)
	resp.Header.Set("Last-Modified", "Thu, 04 Jan 2024 00:00:00 GMT")

	normalized := "https://example.com/spec.json"
	summary := types.SpecSummary{Title: "Test", Version: "1.0"}

	l.storeInDisk(normalized, []byte(testSpec), summary, resp)

	// Verify stored meta
	_, meta, ok := l.diskCache.Get(normalized)
	if !ok {
		t.Fatal("expected disk cache entry")
	}
	if meta.ETag != `"test-etag"` {
		t.Errorf("expected ETag %q, got %q", `"test-etag"`, meta.ETag)
	}
	if meta.LastModified != "Thu, 04 Jan 2024 00:00:00 GMT" {
		t.Errorf("expected LastModified %q, got %q", "Thu, 04 Jan 2024 00:00:00 GMT", meta.LastModified)
	}
	if meta.Fingerprint == "" {
		t.Error("expected non-empty fingerprint")
	}
	if meta.Summary.Title != "Test" {
		t.Errorf("expected summary title %q, got %q", "Test", meta.Summary.Title)
	}
}

func TestLoadFromURL_DiskCache_SurvivesRestart(t *testing.T) {
	// Simulate process restart: create a loader, populate disk cache via HTTP,
	// then create a NEW loader pointing to the same cache dir,
	// and verify the spec is available without any HTTP requests.

	dir := t.TempDir()
	cfg := config.Config{
		CacheTTL:         5 * time.Minute,
		MaxCacheSize:     10,
		MaxSpecSize:      20 * 1024 * 1024,
		FetchTimeout:     5 * time.Second,
		DefaultFormat:    "json",
		CacheDir:         dir,
		DiskCacheTTL:     1 * time.Hour,
		ConditionalFetch: true,
		MaxDiskEntries:   10,
	}

	var requestCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"restart-etag"`)
		w.Write([]byte(testSpec))
	}))
	defer srv.Close()

	// First "process": create loader, load spec via HTTP
	loader1 := New(cfg)
	doc1, summary1, err := loader1.LoadFromURL(context.Background(), srv.URL+"/spec.json")
	if err != nil {
		t.Fatalf("first load error: %v", err)
	}
	if doc1 == nil || summary1 == nil {
		t.Fatal("expected non-nil doc and summary on first load")
	}
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Fatalf("expected 1 HTTP request on first load, got %d", requestCount)
	}

	// Second "process": create a brand new loader with the same cache dir
	// This simulates a process restart — fresh memory, same disk
	loader2 := New(cfg)

	// Should hit L2 disk cache, no new HTTP request
	doc2, summary2, err := loader2.LoadFromURL(context.Background(), srv.URL+"/spec.json")
	if err != nil {
		t.Fatalf("second load error: %v", err)
	}
	if doc2 == nil || summary2 == nil {
		t.Fatal("expected non-nil doc and summary from disk cache after restart")
	}
	if summary2.Title != "Test" {
		t.Errorf("expected title %q, got %q", "Test", summary2.Title)
	}
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Errorf("expected no additional HTTP requests after restart (L2 hit), got %d total", requestCount)
	}
}

func TestLoadFromURL_ConditionalHeaders_Sent(t *testing.T) {
	l, _ := testLoaderWithDisk(t)

	var capturedETag string
	var capturedLastModified string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedETag = r.Header.Get("If-None-Match")
		capturedLastModified = r.Header.Get("If-Modified-Since")
		// Return 304 to keep it simple
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	normalized := normalizeURL(srv.URL + "/headers-test.json")

	// Seed L2 with STALE data that has ETag and LastModified
	meta := types.DiskCacheMeta{
		URL:          normalized,
		Fingerprint:  fingerprint([]byte(testSpec)),
		FetchedAt:    time.Now().Add(-2 * time.Hour), // Stale
		SpecSize:     int64(len(testSpec)),
		ETag:         `"etag-verify"`,
		LastModified: "Fri, 05 Jan 2024 10:00:00 GMT",
		Summary: types.SpecSummary{
			Title:       "Test",
			Version:     "1.0",
			SpecVersion: "3.0.3",
		},
	}
	seedDiskCache(t, l.diskCache, normalized, []byte(testSpec), meta)

	// Load — should trigger conditional fetch since entry is stale
	_, _, err := l.LoadFromURL(context.Background(), srv.URL+"/headers-test.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the actual HTTP headers that were sent
	if capturedETag != `"etag-verify"` {
		t.Errorf("expected If-None-Match header %q, got %q", `"etag-verify"`, capturedETag)
	}
	if capturedLastModified != "Fri, 05 Jan 2024 10:00:00 GMT" {
		t.Errorf("expected If-Modified-Since header %q, got %q", "Fri, 05 Jan 2024 10:00:00 GMT", capturedLastModified)
	}
}
