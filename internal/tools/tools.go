package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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

// Registry holds the shared dependencies for all tool handlers.
type Registry struct {
	loader *loader.Loader
	cfg    config.Config
}

// New creates a new tool Registry with the given config.
func New(cfg config.Config) *Registry {
	return &Registry{
		loader: loader.New(cfg),
		cfg:    cfg,
	}
}

// Register registers all MCP tools on the server.
func (r *Registry) Register(s *server.MCPServer) {
	s.AddTool(fetchSpecTool(), r.handleFetchSpec)
	s.AddTool(listEndpointsTool(), r.handleListEndpoints)
	s.AddTool(getEndpointTool(), r.handleGetEndpoint)
	s.AddTool(getSchemaTool(), r.handleGetSchema)
	s.AddTool(searchSpecTool(), r.handleSearchSpec)
	s.AddTool(analyzeTagsTool(), r.handleAnalyzeTags)
	s.AddTool(diffEndpointsTool(), r.handleDiffEndpoints)
	s.AddTool(specStatusTool(), r.handleSpecStatus)
	s.AddTool(refreshSpecTool(), r.handleRefreshSpec)
	s.AddTool(generateTypesTool(), r.handleGenerateTypes)
}

// --- Tool Definitions ---

func fetchSpecTool() mcp.Tool {
	return mcp.NewTool("fetch_spec",
		mcp.WithDescription("Download and cache an OpenAPI/Swagger spec. Returns a compact summary (title, version, base URL, endpoint/tag/schema counts). Call this FIRST to understand the spec before querying endpoints. Use refresh=true to bypass cache and force a fresh fetch."),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL of the OpenAPI/Swagger spec (JSON or YAML)")),
		mcp.WithString("refresh", mcp.Description("Set to 'true' to bypass cache and force a fresh fetch from the server")),
	)
}

func listEndpointsTool() mcp.Tool {
	return mcp.NewTool("list_endpoints",
		mcp.WithDescription("List endpoints from an OpenAPI spec. IMPORTANT: Use analyze_tags first to discover available tags, then filter by tag here. Results are auto-limited (default 50). Always use filters to get precise results instead of browsing all endpoints."),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL of the OpenAPI/Swagger spec")),
		mcp.WithString("tag", mcp.Description("Filter by tag name (case-insensitive). Use analyze_tags to discover tags first.")),
		mcp.WithString("method", mcp.Description("Filter by HTTP method (GET, POST, PUT, DELETE, PATCH)")),
		mcp.WithString("path_pattern", mcp.Description("Filter by path substring (e.g. '/users')")),
		mcp.WithString("limit", mcp.Description("Max results to return (default: 50, 0 = unlimited)")),
		mcp.WithString("format", mcp.Description("Output format: toon (default, compact) or json")),
	)
}

func getEndpointTool() mcp.Tool {
	return mcp.NewTool("get_endpoint",
		mcp.WithDescription("Get full details of a single endpoint: parameters, request body, responses, and resolved schemas. Use this after finding the endpoint via list_endpoints or search_spec."),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL of the OpenAPI/Swagger spec")),
		mcp.WithString("method", mcp.Required(), mcp.Description("HTTP method (GET, POST, PUT, DELETE, PATCH)")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Endpoint path (e.g. '/pets/{petId}')")),
		mcp.WithString("resolve_depth", mcp.Description("Max schema resolution depth (0-10). Default: 10. Lower values reduce output tokens for complex schemas. 0 = no resolution.")),
		mcp.WithString("format", mcp.Description("Output format: toon (default, compact) or json")),
	)
}

func getSchemaTool() mcp.Tool {
	return mcp.NewTool("get_schema",
		mcp.WithDescription("Get a schema/model with all $refs resolved. Use when you need the data structure for a specific model."),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL of the OpenAPI/Swagger spec")),
		mcp.WithString("name", mcp.Required(), mcp.Description("Schema name (e.g. 'User', 'Pet')")),
		mcp.WithString("resolve_depth", mcp.Description("Max schema resolution depth (0-10). Default: 10. Lower values reduce output tokens for complex schemas. 0 = no resolution.")),
		mcp.WithString("format", mcp.Description("Output format: toon (default, compact) or json")),
	)
}

func searchSpecTool() mcp.Tool {
	return mcp.NewTool("search_spec",
		mcp.WithDescription("Search endpoints by keyword across paths, summaries, descriptions, and operation IDs. Results are ranked by relevance and auto-limited (default 50). Use specific keywords for precise results."),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL of the OpenAPI/Swagger spec")),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query (case-insensitive)")),
		mcp.WithString("limit", mcp.Description("Max results to return (default: 50, 0 = unlimited)")),
		mcp.WithString("format", mcp.Description("Output format: toon (default, compact) or json")),
	)
}

