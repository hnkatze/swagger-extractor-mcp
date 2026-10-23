package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/hnkatze/swagger-mcp-go/internal/config"
	"github.com/hnkatze/swagger-mcp-go/internal/types"
)

// ---------------------------------------------------------------------------
// getFormat — config-aware format resolution
// ---------------------------------------------------------------------------

func TestGetFormat_ExplicitTOON(t *testing.T) {
	r := &Registry{cfg: config.Config{DefaultFormat: "json"}}
	req := makeRequest(map[string]interface{}{"format": "toon"})
	if got := r.getFormat(req); got != types.FormatTOON {
		t.Errorf("getFormat = %q, want toon", got)
	}
}

func TestGetFormat_ExplicitJSON(t *testing.T) {
	r := &Registry{cfg: config.Config{DefaultFormat: "toon"}}
	req := makeRequest(map[string]interface{}{"format": "json"})
	if got := r.getFormat(req); got != types.FormatJSON {
		t.Errorf("getFormat = %q, want json", got)
	}
}

func TestGetFormat_DefaultTOON(t *testing.T) {
	r := &Registry{cfg: config.Config{DefaultFormat: "toon"}}
	req := makeRequest(map[string]interface{}{})
	if got := r.getFormat(req); got != types.FormatTOON {
		t.Errorf("getFormat = %q, want toon (from config default)", got)
	}
}

func TestGetFormat_DefaultJSON(t *testing.T) {
	r := &Registry{cfg: config.Config{DefaultFormat: "json"}}
	req := makeRequest(map[string]interface{}{})
	if got := r.getFormat(req); got != types.FormatJSON {
		t.Errorf("getFormat = %q, want json (from config default)", got)
	}
}

func TestGetFormat_ExplicitOverridesDefault(t *testing.T) {
	r := &Registry{cfg: config.Config{DefaultFormat: "toon"}}
	req := makeRequest(map[string]interface{}{"format": "JSON"})
	if got := r.getFormat(req); got != types.FormatJSON {
		t.Errorf("getFormat = %q, explicit JSON should override toon default", got)
	}
}

func TestGetFormat_InvalidFallsToDefault(t *testing.T) {
	r := &Registry{cfg: config.Config{DefaultFormat: "toon"}}
	req := makeRequest(map[string]interface{}{"format": "xml"})
	if got := r.getFormat(req); got != types.FormatTOON {
		t.Errorf("getFormat = %q, invalid format should fall back to config default toon", got)
	}
}

// ---------------------------------------------------------------------------
// getLimit — config-aware limit resolution
// ---------------------------------------------------------------------------

func TestGetLimit_ExplicitValue(t *testing.T) {
	r := &Registry{cfg: config.Config{DefaultLimit: 50}}
	req := makeRequest(map[string]interface{}{"limit": "10"})
	if got := r.getLimit(req); got != 10 {
		t.Errorf("getLimit = %d, want 10", got)
	}
}

func TestGetLimit_ExplicitZero(t *testing.T) {
	r := &Registry{cfg: config.Config{DefaultLimit: 50}}
	req := makeRequest(map[string]interface{}{"limit": "0"})
	if got := r.getLimit(req); got != 0 {
		t.Errorf("getLimit = %d, want 0 (unlimited)", got)
	}
}

func TestGetLimit_Default(t *testing.T) {
	r := &Registry{cfg: config.Config{DefaultLimit: 50}}
	req := makeRequest(map[string]interface{}{})
	if got := r.getLimit(req); got != 50 {
		t.Errorf("getLimit = %d, want 50 (config default)", got)
	}
}

func TestGetLimit_InvalidFallsToDefault(t *testing.T) {
	r := &Registry{cfg: config.Config{DefaultLimit: 50}}
	req := makeRequest(map[string]interface{}{"limit": "abc"})
	if got := r.getLimit(req); got != 50 {
		t.Errorf("getLimit = %d, want 50 (fallback on invalid)", got)
	}
}

func TestGetLimit_NegativeFallsToDefault(t *testing.T) {
	r := &Registry{cfg: config.Config{DefaultLimit: 50}}
	req := makeRequest(map[string]interface{}{"limit": "-5"})
	if got := r.getLimit(req); got != 50 {
		t.Errorf("getLimit = %d, want 50 (fallback on negative)", got)
	}
}

