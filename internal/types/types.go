package types

import "time"

// SpecSummary is returned by fetch_spec with basic spec info.
type SpecSummary struct {
	Title        string `json:"title"`
	Version      string `json:"version"`
	Description  string `json:"description,omitempty"`
	BaseURL      string `json:"base_url,omitempty"`
	EndpointCount int   `json:"endpoint_count"`
	TagCount     int    `json:"tag_count"`
	SchemaCount  int    `json:"schema_count"`
	SpecVersion  string `json:"spec_version"`
}

// EndpointSummary is a brief representation of an endpoint for listing.
type EndpointSummary struct {
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	Summary     string   `json:"summary,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Deprecated  bool     `json:"deprecated,omitempty"`
}

// EndpointDetail is the full representation of a single endpoint.
type EndpointDetail struct {
	Method      string              `json:"method"`
	Path        string              `json:"path"`
	OperationID string              `json:"operation_id,omitempty"`
	Summary     string              `json:"summary,omitempty"`
	Description string              `json:"description,omitempty"`
	Tags        []string            `json:"tags,omitempty"`
	Deprecated  bool                `json:"deprecated,omitempty"`
	Parameters  []ParameterDetail   `json:"parameters,omitempty"`
	RequestBody *RequestBodyDetail  `json:"request_body,omitempty"`
	Responses   []ResponseDetail    `json:"responses,omitempty"`
	Security    []map[string][]string `json:"security,omitempty"`
}

// ParameterDetail represents a single parameter.
type ParameterDetail struct {
	Name        string      `json:"name"`
	In          string      `json:"in"`
	Required    bool        `json:"required"`
	Description string      `json:"description,omitempty"`
	Schema      interface{} `json:"schema,omitempty"`
}

// RequestBodyDetail represents the request body.
type RequestBodyDetail struct {
	Required    bool                   `json:"required"`
	Description string                 `json:"description,omitempty"`
	Content     map[string]MediaDetail `json:"content,omitempty"`
}

// MediaDetail represents a media type content.
type MediaDetail struct {
	Schema interface{} `json:"schema"`
}

// ResponseDetail represents a single response.
type ResponseDetail struct {
	StatusCode  string                 `json:"status_code"`
	Description string                 `json:"description,omitempty"`
	Content     map[string]MediaDetail `json:"content,omitempty"`
}

// SchemaDetail represents a resolved schema.
type SchemaDetail struct {
	Name       string      `json:"name"`
	Schema     interface{} `json:"schema"`
}

// ListResult wraps a list of endpoints with truncation metadata.
type ListResult struct {
	Total     int               `json:"total"`
	Showing   int               `json:"showing"`
	Truncated bool              `json:"truncated"`
	Endpoints []EndpointSummary `json:"endpoints"`
}

// TagSummary represents tag analysis info.
type TagSummary struct {
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	EndpointCount  int            `json:"endpoint_count"`
	MethodBreakdown map[string]int `json:"method_breakdown"`
}

// DiffResult represents differences between two specs.
type DiffResult struct {
	Added   []EndpointSummary `json:"added,omitempty"`
	Removed []EndpointSummary `json:"removed,omitempty"`
	Changed []EndpointChange  `json:"changed,omitempty"`
}

// EndpointChange represents a changed endpoint between two specs.
type EndpointChange struct {
	Method  string   `json:"method"`
	Path    string   `json:"path"`
	Changes []string `json:"changes"`
}

// RefreshResult is returned by refresh_spec with before/after comparison.
type RefreshResult struct {
	URL            string      `json:"url"`
	Changed        bool        `json:"changed"`
	OldFingerprint string      `json:"old_fingerprint,omitempty"`
	NewFingerprint string      `json:"new_fingerprint,omitempty"`
	FetchDurationMs int64      `json:"fetch_duration_ms"`
	Summary        SpecSummary `json:"summary"`
}

// CacheEntry holds a cached spec with metadata.
type CacheEntry struct {
	URL       string
	FetchedAt time.Time
	Summary   SpecSummary
}

// DiskCacheMeta holds metadata for a disk-cached spec entry.
type DiskCacheMeta struct {
	URL          string      `json:"url"`
	ETag         string      `json:"etag,omitempty"`
	LastModified string      `json:"last_modified,omitempty"`
	Fingerprint  string      `json:"fingerprint"`
	FetchedAt    time.Time   `json:"fetched_at"`
	SpecSize     int64       `json:"spec_size"`
	Summary      SpecSummary `json:"summary"`
}

// DiskStats holds disk cache usage statistics.
type DiskStats struct {
	EntryCount int    `json:"entry_count"`
	TotalBytes int64  `json:"total_bytes"`
	CacheDir   string `json:"cache_dir"`
}

// SpecStatus holds the full status of a cached spec.
type SpecStatus struct {
	URL              string       `json:"url"`
	Cached           bool         `json:"cached"`
	Source           string       `json:"source"`
	Fingerprint      string       `json:"fingerprint,omitempty"`
	FetchedAt        *time.Time   `json:"fetched_at,omitempty"`
	AgeSeconds       int64        `json:"age_seconds,omitempty"`
	ETag             string       `json:"etag,omitempty"`
	LastModified     string       `json:"last_modified,omitempty"`
	Summary          *SpecSummary `json:"summary,omitempty"`
	DiskStats        *DiskStats   `json:"disk_stats,omitempty"`
	DiskCacheEnabled bool         `json:"disk_cache_enabled"`
}

// GenerateTypesResult is returned by generate_types with the generated code.
type GenerateTypesResult struct {
	Language string   `json:"language"`
	Types    string   `json:"types"`
	Names    []string `json:"names"`
}

// ToolError is a structured error response that implements the error interface.
type ToolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// Error returns a human-readable error string.
func (e *ToolError) Error() string {
	if e.Details != "" {
		return e.Code + ": " + e.Message + " (" + e.Details + ")"
	}
	return e.Code + ": " + e.Message
}

// Error codes
const (
	ErrInvalidURL          = "INVALID_URL"
	ErrNetworkError        = "NETWORK_ERROR"
	ErrHTTPError           = "HTTP_ERROR"
	ErrParseError          = "PARSE_ERROR"
	ErrInvalidSpec         = "INVALID_SPEC"
	ErrUnsupportedVersion  = "UNSUPPORTED_VERSION"
	ErrEndpointNotFound    = "ENDPOINT_NOT_FOUND"
	ErrSchemaNotFound      = "SCHEMA_NOT_FOUND"
	ErrInvalidQuery        = "INVALID_QUERY"
	ErrInvalidFormat       = "INVALID_FORMAT"
	ErrFetchFailed         = "FETCH_FAILED"
	ErrInternalError       = "INTERNAL_ERROR"
	ErrCacheError          = "CACHE_ERROR"
)

// OutputFormat for formatter selection.
type OutputFormat string

const (
	FormatJSON OutputFormat = "json"
	FormatTOON OutputFormat = "toon"
)
