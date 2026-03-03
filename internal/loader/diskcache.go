package loader

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hnkatze/swagger-mcp-go/internal/types"
)

// DiskCache is a file-system-backed cache for raw OpenAPI spec data.
// Thread-safe via sync.Mutex. Uses atomic writes (temp + rename).
type DiskCache struct {
	mu         sync.Mutex
	dir        string
	ttl        time.Duration
	maxEntries int
	enabled    bool
}

// NewDiskCache creates a new DiskCache backed by the given directory.
// It creates the directory if it does not exist and cleans up stale .tmp files.
// Returns an error if the directory cannot be created.
func NewDiskCache(dir string, ttl time.Duration, maxEntries int) (*DiskCache, error) {
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, err
	}

	dc := &DiskCache{
		dir:        dir,
		ttl:        ttl,
		maxEntries: maxEntries,
		enabled:    true,
	}

	dc.cleanStaleTmps()

	return dc, nil
}

// Get returns the cached spec data and metadata for the given URL.
// Returns the data regardless of TTL — the caller uses IsExpired to decide
// whether the entry is stale. Updates file mtime for LRU tracking on a hit.
// Returns ok=false if the entry is missing or corrupt.
func (dc *DiskCache) Get(normalizedURL string) ([]byte, *types.DiskCacheMeta, bool) {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	hash := hashURL(normalizedURL)

	metaBytes, err := os.ReadFile(dc.metaPath(hash))
	if err != nil {
		return nil, nil, false
	}

	var meta types.DiskCacheMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		// Corrupt meta — self-heal by deleting both files
		dc.deleteLocked(hash)
		return nil, nil, false
	}

	specData, err := os.ReadFile(dc.specPath(hash))
	if err != nil {
		// Spec file missing or unreadable — self-heal
		dc.deleteLocked(hash)
		return nil, nil, false
	}

	// Touch mtime for LRU tracking
	now := time.Now()
	_ = os.Chtimes(dc.metaPath(hash), now, now)
	_ = os.Chtimes(dc.specPath(hash), now, now)

	return specData, &meta, true
}

// Set writes the spec data and metadata for the given URL to disk.
// Uses atomic writes (temp file + rename) to avoid partial files.
// Evicts oldest entries if maxEntries is exceeded. Errors are logged
// but not propagated (best-effort caching).
func (dc *DiskCache) Set(normalizedURL string, specData []byte, meta types.DiskCacheMeta) {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	hash := hashURL(normalizedURL)

	metaBytes, err := json.Marshal(meta)
	if err != nil {
		log.Printf("[disk-cache] failed to marshal meta for %s: %v", normalizedURL, err)
		return
	}

	if err := writeAtomic(dc.metaPath(hash), metaBytes); err != nil {
		log.Printf("[disk-cache] failed to write meta for %s: %v", normalizedURL, err)
		return
	}

	if err := writeAtomic(dc.specPath(hash), specData); err != nil {
		log.Printf("[disk-cache] failed to write spec for %s: %v", normalizedURL, err)
		// Clean up the meta file we just wrote
		_ = os.Remove(dc.metaPath(hash))
		return
	}

	dc.evictIfNeeded()
}

// Meta returns only the metadata for the given URL (no spec data).
// Useful for lightweight status checks without reading the full spec.
func (dc *DiskCache) Meta(normalizedURL string) (*types.DiskCacheMeta, bool) {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	hash := hashURL(normalizedURL)

	metaBytes, err := os.ReadFile(dc.metaPath(hash))
	if err != nil {
		return nil, false
	}

	var meta types.DiskCacheMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		dc.deleteLocked(hash)
		return nil, false
	}

	return &meta, true
}

// Delete removes both the spec and meta files for the given URL.
func (dc *DiskCache) Delete(normalizedURL string) {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	hash := hashURL(normalizedURL)
	dc.deleteLocked(hash)
}

// Stats returns disk cache usage statistics.
func (dc *DiskCache) Stats() types.DiskStats {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	stats := types.DiskStats{CacheDir: dc.dir}

	entries, err := os.ReadDir(dc.dir)
	if err != nil {
		return stats
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".meta.json") {
			stats.EntryCount++
		}
		if strings.HasSuffix(name, ".spec.json") {
			info, err := entry.Info()
			if err == nil {
				stats.TotalBytes += info.Size()
			}
		}
	}

	return stats
}

// IsExpired returns true if the given metadata's FetchedAt is older than the TTL.
func (dc *DiskCache) IsExpired(meta *types.DiskCacheMeta) bool {
	return time.Since(meta.FetchedAt) > dc.ttl
}

// Enabled returns true if the disk cache is active.
func (dc *DiskCache) Enabled() bool {
	return dc.enabled
}

// --- Internal helpers ---

// hashURL returns the SHA-256 hex digest of the given URL string.
func hashURL(normalizedURL string) string {
	h := sha256.Sum256([]byte(normalizedURL))
	return hex.EncodeToString(h[:])
}

// fingerprint computes SHA-256 hex digest of raw spec bytes for change detection.
// Returns "sha256:<hex>" format.
func fingerprint(data []byte) string {
	h := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(h[:])
}

// specPath returns the full file path for a spec data file.
func (dc *DiskCache) specPath(hash string) string {
	return filepath.Join(dc.dir, hash+".spec.json")
}

// metaPath returns the full file path for a metadata file.
func (dc *DiskCache) metaPath(hash string) string {
	return filepath.Join(dc.dir, hash+".meta.json")
}

// writeAtomic writes data to a temporary file then renames it to the target path.
// This prevents partial/corrupt files from being visible.
func writeAtomic(targetPath string, data []byte) error {
	tmpPath := targetPath + ".tmp"

	if err := os.WriteFile(tmpPath, data, 0640); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	return nil
}

// deleteLocked removes both spec and meta files for the given hash.
// Caller must hold the mutex.
func (dc *DiskCache) deleteLocked(hash string) {
	_ = os.Remove(dc.specPath(hash))
	_ = os.Remove(dc.metaPath(hash))
}

// evictIfNeeded removes the oldest entries (by mtime) if the entry count exceeds maxEntries.
// Caller must hold the mutex.
func (dc *DiskCache) evictIfNeeded() {
	entries, err := os.ReadDir(dc.dir)
	if err != nil {
		return
	}

	// Collect meta files with their modification times
	type metaFile struct {
		hash    string
		modTime time.Time
	}
	var metas []metaFile

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".meta.json") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		hash := strings.TrimSuffix(name, ".meta.json")
		metas = append(metas, metaFile{hash: hash, modTime: info.ModTime()})
	}

	if len(metas) <= dc.maxEntries {
		return
	}

	// Sort by modification time ascending (oldest first)
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].modTime.Before(metas[j].modTime)
	})

	// Remove oldest entries until we're at capacity
	toRemove := len(metas) - dc.maxEntries
	for i := 0; i < toRemove; i++ {
		dc.deleteLocked(metas[i].hash)
	}
}

// cleanStaleTmps removes any leftover .tmp files from interrupted writes.
func (dc *DiskCache) cleanStaleTmps() {
	entries, err := os.ReadDir(dc.dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".tmp") {
			_ = os.Remove(filepath.Join(dc.dir, entry.Name()))
		}
	}
}
