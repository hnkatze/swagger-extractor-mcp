package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/hnkatze/swagger-extractor-mcp/internal/store"
)

// ---------------------------------------------------------------------------
// T3 discovery (S5): list_specs and the all_specs sweep
// ---------------------------------------------------------------------------

// specWithSchema builds a deterministic spec whose single GET references one
// component schema, so a sweep test can tell schemas of two specs apart and
// check both field and usage counts.
func specWithSchema(t *testing.T, title, path, schemaName string) []byte {
	t.Helper()
	ref := map[string]any{"$ref": "#/components/schemas/" + schemaName}
	doc := map[string]any{
		"openapi": "3.0.3",
		"info":    map[string]any{"title": title, "version": "1.0.0"},
		"paths": map[string]any{path: map[string]any{
			"get": map[string]any{
				"summary": title + " " + path,
				"tags":    []string{title},
				"responses": map[string]any{"200": map[string]any{
					"description": "ok",
					"content":     map[string]any{"application/json": map[string]any{"schema": ref}},
				}},
			},
		}},
		"components": map[string]any{"schemas": map[string]any{
			schemaName: map[string]any{
				"type":       "object",
				"properties": map[string]any{"id": map[string]any{"type": "string"}},
			},
		}},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("build fixture: %v", err)
	}
	return raw
}

// list_specs is the discovery entry point: it must describe every indexed spec
// from the store metadata alone, with no spec argument and no document load.
func TestListSpecs_ListsEveryIndexedSpec(t *testing.T) {
	projectDir(t)
	indexSpec(t, "orders", tinySpec(t, "Orders", "/orders"))
	indexSpec(t, "petstore", tinySpec(t, "Petstore", "/pets", "/pets/{id}"))
	r := newRegistry(t)

	res, err := r.handleListSpecs(context.Background(), makeRequest(map[string]interface{}{}))
	if err != nil {
		t.Fatalf("handleListSpecs error: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_specs must answer from the store, got %q", extractText(res))
	}

	text := extractText(res)
	for _, want := range []string{
		"orders", "Orders v1.0.0", "fixture://orders", "endpoints 1, schemas 0",
		"petstore", "Petstore v1.0.0", "fixture://petstore", "endpoints 2, schemas 0",
		"2025-01-02T03:04:05Z",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("list_specs should report %q, got %q", want, text)
		}
	}
}

func TestListSpecs_WithoutStoreNamesIndexCommand(t *testing.T) {
	ctx := context.Background()

	t.Run("no store directory", func(t *testing.T) {
		projectDir(t)
		res, err := newRegistry(t).handleListSpecs(ctx, makeRequest(map[string]interface{}{}))
		assertNamesIndexCommand(t, res, err)
	})

	t.Run("empty store", func(t *testing.T) {
		projectDir(t)
		openEmptyStore(t)
		res, err := newRegistry(t).handleListSpecs(ctx, makeRequest(map[string]interface{}{}))
		assertNamesIndexCommand(t, res, err)
	})
}

// Without all_specs the S4a rules are unchanged: several indexed specs still
// refuse to guess. Opting in sweeps the store and prefixes every result line
// with its spec name.
func TestListEndpoints_AllSpecsSweepsAndPrefixesNames(t *testing.T) {
	projectDir(t)
	indexSpec(t, "orders", tinySpec(t, "Orders", "/orders"))
	indexSpec(t, "petstore", tinySpec(t, "Petstore", "/pets"))
	r := newRegistry(t)
	ctx := context.Background()

	res, err := r.handleListEndpoints(ctx, makeRequest(map[string]interface{}{}))
	if err != nil {
		t.Fatalf("handleListEndpoints error: %v", err)
	}
	if !res.IsError {
		t.Error("without all_specs, several indexed specs must still be an error")
	}

	res, err = r.handleListEndpoints(ctx, makeRequest(map[string]interface{}{"all_specs": true}))
	if err != nil {
		t.Fatalf("handleListEndpoints(all_specs) error: %v", err)
	}
	if res.IsError {
		t.Fatalf("all_specs must sweep the store, got %q", extractText(res))
	}
	text := extractText(res)
	for _, want := range []string{"orders: GET /orders", "petstore: GET /pets", "2 specs"} {
		if !strings.Contains(text, want) {
			t.Errorf("swept output should contain %q, got %q", want, text)
		}
	}
}

