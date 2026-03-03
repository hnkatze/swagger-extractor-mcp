package formatter

import (
	"encoding/json"
	"fmt"

	"github.com/hnkatze/swagger-mcp-go/internal/types"
)

// FormatJSON marshals data to indented JSON with 2-space indent.
func FormatJSON(data interface{}) (string, error) {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", fmt.Errorf("json marshal error: %w", err)
	}
	return string(b), nil
}

// FormatEndpointJSON formats an endpoint detail as clean JSON.
func FormatEndpointJSON(detail *types.EndpointDetail) (string, error) {
	return FormatJSON(detail)
}

// FormatEndpointsJSON formats an endpoint list as JSON.
func FormatEndpointsJSON(endpoints []types.EndpointSummary) (string, error) {
	return FormatJSON(endpoints)
}

// FormatTagsJSON formats tags analysis as JSON.
func FormatTagsJSON(tags []types.TagSummary) (string, error) {
	return FormatJSON(tags)
}

// FormatSchemaJSON formats a schema as JSON.
func FormatSchemaJSON(schema *types.SchemaDetail) (string, error) {
	return FormatJSON(schema)
}

// FormatDiffJSON formats a diff result as JSON.
func FormatDiffJSON(diff *types.DiffResult) (string, error) {
	return FormatJSON(diff)
}

// FormatListResultJSON formats a ListResult (with truncation metadata) as JSON.
func FormatListResultJSON(result *types.ListResult) (string, error) {
	return FormatJSON(result)
}
