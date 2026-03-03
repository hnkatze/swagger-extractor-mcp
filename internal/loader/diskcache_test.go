package loader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hnkatze/swagger-mcp-go/internal/types"
)

// newTestMeta creates a minimal DiskCacheMeta for testing.
func newTestMeta(t *testing.T, url string) types.DiskCacheMeta {
	t.Helper()
	return types.DiskCacheMeta{
		URL:         url,
		Fingerprint: "abc123",
		FetchedAt:   time.Now(),
		SpecSize:    42,
		Summary: types.SpecSummary{
			Title:   "Test API",
			Version: "1.0.0",
		},
	}
}

func TestNewDiskCache(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")

	dc, err := NewDiskCache(dir, 1*time.Hour, 50)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}
	if dc == nil {
		t.Fatal("expected non-nil DiskCache")
	}
	if dc.dir != dir {
		t.Errorf("expected dir %q, got %q", dir, dc.dir)
	}
	if dc.ttl != 1*time.Hour {
		t.Errorf("expected ttl %v, got %v", 1*time.Hour, dc.ttl)
	}
	if dc.maxEntries != 50 {
		t.Errorf("expected maxEntries %d, got %d", 50, dc.maxEntries)
	}
	if !dc.enabled {
		t.Error("expected enabled=true")
	}

	// Verify directory was created
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("cache dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected cache path to be a directory")
	}
}

func TestDiskCache_SetAndGet(t *testing.T) {
	dc, err := NewDiskCache(t.TempDir(), 1*time.Hour, 50)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}

	url := "https://example.com/api/spec.json"
	specData := []byte(`{"openapi":"3.0.3","info":{"title":"Test","version":"1.0"}}`)
	meta := newTestMeta(t, url)

	dc.Set(url, specData, meta)

	gotData, gotMeta, ok := dc.Get(url)
	if !ok {
		t.Fatal("expected cache hit, got miss")
	}
	if string(gotData) != string(specData) {
		t.Errorf("spec data mismatch: got %q, want %q", string(gotData), string(specData))
	}
	if gotMeta == nil {
		t.Fatal("expected non-nil meta")
	}
	if gotMeta.URL != url {
		t.Errorf("expected meta URL %q, got %q", url, gotMeta.URL)
	}
	if gotMeta.Fingerprint != "abc123" {
		t.Errorf("expected fingerprint %q, got %q", "abc123", gotMeta.Fingerprint)
	}
	if gotMeta.Summary.Title != "Test API" {
		t.Errorf("expected summary title %q, got %q", "Test API", gotMeta.Summary.Title)
	}
}

func TestDiskCache_GetMiss(t *testing.T) {
	dc, err := NewDiskCache(t.TempDir(), 1*time.Hour, 50)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}

	data, meta, ok := dc.Get("https://nonexistent.com/spec.json")
	if ok {
		t.Fatal("expected cache miss, got hit")
	}
	if data != nil {
		t.Error("expected nil data on miss")
	}
	if meta != nil {
		t.Error("expected nil meta on miss")
	}
}

func TestDiskCache_Meta(t *testing.T) {
	dc, err := NewDiskCache(t.TempDir(), 1*time.Hour, 50)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}

	url := "https://example.com/meta-test.json"
	specData := []byte(`{"test":"data"}`)
	meta := newTestMeta(t, url)
	meta.ETag = `"etag-abc"`
	meta.LastModified = "Wed, 01 Jan 2025 00:00:00 GMT"

	dc.Set(url, specData, meta)

	gotMeta, ok := dc.Meta(url)
	if !ok {
		t.Fatal("expected meta hit, got miss")
	}
	if gotMeta.URL != url {
		t.Errorf("expected URL %q, got %q", url, gotMeta.URL)
	}
	if gotMeta.ETag != `"etag-abc"` {
		t.Errorf("expected ETag %q, got %q", `"etag-abc"`, gotMeta.ETag)
	}
	if gotMeta.LastModified != "Wed, 01 Jan 2025 00:00:00 GMT" {
		t.Errorf("expected LastModified %q, got %q", "Wed, 01 Jan 2025 00:00:00 GMT", gotMeta.LastModified)
	}

	// Meta miss
	_, ok = dc.Meta("https://nonexistent.com/spec.json")
	if ok {
		t.Error("expected meta miss for nonexistent URL")
	}
}

