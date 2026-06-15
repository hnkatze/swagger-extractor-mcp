package tools

import (
	"context"
	_ "embed"
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
	s.AddTool(usageGuideTool(), r.handleUsageGuide)
}

// --- Tool Definitions ---

func fetchSpecTool() mcp.Tool {
	return mcp.NewTool("fetch_spec",
		mcp.WithDescription("Fetch & cache a spec, returns a compact summary (title, version, counts). Call FIRST. refresh=true bypasses cache."),
		mcp.WithString("url", mcp.Required(), mcp.Description("Spec URL (JSON or YAML)")),
		mcp.WithString("refresh", mcp.Description("'true' to force a fresh fetch")),
	)
}

func listEndpointsTool() mcp.Tool {
	return mcp.NewTool("list_endpoints",
		mcp.WithDescription("List endpoints (METHOD path — summary [tags]). Run analyze_tags first, then filter by tag/method/path here. Auto-limited to 50; always filter rather than browse all."),
		mcp.WithString("url", mcp.Required(), mcp.Description("Spec URL")),
		mcp.WithString("tag", mcp.Description("Filter by tag (case-insensitive); discover via analyze_tags")),
		mcp.WithString("method", mcp.Description("Filter by HTTP method")),
		mcp.WithString("path_pattern", mcp.Description("Filter by path substring, e.g. '/users'")),
		mcp.WithString("limit", mcp.Description("Max results (default 50, 0 = unlimited)")),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func getEndpointTool() mcp.Tool {
	return mcp.NewTool("get_endpoint",
		mcp.WithDescription("Full detail of one endpoint: params, request body, responses, resolved schemas (with field descriptions). Shared schemas are shown once then referenced as $ref(Name) — fetch those via get_schema. Use after list_endpoints/search_spec."),
		mcp.WithString("url", mcp.Required(), mcp.Description("Spec URL")),
		mcp.WithString("method", mcp.Required(), mcp.Description("HTTP method")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Endpoint path, e.g. '/pets/{petId}'")),
		mcp.WithString("resolve_depth", mcp.Description("Schema nesting depth 0-10 (default 3). Raise for deep models, lower for fewer tokens. 0 = names only.")),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func getSchemaTool() mcp.Tool {
	return mcp.NewTool("get_schema",
		mcp.WithDescription("Resolve a named schema/model with field types and descriptions. Use to expand a $ref(Name) seen in get_endpoint."),
		mcp.WithString("url", mcp.Required(), mcp.Description("Spec URL")),
		mcp.WithString("name", mcp.Required(), mcp.Description("Schema name, e.g. 'User'")),
		mcp.WithString("resolve_depth", mcp.Description("Schema nesting depth 0-10 (default 3). 0 = names only.")),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func searchSpecTool() mcp.Tool {
	return mcp.NewTool("search_spec",
		mcp.WithDescription("Keyword search across paths, summaries, descriptions, operation IDs, and body field names. Ranked by relevance, auto-limited to 50. Use specific keywords."),
		mcp.WithString("url", mcp.Required(), mcp.Description("Spec URL")),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query (case-insensitive)")),
		mcp.WithString("limit", mcp.Description("Max results (default 50, 0 = unlimited)")),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func analyzeTagsTool() mcp.Tool {
	return mcp.NewTool("analyze_tags",
		mcp.WithDescription("Tags with endpoint counts and method breakdown. START HERE to map the API, then filter list_endpoints by tag."),
		mcp.WithString("url", mcp.Required(), mcp.Description("Spec URL")),
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
		mcp.WithString("url", mcp.Required(), mcp.Description("Spec URL to check")),
	)
}

func refreshSpecTool() mcp.Tool {
	return mcp.NewTool("refresh_spec",
		mcp.WithDescription("Invalidate caches and re-fetch. Reports whether the spec changed (fingerprint) plus a fresh summary. Use when the API spec was updated."),
		mcp.WithString("url", mcp.Required(), mcp.Description("Spec URL to refresh")),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func generateTypesTool() mcp.Tool {
	return mcp.NewTool("generate_types",
		mcp.WithDescription("Emit copy-paste-ready type definitions (TypeScript interfaces or Go structs) from an endpoint or a named schema."),
		mcp.WithString("url", mcp.Required(), mcp.Description("Spec URL")),
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

func (r *Registry) handleUsageGuide(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText(llmGuide), nil
}
