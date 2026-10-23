package tools

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/hnkatze/swagger-extractor-mcp/internal/config"
	"github.com/hnkatze/swagger-extractor-mcp/internal/store"
)

// S13: index_spec is the explicit write path for a client without a shell. It
// has to do exactly what `swagger-mcp index` does — same read, same parse and
// validation rules, same name derivation, same collision refusal, same store —
// and answer with a summary a caller can act on.

// indexSpecBytes builds a spec whose title identifies it and whose endpoint count
// is len(paths); it carries one component schema so the summary has a real
// schema count.
func indexSpecBytes(t *testing.T, title string, paths ...string) []byte {
	t.Helper()
	ops := map[string]any{}
	for _, p := range paths {
		ops[p] = map[string]any{
			"get": map[string]any{
				"summary": title + " " + p,
				"tags":    []string{title},
				"responses": map[string]any{"200": map[string]any{
					"description": "ok",
					"content": map[string]any{"application/json": map[string]any{
						"schema": map[string]any{"$ref": "#/components/schemas/Thing"},
					}},
				}},
			},
		}
	}
	doc := map[string]any{
		"openapi": "3.0.3",
		"info":    map[string]any{"title": title, "version": "1.0.0"},
		"paths":   ops,
		"components": map[string]any{"schemas": map[string]any{
			"Thing": map[string]any{
				"type":       "object",
				"properties": map[string]any{"id": map[string]any{"type": "string"}},
			},
		}},
	}
	for p, op := range ops {
		if strings.Contains(p, "{id}") {
			op.(map[string]any)["parameters"] = []map[string]any{{
				"name": "id", "in": "path", "required": true,
				"schema": map[string]any{"type": "string"},
			}}
		}
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("build spec fixture: %v", err)
	}
	return raw
}

// writeIndexSpecFile writes a spec source at path and returns the path.
func writeIndexSpecFile(t *testing.T, path, title string, paths ...string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, indexSpecBytes(t, title, paths...), 0o644); err != nil {
		t.Fatalf("write spec %s: %v", path, err)
	}
	return path
}

// callIndexSpec invokes the handler and fails on a transport-level error.
func callIndexSpec(t *testing.T, r *Registry, args map[string]interface{}) *mcp.CallToolResult {
	t.Helper()
	res, err := r.handleIndexSpec(context.Background(), makeRequest(args))
	if err != nil {
		t.Fatalf("handleIndexSpec(%v): %v", args, err)
	}
	return res
}