func TestSearchSpec_AllSpecsSweepsAndPrefixesNames(t *testing.T) {
	projectDir(t)
	indexSpec(t, "orders", tinySpec(t, "Orders", "/orders"))
	indexSpec(t, "petstore", tinySpec(t, "Petstore", "/pets"))
	r := newRegistry(t)

	// "e" occurs in both paths, so both specs contribute a prefixed line.
	res, err := r.handleSearchSpec(context.Background(), makeRequest(map[string]interface{}{
		"query":     "e",
		"all_specs": true,
	}))
	if err != nil {
		t.Fatalf("handleSearchSpec error: %v", err)
	}
	if res.IsError {
		t.Fatalf("all_specs search must sweep the store, got %q", extractText(res))
	}
	text := extractText(res)
	for _, want := range []string{"orders: GET /orders", "petstore: GET /pets"} {
		if !strings.Contains(text, want) {
			t.Errorf("swept search should contain %q, got %q", want, text)
		}
	}
}

func TestAnalyzeTags_AllSpecsGroupsBySpec(t *testing.T) {
	projectDir(t)
	indexSpec(t, "orders", tinySpec(t, "Orders", "/orders"))
	indexSpec(t, "petstore", tinySpec(t, "Petstore", "/pets", "/pets/{id}"))
	r := newRegistry(t)

	res, err := r.handleAnalyzeTags(context.Background(), makeRequest(map[string]interface{}{"all_specs": true}))
	if err != nil {
		t.Fatalf("handleAnalyzeTags error: %v", err)
	}
	if res.IsError {
		t.Fatalf("all_specs analyze_tags must sweep the store, got %q", extractText(res))
	}
	text := extractText(res)
	for _, want := range []string{"# tags across 2 specs", "orders:", "petstore:", "  Orders", "  Petstore", "endpoints: 2"} {
		if !strings.Contains(text, want) {
			t.Errorf("swept tags should contain %q, got %q", want, text)
		}
	}
}

func TestListSchemas_AllSpecsSweepsAndPrefixesNames(t *testing.T) {
	projectDir(t)
	indexSpec(t, "orders", specWithSchema(t, "Orders", "/orders", "Order"))
	indexSpec(t, "petstore", specWithSchema(t, "Petstore", "/pets", "Pet"))
	r := newRegistry(t)

	res, err := r.handleListSchemas(context.Background(), makeRequest(map[string]interface{}{"all_specs": true}))
	if err != nil {
		t.Fatalf("handleListSchemas error: %v", err)
	}
	if res.IsError {
		t.Fatalf("all_specs list_schemas must sweep the store, got %q", extractText(res))
	}
	text := extractText(res)
	for _, want := range []string{
		"# 2 schemas across 2 specs",
		"orders: Order (1 fields, used by 1)",
		"petstore: Pet (1 fields, used by 1)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("swept schemas should contain %q, got %q", want, text)
		}
	}
}

// The sweep is an explicit opt-in declared on the four discovery tools, and
// list_specs is the one tool that must NOT take a spec argument.
func TestDiscoveryTools_DeclareScopingParams(t *testing.T) {
	for _, tool := range []mcp.Tool{listEndpointsTool(), searchSpecTool(), analyzeTagsTool(), listSchemasTool()} {
		if got := paramSchemaType(t, tool, "all_specs"); got != "boolean" {
			t.Errorf("%s all_specs declared type = %q, want boolean", tool.Name, got)
		}
	}
	if _, ok := listSpecsTool().InputSchema.Properties["spec"]; ok {
		t.Error("list_specs must not take a spec argument: it lists them all")
	}
}