func TestDiskCache_Delete(t *testing.T) {
	dc, err := NewDiskCache(t.TempDir(), 1*time.Hour, 50)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}

	url := "https://example.com/delete-me.json"
	dc.Set(url, []byte(`{"test":true}`), newTestMeta(t, url))

	// Verify it exists
	_, _, ok := dc.Get(url)
	if !ok {
		t.Fatal("expected entry to exist before deletion")
	}

	dc.Delete(url)

	// Verify it's gone
	_, _, ok = dc.Get(url)
	if ok {
		t.Error("expected entry to be absent after deletion")
	}

	// Verify files are removed
	hash := hashURL(url)
	if _, err := os.Stat(dc.specPath(hash)); !os.IsNotExist(err) {
		t.Error("expected spec file to be removed")
	}
	if _, err := os.Stat(dc.metaPath(hash)); !os.IsNotExist(err) {
		t.Error("expected meta file to be removed")
	}
}

func TestDiskCache_Stats(t *testing.T) {
	dc, err := NewDiskCache(t.TempDir(), 1*time.Hour, 50)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}

	// Empty cache
	stats := dc.Stats()
	if stats.EntryCount != 0 {
		t.Errorf("expected 0 entries, got %d", stats.EntryCount)
	}
	if stats.TotalBytes != 0 {
		t.Errorf("expected 0 bytes, got %d", stats.TotalBytes)
	}

	// Add two entries
	specA := []byte(`{"spec":"a"}`)
	specB := []byte(`{"spec":"b","extra":"data"}`)

	dc.Set("https://a.com/spec.json", specA, newTestMeta(t, "https://a.com/spec.json"))
	dc.Set("https://b.com/spec.json", specB, newTestMeta(t, "https://b.com/spec.json"))

	stats = dc.Stats()
	if stats.EntryCount != 2 {
		t.Errorf("expected 2 entries, got %d", stats.EntryCount)
	}
	expectedBytes := int64(len(specA) + len(specB))
	if stats.TotalBytes != expectedBytes {
		t.Errorf("expected %d bytes, got %d", expectedBytes, stats.TotalBytes)
	}
	if stats.CacheDir != dc.dir {
		t.Errorf("expected CacheDir %q, got %q", dc.dir, stats.CacheDir)
	}
}

func TestDiskCache_IsExpired(t *testing.T) {
	dc, err := NewDiskCache(t.TempDir(), 100*time.Millisecond, 50)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}

	freshMeta := &types.DiskCacheMeta{FetchedAt: time.Now()}
	if dc.IsExpired(freshMeta) {
		t.Error("expected fresh entry to not be expired")
	}

	staleMeta := &types.DiskCacheMeta{FetchedAt: time.Now().Add(-1 * time.Hour)}
	if !dc.IsExpired(staleMeta) {
		t.Error("expected stale entry to be expired")
	}
}

func TestDiskCache_EvictOldest(t *testing.T) {
	dc, err := NewDiskCache(t.TempDir(), 1*time.Hour, 3)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}

	urls := []string{
		"https://example.com/first.json",
		"https://example.com/second.json",
		"https://example.com/third.json",
	}

	// Add 3 entries with staggered mtimes
	for i, url := range urls {
		dc.Set(url, []byte(`{"n":`+string(rune('1'+i))+`}`), newTestMeta(t, url))
		// Ensure distinguishable mtime ordering on the filesystem
		hash := hashURL(url)
		past := time.Now().Add(time.Duration(i) * time.Second)
		_ = os.Chtimes(dc.metaPath(hash), past, past)
		_ = os.Chtimes(dc.specPath(hash), past, past)
	}

	// Adding a 4th entry should trigger eviction of the oldest (first)
	fourthURL := "https://example.com/fourth.json"
	dc.Set(fourthURL, []byte(`{"n":4}`), newTestMeta(t, fourthURL))

	// First (oldest mtime) should be evicted
	_, _, ok := dc.Get(urls[0])
	if ok {
		t.Error("expected first (oldest) entry to be evicted")
	}

	// Others should still be present
	for _, url := range urls[1:] {
		_, _, ok := dc.Get(url)
		if !ok {
			t.Errorf("expected %q to still be present after eviction", url)
		}
	}
	_, _, ok = dc.Get(fourthURL)
	if !ok {
		t.Error("expected fourth entry to be present")
	}
}

