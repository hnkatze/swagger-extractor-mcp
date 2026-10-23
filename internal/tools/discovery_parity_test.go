package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/hnkatze/swagger-extractor-mcp/internal/store"
)

// paritySpec builds a spec that exercises everything the discovery answers have
// to reproduce: declared tags with descriptions, an undeclared tag, an untagged
// endpoint, a deprecated endpoint, operation ids, descriptions, parameters, a
// request body with an array of $refs, response bodies and schemas that differ
// in how many endpoints reach them (including one nothing reaches).
func paritySpec(t *testing.T) []byte {
	t.Helper()
	type obj = map[string]any
	prop := func(typ string) obj { return obj{"type": typ} }
	ref := func(name string) obj { return obj{"$ref": "#/components/schemas/" + name} }
	jsonBody := func(schema obj) obj { return obj{"application/json": obj{"schema": schema}} }
	ok := func(schema obj) obj { return obj{"description": "ok", "content": jsonBody(schema)} }

	doc := obj{
		"openapi": "3.0.3",
		"info":    obj{"title": "Parity Shop", "version": "2.1.0"},
		"tags": []any{
			obj{"name": "orders", "description": "Order lifecycle: create, read and cancel."},
			obj{"name": "writes", "description": "Operations that change state."},
			obj{"name": "declared-unused", "description": "Declared but never used by an operation."},
		},
		"paths": obj{
			"/orders": obj{
				"get": obj{
					"operationId": "listOrders",
					"summary":     "List orders",
					"description": "Returns every order, newest first.",
					"tags":        []string{"orders"},
					"parameters": []any{
						obj{"name": "pageSize", "in": "query", "schema": prop("integer")},
						obj{"name": "status", "in": "query", "schema": prop("string")},
					},
					"responses": obj{"200": ok(obj{"type": "array", "items": ref("Order")})},
				},
				"post": obj{
					"operationId": "createOrder",
					"summary":     "Create an order",
					"tags":        []string{"orders", "writes"},
					"requestBody": obj{"required": true, "content": jsonBody(ref("NewOrder"))},
					"responses":   obj{"201": ok(ref("Order"))},
				},
			},
			"/orders/{orderId}": obj{
				"get": obj{
					"operationId": "getOrder",
					"summary":     "Get an order",
					"description": "Returns a single order or 404.",
					"tags":        []string{"orders"},
					"deprecated":  true,
					"parameters": []any{
						obj{"name": "orderId", "in": "path", "required": true, "schema": prop("string")},
					},
					"responses": obj{"200": ok(ref("Order"))},
				},
			},
			"/orders/{orderId}/cancel": obj{
				"delete": obj{
					"operationId": "cancelOrder",
					"summary":     "Cancel an order",
					"tags":        []string{"orders", "writes"},
					"responses":   obj{"204": obj{"description": "cancelled"}},
				},
			},
			"/health": obj{
				"get": obj{
					"operationId": "healthCheck",
					"summary":     "Liveness probe",
					"responses":   obj{"200": obj{"description": "ok"}},
				},
			},
		},
		"components": obj{"schemas": obj{
			"Order": obj{
				"type":        "object",
				"description": "A placed order.",
				"properties": obj{
					"id":       prop("string"),
					"total":    prop("number"),
					"customer": ref("Customer"),
				},
			},
			"NewOrder": obj{
				"type":        "object",
				"description": "Payload accepted when creating an order.",
				"properties": obj{
					"customerId": prop("string"),
					"lineItems":  obj{"type": "array", "items": ref("LineItem")},
				},
			},
			"Customer": obj{
				"type":       "object",
				"properties": obj{"email": prop("string"), "phoneNumber": prop("string")},
			},
			"LineItem": obj{
				"type":       "object",
				"properties": obj{"sku": prop("string"), "quantity": prop("integer")},
			},
			"Unreferenced": obj{
				"type":        "object",
				"description": "No operation reaches this model.",
				"properties":  obj{"x": prop("string")},
			},
		}},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("build parity fixture: %v", err)
	}
	return raw
}

// petstoreFixture is the repository fixture, read once per test. The path comes
// from this file's location because a test that switches to a project directory
// has no usable relative path back to the repository.
func petstoreFixture(t testing.TB) []byte {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate the test source file")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "testdata", "petstore.json"))
	if err != nil {
		t.Fatalf("read petstore fixture: %v", err)
	}
	return raw
}