// ---------------------------------------------------------------------------
// getStringArg
// ---------------------------------------------------------------------------

func TestGetStringArg_Present(t *testing.T) {
	req := makeRequest(map[string]interface{}{"url": "https://example.com"})
	if got := getStringArg(req, "url"); got != "https://example.com" {
		t.Errorf("getStringArg = %q, want https://example.com", got)
	}
}

func TestGetStringArg_Missing(t *testing.T) {
	req := makeRequest(map[string]interface{}{})
	if got := getStringArg(req, "url"); got != "" {
		t.Errorf("getStringArg = %q, want empty", got)
	}
}

func TestGetStringArg_NonString(t *testing.T) {
	req := makeRequest(map[string]interface{}{"limit": 42})
	if got := getStringArg(req, "limit"); got != "" {
		t.Errorf("getStringArg = %q, want empty for non-string value", got)
	}
}

// ---------------------------------------------------------------------------
// Registry construction
// ---------------------------------------------------------------------------

func TestNew_StoresConfig(t *testing.T) {
	cfg := config.Config{
		DefaultFormat: "toon",
		DefaultLimit:  25,
	}
	r := New(cfg)
	if r.cfg.DefaultFormat != "toon" {
		t.Errorf("cfg.DefaultFormat = %q, want toon", r.cfg.DefaultFormat)
	}
	if r.cfg.DefaultLimit != 25 {
		t.Errorf("cfg.DefaultLimit = %d, want 25", r.cfg.DefaultLimit)
	}
	if r.loader == nil {
		t.Error("loader should not be nil")
	}
}

// ---------------------------------------------------------------------------
// Tool definitions — verify limit param exists
// ---------------------------------------------------------------------------

func TestListEndpointsTool_HasLimitParam(t *testing.T) {
	tool := listEndpointsTool()
	assertToolHasParam(t, tool, "limit")
	assertToolHasParam(t, tool, "tag")
	assertToolHasParam(t, tool, "method")
	assertToolHasParam(t, tool, "path_pattern")
	assertToolHasParam(t, tool, "format")
}

func TestSearchSpecTool_HasLimitParam(t *testing.T) {
	tool := searchSpecTool()
	assertToolHasParam(t, tool, "limit")
	assertToolHasParam(t, tool, "query")
	assertToolHasParam(t, tool, "format")
}

func TestAnalyzeTagsTool_DescriptionGuidesWorkflow(t *testing.T) {
	tool := analyzeTagsTool()
	desc := tool.Description
	if desc == "" {
		t.Fatal("analyze_tags should have a description")
	}
}

func TestUsageGuideTool_NoParams(t *testing.T) {
	tool := usageGuideTool()
	if tool.Description == "" {
		t.Fatal("usage_guide should have a description")
	}
	if len(tool.InputSchema.Properties) != 0 {
		t.Errorf("usage_guide should take no params, got %d", len(tool.InputSchema.Properties))
	}
}

func TestHandleUsageGuide_ReturnsEmbeddedGuide(t *testing.T) {
	r := New(config.Default())
	result, err := r.handleUsageGuide(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("handleUsageGuide error: %v", err)
	}
	text := extractText(result)
	if text == "" {
		t.Fatal("usage guide should not be empty")
	}
	// Spot-check that the guide carries its key signals for the LLM.
	for _, want := range []string{"analyze_tags", "$ref(", "resolve_depth", "Anti-patterns"} {
		if !strings.Contains(text, want) {
			t.Errorf("usage guide should mention %q", want)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeRequest(args map[string]interface{}) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: args,
		},
	}
}

func assertToolHasParam(t *testing.T, tool mcp.Tool, paramName string) {
	t.Helper()
	props := tool.InputSchema.Properties
	if props == nil {
		t.Fatalf("tool %q has nil Properties", tool.Name)
	}
	if _, ok := props[paramName]; !ok {
		t.Errorf("tool %q missing parameter %q", tool.Name, paramName)
	}
}

