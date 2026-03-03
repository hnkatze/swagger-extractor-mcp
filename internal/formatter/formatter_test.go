package formatter

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hnkatze/swagger-mcp-go/internal/types"
)

// ---------------------------------------------------------------------------
// FormatJSON
// ---------------------------------------------------------------------------

func TestFormatJSON_Simple(t *testing.T) {
	data := map[string]string{"hello": "world"}
	out, err := FormatJSON(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Must be valid JSON
	var parsed map[string]string
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if parsed["hello"] != "world" {
		t.Errorf("expected hello=world, got %q", parsed["hello"])
	}
}

func TestFormatJSON_EndpointSummary(t *testing.T) {
	ep := types.EndpointSummary{
		Method:  "GET",
		Path:    "/users",
		Summary: "List users",
		Tags:    []string{"users"},
	}
	out, err := FormatJSON(ep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"method"`) {
		t.Error("expected output to contain method field")
	}
	if !strings.Contains(out, `"path"`) {
		t.Error("expected output to contain path field")
	}
	if !strings.Contains(out, "GET") {
		t.Error("expected output to contain GET")
	}
	if !strings.Contains(out, "/users") {
		t.Error("expected output to contain /users")
	}
}

// ---------------------------------------------------------------------------
// Format dispatcher
// ---------------------------------------------------------------------------

func TestFormat_JSON(t *testing.T) {
	data := map[string]string{"key": "value"}
	out, err := Format(data, types.FormatJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]string
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if parsed["key"] != "value" {
		t.Errorf("expected key=value, got %q", parsed["key"])
	}
}

func TestFormat_TOON(t *testing.T) {
	data := map[string]interface{}{"name": "test"}
	out, err := Format(data, types.FormatTOON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "name") {
		t.Error("expected TOON output to contain 'name'")
	}
	if !strings.Contains(out, "test") {
		t.Error("expected TOON output to contain 'test'")
	}
}

func TestFormat_Invalid(t *testing.T) {
	_, err := Format("data", types.OutputFormat("xml"))
	if err == nil {
		t.Fatal("expected error for unsupported format, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported format") {
		t.Errorf("expected 'unsupported format' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// FormatEndpointTOON
// ---------------------------------------------------------------------------

func TestFormatEndpointTOON_Basic(t *testing.T) {
	detail := &types.EndpointDetail{
		Method:  "GET",
		Path:    "/pets",
		Summary: "List all pets",
		Parameters: []types.ParameterDetail{
			{
				Name:     "limit",
				In:       "query",
				Required: false,
				Schema:   map[string]interface{}{"type": "integer"},
			},
		},
	}

	out := FormatEndpointTOON(detail)

	if !strings.HasPrefix(out, "GET /pets") {
		t.Errorf("expected output to start with 'GET /pets', got: %q", out[:min(len(out), 30)])
	}
	if !strings.Contains(out, "summary: List all pets") {
		t.Error("expected output to contain summary line")
	}
	if !strings.Contains(out, "limit") {
		t.Error("expected output to contain parameter 'limit'")
	}
	if !strings.Contains(out, "query") {
		t.Error("expected output to contain parameter location 'query'")
	}
}

func TestFormatEndpointTOON_WithRequestBody(t *testing.T) {
	detail := &types.EndpointDetail{
		Method:  "POST",
		Path:    "/pets",
		Summary: "Create a pet",
		RequestBody: &types.RequestBodyDetail{
			Required:    true,
			Description: "Pet to create",
			Content: map[string]types.MediaDetail{
				"application/json": {
					Schema: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"name": map[string]interface{}{"type": "string"},
						},
					},
				},
			},
		},
	}

	out := FormatEndpointTOON(detail)

	if !strings.Contains(out, "request_body") {
		t.Error("expected output to contain 'request_body'")
	}
	if !strings.Contains(out, "required") {
		t.Error("expected output to contain 'required' for request body")
	}
	if !strings.Contains(out, "application/json") {
		t.Error("expected output to contain content type")
	}
}

func TestFormatEndpointTOON_Deprecated(t *testing.T) {
	detail := &types.EndpointDetail{
		Method:     "DELETE",
		Path:       "/pets/{id}",
		Deprecated: true,
	}

	out := FormatEndpointTOON(detail)

	if !strings.Contains(out, "deprecated: true") {
		t.Error("expected output to contain 'deprecated: true'")
	}
}

// ---------------------------------------------------------------------------
// FormatEndpointsTOON
// ---------------------------------------------------------------------------

func TestFormatEndpointsTOON_MultipleEndpoints(t *testing.T) {
	endpoints := []types.EndpointSummary{
		{Method: "GET", Path: "/pets", Summary: "List pets"},
		{Method: "POST", Path: "/pets", Summary: "Create pet"},
	}

	out := FormatEndpointsTOON(endpoints)
	lines := strings.Split(out, "\n")

	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "GET") || !strings.Contains(lines[0], "/pets") {
		t.Errorf("first line should contain GET /pets, got: %q", lines[0])
	}
	if !strings.Contains(lines[1], "POST") || !strings.Contains(lines[1], "/pets") {
		t.Errorf("second line should contain POST /pets, got: %q", lines[1])
	}
}

func TestFormatEndpointsTOON_WithTags(t *testing.T) {
	endpoints := []types.EndpointSummary{
		{Method: "GET", Path: "/users", Summary: "List users", Tags: []string{"users", "admin"}},
	}

	out := FormatEndpointsTOON(endpoints)

	if !strings.Contains(out, "[users, admin]") {
		t.Errorf("expected tags in brackets [users, admin], got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// FormatTagsTOON
// ---------------------------------------------------------------------------

func TestFormatTagsTOON_Basic(t *testing.T) {
	tags := []types.TagSummary{
		{Name: "pets", EndpointCount: 5},
	}

	out := FormatTagsTOON(tags)

	if !strings.Contains(out, "pets") {
		t.Error("expected output to contain tag name 'pets'")
	}
	if !strings.Contains(out, "endpoints: 5") {
		t.Error("expected output to contain 'endpoints: 5'")
	}
}

func TestFormatTagsTOON_WithMethods(t *testing.T) {
	tags := []types.TagSummary{
		{
			Name:          "users",
			EndpointCount: 3,
			MethodBreakdown: map[string]int{
				"GET":  2,
				"POST": 1,
			},
		},
	}

	out := FormatTagsTOON(tags)

	if !strings.Contains(out, "methods:") {
		t.Error("expected output to contain 'methods:'")
	}
	if !strings.Contains(out, "GET: 2") {
		t.Error("expected output to contain 'GET: 2'")
	}
	if !strings.Contains(out, "POST: 1") {
		t.Error("expected output to contain 'POST: 1'")
	}
}

// ---------------------------------------------------------------------------
// FormatSchemaTOON
// ---------------------------------------------------------------------------

func TestFormatSchemaTOON_SimpleObject(t *testing.T) {
	schema := &types.SchemaDetail{
		Name: "User",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":  map[string]interface{}{"type": "string"},
				"email": map[string]interface{}{"type": "string"},
			},
		},
	}

	out := FormatSchemaTOON(schema)

	if !strings.HasPrefix(out, "User:") {
		t.Errorf("expected output to start with 'User:', got: %q", out[:min(len(out), 20)])
	}
	if !strings.Contains(out, "name: string") {
		t.Error("expected output to contain 'name: string'")
	}
	if !strings.Contains(out, "email: string") {
		t.Error("expected output to contain 'email: string'")
	}
}

// ---------------------------------------------------------------------------
// FormatDiffTOON
// ---------------------------------------------------------------------------

func TestFormatDiffTOON_Added(t *testing.T) {
	diff := &types.DiffResult{
		Added: []types.EndpointSummary{
			{Method: "GET", Path: "/new-endpoint", Summary: "New"},
		},
	}

	out := FormatDiffTOON(diff)

	if !strings.Contains(out, "added:") {
		t.Error("expected output to contain 'added:'")
	}
	if !strings.Contains(out, "+ GET /new-endpoint") {
		t.Error("expected output to contain '+ GET /new-endpoint'")
	}
}

func TestFormatDiffTOON_Removed(t *testing.T) {
	diff := &types.DiffResult{
		Removed: []types.EndpointSummary{
			{Method: "DELETE", Path: "/old-endpoint", Summary: "Old"},
		},
	}

	out := FormatDiffTOON(diff)

	if !strings.Contains(out, "removed:") {
		t.Error("expected output to contain 'removed:'")
	}
	if !strings.Contains(out, "- DELETE /old-endpoint") {
		t.Error("expected output to contain '- DELETE /old-endpoint'")
	}
}

func TestFormatDiffTOON_Changed(t *testing.T) {
	diff := &types.DiffResult{
		Changed: []types.EndpointChange{
			{
				Method:  "PUT",
				Path:    "/users/{id}",
				Changes: []string{"summary changed", "parameter added: role"},
			},
		},
	}

	out := FormatDiffTOON(diff)

	if !strings.Contains(out, "changed:") {
		t.Error("expected output to contain 'changed:'")
	}
	if !strings.Contains(out, "~ PUT /users/{id}") {
		t.Error("expected output to contain '~ PUT /users/{id}'")
	}
	if !strings.Contains(out, "summary changed") {
		t.Error("expected output to contain change detail 'summary changed'")
	}
	if !strings.Contains(out, "parameter added: role") {
		t.Error("expected output to contain change detail 'parameter added: role'")
	}
}

func TestFormatDiffTOON_Empty(t *testing.T) {
	diff := &types.DiffResult{}
	out := FormatDiffTOON(diff)
	if out != "" {
		t.Errorf("expected empty string for empty diff, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// Helpers: needsQuotes (table-driven)
// ---------------------------------------------------------------------------

func TestNeedsQuotes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "WithColon",
			input:    "key: value",
			expected: true,
		},
		{
			name:     "WithNewline",
			input:    "line1\nline2",
			expected: true,
		},
		{
			name:     "LeadingSpace",
			input:    " hello",
			expected: true,
		},
		{
			name:     "TrailingSpace",
			input:    "hello ",
			expected: true,
		},
		{
			name:     "Normal",
			input:    "hello",
			expected: false,
		},
		{
			name:     "Empty",
			input:    "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := needsQuotes(tt.input)
			if got != tt.expected {
				t.Errorf("needsQuotes(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers: quote
// ---------------------------------------------------------------------------

func TestQuote_Basic(t *testing.T) {
	out := quote("hello world")
	if out != `"hello world"` {
		t.Errorf("expected %q, got %q", `"hello world"`, out)
	}
}

func TestQuote_EscapesInternalQuotes(t *testing.T) {
	out := quote(`say "hi"`)
	if !strings.HasPrefix(out, `"`) || !strings.HasSuffix(out, `"`) {
		t.Errorf("expected quoted output, got: %s", out)
	}
	if !strings.Contains(out, `\"`) {
		t.Error("expected escaped internal quotes")
	}
}

func TestQuote_EscapesNewlines(t *testing.T) {
	out := quote("line1\nline2")
	if strings.Contains(out, "\n") && !strings.Contains(out, `\n`) {
		t.Error("expected newlines to be escaped")
	}
	if !strings.Contains(out, `\n`) {
		t.Error("expected output to contain escaped newline sequence")
	}
}

// ---------------------------------------------------------------------------
// Helpers: extractSchemaType (table-driven)
// ---------------------------------------------------------------------------

func TestExtractSchemaType(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{
			name:     "String",
			input:    map[string]interface{}{"type": "string"},
			expected: "string",
		},
		{
			name:     "StringWithFormat",
			input:    map[string]interface{}{"type": "string", "format": "email"},
			expected: "string(email)",
		},
		{
			name:     "Integer",
			input:    map[string]interface{}{"type": "integer", "format": "int64"},
			expected: "integer(int64)",
		},
		{
			name: "Array",
			input: map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
			expected: "[]string",
		},
		{
			name: "ArrayNoItems",
			input: map[string]interface{}{
				"type": "array",
			},
			expected: "[]",
		},
		{
			name: "Enum",
			input: map[string]interface{}{
				"enum": []interface{}{"active", "inactive", "pending"},
			},
			expected: "enum(active, inactive, pending)",
		},
		{
			name:     "Nil",
			input:    nil,
			expected: "",
		},
		{
			name:     "NotAMap",
			input:    "just a string",
			expected: "",
		},
		{
			name: "Ref",
			input: map[string]interface{}{
				"$ref": "#/components/schemas/Pet",
			},
			expected: "$ref(Pet)",
		},
		{
			name: "ObjectWithProperties",
			input: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSchemaType(tt.input)
			if got != tt.expected {
				t.Errorf("extractSchemaType() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers: hasKey
// ---------------------------------------------------------------------------

func TestHasKey_Present(t *testing.T) {
	m := map[string]interface{}{"name": "test", "age": 30}
	if !hasKey(m, "name") {
		t.Error("expected hasKey to return true for existing key 'name'")
	}
	if !hasKey(m, "age") {
		t.Error("expected hasKey to return true for existing key 'age'")
	}
}

func TestHasKey_Absent(t *testing.T) {
	m := map[string]interface{}{"name": "test"}
	if hasKey(m, "missing") {
		t.Error("expected hasKey to return false for missing key")
	}
}

func TestHasKey_NilValue(t *testing.T) {
	m := map[string]interface{}{"key": nil}
	if !hasKey(m, "key") {
		t.Error("expected hasKey to return true even when value is nil")
	}
}

// ---------------------------------------------------------------------------
// min helper for Go < 1.21 compat
// ---------------------------------------------------------------------------

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