func TestDiskCache_CorruptMeta(t *testing.T) {
	dir := t.TempDir()
	dc, err := NewDiskCache(dir, 1*time.Hour, 50)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}

	url := "https://example.com/corrupt-meta.json"
	dc.Set(url, []byte(`{"valid":"spec"}`), newTestMeta(t, url))

	// Corrupt the meta file
	hash := hashURL(url)
	if err := os.WriteFile(dc.metaPath(hash), []byte(`{invalid json`), 0640); err != nil {
		t.Fatalf("failed to corrupt meta: %v", err)
	}

	// Get should return false and self-heal
	data, meta, ok := dc.Get(url)
	if ok {
		t.Error("expected miss for corrupt meta")
	}
	if data != nil {
		t.Error("expected nil data for corrupt meta")
	}
	if meta != nil {
		t.Error("expected nil meta for corrupt meta")
	}

	// Both files should be cleaned up
	if _, err := os.Stat(dc.metaPath(hash)); !os.IsNotExist(err) {
		t.Error("expected meta file to be removed after self-healing")
	}
	if _, err := os.Stat(dc.specPath(hash)); !os.IsNotExist(err) {
		t.Error("expected spec file to be removed after self-healing")
	}
}

func TestDiskCache_CorruptSpec(t *testing.T) {
	dir := t.TempDir()
	dc, err := NewDiskCache(dir, 1*time.Hour, 50)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}

	url := "https://example.com/corrupt-spec.json"
	dc.Set(url, []byte(`{"valid":"spec"}`), newTestMeta(t, url))

	// Remove the spec file to simulate corruption/missing
	hash := hashURL(url)
	if err := os.Remove(dc.specPath(hash)); err != nil {
		t.Fatalf("failed to remove spec file: %v", err)
	}

	// Get should return false and self-heal
	data, meta, ok := dc.Get(url)
	if ok {
		t.Error("expected miss for missing spec")
	}
	if data != nil {
		t.Error("expected nil data for missing spec")
	}
	if meta != nil {
		t.Error("expected nil meta for missing spec")
	}

	// Meta file should also be cleaned up
	if _, err := os.Stat(dc.metaPath(hash)); !os.IsNotExist(err) {
		t.Error("expected meta file to be removed after self-healing")
	}
}

func TestDiskCache_CorruptMetaInMetaOnly(t *testing.T) {
	dir := t.TempDir()
	dc, err := NewDiskCache(dir, 1*time.Hour, 50)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}

	url := "https://example.com/corrupt-meta-only.json"
	dc.Set(url, []byte(`{"valid":"spec"}`), newTestMeta(t, url))

	// Corrupt the meta file
	hash := hashURL(url)
	if err := os.WriteFile(dc.metaPath(hash), []byte(`not json`), 0640); err != nil {
		t.Fatalf("failed to corrupt meta: %v", err)
	}

	// Meta-only read should also self-heal
	gotMeta, ok := dc.Meta(url)
	if ok {
		t.Error("expected miss for corrupt meta via Meta()")
	}
	if gotMeta != nil {
		t.Error("expected nil from Meta() with corrupt data")
	}

	// Both files cleaned up
	if _, err := os.Stat(dc.metaPath(hash)); !os.IsNotExist(err) {
		t.Error("expected meta file removed after Meta() self-heal")
	}
	if _, err := os.Stat(dc.specPath(hash)); !os.IsNotExist(err) {
		t.Error("expected spec file removed after Meta() self-heal")
	}
}

func TestDiskCache_CleanStaleTmps(t *testing.T) {
	dir := t.TempDir()

	// Create some .tmp files before constructing the cache
	tmpFiles := []string{"abc123.spec.json.tmp", "def456.meta.json.tmp", "random.tmp"}
	for _, name := range tmpFiles {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("stale"), 0640); err != nil {
			t.Fatalf("failed to create tmp file: %v", err)
		}
	}

	// Also create a non-tmp file that should NOT be removed
	keepFile := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(keepFile, []byte("keep me"), 0640); err != nil {
		t.Fatalf("failed to create keep file: %v", err)
	}

	dc, err := NewDiskCache(dir, 1*time.Hour, 50)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}
	_ = dc

	// Verify .tmp files are removed
	for _, name := range tmpFiles {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed, but it still exists", name)
		}
	}

	// Verify non-tmp file is untouched
	if _, err := os.Stat(keepFile); err != nil {
		t.Error("expected keep.txt to still exist")
	}
}