// ---------------------------------------------------------------------------
// T4 staleness (S6): spec_status reports age and an explicit verdict
// ---------------------------------------------------------------------------

func TestSpecStatus_FreshCopyIsNotStale(t *testing.T) {
	projectDir(t)
	indexSpecAt(t, "petstore", tinySpec(t, "Petstore", "/pets"), time.Now().UTC().Add(-time.Hour))
	r := newRegistry(t)

	res, err := r.handleSpecStatus(context.Background(), makeRequest(map[string]interface{}{}))
	if err != nil || res.IsError {
		t.Fatalf("spec_status failed: %v %q", err, extractText(res))
	}
	text := extractText(res)
	for _, want := range []string{`"stale": false`, `"age": "1h"`, `"fetched_at"`, `"etag"`} {
		if !strings.Contains(text, want) {
			t.Errorf("fresh status should report %q, got %q", want, text)
		}
	}
}

func TestSpecStatus_OldCopyIsStaleAndNamesRefreshCommand(t *testing.T) {
	projectDir(t)
	indexSpecAt(t, "petstore", tinySpec(t, "Petstore", "/pets"), time.Now().UTC().Add(-8*24*time.Hour))
	r := newRegistry(t)

	res, err := r.handleSpecStatus(context.Background(), makeRequest(map[string]interface{}{}))
	if err != nil || res.IsError {
		t.Fatalf("spec_status failed: %v %q", err, extractText(res))
	}
	text := extractText(res)
	for _, want := range []string{`"stale": true`, `"age": "8d"`, "swagger-mcp index --refresh fixture://petstore"} {
		if !strings.Contains(text, want) {
			t.Errorf("stale status should report %q, got %q", want, text)
		}
	}
}

// ---------------------------------------------------------------------------
// diff_endpoints correction: a diff always needs two indexed specs
// ---------------------------------------------------------------------------

func TestDiffEndpoints_RequiresTwoIndexedSpecs(t *testing.T) {
	ctx := context.Background()

	t.Run("one indexed spec", func(t *testing.T) {
		projectDir(t)
		indexSpec(t, "petstore", tinySpec(t, "Petstore", "/pets"))
		r := newRegistry(t)

		res, err := r.handleDiffEndpoints(ctx, makeRequest(map[string]interface{}{}))
		if err != nil {
			t.Fatalf("handleDiffEndpoints error: %v", err)
		}
		if !res.IsError {
			t.Fatalf("a diff against itself is never a real answer, got %q", extractText(res))
		}
		text := extractText(res)
		for _, want := range []string{"two indexed specs", "petstore"} {
			if !strings.Contains(text, want) {
				t.Errorf("error %q should mention %q", text, want)
			}
		}
	})

	t.Run("two indexed specs without names", func(t *testing.T) {
		projectDir(t)
		indexSpec(t, "orders", tinySpec(t, "Orders", "/orders"))
		indexSpec(t, "petstore", tinySpec(t, "Petstore", "/pets"))
		r := newRegistry(t)

		res, err := r.handleDiffEndpoints(ctx, makeRequest(map[string]interface{}{}))
		if err != nil {
			t.Fatalf("handleDiffEndpoints error: %v", err)
		}
		if !res.IsError {
			t.Fatal("with two specs and no names the tool must ask for names instead of guessing")
		}
		text := extractText(res)
		for _, want := range []string{"spec_old", "spec_new", "orders", "petstore"} {
			if !strings.Contains(text, want) {
				t.Errorf("error %q should mention %q", text, want)
			}
		}
	})

	t.Run("two indexed specs with both names", func(t *testing.T) {
		projectDir(t)
		indexSpec(t, "orders", tinySpec(t, "Orders", "/orders"))
		indexSpec(t, "petstore", tinySpec(t, "Petstore", "/pets"))
		r := newRegistry(t)

		res, err := r.handleDiffEndpoints(ctx, makeRequest(map[string]interface{}{
			"spec_old": "orders",
			"spec_new": "petstore",
		}))
		if err != nil {
			t.Fatalf("handleDiffEndpoints error: %v", err)
		}
		if res.IsError {
			t.Fatalf("two explicit names must compare, got %q", extractText(res))
		}
		text := extractText(res)
		for _, want := range []string{"added:", "/pets", "removed:", "/orders"} {
			if !strings.Contains(text, want) {
				t.Errorf("diff should report %q, got %q", want, text)
			}
		}
	})
}

