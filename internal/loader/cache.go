package loader

import (
	"sync"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/hnkatze/swagger-mcp-go/internal/types"
)

// Cache is an in-memory LRU cache for parsed OpenAPI specs.
type Cache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	order   []string // LRU order (most recent at end)
	maxSize int
	ttl     time.Duration
}

type cacheEntry struct {
	doc       *openapi3.T
	summary   types.SpecSummary
	fetchedAt time.Time
}

// NewCache creates a new LRU cache with the given max size and TTL.
func NewCache(maxSize int, ttl time.Duration) *Cache {
	return &Cache{
		entries: make(map[string]*cacheEntry),
		order:   make([]string, 0, maxSize),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// Get returns the cached doc and summary for a URL if present and not expired.
// It updates the LRU order on a cache hit. Returns false if not found or expired.
func (c *Cache) Get(url string) (*openapi3.T, *types.SpecSummary, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[url]
	if !ok {
		return nil, nil, false
	}

	// Lazy TTL expiration: remove if expired
	if time.Since(entry.fetchedAt) > c.ttl {
		c.deleteLocked(url)
		return nil, nil, false
	}

	// Move to end of LRU order (most recently used)
	c.moveToEnd(url)

	summary := entry.summary
	return entry.doc, &summary, true
}

// Set adds or updates an entry in the cache. Evicts the oldest entry if at capacity.
func (c *Cache) Set(url string, doc *openapi3.T, summary types.SpecSummary) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If already present, update in place and refresh LRU position
	if _, ok := c.entries[url]; ok {
		c.entries[url] = &cacheEntry{
			doc:       doc,
			summary:   summary,
			fetchedAt: time.Now(),
		}
		c.moveToEnd(url)
		return
	}

	// Evict oldest if at capacity
	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	c.entries[url] = &cacheEntry{
		doc:       doc,
		summary:   summary,
		fetchedAt: time.Now(),
	}
	c.order = append(c.order, url)
}

// Delete removes a URL from the cache.
func (c *Cache) Delete(url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleteLocked(url)
}

// deleteLocked removes a URL from the cache without acquiring the lock.
// Caller must hold the write lock.
func (c *Cache) deleteLocked(url string) {
	delete(c.entries, url)
	c.removeFromOrder(url)
}

// evictOldest removes the least recently used entry from the cache.
// Caller must hold the write lock.
func (c *Cache) evictOldest() {
	if len(c.order) == 0 {
		return
	}
	oldest := c.order[0]
	c.order = c.order[1:]
	delete(c.entries, oldest)
}

// moveToEnd moves the given URL to the end of the LRU order list.
// Caller must hold the write lock.
func (c *Cache) moveToEnd(url string) {
	c.removeFromOrder(url)
	c.order = append(c.order, url)
}

// removeFromOrder removes a URL from the order slice.
// Caller must hold the write lock.
func (c *Cache) removeFromOrder(url string) {
	for i, u := range c.order {
		if u == url {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}
