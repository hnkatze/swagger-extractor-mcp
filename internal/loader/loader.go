package loader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/hnkatze/swagger-mcp-go/internal/config"
	"github.com/hnkatze/swagger-mcp-go/internal/types"
)

// Loader handles fetching and parsing OpenAPI specs.
type Loader struct {
	cache     *Cache
	diskCache *DiskCache
	config    config.Config
	client    *http.Client
}

// New creates a new Loader with the given configuration.
func New(cfg config.Config) *Loader {
	l := &Loader{
		cache:  NewCache(cfg.MaxCacheSize, cfg.CacheTTL),
		config: cfg,
		client: &http.Client{
			Timeout: cfg.FetchTimeout,
		},
	}

	// Initialize disk cache if CacheDir is configured
	if cfg.CacheDir != "" {
		dc, err := NewDiskCache(cfg.CacheDir, cfg.DiskCacheTTL, cfg.MaxDiskEntries)
		if err != nil {
			// Graceful degradation: log warning, continue without disk cache
			fmt.Fprintf(os.Stderr, "[swagger-mcp] WARNING: disk cache disabled: %v\n", err)
		} else {
			l.diskCache = dc
		}
	}

	return l
}

// LoadFromURL fetches an OpenAPI spec from a URL, parses it, validates it,
// caches the result, and returns the parsed document along with a summary.
//
// Cache hierarchy: L1 (memory) -> L2 (disk) -> conditional HTTP -> full fetch.
func (l *Loader) LoadFromURL(ctx context.Context, rawURL string) (*openapi3.T, *types.SpecSummary, error) {
	normalized := normalizeURL(rawURL)

	// L1: Check in-memory cache
	if doc, summary, ok := l.cache.Get(normalized); ok {
		return doc, summary, nil
	}

	// L2: Check disk cache
	if l.diskCache != nil && l.diskCache.Enabled() {
		diskData, meta, ok := l.diskCache.Get(normalized)
		if ok {
			if !l.diskCache.IsExpired(meta) {
				// Fresh disk data — promote to L1 without HTTP
				return l.promoteFromDisk(ctx, normalized, diskData, meta)
			}
			if l.config.ConditionalFetch {
				// Stale but has metadata — try conditional fetch
				return l.conditionalFetch(ctx, normalized, diskData, meta)
			}
		}
	}

	// Full HTTP fetch (existing logic, enhanced with L2 storage)
	return l.fullFetch(ctx, normalized, rawURL)
}

// promoteFromDisk re-parses disk-cached data, validates, and stores in L1.
// Returns the doc and summary, or error if parse/validate fails.
func (l *Loader) promoteFromDisk(ctx context.Context, normalized string, specData []byte, meta *types.DiskCacheMeta) (*openapi3.T, *types.SpecSummary, error) {
	doc, err := l.LoadFromData(specData)
	if err != nil {
		return nil, nil, err
	}

	// Validate — skip for Swagger 2.0 specs converted to v3
	if doc.OpenAPI != "" {
		if err := doc.Validate(ctx); err != nil {
			return nil, nil, &types.ToolError{
				Code:    types.ErrInvalidSpec,
				Message: "spec validation failed",
				Details: err.Error(),
			}
		}
	}

	// Use the summary stored in meta if available; otherwise rebuild
	summary := meta.Summary
	if summary.Title == "" && doc.Info != nil {
		summary = buildSummary(doc)
	}

	// Promote to L1
	l.cache.Set(normalized, doc, summary)

	return doc, &summary, nil
}