// storedMetas reads the project's store metadata after a write.
func storedMetas(t *testing.T) []store.SpecMeta {
	t.Helper()
	dir, err := store.DirFor(".")
	if err != nil {
		t.Fatalf("locate store: %v", err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	metas, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return metas
}

func TestIndexSpecToolParameters(t *testing.T) {
	tool := indexSpecTool()
	props := tool.InputSchema.Properties
	if len(props) != 3 {
		t.Fatalf("index_spec exposes %d parameters, want source, name and refresh", len(props))
	}
	required := map[string]bool{}
	for _, name := range tool.InputSchema.Required {
		required[name] = true
	}
	for _, name := range []string{"source", "name", "refresh"} {
		if _, ok := props[name]; !ok {
			t.Errorf("index_spec does not declare %q", name)
		}
	}
	if !required["source"] {
		t.Error("index_spec must require source")
	}
	for _, name := range []string{"name", "refresh"} {
		if required[name] {
			t.Errorf("index_spec must not require %q", name)
		}
	}
}

func TestIndexSpecRequiresASource(t *testing.T) {
	projectDir(t)
	res := callIndexSpec(t, newRegistry(t), map[string]interface{}{})
	if !res.IsError {
		t.Fatalf("index_spec without a source succeeded: %s", extractText(res))
	}
	if text := extractText(res); !strings.Contains(text, "source") {
		t.Errorf("error %q does not name the missing parameter", text)
	}
}

// TestIndexSpecAfterAQueryReportedNoStore covers the real first session: the
// query fails because no store exists yet, and the next call must succeed once
// index_spec has created it.
func TestIndexSpecAfterAQueryReportedNoStore(t *testing.T) {
	dir := projectDir(t)
	source := writeIndexSpecFile(t, filepath.Join(dir, "petstore.json"), "Petstore", "/pets")
	r := newRegistry(t)

	first := mustCall(t, func() (*mcp.CallToolResult, error) {
		return r.handleListEndpoints(context.Background(), makeRequest(map[string]interface{}{}))
	})
	if !first.IsError || !strings.Contains(extractText(first), "[NO_STORE]") {
		t.Fatalf("a query before indexing should report NO_STORE, got %q", extractText(first))
	}

	if res := callIndexSpec(t, r, map[string]interface{}{"source": source}); res.IsError {
		t.Fatalf("index_spec after NO_STORE failed: %s", extractText(res))
	}
	if text := extractText(mustCall(t, func() (*mcp.CallToolResult, error) {
		return r.handleListEndpoints(context.Background(), makeRequest(map[string]interface{}{}))
	})); !strings.Contains(text, "GET /pets") {
		t.Errorf("the query still does not see the store index_spec created: %q", text)
	}
}

func TestIndexSpecIndexesALocalFileAndQueriesAnswerFromIt(t *testing.T) {
	dir := projectDir(t)
	source := writeIndexSpecFile(t, filepath.Join(dir, "petstore.json"), "Petstore", "/pets", "/pets/{id}")
	r := newRegistry(t)

	res := callIndexSpec(t, r, map[string]interface{}{"source": source})
	if res.IsError {
		t.Fatalf("index_spec failed: %s", extractText(res))
	}
	for _, want := range []string{
		"indexed petstore:", "Petstore", "1.0.0", "(2 endpoints, 1 schemas)",
		storeFilePath(t),
	} {
		if !strings.Contains(extractText(res), want) {
			t.Errorf("summary %q does not report %q", extractText(res), want)
		}
	}

	// The write created the project store, like the CLI does.
	if _, err := os.Stat(storeFilePath(t)); err != nil {
		t.Fatalf("index_spec did not create the store: %v", err)
	}

	metas := storedMetas(t)
	if len(metas) != 1 || metas[0].Name != "petstore" || metas[0].Source != source {
		t.Fatalf("stored rows = %+v, want one petstore row from %s", metas, source)
	}

	// A single spec needs no `spec` argument on any query.
	if text := extractText(mustCall(t, func() (*mcp.CallToolResult, error) {
		return r.handleListSpecs(context.Background(), makeRequest(map[string]interface{}{}))
	})); !strings.Contains(text, "petstore") {
		t.Errorf("list_specs does not report the indexed spec: %q", text)
	}
	if text := extractText(mustCall(t, func() (*mcp.CallToolResult, error) {
		return r.handleListEndpoints(context.Background(), makeRequest(map[string]interface{}{}))
	})); !strings.Contains(text, "GET /pets/{id}") {
		t.Errorf("list_endpoints does not answer from the new store: %q", text)
	}
}

// mustCall turns the (result, error) shape of a handler into a result, failing
// the test on a transport error.
func mustCall(t *testing.T, call func() (*mcp.CallToolResult, error)) *mcp.CallToolResult {
	t.Helper()
	res, err := call()
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return res
}

func TestIndexSpecNameOverride(t *testing.T) {
	dir := projectDir(t)
	source := writeIndexSpecFile(t, filepath.Join(dir, "api.json"), "Billing", "/billing")
	r := newRegistry(t)

	res := callIndexSpec(t, r, map[string]interface{}{"source": source, "name": "billing-v1"})
	if res.IsError {
		t.Fatalf("index_spec with name failed: %s", extractText(res))
	}
	if !strings.Contains(extractText(res), "indexed billing-v1:") {
		t.Errorf("summary %q does not report the chosen name", extractText(res))
	}
	metas := storedMetas(t)
	if len(metas) != 1 || metas[0].Name != "billing-v1" {
		t.Fatalf("stored rows = %+v, want a single billing-v1 row", metas)
	}
}

// TestIndexSpecRefusesANameOwnedByAnotherSource covers the data-loss refusal: the
// same derived name for a different source must fail, name the owner and point at
// the tool's own parameter (name=), not the CLI flag.
func TestIndexSpecRefusesANameOwnedByAnotherSource(t *testing.T) {
	dir := projectDir(t)
	v1 := writeIndexSpecFile(t, filepath.Join(dir, "v1", "api.json"), "V1", "/v1")
	v2 := writeIndexSpecFile(t, filepath.Join(t.TempDir(), "v2", "api.json"), "V2", "/v2")
	r := newRegistry(t)

	if res := callIndexSpec(t, r, map[string]interface{}{"source": v1}); res.IsError {
		t.Fatalf("first index_spec failed: %s", extractText(res))
	}

	res := callIndexSpec(t, r, map[string]interface{}{"source": v2})
	if !res.IsError {
		t.Fatalf("a second source with the same derived name was accepted: %s", extractText(res))
	}
	text := extractText(res)
	for _, want := range []string{"[INDEX_FAILED]", v1, "name="} {
		if !strings.Contains(text, want) {
			t.Errorf("collision error %q does not mention %q", text, want)
		}
	}
	if strings.Contains(text, "--name") {
		t.Errorf("collision error %q tells a tool caller to use the CLI flag", text)
	}

	metas := storedMetas(t)
	if len(metas) != 1 || metas[0].Source != v1 {
		t.Fatalf("the failed index changed the store: %+v", metas)
	}
}

// TestIndexSpecRefreshUpdatesInPlace covers the explicit re-read: an unchanged
// source is reported and not rewritten, and refresh rewrites the same row (no
// duplicate) so the next query sees the new document.
func TestIndexSpecRefreshUpdatesInPlace(t *testing.T) {
	dir := projectDir(t)
	source := writeIndexSpecFile(t, filepath.Join(dir, "petstore.json"), "Petstore", "/pets")
	r := newRegistry(t)

	callIndexSpec(t, r, map[string]interface{}{"source": source})
	first := storedMetas(t)
	if len(first) != 1 {
		t.Fatalf("first index wrote %d rows, want 1", len(first))
	}

	// A query between the writes opens the read-only store and caches the entry,
	// which is the sequence a session actually runs.
	mustCall(t, func() (*mcp.CallToolResult, error) {
		return r.handleListEndpoints(context.Background(), makeRequest(map[string]interface{}{}))
	})

	unchanged := callIndexSpec(t, r, map[string]interface{}{"source": source})
	if unchanged.IsError || !strings.Contains(extractText(unchanged), "unchanged petstore") {
		t.Fatalf("re-indexing identical bytes was not reported as unchanged: %s", extractText(unchanged))
	}
	if got := storedMetas(t); !got[0].FetchedAt.Equal(first[0].FetchedAt) {
		t.Errorf("an unchanged re-index moved fetched_at: %v then %v", first[0].FetchedAt, got[0].FetchedAt)
	}

	// Rewrite the source and refresh: same row, new content, new fetch time.
	writeIndexSpecFile(t, source, "Petstore", "/pets", "/pets/{id}")
	refreshed := callIndexSpec(t, r, map[string]interface{}{"source": source, "refresh": true})
	if refreshed.IsError {
		t.Fatalf("refresh failed: %s", extractText(refreshed))
	}
	if !strings.Contains(extractText(refreshed), "(2 endpoints, 1 schemas)") {
		t.Errorf("refresh summary %q does not report the new counts", extractText(refreshed))
	}

	metas := storedMetas(t)
	if len(metas) != 1 {
		t.Fatalf("refresh duplicated the row: %+v", metas)
	}
	if !metas[0].FetchedAt.After(first[0].FetchedAt) {
		t.Errorf("refresh did not rewrite fetched_at: %v then %v", first[0].FetchedAt, metas[0].FetchedAt)
	}
	if text := extractText(mustCall(t, func() (*mcp.CallToolResult, error) {
		return r.handleListEndpoints(context.Background(), makeRequest(map[string]interface{}{}))
	})); !strings.Contains(text, "GET /pets/{id}") {
		t.Errorf("a query after refresh still answers from the old document: %q", text)
	}
}

func TestIndexSpecRefusesASourceOverTheSizeCap(t *testing.T) {
	dir := projectDir(t)
	source := filepath.Join(dir, "big.json")
	if err := os.WriteFile(source, []byte(strings.Repeat(" ", 4096)), 0o644); err != nil {
		t.Fatalf("write oversized source: %v", err)
	}
	r := New(config.Config{MaxSpecSize: 512, FetchTimeout: 5 * time.Second, DefaultLimit: 50})
	t.Cleanup(func() { _ = r.Close() })

	res := callIndexSpec(t, r, map[string]interface{}{"source": source})
	if !res.IsError {
		t.Fatalf("a source over the size cap was indexed: %s", extractText(res))
	}
	for _, want := range []string{"[INDEX_FAILED]", "512", source} {
		if !strings.Contains(extractText(res), want) {
			t.Errorf("error %q does not mention %q", extractText(res), want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, store.DirName)); !os.IsNotExist(err) {
		t.Errorf("a refused source created the store directory: %v", err)
	}
}

func TestIndexSpecRefusesAnUnparsableFileAndWritesNothing(t *testing.T) {
	dir := projectDir(t)
	source := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(source, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write broken source: %v", err)
	}
	r := newRegistry(t)

	res := callIndexSpec(t, r, map[string]interface{}{"source": source})
	if !res.IsError {
		t.Fatalf("an unparsable source was indexed: %s", extractText(res))
	}
	for _, want := range []string{"[INDEX_FAILED]", "parse OpenAPI spec"} {
		if !strings.Contains(extractText(res), want) {
			t.Errorf("error %q does not mention %q", extractText(res), want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, store.DirName)); !os.IsNotExist(err) {
		t.Errorf("a refused source created the store directory: %v", err)
	}
}

func TestIndexSpecReadsAURLSource(t *testing.T) {
	projectDir(t)
	raw := indexSpecBytes(t, "Remote", "/remote")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write(raw)
	}))
	defer srv.Close()
	r := newRegistry(t)

	res := callIndexSpec(t, r, map[string]interface{}{"source": srv.URL + "/api.json", "name": "remote"})
	if res.IsError {
		t.Fatalf("index_spec from a URL failed: %s", extractText(res))
	}
	metas := storedMetas(t)
	if len(metas) != 1 || metas[0].Source != srv.URL+"/api.json" || metas[0].ETag != `"v1"` {
		t.Fatalf("stored row = %+v, want the URL and its etag", metas)
	}
	if text := extractText(mustCall(t, func() (*mcp.CallToolResult, error) {
		return r.handleListEndpoints(context.Background(), makeRequest(map[string]interface{}{"spec": "remote"}))
	})); !strings.Contains(text, "GET /remote") {
		t.Errorf("a query does not answer from the URL-indexed spec: %q", text)
	}
}

func TestIndexSpecUnreachableURLFailsPromptly(t *testing.T) {
	projectDir(t)

	// Reserve a port, then close it, so the URL is guaranteed to refuse.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a local port: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close the reserved listener: %v", err)
	}
	url := "http://" + addr + "/openapi.json"

	cfg := config.Default()
	cfg.FetchTimeout = 2 * time.Second
	r := New(cfg)
	t.Cleanup(func() { _ = r.Close() })

	done := make(chan *mcp.CallToolResult, 1)
	start := time.Now()
	go func() {
		res, callErr := r.handleIndexSpec(context.Background(), makeRequest(map[string]interface{}{"source": url}))
		if callErr != nil {
			done <- nil
			return
		}
		done <- res
	}()

	select {
	case res := <-done:
		if res == nil || !res.IsError {
			t.Fatal("index_spec accepted an unreachable URL")
		}
		if !strings.Contains(extractText(res), url) {
			t.Errorf("error %q does not name the URL", extractText(res))
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Errorf("an unreachable URL took %v; it must fail promptly", elapsed)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("index_spec hung on an unreachable URL")
	}
}