func analyzeTagsTool() mcp.Tool {
	return mcp.NewTool("analyze_tags",
		mcp.WithDescription("Get all tags with endpoint counts and method breakdown. START HERE to understand the API structure, then use tag filters in list_endpoints for targeted results."),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL of the OpenAPI/Swagger spec")),
		mcp.WithString("format", mcp.Description("Output format: toon (default, compact) or json")),
	)
}

func diffEndpointsTool() mcp.Tool {
	return mcp.NewTool("diff_endpoints",
		mcp.WithDescription("Compare two OpenAPI spec versions. Shows added, removed, and changed endpoints."),
		mcp.WithString("url_old", mcp.Required(), mcp.Description("URL of the old/previous spec version")),
		mcp.WithString("url_new", mcp.Required(), mcp.Description("URL of the new/current spec version")),
		mcp.WithString("path", mcp.Description("Filter diff by path substring")),
		mcp.WithString("method", mcp.Description("Filter diff by HTTP method")),
		mcp.WithString("format", mcp.Description("Output format: toon (default, compact) or json")),
	)
}

func specStatusTool() mcp.Tool {
	return mcp.NewTool("spec_status",
		mcp.WithDescription("Check cache status of a spec without fetching. Returns cache source, fingerprint, age, and disk stats."),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL of the OpenAPI/Swagger spec to check")),
	)
}

func refreshSpecTool() mcp.Tool {
	return mcp.NewTool("refresh_spec",
		mcp.WithDescription("Force-refresh a cached spec by invalidating all caches and re-fetching from the server. Returns whether the spec changed (fingerprint comparison) and a fresh summary. Use this when the API spec has been updated and you need the latest version."),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL of the OpenAPI/Swagger spec to refresh")),
		mcp.WithString("format", mcp.Description("Output format: toon (default, compact) or json")),
	)
}

func generateTypesTool() mcp.Tool {
	return mcp.NewTool("generate_types",
		mcp.WithDescription("Generate ready-to-use type definitions (TypeScript interfaces or Go structs) from an endpoint's schemas or a named schema. Saves time by producing copy-paste-ready code instead of raw schema output."),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL of the OpenAPI/Swagger spec")),
		mcp.WithString("method", mcp.Description("HTTP method (required with path, for endpoint mode)")),
		mcp.WithString("path", mcp.Description("Endpoint path (required with method, for endpoint mode)")),
		mcp.WithString("schema", mcp.Description("Schema name for direct schema mode (alternative to method+path)")),
		mcp.WithString("language", mcp.Description("Target language: typescript (default) or go")),
		mcp.WithString("format", mcp.Description("Output format: toon (default, plain code) or json (structured with metadata)")),
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

// getLimit returns the result limit from the request, falling back to config default.
func (r *Registry) getLimit(req mcp.CallToolRequest) int {
	s := getStringArg(req, "limit")
	if s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			return n
		}
	}
	return r.cfg.DefaultLimit
}