// discoveryCase is one captured discovery answer: the tool, its arguments and
// the fixture group it runs against.
type discoveryCase struct {
	Name  string                 `json:"name"`
	Group string                 `json:"group"`
	Tool  string                 `json:"tool"`
	Args  map[string]interface{} `json:"args"`
}

// discoveryCases is the matrix the SQL answers must reproduce exactly.
func discoveryCases() []discoveryCase {
	cases := []discoveryCase{
		{"list_specs/all", "two", "list_specs", map[string]interface{}{}},

		{"list_endpoints/petstore/all", "two", "list_endpoints", map[string]interface{}{"spec": "petstore"}},
		{"list_endpoints/petstore/json", "two", "list_endpoints", map[string]interface{}{"spec": "petstore", "format": "json"}},
		{"list_endpoints/petstore/deprecated", "two", "list_endpoints", map[string]interface{}{"spec": "petstore", "deprecated": true}},
		{"list_endpoints/petstore/current-only", "two", "list_endpoints", map[string]interface{}{"spec": "petstore", "deprecated": false}},
		{"list_endpoints/orders/tag-ci", "two", "list_endpoints", map[string]interface{}{"spec": "orders", "tag": "ORDERS"}},
		{"list_endpoints/orders/tag-untagged", "two", "list_endpoints", map[string]interface{}{"spec": "orders", "tag": "untagged"}},
		{"list_endpoints/orders/method-lower", "two", "list_endpoints", map[string]interface{}{"spec": "orders", "method": "get"}},
		{"list_endpoints/orders/path-pattern", "two", "list_endpoints", map[string]interface{}{"spec": "orders", "path_pattern": "{orderId}"}},
		{"list_endpoints/orders/limit-offset", "two", "list_endpoints", map[string]interface{}{"spec": "orders", "limit": 2, "offset": 1}},
		{"list_endpoints/orders/offset-past-end", "two", "list_endpoints", map[string]interface{}{"spec": "orders", "offset": 99}},
		{"list_endpoints/orders/unlimited-json", "two", "list_endpoints", map[string]interface{}{"spec": "orders", "limit": 0, "format": "json"}},
		{"list_endpoints/orders/no-match", "two", "list_endpoints", map[string]interface{}{"spec": "orders", "tag": "nope"}},
		{"list_endpoints/all-specs", "two", "list_endpoints", map[string]interface{}{"all_specs": true}},
		{"list_endpoints/all-specs/page-json", "two", "list_endpoints", map[string]interface{}{"all_specs": true, "limit": 2, "offset": 2, "format": "json"}},

		{"search_spec/orders/path", "two", "search_spec", map[string]interface{}{"spec": "orders", "query": "orders"}},
		{"search_spec/orders/summary", "two", "search_spec", map[string]interface{}{"spec": "orders", "query": "List orders"}},
		{"search_spec/orders/operation-id", "two", "search_spec", map[string]interface{}{"spec": "orders", "query": "createOrder"}},
		{"search_spec/orders/description", "two", "search_spec", map[string]interface{}{"spec": "orders", "query": "newest first"}},
		{"search_spec/orders/parameter", "two", "search_spec", map[string]interface{}{"spec": "orders", "query": "pageSize"}},
		{"search_spec/orders/request-body", "two", "search_spec", map[string]interface{}{"spec": "orders", "query": "customerId"}},
		{"search_spec/orders/response-body", "two", "search_spec", map[string]interface{}{"spec": "orders", "query": "total"}},
		{"search_spec/orders/nested-model-only", "two", "search_spec", map[string]interface{}{"spec": "orders", "query": "phoneNumber"}},
		{"search_spec/orders/no-match", "two", "search_spec", map[string]interface{}{"spec": "orders", "query": "zzzz"}},
		{"search_spec/orders/json-limit", "two", "search_spec", map[string]interface{}{"spec": "orders", "query": "order", "format": "json", "limit": 3}},
		{"search_spec/petstore/operation-id", "two", "search_spec", map[string]interface{}{"spec": "petstore", "query": "listPets"}},
		{"search_spec/search/all-specs", "two", "search_spec", map[string]interface{}{"all_specs": true, "query": "order"}},

		{"list_schemas/orders/all", "two", "list_schemas", map[string]interface{}{"spec": "orders"}},
		{"list_schemas/orders/filter", "two", "list_schemas", map[string]interface{}{"spec": "orders", "filter": "order"}},
		{"list_schemas/orders/page", "two", "list_schemas", map[string]interface{}{"spec": "orders", "limit": 2, "offset": 2}},
		{"list_schemas/orders/json", "two", "list_schemas", map[string]interface{}{"spec": "orders", "format": "json"}},
		{"list_schemas/orders/no-match", "two", "list_schemas", map[string]interface{}{"spec": "orders", "filter": "zzzz"}},
		{"list_schemas/petstore/all", "two", "list_schemas", map[string]interface{}{"spec": "petstore"}},
		{"list_schemas/all-specs", "two", "list_schemas", map[string]interface{}{"all_specs": true, "filter": "order"}},

		{"analyze_tags/orders", "two", "analyze_tags", map[string]interface{}{"spec": "orders"}},
		{"analyze_tags/orders/json", "two", "analyze_tags", map[string]interface{}{"spec": "orders", "format": "json"}},
		{"analyze_tags/petstore", "two", "analyze_tags", map[string]interface{}{"spec": "petstore"}},
		{"analyze_tags/all-specs", "two", "analyze_tags", map[string]interface{}{"all_specs": true}},

		{"list_endpoints/fixture/default", "large", "list_endpoints", map[string]interface{}{}},
		{"list_endpoints/fixture/tag", "large", "list_endpoints", map[string]interface{}{"tag": "Auth"}},
		{"list_endpoints/fixture/path-pattern", "large", "list_endpoints", map[string]interface{}{"path_pattern": "/api/v1/products"}},
		{"list_endpoints/fixture/page-json", "large", "list_endpoints", map[string]interface{}{"limit": 5, "offset": 3, "format": "json"}},
		{"list_endpoints/fixture/deprecated", "large", "list_endpoints", map[string]interface{}{"deprecated": true}},
		{"list_endpoints/fixture/all-specs", "large", "list_endpoints", map[string]interface{}{"all_specs": true, "limit": 10}},
		{"search_spec/fixture/summary", "large", "search_spec", map[string]interface{}{"query": "product", "limit": 10}},
		{"search_spec/fixture/parameter", "large", "search_spec", map[string]interface{}{"query": "pageSize", "limit": 10}},
		{"list_schemas/fixture/all", "large", "list_schemas", map[string]interface{}{"limit": 10}},
		{"list_schemas/fixture/filter", "large", "list_schemas", map[string]interface{}{"filter": "resource"}},
		{"analyze_tags/fixture/all", "large", "analyze_tags", map[string]interface{}{}},
	}
	return cases
}

