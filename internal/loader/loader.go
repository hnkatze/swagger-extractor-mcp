package loader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/hnkatze/swagger-mcp-go/internal/config"
	"github.com/hnkatze/swagger-mcp-go/internal/types"
)

// Loader handles fetching and parsing OpenAPI specs.
type Loader struct {
	cache  *Cache
	config config.Config
	client *http.Client
}

// New creates a new Loader with the given configuration.
func New(cfg config.Config) *Loader {
	return &Loader{
		cache: NewCache(cfg.MaxCacheSize, cfg.CacheTTL),
		config: cfg,
		client: &http.Client{
			Timeout: cfg.FetchTimeout,
		},
	}
}

// LoadFromURL fetches an OpenAPI spec from a URL, parses it, validates it,
// caches the result, and returns the parsed document along with a summary.
func (l *Loader) LoadFromURL(ctx context.Context, rawURL string) (*openapi3.T, *types.SpecSummary, error) {
	normalized := normalizeURL(rawURL)

	// Check cache first
	if doc, summary, ok := l.cache.Get(normalized); ok {
		return doc, summary, nil
	}

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

	// Cache the result
	l.cache.Set(normalized, doc, summary)

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