func TestDiffEndpoints_UnknownNameListsIndexedSpecs(t *testing.T) {
	projectDir(t)
	indexSpec(t, "orders", tinySpec(t, "Orders", "/orders"))
	indexSpec(t, "petstore", tinySpec(t, "Petstore", "/pets"))
	r := newRegistry(t)

	res, err := r.handleDiffEndpoints(context.Background(), makeRequest(map[string]interface{}{
		"spec_old": "zwagger",
		"spec_new": "petstore",
	}))
	if err != nil {
		t.Fatalf("handleDiffEndpoints error: %v", err)
	}
	if !res.IsError {
		t.Fatal("an unknown spec name must be an error")
	}
	text := extractText(res)
	for _, want := range []string{"zwagger", "orders", "petstore"} {
		if !strings.Contains(text, want) {
			t.Errorf("error %q should mention %q", text, want)
		}
	}
}

// A sweep header must read like a sentence: one result is "1 endpoint", never
// "1 endpoints".
func TestSweepHeader_PluralisesTheCount(t *testing.T) {
	projectDir(t)
	indexSpec(t, "petstore", tinySpec(t, "Petstore", "/pets"))
	r := newRegistry(t)

	res, err := r.handleListEndpoints(context.Background(), makeRequest(map[string]interface{}{"all_specs": true}))
	if err != nil || res.IsError {
		t.Fatalf("list_endpoints(all_specs) failed: %v %q", err, extractText(res))
	}
	text := extractText(res)
	if !strings.Contains(text, "# 1 endpoint across 1 spec") {
		t.Errorf("sweep header should read %q, got %q", "# 1 endpoint across 1 spec", text)
	}
	if strings.Contains(text, "1 endpoints") {
		t.Errorf("sweep header pluralised a single result, got %q", text)
	}
}

// The paged sweep header still reports the range and the next offset: the
// pluralisation fix must not change the other header shapes.
func TestSweepHeader_KeepsThePagedShape(t *testing.T) {
	projectDir(t)
	indexSpec(t, "orders", tinySpec(t, "Orders", "/orders", "/order-items"))
	r := newRegistry(t)

	res, err := r.handleListEndpoints(context.Background(), makeRequest(map[string]interface{}{
		"all_specs": true,
		"limit":     1,
	}))
	if err != nil || res.IsError {
		t.Fatalf("paged sweep failed: %v %q", err, extractText(res))
	}
	text := extractText(res)
	want := "# showing 1 of 2 endpoints across 1 spec — pass spec=<name> to narrow, or page with limit/offset"
	if !strings.Contains(text, want) {
		t.Errorf("paged header should read %q, got %q", want, text)
	}
}

// A source with a space still gets quoted, and a stored name without one stays
// bare: the refresh command is a shell command, not a display string.
func TestRefreshCommand_QuotesOnlyWhatNeedsIt(t *testing.T) {
	derived := store.SpecMeta{Name: "petstore", Source: "/specs/petstore.json"}
	if got, want := refreshCommand(derived), "swagger-mcp index --refresh /specs/petstore.json"; got != want {
		t.Errorf("refreshCommand = %q, want %q", got, want)
	}
	spaced := store.SpecMeta{Name: "pet store", Source: "/my specs/api.json"}
	want := `swagger-mcp index --refresh --name "pet store" "/my specs/api.json"`
	if got := refreshCommand(spaced); got != want {
		t.Errorf("refreshCommand = %q, want %q", got, want)
	}
}
