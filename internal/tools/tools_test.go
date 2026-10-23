package tools

import (
	"context"
	"strings"
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
	req := makeRequest(map[string]interface{}{"spec": "petstore"})
	if got := getStringArg(req, "spec"); got != "petstore" {
		t.Errorf("getStringArg = %q, want petstore", got)
	}
}

func TestGetStringArg_Missing(t *testing.T) {
	req := makeRequest(map[string]interface{}{})
	if got := getStringArg(req, "spec"); got != "" {
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
	if r.specs == nil {
		t.Error("specs should not be nil")
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
	r := newRegistry(t)
	result, err := r.handleUsageGuide(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("handleUsageGuide error: %v", err)
	}
	text := extractText(result)
	if text == "" {
		t.Fatal("usage guide should not be empty")
	}
	// Spot-check that the guide carries its key signals for the LLM.
	for _, want := range []string{"analyze_tags", "$ref(", "resolve_depth", "Anti-patterns", "swagger-mcp index"} {
		if !strings.Contains(text, want) {
			t.Errorf("usage guide should mention %q", want)
		}
	}
	// The guide must not teach the removed fetch path (S4a).
	for _, gone := range []string{"fetch_spec", "refresh_spec", "`url`", "url)"} {
		if strings.Contains(text, gone) {
			t.Errorf("usage guide still mentions %q", gone)
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
		{listEndpointsTool(), "deprecated", "boolean"},
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
	assertToolHasParam(t, tool, "spec")
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
// Tool surface (S4a): a spec name addresses a query, nothing else
// ---------------------------------------------------------------------------

func TestSecuritySchemesTool_HasParams(t *testing.T) {
	tool := securitySchemesTool()
	assertToolHasParam(t, tool, "spec")
	assertToolHasParam(t, tool, "format")
	for _, req := range tool.InputSchema.Required {
		if req == "spec" {
			t.Error("security_schemes must not require spec")
		}
	}
}

// tagSpecFixture indexes a tiny spec: tag "pets" uses Pet, "other" uses Thing,
// and "bare" has an endpoint with no component refs.
func tagSpecFixture(t *testing.T) {
	t.Helper()
	const spec = `{"openapi":"3.0.0","info":{"title":"t","version":"1"},"paths":{
"/pets":{"get":{"tags":["pets"],"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Pet"}}}}}}},
"/things":{"get":{"tags":["other"],"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Thing"}}}}}}},
"/bare":{"get":{"tags":["bare"],"responses":{"204":{"description":"none"}}}}},
"components":{"schemas":{"Pet":{"type":"object","properties":{"name":{"type":"string"}}},"Thing":{"type":"object","properties":{"id":{"type":"string"}}}}}}`
	projectDir(t)
	indexSpec(t, "tags", []byte(spec))
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
	tagSpecFixture(t)
	r := newRegistry(t)
	for _, lang := range []string{"typescript", "go"} {
		res, err := r.handleGenerateTypes(context.Background(), makeRequest(map[string]interface{}{
			"tag": "pets", "language": lang,
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
	tagSpecFixture(t)
	r := newRegistry(t)
	// schema is valid but the tag matches nothing: the tag path must run and fail.
	res, _ := r.handleGenerateTypes(context.Background(), makeRequest(map[string]interface{}{
		"tag": "missing", "schema": "Pet",
	}))
	if !res.IsError || !strings.Contains(extractText(res), "missing") {
		t.Fatalf("expected tag-mode error naming the tag, got %q", extractText(res))
	}
	// tag is valid and schema names a different schema: tag output wins.
	res, _ = r.handleGenerateTypes(context.Background(), makeRequest(map[string]interface{}{
		"tag": "pets", "schema": "Thing",
	}))
	text := extractText(res)
	if res.IsError || !strings.Contains(text, "Pet") || strings.Contains(text, "Thing") {
		t.Fatalf("tag should win over schema, got %q", text)
	}
}

func TestHandleGenerateTypes_TagErrorsAreDistinct(t *testing.T) {
	tagSpecFixture(t)
	r := newRegistry(t)
	unknown, _ := r.handleGenerateTypes(context.Background(), makeRequest(map[string]interface{}{"tag": "zzz"}))
	bare, _ := r.handleGenerateTypes(context.Background(), makeRequest(map[string]interface{}{"tag": "bare"}))
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
	tagSpecFixture(t)
	r := newRegistry(t)
	res, _ := r.handleGenerateTypes(context.Background(), makeRequest(map[string]interface{}{
		"tag": "pets", "format": "json",
	}))
	text := extractText(res)
	if res.IsError || !strings.Contains(text, `"schema_count"`) || !strings.Contains(text, `"Pet"`) {
		t.Fatalf("expected schema_count and names, got %q", text)
	}
}

func TestHandleGenerateTypes_SchemaModeStillWorks(t *testing.T) {
	tagSpecFixture(t)
	r := newRegistry(t)
	res, _ := r.handleGenerateTypes(context.Background(), makeRequest(map[string]interface{}{
		"schema": "Thing",
	}))
	if res.IsError || !strings.Contains(extractText(res), "Thing") {
		t.Fatalf("schema mode regressed: %q", extractText(res))
	}
}

func TestValidateSpecTool_HasParams(t *testing.T) {
	tool := validateSpecTool()
	assertToolHasParam(t, tool, "spec")
	assertToolHasParam(t, tool, "format")
	for _, req := range tool.InputSchema.Required {
		if req == "spec" {
			t.Error("validate_spec must not require spec")
		}
	}
}

func TestHandleValidateSpec_LoadsWithViolations(t *testing.T) {
	// No "openapi" field: the loader relaxes validation so this still indexes,
	// but a strict validation pass reports it.
	projectDir(t)
	indexSpec(t, "loose", []byte(`{"info":{"title":"t","version":"1"},"paths":{}}`))
	r := newRegistry(t)

	res, err := r.handleValidateSpec(context.Background(), makeRequest(map[string]interface{}{}))
	if err != nil || res.IsError {
		t.Fatalf("report must be a normal result: %v %v", err, res)
	}
	if text := extractText(res); !strings.Contains(text, "INVALID") {
		t.Errorf("expected INVALID verdict, got %q", text)
	}
}

func TestHandleValidateSpec_ReportsSpecTheLoaderRejects(t *testing.T) {
	const spec = `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{"/pets/{id}":{"get":{"responses":{"200":{"description":"ok"}}}}}}`
	projectDir(t)
	indexSpec(t, "missing-param", []byte(spec))
	r := newRegistry(t)

	res, err := r.handleValidateSpec(context.Background(), makeRequest(map[string]interface{}{}))
	if err != nil || res.IsError {
		t.Fatalf("report must be a normal result: %v %q", err, extractText(res))
	}
	text := extractText(res)
	if !strings.Contains(text, "INVALID") || strings.Contains(text, "INVALID_SPEC") {
		t.Errorf("expected INVALID verdict, got %q", text)
	}
	if !strings.Contains(text, "id") {
		t.Errorf("expected the violation to be listed, got %q", text)
	}
}
