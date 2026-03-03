package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/hnkatze/swagger-mcp-go/internal/analyzer"
	"github.com/hnkatze/swagger-mcp-go/internal/config"
	"github.com/hnkatze/swagger-mcp-go/internal/extractor"
	"github.com/hnkatze/swagger-mcp-go/internal/formatter"
	"github.com/hnkatze/swagger-mcp-go/internal/loader"
	"github.com/hnkatze/swagger-mcp-go/internal/types"
)

// Registry holds the shared dependencies for all tool handlers.
type Registry struct {
	loader *loader.Loader
}

// New creates a new tool Registry with the given config.
func New(cfg config.Config) *Registry {
	return &Registry{
		loader: loader.New(cfg),
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
}

// --- Tool Definitions ---

func fetchSpecTool() mcp.Tool {
	return mcp.NewTool("fetch_spec",
		mcp.WithDescription("Download and cache an OpenAPI/Swagger spec from a URL. Returns a summary with title, version, base URL, endpoint count, tag count, and schema count."),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL of the OpenAPI/Swagger spec (JSON or YAML)")),
	)
}

func listEndpointsTool() mcp.Tool {
	return mcp.NewTool("list_endpoints",
		mcp.WithDescription("List all endpoints in an OpenAPI spec. Optionally filter by tag, HTTP method, or path pattern."),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL of the OpenAPI/Swagger spec")),
		mcp.WithString("tag", mcp.Description("Filter by tag name (case-insensitive)")),
		mcp.WithString("method", mcp.Description("Filter by HTTP method (GET, POST, PUT, DELETE, PATCH)")),
		mcp.WithString("path_pattern", mcp.Description("Filter by path substring (e.g. '/users')")),
		mcp.WithString("format", mcp.Description("Output format: json (default) or toon")),
	)
}

func getEndpointTool() mcp.Tool {
	return mcp.NewTool("get_endpoint",
		mcp.WithDescription("Get full details of a single endpoint including parameters, request body, responses, and resolved schemas. No $ref in output."),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL of the OpenAPI/Swagger spec")),
		mcp.WithString("method", mcp.Required(), mcp.Description("HTTP method (GET, POST, PUT, DELETE, PATCH)")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Endpoint path (e.g. '/pets/{petId}')")),
		mcp.WithString("format", mcp.Description("Output format: json (default) or toon")),
	)
}

func getSchemaTool() mcp.Tool {
	return mcp.NewTool("get_schema",
		mcp.WithDescription("Get a specific schema/model from the spec with all nested $refs fully resolved."),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL of the OpenAPI/Swagger spec")),
		mcp.WithString("name", mcp.Required(), mcp.Description("Schema name (e.g. 'User', 'Pet')")),
		mcp.WithString("format", mcp.Description("Output format: json (default) or toon")),
	)
}

func searchSpecTool() mcp.Tool {
	return mcp.NewTool("search_spec",
		mcp.WithDescription("Full-text search across endpoint paths, summaries, descriptions, operation IDs, and parameter names."),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL of the OpenAPI/Swagger spec")),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query (case-insensitive)")),
		mcp.WithString("format", mcp.Description("Output format: json (default) or toon")),
	)
}

func analyzeTagsTool() mcp.Tool {
	return mcp.NewTool("analyze_tags",
		mcp.WithDescription("Get a summary of all tags with endpoint counts and HTTP method breakdown."),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL of the OpenAPI/Swagger spec")),
		mcp.WithString("format", mcp.Description("Output format: json (default) or toon")),
	)
}

func diffEndpointsTool() mcp.Tool {
	return mcp.NewTool("diff_endpoints",
		mcp.WithDescription("Compare two OpenAPI spec versions. Shows added, removed, and changed endpoints."),
		mcp.WithString("url_old", mcp.Required(), mcp.Description("URL of the old/previous spec version")),
		mcp.WithString("url_new", mcp.Required(), mcp.Description("URL of the new/current spec version")),
		mcp.WithString("path", mcp.Description("Filter diff by path substring")),
		mcp.WithString("method", mcp.Description("Filter diff by HTTP method")),
		mcp.WithString("format", mcp.Description("Output format: json (default) or toon")),
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

func getFormat(req mcp.CallToolRequest) types.OutputFormat {
	f := strings.ToLower(getStringArg(req, "format"))
	if f == "toon" {
		return types.FormatTOON
	}
	return types.FormatJSON
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

	_, summary, err := r.loader.LoadFromURL(ctx, url)
	if err != nil {
		return toolError(types.ErrFetchFailed, err.Error()), nil
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
	format := getFormat(req)

	var output string
	if format == types.FormatTOON {
		output = formatter.FormatEndpointsTOON(endpoints)
	} else {
		var fmtErr error
		output, fmtErr = formatter.FormatEndpointsJSON(endpoints)
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

	detail, err := extractor.GetEndpoint(doc, method, path)
	if err != nil {
		return toolError(types.ErrEndpointNotFound, fmt.Sprintf("%s %s not found", strings.ToUpper(method), path)), nil
	}

	format := getFormat(req)
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

	schema, err := extractor.GetSchema(doc, name)
	if err != nil {
		return toolError(types.ErrSchemaNotFound, fmt.Sprintf("schema '%s' not found", name)), nil
	}

	format := getFormat(req)
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
	format := getFormat(req)

	var output string
	if format == types.FormatTOON {
		output = formatter.FormatEndpointsTOON(results)
	} else {
		var fmtErr error
		output, fmtErr = formatter.FormatEndpointsJSON(results)
		if fmtErr != nil {
			return toolError(types.ErrInternalError, fmtErr.Error()), nil
		}
	}

	if output == "" || output == "null" || output == "[]" {
		return mcp.NewToolResultText("No endpoints found matching query: " + query), nil
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
	format := getFormat(req)

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
	format := getFormat(req)

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
