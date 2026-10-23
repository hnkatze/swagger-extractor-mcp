package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

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

// Registry holds the shared dependencies for all tool handlers. Queries answer
// from specs (the local index); nothing here can fetch a document (S4).
type Registry struct {
	specs *specSource
	cfg   config.Config
}

// New creates a new tool Registry with the given config, resolving the project's
// spec store from the working directory (S2).
func New(cfg config.Config) *Registry {
	workDir, err := os.Getwd()
	if err != nil {
		workDir = "."
	}
	return &Registry{
		specs: newSpecSource(workDir),
		cfg:   cfg,
	}
}

// StorePath is the database file this session reads (shown at startup so a user
// can see which project's index the server is answering from).
func (r *Registry) StorePath() string { return r.specs.Path() }

// Close releases the store if a query opened it.
func (r *Registry) Close() error { return r.specs.Close() }

// toolDef pairs a tool's declaration with its handler, so the surface can be
// registered and inspected from one list.
type toolDef struct {
	tool    mcp.Tool
	handler server.ToolHandlerFunc
}

// definitions is the complete tool surface, in registration order. Indexing is
// the CLI's job, so there is no fetch and no refresh tool (S3, S4). Every handler
// is wrapped so a request that still carries the removed `url` argument is told
// what replaced it instead of being answered about a different spec.
func (r *Registry) definitions() []toolDef {
	defs := []toolDef{
		{listSpecsTool(), r.handleListSpecs},
		{listEndpointsTool(), r.handleListEndpoints},
		{getEndpointTool(), r.handleGetEndpoint},
		{getSchemaTool(), r.handleGetSchema},
		{searchSpecTool(), r.handleSearchSpec},
		{listSchemasTool(), r.handleListSchemas},
		{securitySchemesTool(), r.handleListSecuritySchemes},
		{validateSpecTool(), r.handleValidateSpec},
		{analyzeTagsTool(), r.handleAnalyzeTags},
		{diffEndpointsTool(), r.handleDiffEndpoints},
		{specStatusTool(), r.handleSpecStatus},
		{generateTypesTool(), r.handleGenerateTypes},
		{usageGuideTool(), r.handleUsageGuide},
	}
	for i := range defs {
		defs[i].handler = rejectURLArgument(defs[i].handler)
	}
	return defs
}

// rejectURLArgument turns a request carrying `url` into an explicit error. The
// parameter is gone (S4a), and silently ignoring it would answer about the only
// indexed spec while the caller believes it asked about another document.
func rejectURLArgument(next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if stale := strings.TrimSpace(getStringArg(req, "url")); stale != "" {
			return toolError(types.ErrInvalidQuery,
				"the url argument is gone: specs are indexed with `swagger-mcp index <url|file>` and queries address `spec=<name>` (run list_specs to see the indexed names)"), nil
		}
		return next(ctx, req)
	}
}

// Register registers all MCP tools on the server.
func (r *Registry) Register(s *server.MCPServer) {
	for _, def := range r.definitions() {
		s.AddTool(def.tool, def.handler)
	}
}

// --- Tool Definitions ---

const specArgDescription = "Indexed spec name written by `swagger-mcp index` (omit when the project has exactly one spec)"

const allSpecsArgDescription = "true = sweep every indexed spec and prefix each line with its spec name (default false: one spec)"