// ---------------------------------------------------------------------------
// Numeric and boolean argument tolerance (S1)
// ---------------------------------------------------------------------------

// MCP clients that decode JSON send numbers as float64, and older calls send
// strings. Both must resolve, or a declared number silently falls back to the
// default limit and the caller never learns why.
func TestGetLimit_NumericArg(t *testing.T) {
	cases := []struct {
		name string
		arg  interface{}
		want int
	}{
		{"json number", float64(10), 10},
		{"go int", 10, 10},
		{"legacy string", "10", 10},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Registry{cfg: config.Config{DefaultLimit: 50}}
			req := makeRequest(map[string]interface{}{"limit": tc.arg})
			if got := r.getLimit(req); got != tc.want {
				t.Errorf("getLimit(%T %v) = %d, want %d", tc.arg, tc.arg, got, tc.want)
			}
		})
	}
}

func TestGetResolveDepth_NumericArg(t *testing.T) {
	cases := []struct {
		name string
		arg  interface{}
		want int
	}{
		{"json number", float64(5), 5},
		{"go int", 5, 5},
		{"legacy string", "5", 5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := makeRequest(map[string]interface{}{"resolve_depth": tc.arg})
			if got := getResolveDepth(req); got != tc.want {
				t.Errorf("getResolveDepth(%T %v) = %d, want %d", tc.arg, tc.arg, got, tc.want)
			}
		})
	}
}

func TestGetBoolArg_BooleanArg(t *testing.T) {
	cases := []struct {
		name string
		arg  interface{}
		want bool
	}{
		{"json boolean", true, true},
		{"legacy string", "true", true},
		{"json false", false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := makeRequest(map[string]interface{}{"refresh": tc.arg})
			if got := getBoolArg(req, "refresh"); got != tc.want {
				t.Errorf("getBoolArg(%T %v) = %v, want %v", tc.arg, tc.arg, got, tc.want)
			}
		})
	}
}

// The declared schema type is what the model sees; declaring a number as a
// string is what forces tool calls to quote it.
func TestToolParamTypes_AreNotAllStrings(t *testing.T) {
	cases := []struct {
		tool  mcp.Tool
		param string
		want  string
	}{
		{listEndpointsTool(), "limit", "number"},
		{searchSpecTool(), "limit", "number"},
		{getEndpointTool(), "resolve_depth", "number"},
		{fetchSpecTool(), "refresh", "boolean"},
	}

	for _, tc := range cases {
		t.Run(tc.tool.Name+"/"+tc.param, func(t *testing.T) {
			got := paramSchemaType(t, tc.tool, tc.param)
			if got != tc.want {
				t.Errorf("%s param %q declared type = %q, want %q", tc.tool.Name, tc.param, got, tc.want)
			}
		})
	}
}

func paramSchemaType(t *testing.T, tool mcp.Tool, param string) string {
	t.Helper()
	raw, ok := tool.InputSchema.Properties[param]
	if !ok {
		t.Fatalf("%s has no param %q", tool.Name, param)
	}
	schema, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("%s param %q is not a schema object (%T)", tool.Name, param, raw)
	}
	typ, _ := schema["type"].(string)
	return typ
}

// ---------------------------------------------------------------------------
// T2 tool surface: list_schemas, deprecated filter, offset (S3/S5/S6)
// ---------------------------------------------------------------------------

func TestListSchemasTool_HasParams(t *testing.T) {
	tool := listSchemasTool()
	assertToolHasParam(t, tool, "url")
	assertToolHasParam(t, tool, "filter")
	if got := paramSchemaType(t, tool, "limit"); got != "number" {
		t.Errorf("limit declared type = %q, want number", got)
	}
}

func TestListEndpointsTool_HasDeprecatedAndOffset(t *testing.T) {
	tool := listEndpointsTool()
	if got := paramSchemaType(t, tool, "deprecated"); got != "boolean" {
		t.Errorf("deprecated declared type = %q, want boolean", got)
	}
	if got := paramSchemaType(t, tool, "offset"); got != "number" {
		t.Errorf("offset declared type = %q, want number", got)
	}
}

