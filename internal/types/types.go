package types

// SpecSummary describes an indexed spec: the compact answer the indexer stores.
type SpecSummary struct {
	Title         string `json:"title"`
	Version       string `json:"version"`
	Description   string `json:"description,omitempty"`
	BaseURL       string `json:"base_url,omitempty"`
	EndpointCount int    `json:"endpoint_count"`
	TagCount      int    `json:"tag_count"`
	SchemaCount   int    `json:"schema_count"`
	SpecVersion   string `json:"spec_version"`
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
	Method      string                `json:"method"`
	Path        string                `json:"path"`
	OperationID string                `json:"operation_id,omitempty"`
	Summary     string                `json:"summary,omitempty"`
	Description string                `json:"description,omitempty"`
	Tags        []string              `json:"tags,omitempty"`
	Deprecated  bool                  `json:"deprecated,omitempty"`
	Parameters  []ParameterDetail     `json:"parameters,omitempty"`
	RequestBody *RequestBodyDetail    `json:"request_body,omitempty"`
	Responses   []ResponseDetail      `json:"responses,omitempty"`
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

// HeaderDetail represents a single response header.
type HeaderDetail struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Required    bool        `json:"required,omitempty"`
	Deprecated  bool        `json:"deprecated,omitempty"`
	Schema      interface{} `json:"schema,omitempty"`
}

// ResponseDetail represents a single response.
type ResponseDetail struct {
	StatusCode  string                 `json:"status_code"`
	Description string                 `json:"description,omitempty"`
	Headers     []HeaderDetail         `json:"headers,omitempty"`
	Content     map[string]MediaDetail `json:"content,omitempty"`
}

// SchemaDetail represents a resolved schema.
type SchemaDetail struct {
	Name   string      `json:"name"`
	Schema interface{} `json:"schema"`
}

// ListResult wraps a list of endpoints with truncation metadata.
type ListResult struct {
	Total     int               `json:"total"`
	Showing   int               `json:"showing"`
	Offset    int               `json:"offset,omitempty"`
	Truncated bool              `json:"truncated"`
	Endpoints []EndpointSummary `json:"endpoints"`
}

// SchemaInfo is one component schema as seen by list_schemas.
// UsedBy counts how many endpoints reference it, which is the only way to tell
// a core model from a one-off DTO when a spec has a thousand of them.
type SchemaInfo struct {
	Name        string `json:"name"`
	FieldCount  int    `json:"field_count"`
	UsedBy      int    `json:"used_by"`
	Description string `json:"description,omitempty"`
}

// SchemaListResult is a paged list of component schemas.
type SchemaListResult struct {
	Total     int          `json:"total"`
	Showing   int          `json:"showing"`
	Offset    int          `json:"offset,omitempty"`
	Truncated bool         `json:"truncated"`
	Schemas   []SchemaInfo `json:"schemas"`
}

// TagSummary represents tag analysis info.
type TagSummary struct {
	Name            string         `json:"name"`
	Description     string         `json:"description,omitempty"`
	EndpointCount   int            `json:"endpoint_count"`
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
	URL             string      `json:"url"`
	Changed         bool        `json:"changed"`
	OldFingerprint  string      `json:"old_fingerprint,omitempty"`
	NewFingerprint  string      `json:"new_fingerprint,omitempty"`
	FetchDurationMs int64       `json:"fetch_duration_ms"`
	Summary         SpecSummary `json:"summary"`
}

// GenerateTypesResult is returned by generate_types with the generated code.
type GenerateTypesResult struct {
	Language    string   `json:"language"`
	Types       string   `json:"types"`
	Names       []string `json:"names"`
	SchemaCount int      `json:"schema_count"`
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

// SecuritySchemeInfo describes a security scheme definition and its usage.
type SecuritySchemeInfo struct {
	Name             string          `json:"name"`
	Type             string          `json:"type"`
	Scheme           string          `json:"scheme,omitempty"`
	BearerFormat     string          `json:"bearer_format,omitempty"`
	In               string          `json:"in,omitempty"`
	ParamName        string          `json:"param_name,omitempty"`
	OpenIDConnectURL string          `json:"openid_connect_url,omitempty"`
	Description      string          `json:"description,omitempty"`
	Endpoints        int             `json:"endpoints"`
	Scopes           []string        `json:"scopes,omitempty"`
	Flows            []OAuthFlowInfo `json:"flows,omitempty"`
}

// OAuthFlowInfo holds the URLs of one OAuth2 flow.
type OAuthFlowInfo struct {
	Type             string `json:"type"`
	AuthorizationURL string `json:"authorization_url,omitempty"`
	TokenURL         string `json:"token_url,omitempty"`
	RefreshURL       string `json:"refresh_url,omitempty"`
}

// SecurityOverview lists the security schemes of a spec.
// Global holds the document-level requirement as alternatives: each outer entry
// is one way to authenticate and every name inside it is required together. In
// OpenAPI a requirement LIST is an OR of requirement OBJECTS, so
// [{Bearer: []}, {ApiKey: []}] means "Bearer OR ApiKey", not "both".
type SecurityOverview struct {
	Global                  [][]string           `json:"global,omitempty"`
	Schemes                 []SecuritySchemeInfo `json:"schemes"`
	UnauthenticatedOverride int                  `json:"unauthenticated_override"`
}

// ValidationReport is the verdict of validate_spec. Violations may be truncated:
// ViolationCount is always the full number found.
type ValidationReport struct {
	Valid          bool     `json:"valid"`
	ViolationCount int      `json:"violation_count"`
	Violations     []string `json:"violations"`
	SpecVersion    string   `json:"spec_version"`
	Title          string   `json:"title"`
}

// Error codes
const (
	ErrInvalidURL         = "INVALID_URL"
	ErrNetworkError       = "NETWORK_ERROR"
	ErrHTTPError          = "HTTP_ERROR"
	ErrParseError         = "PARSE_ERROR"
	ErrInvalidSpec        = "INVALID_SPEC"
	ErrUnsupportedVersion = "UNSUPPORTED_VERSION"
	ErrEndpointNotFound   = "ENDPOINT_NOT_FOUND"
	ErrSchemaNotFound     = "SCHEMA_NOT_FOUND"
	ErrInvalidQuery       = "INVALID_QUERY"
	ErrInvalidFormat      = "INVALID_FORMAT"
	ErrFetchFailed        = "FETCH_FAILED"
	ErrInternalError      = "INTERNAL_ERROR"
	ErrCacheError         = "CACHE_ERROR"
)

// OutputFormat for formatter selection.
type OutputFormat string

const (
	FormatJSON OutputFormat = "json"
	FormatTOON OutputFormat = "toon"
)