func TestHashURL(t *testing.T) {
	// Deterministic
	hash1 := hashURL("https://example.com/spec.json")
	hash2 := hashURL("https://example.com/spec.json")
	if hash1 != hash2 {
		t.Error("expected hashURL to be deterministic")
	}

	// Consistent length (SHA-256 hex = 64 chars)
	if len(hash1) != 64 {
		t.Errorf("expected hash length 64, got %d", len(hash1))
	}

	// Different URLs produce different hashes
	hash3 := hashURL("https://other.com/spec.json")
	if hash1 == hash3 {
		t.Error("expected different URLs to produce different hashes")
	}
}

func TestDiskCache_Enabled(t *testing.T) {
	dc, err := NewDiskCache(t.TempDir(), 1*time.Hour, 50)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}
	if !dc.Enabled() {
		t.Error("expected Enabled() to return true")
	}
}

func TestDiskCache_OverwriteExisting(t *testing.T) {
	dc, err := NewDiskCache(t.TempDir(), 1*time.Hour, 50)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}

	url := "https://example.com/overwrite.json"
	dc.Set(url, []byte(`{"version":"v1"}`), newTestMeta(t, url))

	// Overwrite with new data
	updatedMeta := newTestMeta(t, url)
	updatedMeta.Fingerprint = "new-fingerprint"
	dc.Set(url, []byte(`{"version":"v2"}`), updatedMeta)

	gotData, gotMeta, ok := dc.Get(url)
	if !ok {
		t.Fatal("expected cache hit after overwrite")
	}
	if string(gotData) != `{"version":"v2"}` {
		t.Errorf("expected updated spec data, got %q", string(gotData))
	}
	if gotMeta.Fingerprint != "new-fingerprint" {
		t.Errorf("expected updated fingerprint %q, got %q", "new-fingerprint", gotMeta.Fingerprint)
	}
}

func TestDiskCache_MetaJSONRoundTrip(t *testing.T) {
	dc, err := NewDiskCache(t.TempDir(), 1*time.Hour, 50)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}

	url := "https://example.com/roundtrip.json"
	original := types.DiskCacheMeta{
		URL:          url,
		ETag:         `W/"abc"`,
		LastModified: "Sat, 01 Feb 2025 12:00:00 GMT",
		Fingerprint:  "sha256hex",
		FetchedAt:    time.Now().Truncate(time.Second),
		SpecSize:     1024,
		Summary: types.SpecSummary{
			Title:         "Round Trip API",
			Version:       "2.0.0",
			EndpointCount: 15,
			TagCount:      3,
			SchemaCount:   8,
			SpecVersion:   "3.1.0",
		},
	}

	dc.Set(url, []byte(`{}`), original)

	gotMeta, ok := dc.Meta(url)
	if !ok {
		t.Fatal("expected meta hit")
	}

	// Compare fields
	if gotMeta.URL != original.URL {
		t.Errorf("URL: got %q, want %q", gotMeta.URL, original.URL)
	}
	if gotMeta.ETag != original.ETag {
		t.Errorf("ETag: got %q, want %q", gotMeta.ETag, original.ETag)
	}
	if gotMeta.LastModified != original.LastModified {
		t.Errorf("LastModified: got %q, want %q", gotMeta.LastModified, original.LastModified)
	}
	if gotMeta.Fingerprint != original.Fingerprint {
		t.Errorf("Fingerprint: got %q, want %q", gotMeta.Fingerprint, original.Fingerprint)
	}
	if !gotMeta.FetchedAt.Equal(original.FetchedAt) {
		t.Errorf("FetchedAt: got %v, want %v", gotMeta.FetchedAt, original.FetchedAt)
	}
	if gotMeta.SpecSize != original.SpecSize {
		t.Errorf("SpecSize: got %d, want %d", gotMeta.SpecSize, original.SpecSize)
	}
	if gotMeta.Summary.Title != original.Summary.Title {
		t.Errorf("Summary.Title: got %q, want %q", gotMeta.Summary.Title, original.Summary.Title)
	}
	if gotMeta.Summary.EndpointCount != original.Summary.EndpointCount {
		t.Errorf("Summary.EndpointCount: got %d, want %d", gotMeta.Summary.EndpointCount, original.Summary.EndpointCount)
	}
}

