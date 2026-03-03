package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/hnkatze/swagger-mcp-go/internal/config"
)

const honducafeURL = "https://tguivzrkzp.us-east-1.awsapprunner.com/swagger/v1/swagger.json"

func newTestRegistry() *Registry {
	cfg := config.Load()
	return New(cfg)
}

// TestIntegration_FetchSpec tests fetching HonduCafé spec.
func TestIntegration_FetchSpec(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	r := newTestRegistry()
	ctx := context.Background()

	req := makeRequest(map[string]interface{}{"url": honducafeURL})
	result, err := r.handleFetchSpec(ctx, req)
	if err != nil {
		t.Fatalf("handleFetchSpec error: %v", err)
	}

	text := extractText(result)
	t.Logf("fetch_spec output (%d chars):\n%s", len(text), text)

	if !strings.Contains(text, "endpoint_count") && !strings.Contains(text, "title") {
		t.Error("expected spec summary in output")
	}
}

// TestIntegration_ListEndpoints_NoFilter tests list_endpoints with default limit.
func TestIntegration_ListEndpoints_NoFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	r := newTestRegistry()
	ctx := context.Background()

	// No filters, no explicit format → should use TOON (config default) with limit 50
	req := makeRequest(map[string]interface{}{"url": honducafeURL})
	result, err := r.handleListEndpoints(ctx, req)
	if err != nil {
		t.Fatalf("handleListEndpoints error: %v", err)
	}

	text := extractText(result)
	t.Logf("list_endpoints (no filter, default TOON, limit 50) output (%d chars):\n%s", len(text), truncateForLog(text, 2000))

	// Should be truncated with metadata header
	if !strings.Contains(text, "showing 50 of") {
		t.Error("expected truncation header 'showing 50 of N'")
	}
	if !strings.Contains(text, "filters") {
		t.Error("expected filter guidance in header")
	}
}

// TestIntegration_ListEndpoints_WithTag tests filtered list.
func TestIntegration_ListEndpoints_WithTag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	r := newTestRegistry()
	ctx := context.Background()

	// First get tags
	tagReq := makeRequest(map[string]interface{}{"url": honducafeURL})
	tagResult, err := r.handleAnalyzeTags(ctx, tagReq)
	if err != nil {
		t.Fatalf("handleAnalyzeTags error: %v", err)
	}

	tagText := extractText(tagResult)
	t.Logf("analyze_tags output (%d chars):\n%s", len(tagText), truncateForLog(tagText, 2000))

	// Now list with a tag filter — pick first tag from output
	// Use a known tag pattern for HonduCafé
	req := makeRequest(map[string]interface{}{
		"url": honducafeURL,
		"tag": "Auth",
	})
	result, err := r.handleListEndpoints(ctx, req)
	if err != nil {
		t.Fatalf("handleListEndpoints (tag=Auth) error: %v", err)
	}

	text := extractText(result)
	t.Logf("list_endpoints (tag=Auth, TOON) output (%d chars):\n%s", len(text), text)
}

// TestIntegration_ListEndpoints_JSON tests backward compat with explicit JSON.
func TestIntegration_ListEndpoints_JSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	r := newTestRegistry()
	ctx := context.Background()

	req := makeRequest(map[string]interface{}{
		"url":    honducafeURL,
		"format": "json",
		"limit":  "5",
	})
	result, err := r.handleListEndpoints(ctx, req)
	if err != nil {
		t.Fatalf("handleListEndpoints (json, limit=5) error: %v", err)
	}

	text := extractText(result)
	t.Logf("list_endpoints (json, limit=5) output (%d chars):\n%s", len(text), text)

	if !strings.Contains(text, `"total"`) {
		t.Error("expected JSON with total field")
	}
	if !strings.Contains(text, `"truncated"`) {
		t.Error("expected JSON with truncated field")
	}
}

// TestIntegration_ListEndpoints_Unlimited tests limit=0 returns all.
func TestIntegration_ListEndpoints_Unlimited(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	r := newTestRegistry()
	ctx := context.Background()

	req := makeRequest(map[string]interface{}{
		"url":   honducafeURL,
		"limit": "0",
	})
	result, err := r.handleListEndpoints(ctx, req)
	if err != nil {
		t.Fatalf("handleListEndpoints (unlimited) error: %v", err)
	}

	text := extractText(result)
	t.Logf("list_endpoints (unlimited) output (%d chars)", len(text))

	// Should NOT be truncated
	if strings.Contains(text, "showing") {
		t.Error("unlimited should not show truncation header")
	}
}

// TestIntegration_SearchSpec tests search with auto-limit.
func TestIntegration_SearchSpec(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	r := newTestRegistry()
	ctx := context.Background()

	req := makeRequest(map[string]interface{}{
		"url":   honducafeURL,
		"query": "product",
	})
	result, err := r.handleSearchSpec(ctx, req)
	if err != nil {
		t.Fatalf("handleSearchSpec error: %v", err)
	}

	text := extractText(result)
	t.Logf("search_spec (query=product, TOON) output (%d chars):\n%s", len(text), truncateForLog(text, 2000))
}

// TestIntegration_TokenComparison compares old vs new token count.
func TestIntegration_TokenComparison(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	r := newTestRegistry()
	ctx := context.Background()

	// "Old" style: JSON, no limit
	oldReq := makeRequest(map[string]interface{}{
		"url":    honducafeURL,
		"format": "json",
		"limit":  "0",
	})
	oldResult, err := r.handleListEndpoints(ctx, oldReq)
	if err != nil {
		t.Fatalf("old-style error: %v", err)
	}
	oldText := extractText(oldResult)

	// "New" style: TOON (default), limit 50
	newReq := makeRequest(map[string]interface{}{
		"url": honducafeURL,
	})
	newResult, err := r.handleListEndpoints(ctx, newReq)
	if err != nil {
		t.Fatalf("new-style error: %v", err)
	}
	newText := extractText(newResult)

	oldTokens := estimateTokens(oldText)
	newTokens := estimateTokens(newText)
	savings := float64(oldTokens-newTokens) / float64(oldTokens) * 100

	t.Logf("=== TOKEN COMPARISON ===")
	t.Logf("OLD (JSON, all):    %d chars → ~%d tokens", len(oldText), oldTokens)
	t.Logf("NEW (TOON, limit 50): %d chars → ~%d tokens", len(newText), newTokens)
	t.Logf("SAVINGS: %.1f%%", savings)

	if savings < 30 {
		t.Errorf("expected at least 30%% savings, got %.1f%%", savings)
	}
}

// --- helpers ---

func extractText(result *mcp.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	return fmt.Sprintf("%v", result.Content)
}

func estimateTokens(text string) int {
	// Rough estimate: ~4 chars per token for English/code
	return len(text) / 4
}

func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + fmt.Sprintf("\n... [truncated, %d total chars]", len(s))
}