// getResolveDepth returns the schema resolution depth from the request.
// Returns -1 if not provided (meaning use default), or 0-10 for explicit values.
func getResolveDepth(req mcp.CallToolRequest) int {
	s := getStringArg(req, "resolve_depth")
	if s == "" {
		return -1 // not provided, use default
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 && n <= 10 {
		return n
	}
	return -1 // invalid value, use default
}

func (r *Registry) loadSpec(ctx context.Context, url string) (*openapi3.T, error) {
	doc, _, err := r.loader.LoadFromURL(ctx, url)
	return doc, err
}

func toolError(code, message string) *mcp.CallToolResult {
	return mcp.NewToolResultError(fmt.Sprintf("[%s] %s", code, message))
}

// --- Handlers ---

func (r *Registry) handleFetchSpec(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	url := getStringArg(req, "url")
	if url == "" {
		return toolError(types.ErrInvalidURL, "url is required"), nil
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

	output, fmtErr := formatter.FormatJSON(summary)
	if fmtErr != nil {
		return toolError(types.ErrInternalError, fmtErr.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func (r *Registry) handleListEndpoints(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	url := getStringArg(req, "url")
	if url == "" {
		return toolError(types.ErrInvalidURL, "url is required"), nil
	}

	doc, err := r.loadSpec(ctx, url)
	if err != nil {
		return toolError(types.ErrFetchFailed, err.Error()), nil
	}

	tag := getStringArg(req, "tag")
	method := getStringArg(req, "method")
	pathPattern := getStringArg(req, "path_pattern")

	endpoints := analyzer.ListEndpoints(doc, tag, method, pathPattern)

	// Apply limit with truncation metadata
	total := len(endpoints)
	limit := r.getLimit(req)
	truncated := false
	if limit > 0 && total > limit {
		endpoints = endpoints[:limit]
		truncated = true
	}

	format := r.getFormat(req)

	var output string
	if format == types.FormatTOON {
		stripped := formatter.StripDescriptions(endpoints)
		result := &types.ListResult{
			Total:     total,
			Showing:   len(stripped),
			Truncated: truncated,
			Endpoints: stripped,
		}
		output = formatter.FormatListResultTOON(result)
	} else {
		result := &types.ListResult{
			Total:     total,
			Showing:   len(endpoints),
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
	url := getStringArg(req, "url")
	if url == "" {
		return toolError(types.ErrInvalidURL, "url is required"), nil
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
	url := getStringArg(req, "url")
	if url == "" {
		return toolError(types.ErrInvalidURL, "url is required"), nil
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
	url := getStringArg(req, "url")
	if url == "" {
		return toolError(types.ErrInvalidURL, "url is required"), nil
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

	// Apply limit with truncation metadata
	total := len(results)
	limit := r.getLimit(req)
	truncated := false
	if limit > 0 && total > limit {
		results = results[:limit]
		truncated = true
	}

	format := r.getFormat(req)

	var output string
	if format == types.FormatTOON {
		stripped := formatter.StripDescriptions(results)
		result := &types.ListResult{
			Total:     total,
			Showing:   len(stripped),
			Truncated: truncated,
			Endpoints: stripped,
		}
		output = formatter.FormatListResultTOON(result)
	} else {
		result := &types.ListResult{
			Total:     total,
			Showing:   len(results),
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

func (r *Registry) handleAnalyzeTags(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	url := getStringArg(req, "url")
	if url == "" {
		return toolError(types.ErrInvalidURL, "url is required"), nil
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
	url := getStringArg(req, "url")
	if url == "" {
		return toolError(types.ErrInvalidURL, "url is required"), nil
	}

	status := r.loader.SpecStatus(url)

	output, err := formatter.FormatJSON(status)
	if err != nil {
		return toolError(types.ErrInternalError, err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func (r *Registry) handleRefreshSpec(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	url := getStringArg(req, "url")
	if url == "" {
		return toolError(types.ErrInvalidURL, "url is required"), nil
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
	url := getStringArg(req, "url")
	if url == "" {
		return toolError(types.ErrInvalidURL, "url is required"), nil
	}

	method := getStringArg(req, "method")
	path := getStringArg(req, "path")
	schemaName := getStringArg(req, "schema")
	language := strings.ToLower(getStringArg(req, "language"))
	if language == "" {
		language = "typescript"
	}

	// Validate: must have either (method+path) or schema, not both, not neither
	hasEndpoint := method != "" || path != ""
	hasSchema := schemaName != ""
	if hasEndpoint && hasSchema {
		return toolError(types.ErrInvalidFormat, "provide either method+path or schema, not both"), nil
	}
	if !hasEndpoint && !hasSchema {
		return toolError(types.ErrInvalidFormat, "provide either method+path (endpoint mode) or schema (schema mode)"), nil
	}
	if hasEndpoint && (method == "" || path == "") {
		return toolError(types.ErrInvalidFormat, "both method and path are required for endpoint mode"), nil
	}
	if language != "typescript" && language != "go" {
		return toolError(types.ErrInvalidFormat, "language must be 'typescript' or 'go'"), nil
	}

	doc, err := r.loadSpec(ctx, url)
	if err != nil {
		return toolError(types.ErrFetchFailed, err.Error()), nil
	}

	var schemas []*generator.NamedSchema
	if hasSchema {
		schemas, err = generator.CollectSchemaByName(doc, schemaName)
	} else {
		schemas, err = generator.CollectEndpointSchemas(doc, method, path)
	}
	if err != nil {
		return toolError(types.ErrSchemaNotFound, err.Error()), nil
	}

	var code string
	switch language {
	case "go":
		code = generator.GenerateGo(schemas)
	default:
		code = generator.GenerateTypeScript(schemas)
	}

	format := r.getFormat(req)
	if format == types.FormatJSON {
		names := make([]string, 0, len(schemas))
		for _, s := range schemas {
			names = append(names, s.Name)
		}
		result := &types.GenerateTypesResult{
			Language: language,
			Types:    code,
			Names:    names,
		}
		output, fmtErr := formatter.FormatJSON(result)
		if fmtErr != nil {
			return toolError(types.ErrInternalError, fmtErr.Error()), nil
		}
		return mcp.NewToolResultText(output), nil
	}

	return mcp.NewToolResultText(code), nil
}
