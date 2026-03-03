package loader

import (
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/hnkatze/swagger-mcp-go/internal/types"
)

// newTestDoc creates a minimal openapi3.T with the given title for testing.
func newTestDoc(t *testing.T, title string) *openapi3.T {
	t.Helper()
	return &openapi3.T{
		OpenAPI: "3.0.3",
		Info: &openapi3.Info{
			Title:   title,
			Version: "1.0.0",
		},
	}
}

// newTestSummary creates a minimal SpecSummary with the given title for testing.
func newTestSummary(t *testing.T, title string) types.SpecSummary {
	t.Helper()
	return types.SpecSummary{
		Title:   title,
		Version: "1.0.0",
	}
}

func TestCache_SetAndGet(t *testing.T) {
	cache := NewCache(10, 5*time.Minute)

	doc := newTestDoc(t, "Test API")
	summary := newTestSummary(t, "Test API")

	cache.Set("https://example.com/spec.json", doc, summary)

	gotDoc, gotSummary, ok := cache.Get("https://example.com/spec.json")
	if !ok {
		t.Fatal("expected cache hit, got miss")
	}
	if gotDoc == nil {
		t.Fatal("expected non-nil doc")
	}
	if gotDoc.Info.Title != "Test API" {
		t.Errorf("expected doc title %q, got %q", "Test API", gotDoc.Info.Title)
	}
	if gotSummary == nil {
		t.Fatal("expected non-nil summary")
	}
	if gotSummary.Title != "Test API" {
		t.Errorf("expected summary title %q, got %q", "Test API", gotSummary.Title)
	}
	if gotSummary.Version != "1.0.0" {
		t.Errorf("expected summary version %q, got %q", "1.0.0", gotSummary.Version)
	}
}

func TestCache_Miss(t *testing.T) {
	cache := NewCache(10, 5*time.Minute)

	gotDoc, gotSummary, ok := cache.Get("https://nonexistent.com/spec.json")
	if ok {
		t.Fatal("expected cache miss, got hit")
	}
	if gotDoc != nil {
		t.Error("expected nil doc on cache miss")
	}
	if gotSummary != nil {
		t.Error("expected nil summary on cache miss")
	}
}

func TestCache_Expiration(t *testing.T) {
	cache := NewCache(10, 1*time.Millisecond)

	doc := newTestDoc(t, "Expiring API")
	summary := newTestSummary(t, "Expiring API")

	cache.Set("https://example.com/expire.json", doc, summary)

	// Wait for TTL to expire
	time.Sleep(5 * time.Millisecond)

	_, _, ok := cache.Get("https://example.com/expire.json")
	if ok {
		t.Fatal("expected cache miss after TTL expiration, got hit")
	}
}

func TestCache_LRUEviction(t *testing.T) {
	cache := NewCache(2, 5*time.Minute)

	// Add 3 items to a cache with maxSize=2
	cache.Set("https://example.com/first.json", newTestDoc(t, "First"), newTestSummary(t, "First"))
	cache.Set("https://example.com/second.json", newTestDoc(t, "Second"), newTestSummary(t, "Second"))
	cache.Set("https://example.com/third.json", newTestDoc(t, "Third"), newTestSummary(t, "Third"))

	// The first (oldest) entry should have been evicted
	_, _, ok := cache.Get("https://example.com/first.json")
	if ok {
		t.Error("expected first entry to be evicted, but it was found")
	}

	// Second and third should still be present
	_, _, ok = cache.Get("https://example.com/second.json")
	if !ok {
		t.Error("expected second entry to be present, but it was not found")
	}

	_, _, ok = cache.Get("https://example.com/third.json")
	if !ok {
		t.Error("expected third entry to be present, but it was not found")
	}
}

func TestCache_LRUOrder(t *testing.T) {
	cache := NewCache(3, 5*time.Minute)

	// Add 3 items
	cache.Set("https://example.com/a.json", newTestDoc(t, "A"), newTestSummary(t, "A"))
	cache.Set("https://example.com/b.json", newTestDoc(t, "B"), newTestSummary(t, "B"))
	cache.Set("https://example.com/c.json", newTestDoc(t, "C"), newTestSummary(t, "C"))

	// Access "A" to move it to most-recently-used position
	_, _, ok := cache.Get("https://example.com/a.json")
	if !ok {
		t.Fatal("expected A to be present before eviction test")
	}

	// LRU order should now be: B (oldest), C, A (newest)
	// Adding a 4th item should evict B (the least recently used)
	cache.Set("https://example.com/d.json", newTestDoc(t, "D"), newTestSummary(t, "D"))

	// B should have been evicted (least recently used)
	_, _, ok = cache.Get("https://example.com/b.json")
	if ok {
		t.Error("expected B to be evicted as least recently used, but it was found")
	}

	// A should still be present (was accessed, so not LRU)
	_, _, ok = cache.Get("https://example.com/a.json")
	if !ok {
		t.Error("expected A to still be present after accessing it, but it was not found")
	}

	// C and D should also be present
	_, _, ok = cache.Get("https://example.com/c.json")
	if !ok {
		t.Error("expected C to be present, but it was not found")
	}

	_, _, ok = cache.Get("https://example.com/d.json")
	if !ok {
		t.Error("expected D to be present, but it was not found")
	}
}

func TestCache_Delete(t *testing.T) {
	cache := NewCache(10, 5*time.Minute)

	doc := newTestDoc(t, "Delete Me")
	summary := newTestSummary(t, "Delete Me")

	cache.Set("https://example.com/delete.json", doc, summary)

	// Verify it was added
	_, _, ok := cache.Get("https://example.com/delete.json")
	if !ok {
		t.Fatal("expected entry to be present before deletion")
	}

	// Delete it
	cache.Delete("https://example.com/delete.json")

	// Verify it's gone
	_, _, ok = cache.Get("https://example.com/delete.json")
	if ok {
		t.Error("expected entry to be absent after deletion, but it was found")
	}
}

func TestCache_UpdateExisting(t *testing.T) {
	cache := NewCache(10, 5*time.Minute)

	// Set initial value
	cache.Set("https://example.com/update.json", newTestDoc(t, "Original"), newTestSummary(t, "Original"))

	// Update with new value
	updatedDoc := newTestDoc(t, "Updated")
	updatedSummary := types.SpecSummary{
		Title:         "Updated",
		Version:       "2.0.0",
		EndpointCount: 10,
	}
	cache.Set("https://example.com/update.json", updatedDoc, updatedSummary)

	// Verify the updated values
	gotDoc, gotSummary, ok := cache.Get("https://example.com/update.json")
	if !ok {
		t.Fatal("expected cache hit after update, got miss")
	}
	if gotDoc.Info.Title != "Updated" {
		t.Errorf("expected updated doc title %q, got %q", "Updated", gotDoc.Info.Title)
	}
	if gotSummary.Title != "Updated" {
		t.Errorf("expected updated summary title %q, got %q", "Updated", gotSummary.Title)
	}
	if gotSummary.Version != "2.0.0" {
		t.Errorf("expected updated version %q, got %q", "2.0.0", gotSummary.Version)
	}
	if gotSummary.EndpointCount != 10 {
		t.Errorf("expected updated endpoint count %d, got %d", 10, gotSummary.EndpointCount)
	}
}