func listSpecsTool() mcp.Tool {
	return mcp.NewTool("list_specs",
		mcp.WithDescription("List every indexed spec with title, version, source, fetch time and counts. Start here when a call reports that several specs are indexed; then pass spec=<name> or all_specs=true to the discovery tools."),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func listEndpointsTool() mcp.Tool {
	return mcp.NewTool("list_endpoints",
		mcp.WithDescription("List endpoints (METHOD path — summary [tags]) from the project's indexed spec. Run analyze_tags first, then filter by tag/method/path here. Auto-limited to 50; always filter rather than browse all."),
		mcp.WithString("spec", mcp.Description(specArgDescription)),
		mcp.WithString("tag", mcp.Description("Filter by tag (case-insensitive); discover via analyze_tags")),
		mcp.WithString("method", mcp.Description("Filter by HTTP method")),
		mcp.WithString("path_pattern", mcp.Description("Filter by path substring, e.g. '/users'")),
		mcp.WithBoolean("deprecated", mcp.Description("true = only deprecated endpoints, false = only non-deprecated; omit for both")),
		mcp.WithBoolean("all_specs", mcp.Description(allSpecsArgDescription)),
		mcp.WithNumber("limit", mcp.Description("Max results (default 50, 0 = unlimited)")),
		mcp.WithNumber("offset", mcp.Description("Skip this many results (default 0); use with limit to page")),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func getEndpointTool() mcp.Tool {
	return mcp.NewTool("get_endpoint",
		mcp.WithDescription("Full detail of one endpoint: params, request body, responses, resolved schemas (with field descriptions). Shared schemas are shown once then referenced as $ref(Name) — fetch those via get_schema. Use after list_endpoints/search_spec."),
		mcp.WithString("spec", mcp.Description(specArgDescription)),
		mcp.WithString("method", mcp.Required(), mcp.Description("HTTP method")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Endpoint path, e.g. '/pets/{petId}'")),
		mcp.WithNumber("resolve_depth", mcp.Description("Schema nesting depth 0-10 (default 3). Raise for deep models, lower for fewer tokens. 0 = names only.")),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func getSchemaTool() mcp.Tool {
	return mcp.NewTool("get_schema",
		mcp.WithDescription("Resolve a named schema/model with field types and descriptions. Use to expand a $ref(Name) seen in get_endpoint."),
		mcp.WithString("spec", mcp.Description(specArgDescription)),
		mcp.WithString("name", mcp.Required(), mcp.Description("Schema name, e.g. 'User'")),
		mcp.WithNumber("resolve_depth", mcp.Description("Schema nesting depth 0-10 (default 3). 0 = names only.")),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func searchSpecTool() mcp.Tool {
	return mcp.NewTool("search_spec",
		mcp.WithDescription("Keyword search across paths, summaries, descriptions, operation IDs, and body field names. Ranked by relevance, auto-limited to 50. Use specific keywords."),
		mcp.WithString("spec", mcp.Description(specArgDescription)),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query (case-insensitive)")),
		mcp.WithBoolean("all_specs", mcp.Description(allSpecsArgDescription)),
		mcp.WithNumber("limit", mcp.Description("Max results (default 50, 0 = unlimited)")),
		mcp.WithNumber("offset", mcp.Description("Skip this many results (default 0); use with limit to page")),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func listSchemasTool() mcp.Tool {
	return mcp.NewTool("list_schemas",
		mcp.WithDescription("List component schemas with field count and usage (endpoints referencing each). Call get_schema with a name to see its fields. Auto-limited to 50; use filter to narrow."),
		mcp.WithString("spec", mcp.Description(specArgDescription)),
		mcp.WithString("filter", mcp.Description("Filter by schema name substring (case-insensitive)")),
		mcp.WithBoolean("all_specs", mcp.Description(allSpecsArgDescription)),
		mcp.WithNumber("limit", mcp.Description("Max results (default 50, 0 = unlimited)")),
		mcp.WithNumber("offset", mcp.Description("Skip this many results (default 0); use with limit to page")),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func securitySchemesTool() mcp.Tool {
	return mcp.NewTool("security_schemes",
		mcp.WithDescription("Security scheme definitions (type, scheme, bearer format, apiKey location, OAuth flow URLs/scopes) with how many endpoints use each. Use after get_endpoint shows a security requirement."),
		mcp.WithString("spec", mcp.Description(specArgDescription)),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func validateSpecTool() mcp.Tool {
	return mcp.NewTool("validate_spec",
		mcp.WithDescription("Validate an indexed spec and report its real violations (the loader relaxes these so specs still index). Returns VALID/INVALID plus the violations."),
		mcp.WithString("spec", mcp.Description(specArgDescription)),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func analyzeTagsTool() mcp.Tool {
	return mcp.NewTool("analyze_tags",
		mcp.WithDescription("Tags with endpoint counts and method breakdown. START HERE to map the API, then filter list_endpoints by tag."),
		mcp.WithString("spec", mcp.Description(specArgDescription)),
		mcp.WithBoolean("all_specs", mcp.Description(allSpecsArgDescription)),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func diffEndpointsTool() mcp.Tool {
	return mcp.NewTool("diff_endpoints",
		mcp.WithDescription("Compare two indexed specs: added, removed, changed endpoints. Needs two indexed specs, named explicitly."),
		mcp.WithString("spec_old", mcp.Description("Old spec name (required; run list_specs to see the names)")),
		mcp.WithString("spec_new", mcp.Description("New spec name (required)")),
		mcp.WithString("path", mcp.Description("Filter by path substring")),
		mcp.WithString("method", mcp.Description("Filter by HTTP method")),
		mcp.WithString("format", mcp.Description("toon (default) or json")),
	)
}

func specStatusTool() mcp.Tool {
	return mcp.NewTool("spec_status",
		mcp.WithDescription("Index status without fetching: source, etag, fetch time, age, a stale verdict (older than 7 days) and the exact `swagger-mcp index --refresh` command. Read-only: it never refreshes."),
		mcp.WithString("spec", mcp.Description(specArgDescription)),
	)
}

func generateTypesTool() mcp.Tool {
	return mcp.NewTool("generate_types",
		mcp.WithDescription("Emit copy-paste-ready type definitions (TypeScript interfaces or Go structs) from a tag, a named schema, or an endpoint. Tag mode covers every schema a tag uses, deduplicated and sorted by name. Precedence: tag wins over schema, which wins over method+path."),
		mcp.WithString("spec", mcp.Description(specArgDescription)),
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

// document resolves the addressed spec from the local index and returns its
// parsed document, or the tool result that reports why it could not.
func (r *Registry) document(req mcp.CallToolRequest) (*openapi3.T, *mcp.CallToolResult) {
	name, err := r.specs.resolve(req, "spec")
	if err != nil {
		return nil, specToolError(err)
	}
	doc, err := r.specs.document(name)
	if err != nil {
		return nil, specToolError(err)
	}
	return doc, nil
}

// specToolError renders an addressing/store failure as a tool error with its
// stable code. The message is always actionable: it names the index command or
// the names that could be used instead.
func specToolError(err error) *mcp.CallToolResult {
	var se *specError
	if errors.As(err, &se) {
		return toolError(se.code, se.message)
	}
	return toolError(types.ErrInternalError, err.Error())
}

func toolError(code, message string) *mcp.CallToolResult {
	return mcp.NewToolResultError(fmt.Sprintf("[%s] %s", code, message))
}

// --- Handlers ---

func (r *Registry) handleListEndpoints(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if getBoolArg(req, "all_specs") {
		return r.handleListEndpointsAllSpecs(req)
	}
	name, err := r.specs.resolve(req, "spec")
	if err != nil {
		return specToolError(err), nil
	}

	// Answered from the catalog: a listing never decompresses or parses the
	// stored document (S9).
	rows, total, err := r.specs.endpoints(name, endpointFilter(req), r.getLimit(req), getOffset(req))
	if err != nil {
		return specToolError(err), nil
	}
	return r.endpointListResult(req, endpointSummaries(rows), total), nil
}

// endpointListResult renders a paged endpoint listing: the one place that turns a
// page and its total into TOON or JSON, so list_endpoints and search_spec keep the
// paging metadata they had when they sliced a parsed document.
func (r *Registry) endpointListResult(req mcp.CallToolRequest, page []types.EndpointSummary, total int) *mcp.CallToolResult {
	start, _, truncated := r.pageWindow(req, total)

	if r.getFormat(req) == types.FormatTOON {
		stripped := formatter.StripDescriptions(page)
		return mcp.NewToolResultText(formatter.FormatListResultTOON(&types.ListResult{
			Total:     total,
			Showing:   len(stripped),
			Offset:    start,
			Truncated: truncated,
			Endpoints: stripped,
		}))
	}

	result := &types.ListResult{
		Total:     total,
		Showing:   len(page),
		Offset:    start,
		Truncated: truncated,
		Endpoints: page,
	}
	output, err := formatter.FormatListResultJSON(result)
	if err != nil {
		return toolError(types.ErrInternalError, err.Error())
	}
	return mcp.NewToolResultText(output)
}

func (r *Registry) handleGetEndpoint(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	doc, errResult := r.document(req)
	if errResult != nil {
		return errResult, nil
	}

	method := getStringArg(req, "method")
	path := getStringArg(req, "path")
	if method == "" || path == "" {
		return toolError(types.ErrEndpointNotFound, "method and path are required"), nil
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
	doc, errResult := r.document(req)
	if errResult != nil {
		return errResult, nil
	}

	name := getStringArg(req, "name")
	if name == "" {
		return toolError(types.ErrSchemaNotFound, "schema name is required"), nil
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
	if getBoolArg(req, "all_specs") {
		return r.handleSearchSpecAllSpecs(req)
	}
	name, err := r.specs.resolve(req, "spec")
	if err != nil {
		return specToolError(err), nil
	}

	query := getStringArg(req, "query")
	if query == "" {
		return toolError(types.ErrInvalidQuery, "search query is required"), nil
	}

	// The catalog carries the operation ids and body field names the search
	// matches on, so a search never parses the stored document (S9).
	rows, total, err := r.specs.search(name, query, r.getLimit(req), getOffset(req))
	if err != nil {
		return specToolError(err), nil
	}
	if total == 0 {
		return mcp.NewToolResultText("No endpoints found matching query: " + query), nil
	}
	return r.endpointListResult(req, endpointSummaries(rows), total), nil
}

func (r *Registry) handleListSchemas(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if getBoolArg(req, "all_specs") {
		return r.handleListSchemasAllSpecs(req)
	}
	name, err := r.specs.resolve(req, "spec")
	if err != nil {
		return specToolError(err), nil
	}

	// Answered from the catalog: schemas, field counts, usage and descriptions
	// are rows, not a parse (S9).
	rows, total, err := r.specs.schemas(name, getStringArg(req, "filter"), r.getLimit(req), getOffset(req))
	if err != nil {
		return specToolError(err), nil
	}
	start, _, truncated := r.pageWindow(req, total)

	schemas := schemaInfos(rows)
	result := &types.SchemaListResult{
		Total:     total,
		Showing:   len(schemas),
		Offset:    start,
		Truncated: truncated,
		Schemas:   schemas,
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
	doc, errResult := r.document(req)
	if errResult != nil {
		return errResult, nil
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

// maxReportedViolations caps the violations returned so a badly broken spec
// cannot flood the context.
const maxReportedViolations = 25

func (r *Registry) handleValidateSpec(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := r.specs.resolve(req, "spec")
	if err != nil {
		return specToolError(err), nil
	}
	doc, err := r.specs.storedDocument(name)
	if err != nil {
		return specToolError(err), nil
	}

	violations := loader.ValidateDocument(doc)
	report := &types.ValidationReport{
		Valid:          len(violations) == 0,
		ViolationCount: len(violations),
		Violations:     violations,
		SpecVersion:    doc.OpenAPI,
	}
	if doc.Info != nil {
		report.Title = doc.Info.Title
	}
	if len(violations) > maxReportedViolations {
		report.Violations = violations[:maxReportedViolations]
	}

	if r.getFormat(req) == types.FormatTOON {
		return mcp.NewToolResultText(formatter.FormatValidationTOON(report)), nil
	}
	output, fmtErr := formatter.FormatJSON(report)
	if fmtErr != nil {
		return toolError(types.ErrInternalError, fmtErr.Error()), nil
	}
	return mcp.NewToolResultText(output), nil
}

func (r *Registry) handleAnalyzeTags(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if getBoolArg(req, "all_specs") {
		return r.handleAnalyzeTagsAllSpecs(req)
	}
	name, err := r.specs.resolve(req, "spec")
	if err != nil {
		return specToolError(err), nil
	}

	// Counts, descriptions and method breakdowns are catalog rows (S9).
	tags, err := r.specs.tags(name)
	if err != nil {
		return specToolError(err), nil
	}
	summaries := tagSummaries(tags)

	var output string
	if r.getFormat(req) == types.FormatTOON {
		output = formatter.FormatTagsTOON(summaries)
	} else {
		var fmtErr error
		output, fmtErr = formatter.FormatTagsJSON(summaries)
		if fmtErr != nil {
			return toolError(types.ErrInternalError, fmtErr.Error()), nil
		}
	}

	return mcp.NewToolResultText(output), nil
}

func (r *Registry) handleDiffEndpoints(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// A diff is between two specs, never between a spec and itself: resolving an
	// omitted name to "the only spec" used to answer "No differences" for every
	// request. Both sides must be named, and the store must hold two specs.
	metas, err := r.specs.metas()
	if err != nil {
		return specToolError(err), nil
	}
	if len(metas) < 2 {
		return specToolError(specErr(codeDiffNeedsSpecs,
			"diff_endpoints compares two indexed specs: %s; run %s to index another one",
			indexedCount(len(metas), metaNames(metas)), indexCommand)), nil
	}

	names := metaNames(metas)
	oldName := strings.TrimSpace(getStringArg(req, "spec_old"))
	newName := strings.TrimSpace(getStringArg(req, "spec_new"))
	if oldName == "" || newName == "" {
		return specToolError(specErr(codeSpecAmbiguous,
			"diff_endpoints needs both names: several specs are indexed (%s); pass spec_old=<name> and spec_new=<name>",
			strings.Join(names, ", "))), nil
	}
	for _, name := range []string{oldName, newName} {
		if !containsName(names, name) {
			return specToolError(specErr(codeSpecNotFound,
				"spec %q is not indexed; indexed specs: %s", name, strings.Join(names, ", "))), nil
		}
	}

	oldDoc, err := r.specs.document(oldName)
	if err != nil {
		return specToolError(err), nil
	}
	newDoc, err := r.specs.document(newName)
	if err != nil {
		return specToolError(err), nil
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
	name, err := r.specs.resolve(req, "spec")
	if err != nil {
		return specToolError(err), nil
	}
	meta, err := r.specs.meta(name)
	if err != nil {
		return specToolError(err), nil
	}

	// Staleness is a judgement about the stored copy, computed from its own
	// fetched_at: nothing here contacts the source (S6).
	age := time.Since(meta.FetchedAt)
	if age < 0 {
		age = 0
	}
	stale := age > staleAfter

	report := &SpecStatusReport{
		Name:           meta.Name,
		Source:         meta.Source,
		Title:          meta.Title,
		Version:        meta.Version,
		ETag:           meta.ETag,
		SHA256:         meta.SHA256,
		FetchedAt:      meta.FetchedAt.UTC().Format(time.RFC3339),
		Age:            humanAge(age),
		AgeSeconds:     int64(age.Seconds()),
		Stale:          stale,
		StaleAfterDays: int(staleAfter.Hours()) / 24,
		Refresh:        refreshCommand(meta),
		EndpointCount:  meta.EndpointCount,
		SchemaCount:    meta.SchemaCount,
		Store:          r.specs.Path(),
	}

	output, err := formatter.FormatJSON(report)
	if err != nil {
		return toolError(types.ErrInternalError, err.Error()), nil
	}

	return mcp.NewToolResultText(output), nil
}

func (r *Registry) handleGenerateTypes(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	doc, errResult := r.document(req)
	if errResult != nil {
		return errResult, nil
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

	var code string
	var names []string
	var err error
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