func TestGetOffsetArg(t *testing.T) {
	cases := []struct {
		name string
		arg  interface{}
		want int
	}{
		{"json number", float64(100), 100},
		{"legacy string", "100", 100},
		{"absent", nil, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]interface{}{}
			if tc.arg != nil {
				args["offset"] = tc.arg
			}
			if got := getOffset(makeRequest(args)); got != tc.want {
				t.Errorf("getOffset(%v) = %d, want %d", tc.arg, got, tc.want)
			}
		})
	}
}

func TestGetOptionalBoolArg(t *testing.T) {
	yes := getOptionalBoolArg(makeRequest(map[string]interface{}{"deprecated": true}), "deprecated")
	if yes == nil || !*yes {
		t.Errorf("explicit true should yield a non-nil true, got %v", yes)
	}

	no := getOptionalBoolArg(makeRequest(map[string]interface{}{"deprecated": false}), "deprecated")
	if no == nil || *no {
		t.Errorf("explicit false should yield a non-nil false, got %v", no)
	}

	absent := getOptionalBoolArg(makeRequest(map[string]interface{}{}), "deprecated")
	if absent != nil {
		t.Errorf("absent should yield nil (meaning both), got %v", *absent)
	}
}

// ---------------------------------------------------------------------------
// Session default URL (S4)
// ---------------------------------------------------------------------------

func TestNew_SeedsActiveURLFromConfig(t *testing.T) {
	cfg := config.Default()
	cfg.DefaultURL = "https://example.com/seed.json"
	r := New(cfg)
	if got := r.activeURL(); got != cfg.DefaultURL {
		t.Errorf("activeURL = %q, want %q", got, cfg.DefaultURL)
	}
}

func TestResolveURL_ExplicitBeatsActive(t *testing.T) {
	r := New(config.Default())
	r.setActiveURL("https://example.com/active.json")
	req := makeRequest(map[string]interface{}{"url": "https://example.com/explicit.json"})
	if got := r.resolveURL(req); got != "https://example.com/explicit.json" {
		t.Errorf("resolveURL = %q, want explicit url", got)
	}
}

func TestResolveURL_FallsBackToActive(t *testing.T) {
	r := New(config.Default())
	r.setActiveURL("https://example.com/active.json")
	if got := r.resolveURL(makeRequest(map[string]interface{}{})); got != "https://example.com/active.json" {
		t.Errorf("resolveURL = %q, want active url", got)
	}
}

func TestResolveURL_NeitherSetIsEmpty(t *testing.T) {
	r := New(config.Default())
	if got := r.resolveURL(makeRequest(map[string]interface{}{})); got != "" {
		t.Errorf("resolveURL = %q, want empty", got)
	}
}

func TestHandlers_NoURLErrorNamesFetchSpec(t *testing.T) {
	r := New(config.Default())
	ctx := context.Background()
	handlers := map[string]func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error){
		"fetch_spec":     r.handleFetchSpec,
		"list_endpoints": r.handleListEndpoints,
		"get_endpoint":   r.handleGetEndpoint,
		"get_schema":     r.handleGetSchema,
		"search_spec":    r.handleSearchSpec,
		"list_schemas":   r.handleListSchemas,
		"analyze_tags":   r.handleAnalyzeTags,
		"spec_status":    r.handleSpecStatus,
		"refresh_spec":   r.handleRefreshSpec,
		"generate_types": r.handleGenerateTypes,
	}
	for name, h := range handlers {
		t.Run(name, func(t *testing.T) {
			result, err := h(ctx, makeRequest(map[string]interface{}{}))
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			if !result.IsError {
				t.Fatal("expected a tool error when no url is available")
			}
			text := extractText(result)
			for _, want := range []string{"url", "fetch_spec"} {
				if !strings.Contains(text, want) {
					t.Errorf("error %q should mention %q", text, want)
				}
			}
		})
	}
}

func TestURLParam_NotRequired(t *testing.T) {
	tools := []mcp.Tool{
		fetchSpecTool(), listEndpointsTool(), getEndpointTool(), getSchemaTool(),
		searchSpecTool(), listSchemasTool(), analyzeTagsTool(), specStatusTool(),
		refreshSpecTool(), generateTypesTool(),
	}
	for _, tool := range tools {
		for _, req := range tool.InputSchema.Required {
			if req == "url" {
				t.Errorf("tool %q still requires url", tool.Name)
			}
		}
	}
}