// indexDiscoveryGroup indexes the fixtures one group of cases runs against.
func indexDiscoveryGroup(t *testing.T, group string) {
	t.Helper()
	switch group {
	case "two":
		indexSpec(t, "orders", paritySpec(t))
		indexSpec(t, "petstore", petstoreFixture(t))
	case "large":
		indexSpec(t, "fixture", buildFixtureSpec())
	default:
		t.Fatalf("unknown fixture group %q", group)
	}
}

// callDiscovery runs one discovery tool and returns its text, failing on a tool
// error: every case in the matrix is supposed to answer.
func callDiscovery(t *testing.T, r *Registry, tool string, args map[string]interface{}) string {
	t.Helper()
	ctx := context.Background()
	var (
		res *mcp.CallToolResult
		err error
	)
	switch tool {
	case "list_specs":
		res, err = r.handleListSpecs(ctx, makeRequest(args))
	case "list_endpoints":
		res, err = r.handleListEndpoints(ctx, makeRequest(args))
	case "search_spec":
		res, err = r.handleSearchSpec(ctx, makeRequest(args))
	case "list_schemas":
		res, err = r.handleListSchemas(ctx, makeRequest(args))
	case "analyze_tags":
		res, err = r.handleAnalyzeTags(ctx, makeRequest(args))
	default:
		t.Fatalf("unknown discovery tool %q", tool)
	}
	if err != nil {
		t.Fatalf("%s: handler error: %v", tool, err)
	}
	if res.IsError {
		t.Fatalf("%s: tool error: %s", tool, extractText(res))
	}
	return extractText(res)
}

// discoveryGoldensPath holds the answers captured from the document-parsed
// implementation before T7 moved discovery onto the catalog.
const discoveryGoldensPath = "testdata/discovery_parity.golden.json"