// conditionalFetch sends an HTTP request with If-None-Match / If-Modified-Since.
// Returns (doc, summary, error). On 304, uses diskData. On 200, parses new body.
// On network error, returns diskData as fallback.
func (l *Loader) conditionalFetch(ctx context.Context, normalized string, diskData []byte, meta *types.DiskCacheMeta) (*openapi3.T, *types.SpecSummary, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, normalized, nil)
	if err != nil {
		// Fall back to stale disk data
		fmt.Fprintf(os.Stderr, "[swagger-mcp] WARNING: conditional fetch request creation failed, using stale disk cache: %v\n", err)
		return l.promoteFromDisk(ctx, normalized, diskData, meta)
	}
	req.Header.Set("Accept", "application/json, application/yaml, application/x-yaml, text/yaml")

	if meta.ETag != "" {
		req.Header.Set("If-None-Match", meta.ETag)
	}
	if meta.LastModified != "" {
		req.Header.Set("If-Modified-Since", meta.LastModified)
	}

	resp, err := l.client.Do(req)
	if err != nil {
		// Network error — fallback to stale disk data
		fmt.Fprintf(os.Stderr, "[swagger-mcp] WARNING: conditional fetch failed, using stale disk cache: %v\n", err)
		return l.promoteFromDisk(ctx, normalized, diskData, meta)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		// 304: spec unchanged — refresh FetchedAt in L2 and promote to L1
		meta.FetchedAt = time.Now()
		l.diskCache.Set(normalized, diskData, *meta)
		return l.promoteFromDisk(ctx, normalized, diskData, meta)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// 200: new data — process like a full fetch
		limitReader := io.LimitReader(resp.Body, l.config.MaxSpecSize)
		data, err := io.ReadAll(limitReader)
		if err != nil {
			// Read error — fallback to stale disk data
			fmt.Fprintf(os.Stderr, "[swagger-mcp] WARNING: failed to read conditional fetch response, using stale disk cache: %v\n", err)
			return l.promoteFromDisk(ctx, normalized, diskData, meta)
		}

		doc, err := l.LoadFromData(data)
		if err != nil {
			return nil, nil, err
		}

		// Validate — skip for Swagger 2.0 specs
		if doc.OpenAPI != "" {
			if err := doc.Validate(ctx); err != nil {
				return nil, nil, &types.ToolError{
					Code:    types.ErrInvalidSpec,
					Message: "spec validation failed",
					Details: err.Error(),
				}
			}
		}

		summary := buildSummary(doc)
		l.cache.Set(normalized, doc, summary)
		l.storeInDisk(normalized, data, summary, resp)

		return doc, &summary, nil
	}

	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		// HTTP 4xx — do NOT use stale data, return the error
		return nil, nil, &types.ToolError{
			Code:    types.ErrHTTPError,
			Message: fmt.Sprintf("HTTP %d", resp.StatusCode),
			Details: fmt.Sprintf("server returned status %d for %s", resp.StatusCode, normalized),
		}
	}

	// Other status codes (5xx etc.) — fallback to stale disk data
	fmt.Fprintf(os.Stderr, "[swagger-mcp] WARNING: conditional fetch returned HTTP %d, using stale disk cache\n", resp.StatusCode)
	return l.promoteFromDisk(ctx, normalized, diskData, meta)
}

// storeInDisk saves spec data + metadata to the disk cache.
func (l *Loader) storeInDisk(normalized string, body []byte, summary types.SpecSummary, resp *http.Response) {
	if l.diskCache == nil || !l.diskCache.Enabled() {
		return
	}
	meta := types.DiskCacheMeta{
		URL:         normalized,
		Fingerprint: fingerprint(body),
		FetchedAt:   time.Now(),
		SpecSize:    int64(len(body)),
		Summary:     summary,
	}
	if resp != nil {
		meta.ETag = resp.Header.Get("ETag")
		meta.LastModified = resp.Header.Get("Last-Modified")
	}
	l.diskCache.Set(normalized, body, meta)
}

// fullFetch performs a full HTTP GET, parses, validates, and caches in L1+L2.
// This is the original LoadFromURL flow for when no cache hit is available.
func (l *Loader) fullFetch(ctx context.Context, normalized, rawURL string) (*openapi3.T, *types.SpecSummary, error) {
	// Validate URL
	parsed, err := url.ParseRequestURI(normalized)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, nil, &types.ToolError{
			Code:    types.ErrInvalidURL,
			Message: "invalid URL",
			Details: fmt.Sprintf("URL must be a valid HTTP or HTTPS URL: %s", rawURL),
		}
	}

	// Build request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, normalized, nil)
	if err != nil {
		return nil, nil, &types.ToolError{
			Code:    types.ErrInvalidURL,
			Message: "failed to create request",
			Details: err.Error(),
		}
	}
	req.Header.Set("Accept", "application/json, application/yaml, application/x-yaml, text/yaml")

	// Fetch
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, nil, &types.ToolError{
			Code:    types.ErrNetworkError,
			Message: "failed to fetch spec",
			Details: err.Error(),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, &types.ToolError{
			Code:    types.ErrHTTPError,
			Message: fmt.Sprintf("HTTP %d", resp.StatusCode),
			Details: fmt.Sprintf("server returned status %d for %s", resp.StatusCode, normalized),
		}
	}

	// Read body with size limit
	limitReader := io.LimitReader(resp.Body, l.config.MaxSpecSize)
	data, err := io.ReadAll(limitReader)
	if err != nil {
		return nil, nil, &types.ToolError{
			Code:    types.ErrNetworkError,
			Message: "failed to read response body",
			Details: err.Error(),
		}
	}

	// Parse
	doc, err := l.LoadFromData(data)
	if err != nil {
		return nil, nil, err
	}

	// Validate — skip for Swagger 2.0 specs converted to v3 by kin-openapi,
	// as the conversion may leave the openapi field empty which fails strict validation.
	if doc.OpenAPI != "" {
		if err := doc.Validate(ctx); err != nil {
			return nil, nil, &types.ToolError{
				Code:    types.ErrInvalidSpec,
				Message: "spec validation failed",
				Details: err.Error(),
			}
		}
	}

	// Build summary
	summary := buildSummary(doc)

	// Cache the result in L1
	l.cache.Set(normalized, doc, summary)

	// Store in L2 disk cache
	l.storeInDisk(normalized, data, summary, resp)

	return doc, &summary, nil
}