func TestActiveURL_ConcurrentAccess(t *testing.T) {
	r := New(config.Default())
	req := makeRequest(map[string]interface{}{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				r.setActiveURL("https://example.com/spec.json")
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = r.resolveURL(req)
				_ = r.activeURL()
			}
		}()
	}
	wg.Wait()
	if got := r.resolveURL(req); got != "https://example.com/spec.json" {
		t.Errorf("resolveURL = %q after concurrent writes", got)
	}
}

func TestSecuritySchemesTool_HasParams(t *testing.T) {
	tool := securitySchemesTool()
	assertToolHasParam(t, tool, "url")
	assertToolHasParam(t, tool, "format")
	for _, req := range tool.InputSchema.Required {
		if req == "url" {
			t.Error("security_schemes must not require url")
		}
	}
}

// tagSpecServer serves a tiny spec: tag "pets" uses Pet, "other" uses Thing,
// and "bare" has an endpoint with no component refs.
func tagSpecServer(t *testing.T) string {
	t.Helper()
	const spec = `{"openapi":"3.0.0","info":{"title":"t","version":"1"},"paths":{
"/pets":{"get":{"tags":["pets"],"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Pet"}}}}}}},
"/things":{"get":{"tags":["other"],"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Thing"}}}}}}},
"/bare":{"get":{"tags":["bare"],"responses":{"204":{"description":"none"}}}}},
"components":{"schemas":{"Pet":{"type":"object","properties":{"name":{"type":"string"}}},"Thing":{"type":"object","properties":{"id":{"type":"string"}}}}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(spec))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestGenerateTypesTool_HasTagParamAndPrecedence(t *testing.T) {
	tool := generateTypesTool()
	if _, ok := tool.InputSchema.Properties["tag"]; !ok {
		t.Fatal("generate_types should expose a tag param")
	}
	desc := strings.ToLower(tool.Description)
	for _, want := range []string{"every schema a tag uses", "deduplicated", "tag wins over schema"} {
		if !strings.Contains(desc, want) {
			t.Errorf("description should mention %q: %s", want, tool.Description)
		}
	}
}

func TestHandleGenerateTypes_TagMode(t *testing.T) {
	r := New(config.Default())
	url := tagSpecServer(t)
	for _, lang := range []string{"typescript", "go"} {
		res, err := r.handleGenerateTypes(context.Background(), makeRequest(map[string]interface{}{
			"url": url, "tag": "pets", "language": lang,
		}))
		if err != nil || res.IsError {
			t.Fatalf("%s: unexpected failure: %v %v", lang, err, res)
		}
		text := extractText(res)
		if !strings.Contains(text, "Pet") || strings.Contains(text, "Thing") {
			t.Errorf("%s: tag mode should emit Pet only:\n%s", lang, text)
		}
	}
}

func TestHandleGenerateTypes_TagWinsOverSchema(t *testing.T) {
	r := New(config.Default())
	url := tagSpecServer(t)
	// schema is valid but the tag matches nothing: the tag path must run and fail.
	res, _ := r.handleGenerateTypes(context.Background(), makeRequest(map[string]interface{}{
		"url": url, "tag": "missing", "schema": "Pet",
	}))
	if !res.IsError || !strings.Contains(extractText(res), "missing") {
		t.Fatalf("expected tag-mode error naming the tag, got %q", extractText(res))
	}
	// tag is valid and schema names a different schema: tag output wins.
	res, _ = r.handleGenerateTypes(context.Background(), makeRequest(map[string]interface{}{
		"url": url, "tag": "pets", "schema": "Thing",
	}))
	text := extractText(res)
	if res.IsError || !strings.Contains(text, "Pet") || strings.Contains(text, "Thing") {
		t.Fatalf("tag should win over schema, got %q", text)
	}
}

func TestHandleGenerateTypes_TagErrorsAreDistinct(t *testing.T) {
	r := New(config.Default())
	url := tagSpecServer(t)
	unknown, _ := r.handleGenerateTypes(context.Background(), makeRequest(map[string]interface{}{"url": url, "tag": "zzz"}))
	bare, _ := r.handleGenerateTypes(context.Background(), makeRequest(map[string]interface{}{"url": url, "tag": "bare"}))
	if !unknown.IsError || !bare.IsError {
		t.Fatal("both cases must be tool errors, not empty output")
	}
	if !strings.Contains(extractText(unknown), "no endpoint") {
		t.Errorf("unknown tag message: %q", extractText(unknown))
	}
	if !strings.Contains(extractText(bare), "no named schemas") {
		t.Errorf("bare tag message: %q", extractText(bare))
	}
}

func TestHandleGenerateTypes_TagModeJSONReportsCount(t *testing.T) {
	r := New(config.Default())
	res, _ := r.handleGenerateTypes(context.Background(), makeRequest(map[string]interface{}{
		"url": tagSpecServer(t), "tag": "pets", "format": "json",
	}))
	text := extractText(res)
	if res.IsError || !strings.Contains(text, `"schema_count"`) || !strings.Contains(text, `"Pet"`) {
		t.Fatalf("expected schema_count and names, got %q", text)
	}
}

func TestHandleGenerateTypes_SchemaModeStillWorks(t *testing.T) {
	r := New(config.Default())
	res, _ := r.handleGenerateTypes(context.Background(), makeRequest(map[string]interface{}{
		"url": tagSpecServer(t), "schema": "Thing",
	}))
	if res.IsError || !strings.Contains(extractText(res), "Thing") {
		t.Fatalf("schema mode regressed: %q", extractText(res))
	}
}

// ---------------------------------------------------------------------------
// progressCtx — fetch heartbeat plumbing (S7)
// ---------------------------------------------------------------------------

// Without a progress token the caller did not ask to be kept informed, so the
// context must pass through untouched and no heartbeat goroutine may start.
func TestProgressCtx_NoTokenIsUnchanged(t *testing.T) {
	r := New(config.Default())
	ctx := context.Background()

	got := r.progressCtx(ctx, makeRequest(map[string]interface{}{}))

	if got != ctx {
		t.Error("progressCtx returned a different context without a progress token")
	}
}

// A token alone is not enough: without the MCP server reachable from ctx there is
// nobody to notify, so the context must still pass through untouched.
func TestProgressCtx_NoServerIsUnchanged(t *testing.T) {
	r := New(config.Default())
	ctx := context.Background()
	req := mcp.CallToolRequest{Params: mcp.CallToolParams{
		Meta: &mcp.Meta{ProgressToken: "token-1"},
	}}

	got := r.progressCtx(ctx, req)

	if got != ctx {
		t.Error("progressCtx attached a heartbeat with no server in the context")
	}
}

func TestValidateSpecTool_HasParams(t *testing.T) {
	tool := validateSpecTool()
	assertToolHasParam(t, tool, "url")
	assertToolHasParam(t, tool, "format")
	for _, req := range tool.InputSchema.Required {
		if req == "url" {
			t.Error("validate_spec must not require url")
		}
	}
}

func TestHandleValidateSpec_LoadsWithViolations(t *testing.T) {
	// No "openapi" field: the loader skips validation so this loads, but a
	// strict validation pass reports it.
	const spec = `{"info":{"title":"t","version":"1"},"paths":{}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(spec))
	}))
	t.Cleanup(srv.Close)

	r := New(config.Default())
	res, err := r.handleValidateSpec(context.Background(), makeRequest(map[string]interface{}{"url": srv.URL}))
	if err != nil || res.IsError {
		t.Fatalf("report must be a normal result: %v %v", err, res)
	}
	if text := extractText(res); !strings.Contains(text, "INVALID") {
		t.Errorf("expected INVALID verdict, got %q", text)
	}
}

func TestHandleValidateSpec_RequiresURL(t *testing.T) {
	r := New(config.Default())
	res, _ := r.handleValidateSpec(context.Background(), makeRequest(map[string]interface{}{}))
	if !res.IsError || !strings.Contains(extractText(res), "fetch_spec") {
		t.Fatalf("expected URL-required error, got %q", extractText(res))
	}
}
