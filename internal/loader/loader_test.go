package loader

import (
	"context"
	"os"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/hnkatze/swagger-mcp-go/internal/config"
	"github.com/hnkatze/swagger-mcp-go/internal/types"
)

func TestNormalizeURL_Basic(t *testing.T) {
	input := "HTTPS://API.Example.COM/v1/"
	want := "https://api.example.com/v1"
	got := normalizeURL(input)
	if got != want {
		t.Errorf("normalizeURL(%q) = %q, want %q", input, got, want)
	}
}

func TestNormalizeURL_PreservesPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "preserves path casing",
			input: "https://api.example.com/V2/Users",
			want:  "https://api.example.com/V2/Users",
		},
		{
			name:  "trims trailing slash",
			input: "https://api.example.com/v1/",
			want:  "https://api.example.com/v1",
		},
		{
			name:  "keeps root path",
			input: "https://api.example.com/",
			want:  "https://api.example.com/",
		},
		{
			name:  "trims multiple trailing slashes",
			input: "https://api.example.com/v1///",
			want:  "https://api.example.com/v1",
		},
		{
			name:  "preserves query string",
			input: "https://api.example.com/v1?format=json",
			want:  "https://api.example.com/v1?format=json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeURL(tt.input)
			if got != tt.want {
				t.Errorf("normalizeURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeURL_InvalidURL(t *testing.T) {
	// url.Parse fails on URLs containing ASCII control characters like \x01.
	// In that case, normalizeURL should return the raw input as-is.
	input := "https://example.com/\x01bad"
	got := normalizeURL(input)
	if got != input {
		t.Errorf("normalizeURL(%q) = %q, want input returned as-is", input, got)
	}
}

// loadPetstoreData is a test helper that reads the petstore.json fixture.
func loadPetstoreData(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/petstore.json")
	if err != nil {
		t.Fatalf("failed to read petstore.json: %v", err)
	}
	return data
}

func TestBuildSummary_Petstore(t *testing.T) {
	data := loadPetstoreData(t)

	loader := New(config.Default())
	doc, err := loader.LoadFromData(data)
	if err != nil {
		t.Fatalf("failed to parse petstore.json: %v", err)
	}

	summary := buildSummary(doc)

	if summary.Title != "Petstore" {
		t.Errorf("expected title %q, got %q", "Petstore", summary.Title)
	}
	if summary.Version != "1.0.0" {
		t.Errorf("expected version %q, got %q", "1.0.0", summary.Version)
	}
	if summary.EndpointCount != 5 {
		t.Errorf("expected 5 endpoints, got %d", summary.EndpointCount)
	}
	// The petstore.json fixture has no top-level "tags" array,
	// so buildSummary counts 0 from doc.Tags. Tags are only referenced
	// inside operations, not declared at the top level.
	if summary.TagCount != 0 {
		t.Errorf("expected 0 top-level tags (none declared in fixture), got %d", summary.TagCount)
	}
	if summary.SchemaCount != 4 {
		t.Errorf("expected 4 schemas, got %d", summary.SchemaCount)
	}
	if summary.SpecVersion != "3.0.3" {
		t.Errorf("expected spec version %q, got %q", "3.0.3", summary.SpecVersion)
	}
	if summary.BaseURL != "https://petstore.example.com/v1" {
		t.Errorf("expected base URL %q, got %q", "https://petstore.example.com/v1", summary.BaseURL)
	}
}

func TestBuildSummary_EmptyDoc(t *testing.T) {
	doc := &openapi3.T{}

	summary := buildSummary(doc)

	if summary.Title != "" {
		t.Errorf("expected empty title, got %q", summary.Title)
	}
	if summary.Version != "" {
		t.Errorf("expected empty version, got %q", summary.Version)
	}
	if summary.EndpointCount != 0 {
		t.Errorf("expected 0 endpoints, got %d", summary.EndpointCount)
	}
	if summary.TagCount != 0 {
		t.Errorf("expected 0 tags, got %d", summary.TagCount)
	}
	if summary.SchemaCount != 0 {
		t.Errorf("expected 0 schemas, got %d", summary.SchemaCount)
	}
}

func TestLoadFromData_ValidJSON(t *testing.T) {
	data := loadPetstoreData(t)

	loader := New(config.Default())
	doc, err := loader.LoadFromData(data)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if doc == nil {
		t.Fatal("expected non-nil doc")
	}
	if doc.Info == nil {
		t.Fatal("expected non-nil doc.Info")
	}
	if doc.Info.Title != "Petstore" {
		t.Errorf("expected title %q, got %q", "Petstore", doc.Info.Title)
	}
}

func TestLoadFromData_InvalidJSON(t *testing.T) {
	loader := New(config.Default())
	_, err := loader.LoadFromData([]byte(`this is not json or yaml at all`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}

	toolErr, ok := err.(*types.ToolError)
	if !ok {
		t.Fatalf("expected *types.ToolError, got %T", err)
	}
	if toolErr.Code != types.ErrParseError {
		t.Errorf("expected error code %q, got %q", types.ErrParseError, toolErr.Code)
	}
}

func TestLoadFromURL_InvalidURL(t *testing.T) {
	loader := New(config.Default())
	_, _, err := loader.LoadFromURL(context.Background(), "not-a-url")
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}

	toolErr, ok := err.(*types.ToolError)
	if !ok {
		t.Fatalf("expected *types.ToolError, got %T", err)
	}
	if toolErr.Code != types.ErrInvalidURL {
		t.Errorf("expected error code %q, got %q", types.ErrInvalidURL, toolErr.Code)
	}
}
