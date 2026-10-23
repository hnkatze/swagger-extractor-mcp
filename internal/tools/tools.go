package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/hnkatze/swagger-mcp-go/internal/analyzer"
	"github.com/hnkatze/swagger-mcp-go/internal/config"
	"github.com/hnkatze/swagger-mcp-go/internal/extractor"
	"github.com/hnkatze/swagger-mcp-go/internal/formatter"
	"github.com/hnkatze/swagger-mcp-go/internal/generator"
	"github.com/hnkatze/swagger-mcp-go/internal/loader"
	"github.com/hnkatze/swagger-mcp-go/internal/types"
)

// llmGuide is the on-demand usage guide returned by the usage_guide tool. It is
// embedded at build time so the server can document itself to an LLM that lacks
// the project's CLAUDE.md / system-prompt context.
//
//go:embed llm_guide.md
var llmGuide string

// Registry holds the shared dependencies for all tool handlers.
type Registry struct {
	loader *loader.Loader
	cfg    config.Config

	// mu guards active, the session default URL shared by concurrent tool calls.
	mu     sync.RWMutex
	active string
}

// New creates a new tool Registry with the given config.
func New(cfg config.Config) *Registry {
	return &Registry{
		loader: loader.New(cfg),
		cfg:    cfg,
		active: cfg.DefaultURL,
	}
}

func (r *Registry) setActiveURL(url string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active = url
}

func (r *Registry) activeURL() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.active
}

// resolveURL returns the explicit url argument, else the session default,
// else "" (the caller reports the error).
func (r *Registry) resolveURL(req mcp.CallToolRequest) string {
	if url := getStringArg(req, "url"); url != "" {
		return url
	}
	return r.activeURL()
}

// Register registers all MCP tools on the server.
func (r *Registry) Register(s *server.MCPServer) {
	s.AddTool(fetchSpecTool(), r.handleFetchSpec)
	s.AddTool(listEndpointsTool(), r.handleListEndpoints)
	s.AddTool(getEndpointTool(), r.handleGetEndpoint)
	s.AddTool(getSchemaTool(), r.handleGetSchema)
	s.AddTool(searchSpecTool(), r.handleSearchSpec)
	s.AddTool(listSchemasTool(), r.handleListSchemas)
	s.AddTool(securitySchemesTool(), r.handleListSecuritySchemes)
	s.AddTool(analyzeTagsTool(), r.handleAnalyzeTags)
	s.AddTool(diffEndpointsTool(), r.handleDiffEndpoints)
	s.AddTool(specStatusTool(), r.handleSpecStatus)
	s.AddTool(refreshSpecTool(), r.handleRefreshSpec)
	s.AddTool(generateTypesTool(), r.handleGenerateTypes)
	s.AddTool(usageGuideTool(), r.handleUsageGuide)
}

// --- Tool Definitions ---

func fetchSpecTool() mcp.Tool {
	return mcp.NewTool("fetch_spec",
		mcp.WithDescription("Fetch & cache a spec, returns a compact summary (title, version, counts). Call FIRST. refresh=true bypasses cache."),
		mcp.WithString("url", mcp.Description("Spec URL (omit to use the session default set by fetch_spec)")),
		mcp.WithBoolean("refresh", mcp.Description("true to force a fresh fetch, bypassing caches")),
	)
}