func TestDiskCache_WriteAtomicIntegrity(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "atomic-test.json")
	data := []byte(`{"atomic":"write"}`)

	if err := writeAtomic(target, data); err != nil {
		t.Fatalf("writeAtomic error: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("data mismatch: got %q, want %q", string(got), string(data))
	}

	// Verify .tmp file doesn't linger
	tmpPath := target + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("expected .tmp file to be cleaned up after atomic write")
	}
}

func TestDiskCache_DeleteNonexistent(t *testing.T) {
	dc, err := NewDiskCache(t.TempDir(), 1*time.Hour, 50)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}

	// Should not panic or error
	dc.Delete("https://nonexistent.com/spec.json")
}

func TestDiskCache_MultipleURLs(t *testing.T) {
	dc, err := NewDiskCache(t.TempDir(), 1*time.Hour, 50)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}

	urls := []string{
		"https://api1.example.com/spec.json",
		"https://api2.example.com/spec.json",
		"https://api3.example.com/spec.json",
	}

	for i, url := range urls {
		data := []byte(`{"api":` + string(rune('1'+i)) + `}`)
		dc.Set(url, data, newTestMeta(t, url))
	}

	for _, url := range urls {
		_, _, ok := dc.Get(url)
		if !ok {
			t.Errorf("expected hit for %q", url)
		}
	}

	stats := dc.Stats()
	if stats.EntryCount != 3 {
		t.Errorf("expected 3 entries, got %d", stats.EntryCount)
	}
}

func TestDiskCache_StatsAfterDelete(t *testing.T) {
	dc, err := NewDiskCache(t.TempDir(), 1*time.Hour, 50)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}

	url := "https://example.com/stats-delete.json"
	dc.Set(url, []byte(`{"data":"test"}`), newTestMeta(t, url))

	stats := dc.Stats()
	if stats.EntryCount != 1 {
		t.Fatalf("expected 1 entry, got %d", stats.EntryCount)
	}

	dc.Delete(url)

	stats = dc.Stats()
	if stats.EntryCount != 0 {
		t.Errorf("expected 0 entries after delete, got %d", stats.EntryCount)
	}
	if stats.TotalBytes != 0 {
		t.Errorf("expected 0 bytes after delete, got %d", stats.TotalBytes)
	}
}

func TestDiskCache_MetaFileFormat(t *testing.T) {
	dc, err := NewDiskCache(t.TempDir(), 1*time.Hour, 50)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}

	url := "https://example.com/format-check.json"
	meta := newTestMeta(t, url)
	dc.Set(url, []byte(`{}`), meta)

	// Read the raw meta file and verify it's valid JSON
	hash := hashURL(url)
	raw, err := os.ReadFile(dc.metaPath(hash))
	if err != nil {
		t.Fatalf("failed to read meta file: %v", err)
	}

	var decoded types.DiskCacheMeta
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("meta file is not valid JSON: %v", err)
	}
	if decoded.URL != url {
		t.Errorf("expected URL %q in meta file, got %q", url, decoded.URL)
	}
}

func TestFingerprint(t *testing.T) {
	// Known input → known output
	data := []byte(`{"openapi":"3.0.0"}`)
	fp := fingerprint(data)

	if !strings.HasPrefix(fp, "sha256:") {
		t.Errorf("fingerprint should start with 'sha256:', got %s", fp)
	}
	if len(fp) != 7+64 { // "sha256:" + 64 hex chars
		t.Errorf("fingerprint should be 71 chars, got %d", len(fp))
	}

	// Deterministic
	fp2 := fingerprint(data)
	if fp != fp2 {
		t.Error("fingerprint should be deterministic")
	}

	// Different data → different fingerprint
	fp3 := fingerprint([]byte(`{"openapi":"3.1.0"}`))
	if fp == fp3 {
		t.Error("different data should produce different fingerprint")
	}

	// Empty data produces valid hash
	fp4 := fingerprint([]byte{})
	if !strings.HasPrefix(fp4, "sha256:") {
		t.Error("empty data should produce valid fingerprint")
	}
}

