package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/hnkatze/swagger-mcp-go/internal/config"
)

// fixtureResource describes one REST resource in the generated spec. Each
// resource yields exactly five endpoints (list, create, get, update, delete), so
// the endpoint count is len(fixtureResources) * 5 = 130.
type fixtureResource struct {
	Name string // path segment, e.g. "products"
	Tag  string // OpenAPI tag
	Noun string // singular noun used in summaries
}

// fixtureResources is a fixed, ordered list: 26 resources x 5 operations.
// "Auth" is the stable tag target and "products" the stable search target.
var fixtureResources = []fixtureResource{
	{"sessions", "Auth", "session"},
	{"tokens", "Auth", "access token"},
	{"credentials", "Auth", "credential"},
	{"roles", "Auth", "role"},
	{"permissions", "Auth", "permission"},
	{"products", "Catalog", "product"},
	{"categories", "Catalog", "category"},
	{"brands", "Catalog", "brand"},
	{"variants", "Catalog", "product variant"},
	{"prices", "Catalog", "price"},
	{"orders", "Orders", "order"},
	{"order-items", "Orders", "order item"},
	{"shipments", "Orders", "shipment"},
	{"returns", "Orders", "return request"},
	{"invoices", "Orders", "invoice"},
	{"customers", "Customers", "customer"},
	{"addresses", "Customers", "address"},
	{"wishlists", "Customers", "wishlist"},
	{"reviews", "Customers", "review"},
	{"loyalty-accounts", "Customers", "loyalty account"},
	{"warehouses", "Inventory", "warehouse"},
	{"stock-levels", "Inventory", "stock level"},
	{"suppliers", "Inventory", "supplier"},
	{"purchase-orders", "Inventory", "purchase order"},
	{"stock-transfers", "Inventory", "stock transfer"},
	{"reports", "Reporting", "report"},
}

// buildFixtureSpec returns a deterministic OpenAPI 3.0 document. Every
// endpoint shares the Resource/ResourceList/ErrorResponse schemas through
// $ref, and every schema field carries a description, so the verbose JSON
// output is as heavy as a real-world spec. encoding/json sorts map keys, and
// resources are walked from a slice, so the output is byte-for-byte stable.
func buildFixtureSpec() []byte {
	type obj = map[string]interface{}

	prop := func(typ, desc string) obj { return obj{"type": typ, "description": desc} }
	ref := func(name string) obj { return obj{"$ref": "#/components/schemas/" + name} }

	schemas := obj{
		"AuditInfo": obj{
			"type":     "object",
			"required": []string{"createdAt"},
			"properties": obj{
				"createdAt": prop("string", "Timestamp (RFC 3339) of when the record was created in the system."),
				"createdBy": prop("string", "Identifier of the user or service account that created the record."),
				"updatedAt": prop("string", "Timestamp (RFC 3339) of the last modification of the record."),
				"updatedBy": prop("string", "Identifier of the user or service account that last modified the record."),
			},
		},
		"Pagination": obj{
			"type": "object",
			"properties": obj{
				"page":       prop("integer", "One-based page number of the returned result set."),
				"pageSize":   prop("integer", "Maximum number of items returned per page."),
				"totalItems": prop("integer", "Total number of items matching the query across all pages."),
			},
		},
		"ErrorResponse": obj{
			"type":     "object",
			"required": []string{"code", "message"},
			"properties": obj{
				"code":    prop("string", "Stable machine readable error code that clients can switch on."),
				"message": prop("string", "Human readable explanation of what went wrong and how to fix it."),
				"traceId": prop("string", "Correlation identifier to quote when contacting support."),
			},
		},
		"Resource": obj{
			"type":     "object",
			"required": []string{"id", "name"},
			"properties": obj{
				"id":          prop("string", "Unique identifier of the resource, assigned by the server."),
				"name":        prop("string", "Display name of the resource, shown in dashboards and receipts."),
				"description": prop("string", "Free text description of the resource for internal documentation."),
				"status":      prop("string", "Lifecycle status of the resource: draft, active or archived."),
				"audit":       ref("AuditInfo"),
			},
		},
		"ResourceList": obj{
			"type": "object",
			"properties": obj{
				"items":      obj{"type": "array", "description": "Items of the current page.", "items": ref("Resource")},
				"pagination": ref("Pagination"),
			},
		},
	}

	jsonContent := func(schema string) obj {
		return obj{"application/json": obj{"schema": ref(schema)}}
	}
	response := func(desc, schema string) obj {
		return obj{"description": desc, "content": jsonContent(schema)}
	}

	paths := obj{}
	for _, res := range fixtureResources {
		base := "/api/v1/" + res.Name
		item := base + "/{id}"

		idParam := []obj{{
			"name": "id", "in": "path", "required": true,
			"description": fmt.Sprintf("Unique identifier of the %s.", res.Noun),
			"schema":      obj{"type": "string"},
		}}
		listParams := []obj{
			{"name": "page", "in": "query", "description": "One-based page number to return.", "schema": obj{"type": "integer"}},
			{"name": "pageSize", "in": "query", "description": "Maximum number of items per page.", "schema": obj{"type": "integer"}},
		}

		op := func(id, summary, desc string, params []obj, withBody bool, okCode string, ok obj) obj {
			o := obj{
				"operationId": id,
				"summary":     summary,
				"description": desc,
				"tags":        []string{res.Tag},
				"responses": obj{
					okCode: ok,
					"400":  response("The request was malformed or failed validation.", "ErrorResponse"),
					"401":  response("Authentication is missing or the credentials are invalid.", "ErrorResponse"),
				},
			}
			if params != nil {
				o["parameters"] = params
			}
			if withBody {
				o["requestBody"] = obj{"required": true, "content": jsonContent("Resource")}
			}
			return o
		}

		paths[base] = obj{
			"get": op("list_"+res.Name, "List all "+res.Name,
				fmt.Sprintf("Returns a paginated list of every %s visible to the caller.", res.Noun),
				listParams, false, "200", response("A page of "+res.Name+".", "ResourceList")),
			"post": op("create_"+res.Name, "Create a "+res.Noun,
				fmt.Sprintf("Creates a new %s and returns the stored representation.", res.Noun),
				nil, true, "201", response("The newly created "+res.Noun+".", "Resource")),
		}
		paths[item] = obj{
			"get": op("get_"+res.Name, "Get a "+res.Noun+" by id",
				fmt.Sprintf("Returns a single %s, or 404 when it does not exist.", res.Noun),
				idParam, false, "200", response("The requested "+res.Noun+".", "Resource")),
			"put": op("update_"+res.Name, "Update a "+res.Noun,
				fmt.Sprintf("Replaces the stored %s with the supplied representation.", res.Noun),
				idParam, true, "200", response("The updated "+res.Noun+".", "Resource")),
			"delete": op("delete_"+res.Name, "Delete a "+res.Noun,
				fmt.Sprintf("Permanently removes the %s. This operation cannot be undone.", res.Noun),
				idParam, false, "204", obj{"description": "The " + res.Noun + " was deleted."}),
		}
	}

	var tags []obj
	seen := map[string]bool{}
	for _, res := range fixtureResources { // slice order => stable tag order
		if !seen[res.Tag] {
			seen[res.Tag] = true
			tags = append(tags, obj{"name": res.Tag, "description": res.Tag + " operations of the fixture API."})
		}
	}

	doc := obj{
		"openapi":    "3.0.3",
		"info":       obj{"title": "Fixture Shop API", "version": "1.0.0", "description": "Deterministic spec generated for integration tests."},
		"tags":       tags,
		"paths":      paths,
		"components": obj{"schemas": schemas},
	}
	out, err := json.Marshal(doc)
	if err != nil {
		panic(err) // static input; cannot fail
	}
	return out
}