func listEndpointsTool() mcp.Tool {
	return mcp.NewTool("list_endpoints",
		mcp.WithDescription("List endpoints (METHOD path — summary [tags]). Run analyze_tags first, then filter by tag/method/path here. Auto-limited to 50; always filter rather than browse all."),
		mcp.WithString("url", mcp.Description("Spec URL (omit to use the session default set by fetch_spec)")),
		mcp.WithString("tag", mcp.Description("Filter by tag (case-insensitive); discover via analyze_tags")),
		mcp.WithString("method", mcp.Description("Filter by HTTP method")),
		mcp.WithString("path_pattern", mcp.Description("Filter by path substring, e.g. '/users'")),
		mcp.WithBoolean("deprecated", mcp.Description("true = only deprecated endpoints, false = only non-deprecated; omit for both")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 50, 0 = unlimited)")),
		mcp.WithNumber("offset", mcp.Description("Skip this many results (default 0); use with limit to page")),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func getEndpointTool() mcp.Tool {
	return mcp.NewTool("get_endpoint",
		mcp.WithDescription("Full detail of one endpoint: params, request body, responses, resolved schemas (with field descriptions). Shared schemas are shown once then referenced as $ref(Name) — fetch those via get_schema. Use after list_endpoints/search_spec."),
		mcp.WithString("url", mcp.Description("Spec URL (omit to use the session default set by fetch_spec)")),
		mcp.WithString("method", mcp.Required(), mcp.Description("HTTP method")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Endpoint path, e.g. '/pets/{petId}'")),
		mcp.WithNumber("resolve_depth", mcp.Description("Schema nesting depth 0-10 (default 3). Raise for deep models, lower for fewer tokens. 0 = names only.")),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func getSchemaTool() mcp.Tool {
	return mcp.NewTool("get_schema",
		mcp.WithDescription("Resolve a named schema/model with field types and descriptions. Use to expand a $ref(Name) seen in get_endpoint."),
		mcp.WithString("url", mcp.Description("Spec URL (omit to use the session default set by fetch_spec)")),
		mcp.WithString("name", mcp.Required(), mcp.Description("Schema name, e.g. 'User'")),
		mcp.WithNumber("resolve_depth", mcp.Description("Schema nesting depth 0-10 (default 3). 0 = names only.")),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func searchSpecTool() mcp.Tool {
	return mcp.NewTool("search_spec",
		mcp.WithDescription("Keyword search across paths, summaries, descriptions, operation IDs, and body field names. Ranked by relevance, auto-limited to 50. Use specific keywords."),
		mcp.WithString("url", mcp.Description("Spec URL (omit to use the session default set by fetch_spec)")),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query (case-insensitive)")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 50, 0 = unlimited)")),
		mcp.WithNumber("offset", mcp.Description("Skip this many results (default 0); use with limit to page")),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func listSchemasTool() mcp.Tool {
	return mcp.NewTool("list_schemas",
		mcp.WithDescription("List component schemas with field count and usage (endpoints referencing each). Call get_schema with a name to see its fields. Auto-limited to 50; use filter to narrow."),
		mcp.WithString("url", mcp.Description("Spec URL (omit to use the session default set by fetch_spec)")),
		mcp.WithString("filter", mcp.Description("Filter by schema name substring (case-insensitive)")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 50, 0 = unlimited)")),
		mcp.WithNumber("offset", mcp.Description("Skip this many results (default 0); use with limit to page")),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func securitySchemesTool() mcp.Tool {
	return mcp.NewTool("security_schemes",
		mcp.WithDescription("Security scheme definitions (type, scheme, bearer format, apiKey location, OAuth flow URLs/scopes) with how many endpoints use each. Use after get_endpoint shows a security requirement."),
		mcp.WithString("url", mcp.Description("Spec URL (omit to use the session default set by fetch_spec)")),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func analyzeTagsTool() mcp.Tool {
	return mcp.NewTool("analyze_tags",
		mcp.WithDescription("Tags with endpoint counts and method breakdown. START HERE to map the API, then filter list_endpoints by tag."),
		mcp.WithString("url", mcp.Description("Spec URL (omit to use the session default set by fetch_spec)")),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func diffEndpointsTool() mcp.Tool {
	return mcp.NewTool("diff_endpoints",
		mcp.WithDescription("Compare two spec versions: added, removed, changed endpoints."),
		mcp.WithString("url_old", mcp.Required(), mcp.Description("Old spec URL")),
		mcp.WithString("url_new", mcp.Required(), mcp.Description("New spec URL")),
		mcp.WithString("path", mcp.Description("Filter by path substring")),
		mcp.WithString("method", mcp.Description("Filter by HTTP method")),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func specStatusTool() mcp.Tool {
	return mcp.NewTool("spec_status",
		mcp.WithDescription("Cache status without fetching: source, fingerprint, age, disk stats."),
		mcp.WithString("url", mcp.Description("Spec URL (omit to use the session default set by fetch_spec)")),
	)
}

func refreshSpecTool() mcp.Tool {
	return mcp.NewTool("refresh_spec",
		mcp.WithDescription("Invalidate caches and re-fetch. Reports whether the spec changed (fingerprint) plus a fresh summary. Use when the API spec was updated."),
		mcp.WithString("url", mcp.Description("Spec URL (omit to use the session default set by fetch_spec)")),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func generateTypesTool() mcp.Tool {
	return mcp.NewTool("generate_types",
		mcp.WithDescription("Emit copy-paste-ready type definitions (TypeScript interfaces or Go structs) from a tag, a named schema, or an endpoint. Tag mode covers every schema a tag uses, deduplicated and sorted by name. Precedence: tag wins over schema, which wins over method+path."),
		mcp.WithString("url", mcp.Description("Spec URL (omit to use the session default set by fetch_spec)")),
		mcp.WithString("tag", mcp.Description("Generate types for every schema reachable from this tag's endpoints")),
		mcp.WithString("method", mcp.Description("HTTP method (with path = endpoint mode)")),
		mcp.WithString("path", mcp.Description("Endpoint path (with method = endpoint mode)")),
		mcp.WithString("schema", mcp.Description("Schema name (alternative to method+path)")),
		mcp.WithString("language", mcp.Description("typescript (default) or go")),
		mcp.WithString("format", mcp.Description("toon (default, plain code) or json (with metadata)")),
	)
}

func usageGuideTool() mcp.Tool {
	return mcp.NewTool("usage_guide",
		mcp.WithDescription("Return how to use this server efficiently: workflow, token-saving rules, output notation ($ref/required/depth), and example call sequences. Call this first if you're unsure how to explore an API."),
	)
}

// --- Helpers ---

func getStringArg(req mcp.CallToolRequest, name string) string {
	args := req.GetArguments()
	if v, ok := args[name]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getBoolArg handles both JSON boolean and string "true"/"false" from MCP clients.
func getBoolArg(req mcp.CallToolRequest, name string) bool {
	args := req.GetArguments()
	v, ok := args[name]
	if !ok {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return strings.EqualFold(val, "true")
	default:
		return false
	}
}

// getOptionalBoolArg returns nil when the argument is absent, otherwise a pointer
// to its value (accepts JSON booleans and "true"/"false" strings).
func getOptionalBoolArg(req mcp.CallToolRequest, name string) *bool {
	if _, ok := req.GetArguments()[name]; !ok {
		return nil
	}
	v := getBoolArg(req, name)
	return &v
}

// getFormat returns the output format from the request, falling back to config default.
func (r *Registry) getFormat(req mcp.CallToolRequest) types.OutputFormat {
	f := strings.ToLower(getStringArg(req, "format"))
	switch f {
	case "toon":
		return types.FormatTOON
	case "json":
		return types.FormatJSON
	default:
		// Fall back to config default
		if strings.ToLower(r.cfg.DefaultFormat) == "json" {
			return types.FormatJSON
		}
		return types.FormatTOON
	}
}

// getIntArg resolves an integer argument from any shape an MCP client may send:
// decoded JSON numbers arrive as float64, older clients send strings, and Go
// callers may pass int. Returns ok=false when absent or not a whole number.
func getIntArg(req mcp.CallToolRequest, name string) (int, bool) {
	raw, ok := req.GetArguments()[name]
	if !ok {
		return 0, false
	}
	switch v := raw.(type) {
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		return int(v), true
	case float32:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n), true
		}
		return 0, false
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n, true
		}
		return 0, false
	default:
		return 0, false
	}
}

// getLimit returns the result limit from the request, falling back to config default.
func (r *Registry) getLimit(req mcp.CallToolRequest) int {
	if n, ok := getIntArg(req, "limit"); ok && n >= 0 {
		return n
	}
	return r.cfg.DefaultLimit
}

// getOffset returns the paging offset from the request; 0 when absent or negative.
func getOffset(req mcp.CallToolRequest) int {
	if n, ok := getIntArg(req, "offset"); ok && n > 0 {
		return n
	}
	return 0
}

// pageWindow computes the [start, end) slice of a total-sized result set for the
// request's offset and limit. truncated is true when more results follow end.
func (r *Registry) pageWindow(req mcp.CallToolRequest, total int) (start, end int, truncated bool) {
	start = getOffset(req)
	if start > total {
		start = total
	}
	end = total
	limit := r.getLimit(req)
	if limit > 0 && total-start > limit {
		end = start + limit
		truncated = true
	}
	return start, end, truncated
}

// getResolveDepth returns the schema resolution depth from the request.
// Returns -1 if not provided (meaning use default), or 0-10 for explicit values.
func getResolveDepth(req mcp.CallToolRequest) int {
	n, ok := getIntArg(req, "resolve_depth")
	if !ok || n < 0 || n > 10 {
		return -1 // not provided or invalid, use default
	}
	return n
}

func (r *Registry) loadSpec(ctx context.Context, url string) (*openapi3.T, error) {
	doc, _, err := r.loader.LoadFromURL(ctx, url)
	return doc, err
}

const errURLRequired = "url is required: pass url or call fetch_spec first to set a session default"

func toolError(code, message string) *mcp.CallToolResult {
	return mcp.NewToolResultError(fmt.Sprintf("[%s] %s", code, message))
}

// --- Handlers ---

func (r *Registry) handleFetchSpec(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	url := r.resolveURL(req)
	if url == "" {
		return toolError(types.ErrInvalidURL, errURLRequired), nil
	}

	refresh := getBoolArg(req, "refresh")

	var summary *types.SpecSummary
	var err error

	if refresh {
		// Force-refresh: invalidate caches and re-fetch
		_, refreshResult, fetchErr := r.loader.ForceLoadFromURL(ctx, url)
		if fetchErr != nil {
			return toolError(types.ErrFetchFailed, fetchErr.Error()), nil
		}
		summary = &refreshResult.Summary
	} else {
		_, summary, err = r.loader.LoadFromURL(ctx, url)
		if err != nil {
			return toolError(types.ErrFetchFailed, err.Error()), nil
		}
	}

	r.setActiveURL(url)

	output, fmtErr := formatter.FormatJSON(summary)
	if fmtErr != nil {
		return toolError(types.ErrInternalError, fmtErr.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func (r *Registry) handleListEndpoints(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	url := r.resolveURL(req)
	if url == "" {
		return toolError(types.ErrInvalidURL, errURLRequired), nil
	}

	doc, err := r.loadSpec(ctx, url)
	if err != nil {
		return toolError(types.ErrFetchFailed, err.Error()), nil
	}

	tag := getStringArg(req, "tag")
	method := getStringArg(req, "method")
	pathPattern := getStringArg(req, "path_pattern")

	endpoints := analyzer.ListEndpoints(doc, analyzer.ListFilter{
		Tag:         tag,
		Method:      method,
		PathPattern: pathPattern,
		Deprecated:  getOptionalBoolArg(req, "deprecated"),
	})

	// Apply offset/limit with truncation metadata
	total := len(endpoints)
	start, end, truncated := r.pageWindow(req, total)
	endpoints = endpoints[start:end]

	format := r.getFormat(req)

	var output string
	if format == types.FormatTOON {
		stripped := formatter.StripDescriptions(endpoints)
		result := &types.ListResult{
			Total:     total,
			Showing:   len(stripped),
			Offset:    start,
			Truncated: truncated,
			Endpoints: stripped,
		}
		output = formatter.FormatListResultTOON(result)
	} else {
		result := &types.ListResult{
			Total:     total,
			Showing:   len(endpoints),
			Offset:    start,
			Truncated: truncated,
			Endpoints: endpoints,
		}
		var fmtErr error
		output, fmtErr = formatter.FormatListResultJSON(result)
		if fmtErr != nil {
			return toolError(types.ErrInternalError, fmtErr.Error()), nil
		}
	}

	return mcp.NewToolResultText(output), nil
}

func (r *Registry) handleGetEndpoint(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	url := r.resolveURL(req)
	if url == "" {
		return toolError(types.ErrInvalidURL, errURLRequired), nil
	}

	method := getStringArg(req, "method")
	path := getStringArg(req, "path")
	if method == "" || path == "" {
		return toolError(types.ErrEndpointNotFound, "method and path are required"), nil
	}

	doc, err := r.loadSpec(ctx, url)
	if err != nil {
		return toolError(types.ErrFetchFailed, err.Error()), nil
	}

	resolveDepth := getResolveDepth(req)
	detail, err := extractor.GetEndpoint(doc, method, path, resolveDepth)
	if err != nil {
		return toolError(types.ErrEndpointNotFound, fmt.Sprintf("%s %s not found", strings.ToUpper(method), path)), nil
	}

	format := r.getFormat(req)
	var output string
	if format == types.FormatTOON {
		output = formatter.FormatEndpointTOON(detail)
	} else {
		var fmtErr error
		output, fmtErr = formatter.FormatEndpointJSON(detail)
		if fmtErr != nil {
			return toolError(types.ErrInternalError, fmtErr.Error()), nil
		}
	}

	return mcp.NewToolResultText(output), nil
}

func (r *Registry) handleGetSchema(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	url := r.resolveURL(req)
	if url == "" {
		return toolError(types.ErrInvalidURL, errURLRequired), nil
	}

	name := getStringArg(req, "name")
	if name == "" {
		return toolError(types.ErrSchemaNotFound, "schema name is required"), nil
	}

	doc, err := r.loadSpec(ctx, url)
	if err != nil {
		return toolError(types.ErrFetchFailed, err.Error()), nil
	}

	resolveDepth := getResolveDepth(req)
	schema, err := extractor.GetSchema(doc, name, resolveDepth)
	if err != nil {
		return toolError(types.ErrSchemaNotFound, fmt.Sprintf("schema '%s' not found", name)), nil
	}

	format := r.getFormat(req)
	var output string
	if format == types.FormatTOON {
		output = formatter.FormatSchemaTOON(schema)
	} else {
		var fmtErr error
		output, fmtErr = formatter.FormatSchemaJSON(schema)
		if fmtErr != nil {
			return toolError(types.ErrInternalError, fmtErr.Error()), nil
		}
	}

	return mcp.NewToolResultText(output), nil
}

func (r *Registry) handleSearchSpec(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	url := r.resolveURL(req)
	if url == "" {
		return toolError(types.ErrInvalidURL, errURLRequired), nil
	}

	query := getStringArg(req, "query")
	if query == "" {
		return toolError(types.ErrInvalidQuery, "search query is required"), nil
	}

	doc, err := r.loadSpec(ctx, url)
	if err != nil {
		return toolError(types.ErrFetchFailed, err.Error()), nil
	}

	results := analyzer.SearchSpec(doc, query)

	if len(results) == 0 {
		return mcp.NewToolResultText("No endpoints found matching query: " + query), nil
	}

	// Apply offset/limit with truncation metadata
	total := len(results)
	start, end, truncated := r.pageWindow(req, total)
	results = results[start:end]

	format := r.getFormat(req)

	var output string
	if format == types.FormatTOON {
		stripped := formatter.StripDescriptions(results)
		result := &types.ListResult{
			Total:     total,
			Showing:   len(stripped),
			Offset:    start,
			Truncated: truncated,
			Endpoints: stripped,
		}
		output = formatter.FormatListResultTOON(result)
	} else {
		result := &types.ListResult{
			Total:     total,
			Showing:   len(results),
			Offset:    start,
			Truncated: truncated,
			Endpoints: results,
		}
		var fmtErr error
		output, fmtErr = formatter.FormatListResultJSON(result)
		if fmtErr != nil {
			return toolError(types.ErrInternalError, fmtErr.Error()), nil
		}
	}

	return mcp.NewToolResultText(output), nil
}

func (r *Registry) handleListSchemas(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	url := r.resolveURL(req)
	if url == "" {
		return toolError(types.ErrInvalidURL, errURLRequired), nil
	}

	doc, err := r.loadSpec(ctx, url)
	if err != nil {
		return toolError(types.ErrFetchFailed, err.Error()), nil
	}

	schemas := analyzer.ListSchemas(doc, getStringArg(req, "filter"))

	total := len(schemas)
	start, end, truncated := r.pageWindow(req, total)
	page := schemas[start:end]

	result := &types.SchemaListResult{
		Total:     total,
		Showing:   len(page),
		Offset:    start,
		Truncated: truncated,
		Schemas:   page,
	}

	if r.getFormat(req) == types.FormatTOON {
		return mcp.NewToolResultText(formatter.FormatSchemaListTOON(result)), nil
	}
	output, fmtErr := formatter.FormatJSON(result)
	if fmtErr != nil {
		return toolError(types.ErrInternalError, fmtErr.Error()), nil
	}
	return mcp.NewToolResultText(output), nil
}

func (r *Registry) handleListSecuritySchemes(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	url := r.resolveURL(req)
	if url == "" {
		return toolError(types.ErrInvalidURL, errURLRequired), nil
	}

	doc, err := r.loadSpec(ctx, url)
	if err != nil {
		return toolError(types.ErrFetchFailed, err.Error()), nil
	}

	overview := analyzer.ListSecuritySchemes(doc)

	if r.getFormat(req) == types.FormatTOON {
		return mcp.NewToolResultText(formatter.FormatSecurityTOON(overview)), nil
	}
	output, fmtErr := formatter.FormatJSON(overview)
	if fmtErr != nil {
		return toolError(types.ErrInternalError, fmtErr.Error()), nil
	}
	return mcp.NewToolResultText(output), nil
}

func (r *Registry) handleAnalyzeTags(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	url := r.resolveURL(req)
	if url == "" {
		return toolError(types.ErrInvalidURL, errURLRequired), nil
	}

	doc, err := r.loadSpec(ctx, url)
	if err != nil {
		return toolError(types.ErrFetchFailed, err.Error()), nil
	}

	tags := analyzer.AnalyzeTags(doc)
	format := r.getFormat(req)

	var output string
	if format == types.FormatTOON {
		output = formatter.FormatTagsTOON(tags)
	} else {
		var fmtErr error
		output, fmtErr = formatter.FormatTagsJSON(tags)
		if fmtErr != nil {
			return toolError(types.ErrInternalError, fmtErr.Error()), nil
		}
	}

	return mcp.NewToolResultText(output), nil
}

func (r *Registry) handleDiffEndpoints(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	urlOld := getStringArg(req, "url_old")
	urlNew := getStringArg(req, "url_new")
	if urlOld == "" || urlNew == "" {
		return toolError(types.ErrInvalidURL, "url_old and url_new are required"), nil
	}

	oldDoc, err := r.loadSpec(ctx, urlOld)
	if err != nil {
		return toolError(types.ErrFetchFailed, "failed to load old spec: "+err.Error()), nil
	}

	newDoc, err := r.loadSpec(ctx, urlNew)
	if err != nil {
		return toolError(types.ErrFetchFailed, "failed to load new spec: "+err.Error()), nil
	}

	filterPath := getStringArg(req, "path")
	filterMethod := getStringArg(req, "method")

	diff := analyzer.DiffSpecs(oldDoc, newDoc, filterPath, filterMethod)
	format := r.getFormat(req)

	var output string
	if format == types.FormatTOON {
		output = formatter.FormatDiffTOON(diff)
	} else {
		var fmtErr error
		output, fmtErr = formatter.FormatDiffJSON(diff)
		if fmtErr != nil {
			return toolError(types.ErrInternalError, fmtErr.Error()), nil
		}
	}

	if output == "" {
		return mcp.NewToolResultText("No differences found between the two specs."), nil
	}

	return mcp.NewToolResultText(output), nil
}

func (r *Registry) handleSpecStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	url := r.resolveURL(req)
	if url == "" {
		return toolError(types.ErrInvalidURL, errURLRequired), nil
	}

	status := r.loader.SpecStatus(url)

	output, err := formatter.FormatJSON(status)
	if err != nil {
		return toolError(types.ErrInternalError, err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func (r *Registry) handleRefreshSpec(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	url := r.resolveURL(req)
	if url == "" {
		return toolError(types.ErrInvalidURL, errURLRequired), nil
	}

	_, result, err := r.loader.ForceLoadFromURL(ctx, url)
	if err != nil {
		return toolError(types.ErrFetchFailed, err.Error()), nil
	}

	format := r.getFormat(req)
	var output string
	if format == types.FormatTOON {
		output = formatter.FormatRefreshResultTOON(result)
	} else {
		var fmtErr error
		output, fmtErr = formatter.FormatRefreshResultJSON(result)
		if fmtErr != nil {
			return toolError(types.ErrInternalError, fmtErr.Error()), nil
		}
	}

	return mcp.NewToolResultText(output), nil
}

func (r *Registry) handleGenerateTypes(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	url := r.resolveURL(req)
	if url == "" {
		return toolError(types.ErrInvalidURL, errURLRequired), nil
	}

	method := getStringArg(req, "method")
	path := getStringArg(req, "path")
	schemaName := getStringArg(req, "schema")
	tag := getStringArg(req, "tag")
	language := strings.ToLower(getStringArg(req, "language"))
	if language == "" {
		language = "typescript"
	}

	// Precedence: tag wins over schema, which wins over method+path. A tag
	// request skips the schema/endpoint conflict check entirely.
	hasTag := tag != ""
	hasEndpoint := method != "" || path != ""
	hasSchema := schemaName != ""
	if !hasTag && hasEndpoint && hasSchema {
		return toolError(types.ErrInvalidFormat, "provide either method+path or schema, not both"), nil
	}
	if !hasTag && !hasEndpoint && !hasSchema {
		return toolError(types.ErrInvalidFormat, "provide tag (tag mode), schema (schema mode) or method+path (endpoint mode)"), nil
	}
	if !hasTag && !hasSchema && (method == "" || path == "") {
		return toolError(types.ErrInvalidFormat, "both method and path are required for endpoint mode"), nil
	}
	if language != "typescript" && language != "go" {
		return toolError(types.ErrInvalidFormat, "language must be 'typescript' or 'go'"), nil
	}

	doc, err := r.loadSpec(ctx, url)
	if err != nil {
		return toolError(types.ErrFetchFailed, err.Error()), nil
	}

	var code string
	var names []string
	if hasTag {
		if len(analyzer.ListEndpoints(doc, analyzer.ListFilter{Tag: tag})) == 0 {
			return toolError(types.ErrSchemaNotFound, fmt.Sprintf("no endpoint carries tag %q; discover tags via analyze_tags", tag)), nil
		}
		roots := analyzer.CollectTagRefs(doc, tag)
		if len(roots) == 0 {
			return toolError(types.ErrSchemaNotFound, fmt.Sprintf("endpoints tagged %q reference no named schemas", tag)), nil
		}
		code, names, err = generator.GenerateForSchemas(doc, roots, language)
		if err != nil {
			return toolError(types.ErrSchemaNotFound, err.Error()), nil
		}
	} else {
		var schemas []*generator.NamedSchema
		if hasSchema {
			schemas, err = generator.CollectSchemaByName(doc, schemaName)
		} else {
			schemas, err = generator.CollectEndpointSchemas(doc, method, path)
		}
		if err != nil {
			return toolError(types.ErrSchemaNotFound, err.Error()), nil
		}

		switch language {
		case "go":
			code = generator.GenerateGo(schemas)
		default:
			code = generator.GenerateTypeScript(schemas)
		}
		names = make([]string, 0, len(schemas))
		for _, s := range schemas {
			names = append(names, s.Name)
		}
	}

	format := r.getFormat(req)
	if format == types.FormatJSON {
		result := &types.GenerateTypesResult{
			Language:    language,
			Types:       code,
			Names:       names,
			SchemaCount: len(names),
		}
		output, fmtErr := formatter.FormatJSON(result)
		if fmtErr != nil {
			return toolError(types.ErrInternalError, fmtErr.Error()), nil
		}
		return mcp.NewToolResultText(output), nil
	}

	return mcp.NewToolResultText(code), nil
}

func (r *Registry) handleUsageGuide(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText(llmGuide), nil
}