// TestDiscoveryParity pins S9: the SQL answers must equal the answers the
// document-parsed implementation gave. The goldens were captured from that
// implementation (see TestDiscoveryParityCapture), so they cannot encode a bug
// introduced by the SQL rewrite.
func TestDiscoveryParity(t *testing.T) {
	capture := os.Getenv("SWA_CAPTURE_PARITY") == "1"
	goldens := map[string]string{}
	if !capture {
		raw, err := os.ReadFile(discoveryGoldensPath)
		if err != nil {
			t.Fatalf("read captured answers (%s): %v", discoveryGoldensPath, err)
		}
		if err := json.Unmarshal(raw, &goldens); err != nil {
			t.Fatalf("decode captured answers: %v", err)
		}
	}
	captured := map[string]string{}

	for _, group := range []string{"two", "large"} {
		t.Run(group, func(t *testing.T) {
			projectDir(t)
			indexDiscoveryGroup(t, group)
			r := newRegistry(t)
			for _, c := range discoveryCases() {
				if c.Group != group {
					continue
				}
				c := c
				t.Run(c.Name, func(t *testing.T) {
					got := callDiscovery(t, r, c.Tool, c.Args)
					if capture {
						captured[c.Name] = got
						return
					}
					want, ok := goldens[c.Name]
					if !ok {
						t.Fatalf("no captured answer for %q: capture the goldens from the document-parsed implementation first", c.Name)
					}
					if got != want {
						t.Errorf("%s answered differently than the document-parsed implementation:\n--- captured\n%s\n--- now\n%s", c.Name, want, got)
					}
				})
			}
		})
	}

	if !capture {
		return
	}
	if len(captured) != len(discoveryCases()) {
		t.Fatalf("captured %d of %d answers", len(captured), len(discoveryCases()))
	}
	raw, err := json.MarshalIndent(captured, "", "  ")
	if err != nil {
		t.Fatalf("encode captured answers: %v", err)
	}
	if err := os.WriteFile(discoveryGoldensPath, append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write captured answers: %v", err)
	}
}