// newSpecServer serves the fixture spec locally. Callers must defer Close.
func newSpecServer() *httptest.Server {
	spec := buildFixtureSpec()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(spec)
	}))
}

// newTestRegistry builds a Registry with default settings and an isolated
// disk cache, so tests never touch the developer's ~/.swagger-mcp/cache.
func newTestRegistry(cacheDir string) *Registry {
	cfg := config.Default()
	cfg.CacheDir = cacheDir
	cfg.FetchTimeout = 10 * time.Second
	return New(cfg)
}

// TestIntegration_FetchSpec tests fetching the fixture spec.
func TestIntegration_FetchSpec(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	srv := newSpecServer()
	defer srv.Close()

	r := newTestRegistry(t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := makeRequest(map[string]interface{}{"url": srv.URL})
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

	srv := newSpecServer()
	defer srv.Close()

	r := newTestRegistry(t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// No filters, no explicit format → should use TOON (config default) with limit 50
	req := makeRequest(map[string]interface{}{"url": srv.URL})
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

	srv := newSpecServer()
	defer srv.Close()

	r := newTestRegistry(t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// First get tags
	tagReq := makeRequest(map[string]interface{}{"url": srv.URL})
	tagResult, err := r.handleAnalyzeTags(ctx, tagReq)
	if err != nil {
		t.Fatalf("handleAnalyzeTags error: %v", err)
	}

	tagText := extractText(tagResult)
	t.Logf("analyze_tags output (%d chars):\n%s", len(tagText), truncateForLog(tagText, 2000))

	// Now list with a tag filter — "Auth" is guaranteed by buildFixtureSpec
	req := makeRequest(map[string]interface{}{
		"url": srv.URL,
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

	srv := newSpecServer()
	defer srv.Close()

	r := newTestRegistry(t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := makeRequest(map[string]interface{}{
		"url":    srv.URL,
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

	srv := newSpecServer()
	defer srv.Close()

	r := newTestRegistry(t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := makeRequest(map[string]interface{}{
		"url":   srv.URL,
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

	srv := newSpecServer()
	defer srv.Close()

	r := newTestRegistry(t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := makeRequest(map[string]interface{}{
		"url":   srv.URL,
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

	srv := newSpecServer()
	defer srv.Close()

	r := newTestRegistry(t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// "Old" style: JSON, no limit
	oldReq := makeRequest(map[string]interface{}{
		"url":    srv.URL,
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
		"url": srv.URL,
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