func TestDiskCache_FingerprintIntegrity(t *testing.T) {
	dc, err := NewDiskCache(t.TempDir(), 1*time.Hour, 50)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}

	url := "https://example.com/fp-integrity.json"
	specData := []byte(`{"openapi":"3.0.3","info":{"title":"FP Test","version":"1.0"},"paths":{}}`)
	correctFP := fingerprint(specData)

	meta := types.DiskCacheMeta{
		URL:         url,
		Fingerprint: correctFP,
		FetchedAt:   time.Now(),
		SpecSize:    int64(len(specData)),
	}
	dc.Set(url, specData, meta)

	// Verify stored fingerprint matches computed fingerprint
	gotData, gotMeta, ok := dc.Get(url)
	if !ok {
		t.Fatal("expected cache hit")
	}
	gotFP := fingerprint(gotData)
	if gotFP != gotMeta.Fingerprint {
		t.Errorf("fingerprint mismatch: stored=%q, computed=%q", gotMeta.Fingerprint, gotFP)
	}

	// Now corrupt the spec file on disk by overwriting with different content
	hash := hashURL(url)
	corruptData := []byte(`{"corrupted":true}`)
	if err := os.WriteFile(dc.specPath(hash), corruptData, 0640); err != nil {
		t.Fatalf("failed to corrupt spec file: %v", err)
	}

	// Get() still succeeds (it reads files but doesn't re-verify fingerprints)
	gotData2, gotMeta2, ok2 := dc.Get(url)
	if !ok2 {
		t.Fatal("expected cache hit even with corrupted spec (no fingerprint check on read)")
	}
	// The returned data should be the corrupted content
	if string(gotData2) != string(corruptData) {
		t.Errorf("expected corrupted data back, got %q", string(gotData2))
	}
	// The fingerprint in meta no longer matches the actual data
	recomputedFP := fingerprint(gotData2)
	if recomputedFP == gotMeta2.Fingerprint {
		t.Error("expected fingerprint mismatch after corruption, but they match")
	}
}

func TestDiskCache_ConcurrentAccess(t *testing.T) {
	dc, err := NewDiskCache(t.TempDir(), 1*time.Hour, 50)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}

	const goroutines = 20
	const opsPerGoroutine = 10
	var wg sync.WaitGroup

	// Concurrent Set/Get/Delete — verify no panics
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				url := fmt.Sprintf("https://example.com/concurrent-%d-%d.json", id, j)
				specData := []byte(fmt.Sprintf(`{"id":%d,"iter":%d}`, id, j))
				meta := types.DiskCacheMeta{
					URL:         url,
					Fingerprint: fingerprint(specData),
					FetchedAt:   time.Now(),
					SpecSize:    int64(len(specData)),
				}

				// Set
				dc.Set(url, specData, meta)

				// Get
				dc.Get(url)

				// Stats
				dc.Stats()

				// Meta
				dc.Meta(url)

				// Delete (every other iteration)
				if j%2 == 0 {
					dc.Delete(url)
				}
			}
		}(i)
	}

	wg.Wait()

	// If we get here without panic, the test passes
	stats := dc.Stats()
	if stats.EntryCount < 0 {
		t.Error("expected non-negative entry count")
	}
}

func TestDiskCache_MaxEntriesEnforced(t *testing.T) {
	maxEntries := 5
	dc, err := NewDiskCache(t.TempDir(), 1*time.Hour, maxEntries)
	if err != nil {
		t.Fatalf("NewDiskCache error: %v", err)
	}

	// Add maxEntries+5 entries with staggered mtimes to ensure eviction ordering
	totalEntries := maxEntries + 5
	for i := 0; i < totalEntries; i++ {
		url := fmt.Sprintf("https://example.com/max-%d.json", i)
		specData := []byte(fmt.Sprintf(`{"n":%d}`, i))
		meta := types.DiskCacheMeta{
			URL:         url,
			Fingerprint: fingerprint(specData),
			FetchedAt:   time.Now(),
			SpecSize:    int64(len(specData)),
		}
		dc.Set(url, specData, meta)

		// Stagger mtimes so eviction order is deterministic
		hash := hashURL(url)
		past := time.Now().Add(time.Duration(i) * time.Second)
		_ = os.Chtimes(dc.metaPath(hash), past, past)
		_ = os.Chtimes(dc.specPath(hash), past, past)
	}

	// Verify the total count never exceeds maxEntries+1
	// (maxEntries is the target after eviction; the entry that triggers eviction
	// may temporarily bring us to maxEntries+1 before evictIfNeeded runs,
	// but after Set completes, eviction has already run)
	stats := dc.Stats()
	if stats.EntryCount > maxEntries {
		t.Errorf("expected at most %d entries after eviction, got %d", maxEntries, stats.EntryCount)
	}
}