// LoadFromData parses raw bytes into an OpenAPI document.
func (l *Loader) LoadFromData(data []byte) (*openapi3.T, error) {
	oaLoader := openapi3.NewLoader()
	doc, err := oaLoader.LoadFromData(data)
	if err != nil {
		return nil, &types.ToolError{
			Code:    types.ErrParseError,
			Message: "failed to parse OpenAPI spec",
			Details: err.Error(),
		}
	}
	return doc, nil
}

// SpecStatus returns the current cache state for a URL.
// This is read-only — it never makes HTTP requests.
func (l *Loader) SpecStatus(rawURL string) *types.SpecStatus {
	normalized := normalizeURL(rawURL)
	status := &types.SpecStatus{
		URL: normalized,
	}

	// Check L1 (memory cache)
	if _, summary, ok := l.cache.Get(normalized); ok {
		status.Cached = true
		status.Source = "memory"
		status.Summary = summary
	}

	// Check L2 (disk cache) — supplements memory info or provides disk-only info
	if l.diskCache != nil && l.diskCache.Enabled() {
		status.DiskCacheEnabled = true

		// Get disk stats
		diskStats := l.diskCache.Stats()
		status.DiskStats = &diskStats

		// Get meta for this URL
		if meta, ok := l.diskCache.Meta(normalized); ok {
			if !status.Cached {
				status.Cached = true
				status.Source = "disk"
				status.Summary = &meta.Summary
			}
			status.Fingerprint = meta.Fingerprint
			status.ETag = meta.ETag
			status.LastModified = meta.LastModified
			fetchedAt := meta.FetchedAt
			status.FetchedAt = &fetchedAt
			status.AgeSeconds = int64(time.Since(meta.FetchedAt).Seconds())
		}
	}

	if !status.Cached {
		status.Source = "none"
	}

	return status
}

// Invalidate removes a URL from the cache.
func (l *Loader) Invalidate(rawURL string) {
	l.cache.Delete(normalizeURL(rawURL))
}

// normalizeURL normalizes a URL by lowercasing scheme+host and trimming trailing slashes.
func normalizeURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	// Lowercase scheme and host
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)

	// Trim trailing slash from path (unless path is just "/")
	if len(parsed.Path) > 1 {
		parsed.Path = strings.TrimRight(parsed.Path, "/")
	}

	return parsed.String()
}

// buildSummary extracts a SpecSummary from a parsed OpenAPI document.
func buildSummary(doc *openapi3.T) types.SpecSummary {
	summary := types.SpecSummary{}

	if doc.Info != nil {
		summary.Title = doc.Info.Title
		summary.Version = doc.Info.Version
		summary.Description = doc.Info.Description
	}

	// Determine base URL from servers
	if len(doc.Servers) > 0 && doc.Servers[0] != nil {
		summary.BaseURL = doc.Servers[0].URL
	}

	// Count endpoints (each method on each path counts as one)
	if doc.Paths != nil {
		for _, pathItem := range doc.Paths.Map() {
			if pathItem.Get != nil {
				summary.EndpointCount++
			}
			if pathItem.Post != nil {
				summary.EndpointCount++
			}
			if pathItem.Put != nil {
				summary.EndpointCount++
			}
			if pathItem.Delete != nil {
				summary.EndpointCount++
			}
			if pathItem.Patch != nil {
				summary.EndpointCount++
			}
			if pathItem.Head != nil {
				summary.EndpointCount++
			}
			if pathItem.Options != nil {
				summary.EndpointCount++
			}
			if pathItem.Trace != nil {
				summary.EndpointCount++
			}
		}
	}

	// Count tags
	summary.TagCount = len(doc.Tags)

	// Count schemas
	if doc.Components != nil && doc.Components.Schemas != nil {
		summary.SchemaCount = len(doc.Components.Schemas)
	}

	// Detect spec version
	summary.SpecVersion = doc.OpenAPI

	return summary
}