// execStoreSQL runs one statement against the project's store file. Discovering
// tools must answer from that file, so the tests damage or mark it directly.
func execStoreSQL(t *testing.T, statement string, args ...any) {
	t.Helper()
	dir, err := store.DirFor(".")
	if err != nil {
		t.Fatalf("locate store: %v", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, store.DBFileName))
	if err != nil {
		t.Fatalf("open store for writing: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(statement, args...); err != nil {
		t.Fatalf("run %q: %v", statement, err)
	}
}

// TestDiscovery_AnswersWithACorruptStoredDocument is S9's proof: a stored
// document that cannot be read must degrade the detail tools only. Every
// discovery answer stays byte-identical, because none of them parses the blob.
func TestDiscovery_AnswersWithACorruptStoredDocument(t *testing.T) {
	projectDir(t)
	indexSpec(t, "orders", paritySpec(t))
	indexSpec(t, "petstore", petstoreFixture(t))
	r := newRegistry(t)

	cases := []discoveryCase{
		{"list_specs", "two", "list_specs", map[string]interface{}{}},
		{"list_endpoints", "two", "list_endpoints", map[string]interface{}{"spec": "petstore"}},
		{"list_endpoints/deprecated", "two", "list_endpoints", map[string]interface{}{"spec": "petstore", "deprecated": true}},
		{"list_endpoints/all-specs", "two", "list_endpoints", map[string]interface{}{"all_specs": true}},
		{"search_spec", "two", "search_spec", map[string]interface{}{"spec": "petstore", "query": "listPets"}},
		{"search_spec/all-specs", "two", "search_spec", map[string]interface{}{"all_specs": true, "query": "order"}},
		{"list_schemas", "two", "list_schemas", map[string]interface{}{"spec": "petstore"}},
		{"list_schemas/all-specs", "two", "list_schemas", map[string]interface{}{"all_specs": true}},
		{"analyze_tags", "two", "analyze_tags", map[string]interface{}{"spec": "petstore"}},
		{"analyze_tags/all-specs", "two", "analyze_tags", map[string]interface{}{"all_specs": true}},
	}

	before := make(map[string]string, len(cases))
	for _, c := range cases {
		before[c.Name] = callDiscovery(t, r, c.Tool, c.Args)
	}

	// The blob is garbage; the metadata and the catalog are untouched, which is
	// what a truncated file, a bad sync or a corrupt checkout looks like.
	execStoreSQL(t, "UPDATE specs SET document = ? WHERE name = 'petstore'", []byte("\x1f\x8bnot a gzip stream"))

	// A fresh session: nothing is cached, so every answer below has to come out
	// of the store as it is now.
	r = newRegistry(t)
	for _, c := range cases {
		if got := callDiscovery(t, r, c.Tool, c.Args); got != before[c.Name] {
			t.Errorf("%s changed after the stored document was corrupted:\n--- before\n%s\n--- after\n%s",
				c.Name, before[c.Name], got)
		}
	}

	// Only the detail tools may fail, and they must say what to do about it.
	res, err := r.handleGetEndpoint(context.Background(), makeRequest(map[string]interface{}{
		"spec": "petstore", "method": "GET", "path": "/pets",
	}))
	if err != nil {
		t.Fatalf("handleGetEndpoint error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("get_endpoint on a corrupt stored document must fail, got %q", extractText(res))
	}
	text := extractText(res)
	for _, want := range []string{"petstore", "swagger-mcp index"} {
		if !strings.Contains(text, want) {
			t.Errorf("detail error %q should mention %q", text, want)
		}
	}

	// The other spec's document is intact, so its detail tools still work.
	res, err = r.handleGetEndpoint(context.Background(), makeRequest(map[string]interface{}{
		"spec": "orders", "method": "GET", "path": "/orders",
	}))
	if err != nil || res.IsError {
		t.Fatalf("get_endpoint on the healthy spec failed: %v %q", err, extractText(res))
	}
}

// TestDiscovery_ReportsAnIncompleteCatalogWithTheIndexCommand covers the store
// state migration leaves behind when a stored document cannot be parsed: the
// spec has no catalog, so discovery must name the command that rebuilds it
// instead of answering as if it had no endpoints.
func TestDiscovery_ReportsAnIncompleteCatalogWithTheIndexCommand(t *testing.T) {
	projectDir(t)
	indexSpec(t, "petstore", petstoreFixture(t))
	execStoreSQL(t, "UPDATE specs SET catalog_sha256 = '' WHERE name = 'petstore'")
	r := newRegistry(t)

	cases := []discoveryCase{
		{"list_endpoints", "two", "list_endpoints", map[string]interface{}{"spec": "petstore"}},
		{"search_spec", "two", "search_spec", map[string]interface{}{"spec": "petstore", "query": "pets"}},
		{"list_schemas", "two", "list_schemas", map[string]interface{}{"spec": "petstore"}},
		{"analyze_tags", "two", "analyze_tags", map[string]interface{}{"spec": "petstore"}},
	}
	for _, c := range cases {
		t.Run(c.Tool, func(t *testing.T) {
			got := callDiscoveryError(t, r, c.Tool, c.Args)
			for _, want := range []string{"swagger-mcp index", "petstore"} {
				if !strings.Contains(got, want) {
					t.Errorf("error %q should mention %q", got, want)
				}
			}
		})
	}

	// Detail tools read the document, which is intact, so they keep working.
	if res, err := r.handleGetEndpoint(context.Background(), makeRequest(map[string]interface{}{
		"spec": "petstore", "method": "GET", "path": "/pets",
	})); err != nil || res.IsError {
		t.Fatalf("get_endpoint should still answer from the document: %v %q", err, extractText(res))
	}
}

// callDiscoveryError runs one discovery tool and returns the error text, failing
// when the tool answers instead.
func callDiscoveryError(t *testing.T, r *Registry, tool string, args map[string]interface{}) string {
	t.Helper()
	ctx := context.Background()
	var (
		res *mcp.CallToolResult
		err error
	)
	switch tool {
	case "list_endpoints":
		res, err = r.handleListEndpoints(ctx, makeRequest(args))
	case "search_spec":
		res, err = r.handleSearchSpec(ctx, makeRequest(args))
	case "list_schemas":
		res, err = r.handleListSchemas(ctx, makeRequest(args))
	case "analyze_tags":
		res, err = r.handleAnalyzeTags(ctx, makeRequest(args))
	default:
		t.Fatalf("unknown discovery tool %q", tool)
	}
	if err != nil {
		t.Fatalf("%s: handler error: %v", tool, err)
	}
	if !res.IsError {
		t.Fatalf("%s answered instead of reporting the incomplete catalog: %q", tool, extractText(res))
	}
	return extractText(res)
}
