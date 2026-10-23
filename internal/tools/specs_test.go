package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/hnkatze/swagger-extractor-mcp/internal/config"
	"github.com/hnkatze/swagger-extractor-mcp/internal/loader"
	"github.com/hnkatze/swagger-extractor-mcp/internal/store"
)

// ---------------------------------------------------------------------------
// T2 addressing (S4, S4a): queries read the local store, addressed by spec name
// ---------------------------------------------------------------------------

// projectDir makes a fresh directory the working directory of the test, so the
// store is discovered by the same walk-up the server performs (S2).
func projectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(store.EnvDir, "")
	t.Chdir(dir)
	return dir
}

// indexSpec writes raw into the project store under name, following the same
// parse → normalize → store path the index command uses (S3). The fetch time is
// a fixed past instant, so a staleness test states its own (S6).
func indexSpec(t *testing.T, name string, raw []byte) {
	t.Helper()
	indexSpecAt(t, name, raw, time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC))
}

// indexSpecAt is indexSpec with an explicit stored fetched_at, so staleness can
// be controlled through the store instead of by sleeping a week.
func indexSpecAt(t *testing.T, name string, raw []byte, fetchedAt time.Time) {
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

	doc, err := loader.ParseDocument(raw)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	loader.PrepareDocument(doc)

	rec := store.NewRecord(doc, raw)
	rec.Name = name
	rec.Source = "fixture://" + name
	rec.ETag = `"etag-` + name + `"`
	rec.FetchedAt = fetchedAt
	if err := st.Put(rec); err != nil {
		t.Fatalf("store fixture %q: %v", name, err)
	}
}

// openEmptyStore creates the store files without indexing anything.
func openEmptyStore(t *testing.T) {
	t.Helper()
	dir, err := store.DirFor(".")
	if err != nil {
		t.Fatalf("locate store: %v", err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
}

// tinySpec builds a minimal, deterministic spec whose summaries carry the title
// so a test can tell two indexed specs apart in the output.
func tinySpec(t *testing.T, title string, paths ...string) []byte {
	t.Helper()
	ops := map[string]any{}
	for _, p := range paths {
		ops[p] = map[string]any{
			"get": map[string]any{
				"summary":   title + " " + p,
				"tags":      []string{title},
				"responses": map[string]any{"200": map[string]any{"description": "ok"}},
			},
		}
	}
	doc := map[string]any{
		"openapi": "3.0.3",
		"info":    map[string]any{"title": title, "version": "1.0.0"},
		"paths":   ops,
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("build fixture: %v", err)
	}
	return raw
}

// newRegistry opens a Registry over the current project's store and closes it
// with the test, so the temporary database can be removed on Windows too.
func newRegistry(t *testing.T) *Registry {
	t.Helper()
	r := New(config.Default())
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestQuery_SingleIndexedSpecIsUsedByDefault(t *testing.T) {
	projectDir(t)
	indexSpec(t, "petstore", tinySpec(t, "Petstore", "/pets", "/pets/{id}"))
	r := newRegistry(t)

	res, err := r.handleListEndpoints(context.Background(), makeRequest(map[string]interface{}{}))
	if err != nil {
		t.Fatalf("handleListEndpoints error: %v", err)
	}
	if res.IsError {
		t.Fatalf("the only indexed spec must be used automatically, got %q", extractText(res))
	}
	text := extractText(res)
	for _, want := range []string{"/pets", "/pets/{id}"} {
		if !strings.Contains(text, want) {
			t.Errorf("output should list %q, got %q", want, text)
		}
	}
}

func TestQuery_SeveralSpecsRequireExplicitName(t *testing.T) {
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
		t.Fatal("with several indexed specs the tool must refuse to guess")
	}
	text := extractText(res)
	for _, want := range []string{"spec=", "orders", "petstore"} {
		if !strings.Contains(text, want) {
			t.Errorf("error %q should mention %q", text, want)
		}
	}

	// An explicit name selects that spec and only that spec.
	res, err = r.handleListEndpoints(ctx, makeRequest(map[string]interface{}{"spec": "orders"}))
	if err != nil {
		t.Fatalf("handleListEndpoints(spec=orders) error: %v", err)
	}
	if res.IsError {
		t.Fatalf("spec=orders should answer, got %q", extractText(res))
	}
	text = extractText(res)
	if !strings.Contains(text, "/orders") {
		t.Errorf("output should come from the orders spec, got %q", text)
	}
	if strings.Contains(text, "/pets") {
		t.Errorf("output should not contain the other spec's endpoints, got %q", text)
	}
}

func TestQuery_UnknownSpecListsIndexedNames(t *testing.T) {
	projectDir(t)
	indexSpec(t, "orders", tinySpec(t, "Orders", "/orders"))
	indexSpec(t, "petstore", tinySpec(t, "Petstore", "/pets"))
	r := newRegistry(t)

	res, err := r.handleListEndpoints(context.Background(), makeRequest(map[string]interface{}{"spec": "zwagger"}))
	if err != nil {
		t.Fatalf("handleListEndpoints error: %v", err)
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

func TestQuery_WithoutStoreNamesIndexCommand(t *testing.T) {
	ctx := context.Background()

	t.Run("no store directory", func(t *testing.T) {
		projectDir(t)
		r := newRegistry(t)
		res, err := r.handleListEndpoints(ctx, makeRequest(map[string]interface{}{}))
		assertNamesIndexCommand(t, res, err)
	})

	t.Run("empty store", func(t *testing.T) {
		projectDir(t)
		openEmptyStore(t)
		r := newRegistry(t)
		res, err := r.handleListEndpoints(ctx, makeRequest(map[string]interface{}{}))
		assertNamesIndexCommand(t, res, err)
	})

	t.Run("explicit name without store", func(t *testing.T) {
		projectDir(t)
		r := newRegistry(t)
		res, err := r.handleListEndpoints(ctx, makeRequest(map[string]interface{}{"spec": "petstore"}))
		assertNamesIndexCommand(t, res, err)
	})
}

func assertNamesIndexCommand(t *testing.T, res *mcp.CallToolResult, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatal("a query without a store must be a tool error")
	}
	text := extractText(res)
	if !strings.Contains(text, "swagger-mcp index") {
		t.Errorf("error %q must name the `swagger-mcp index` command", text)
	}
}

// Every tool that answers from a document must report the same clear error when
// the project has no store, instead of falling back to a fetch (S4).
func TestHandlers_NoStoreErrorNamesIndexCommand(t *testing.T) {
	projectDir(t)
	r := newRegistry(t)
	args := map[string]interface{}{
		"method": "get",
		"path":   "/pets",
		"name":   "Pet",
		"query":  "pets",
		"tag":    "pets",
		"schema": "Pet",
	}

	for _, def := range r.definitions() {
		if def.tool.Name == "usage_guide" {
			continue
		}
		t.Run(def.tool.Name, func(t *testing.T) {
			res, err := def.handler(context.Background(), makeRequest(args))
			assertNamesIndexCommand(t, res, err)
		})
	}
}

// A query must be answerable with no network involved at all: the document comes
// from the store, so rewriting the source file after indexing cannot change what
// a fresh session answers.
func TestQuery_AnswersFromStoreNotFromSource(t *testing.T) {
	dir := projectDir(t)

	raw := tinySpec(t, "Indexed", "/indexed")
	file := filepath.Join(dir, "api.json")
	if err := os.WriteFile(file, raw, 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	// Index exactly the bytes an `swagger-mcp index ./api.json` run would read.
	indexed, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read source file: %v", err)
	}
	indexSpec(t, "petstore", indexed)

	// No route to anywhere: an HTTP attempt would fail rather than answer.
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:1")

	res, err := newRegistry(t).handleListEndpoints(context.Background(), makeRequest(map[string]interface{}{}))
	if err != nil {
		t.Fatalf("handleListEndpoints error: %v", err)
	}
	if res.IsError {
		t.Fatalf("a query must answer from the store alone, got %q", extractText(res))
	}
	if text := extractText(res); !strings.Contains(text, "/indexed") {
		t.Fatalf("output should come from the indexed document, got %q", text)
	}

	// The source file is not the query's input: a new session still reads the
	// stored copy, not the file and certainly not a URL.
	if err := os.WriteFile(file, tinySpec(t, "Rewritten", "/rewritten"), 0o644); err != nil {
		t.Fatalf("rewrite source file: %v", err)
	}
	res, err = newRegistry(t).handleListEndpoints(context.Background(), makeRequest(map[string]interface{}{}))
	if err != nil || res.IsError {
		t.Fatalf("second session should answer from the store: %v %q", err, extractText(res))
	}
	if text := extractText(res); !strings.Contains(text, "/indexed") || strings.Contains(text, "/rewritten") {
		t.Errorf("queries must read the store, got %q", text)
	}
}

// The tool surface itself must have no way to address a URL and no fetch tool:
// indexing is the CLI's job (S4a).
func TestToolSurface_AddressesSpecsNeverURLs(t *testing.T) {
	r := newRegistry(t)
	defs := r.definitions()

	names := map[string]bool{}
	for _, def := range defs {
		names[def.tool.Name] = true
	}
	for _, gone := range []string{"fetch_spec", "refresh_spec"} {
		if names[gone] {
			t.Errorf("%q must not be a tool any more: indexing is the CLI's job", gone)
		}
	}
	for _, want := range []string{
		"list_endpoints", "get_endpoint", "get_schema", "search_spec", "list_schemas",
		"security_schemes", "validate_spec", "analyze_tags", "diff_endpoints",
		"spec_status", "generate_types", "usage_guide",
	} {
		if !names[want] {
			t.Errorf("tool %q should exist", want)
		}
	}

	for _, def := range defs {
		for param := range def.tool.InputSchema.Properties {
			switch param {
			case "url", "url_old", "url_new", "refresh":
				t.Errorf("%s still exposes %q: no query accepts a URL", def.tool.Name, param)
			}
		}
		if def.tool.Name == "usage_guide" || def.tool.Name == "list_specs" {
			continue
		}
		wanted := []string{"spec"}
		if def.tool.Name == "diff_endpoints" {
			wanted = []string{"spec_old", "spec_new"}
		}
		for _, param := range wanted {
			if _, ok := def.tool.InputSchema.Properties[param]; !ok {
				t.Errorf("%s must take %q", def.tool.Name, param)
			}
			for _, required := range def.tool.InputSchema.Required {
				if required == param {
					t.Errorf("%s must not require %q: omitting it means the only spec", def.tool.Name, param)
				}
			}
		}
	}
}

// Repeated calls in one session must reuse the parsed document instead of
// re-reading and re-parsing the stored blob (S4).
func TestQuery_CachesParsedDocument(t *testing.T) {
	projectDir(t)
	indexSpec(t, "petstore", tinySpec(t, "Petstore", "/pets"))
	r := newRegistry(t)

	first, err := r.specs.document("petstore")
	if err != nil {
		t.Fatalf("document: %v", err)
	}
	if res, err := r.handleListEndpoints(context.Background(), makeRequest(map[string]interface{}{})); err != nil || res.IsError {
		t.Fatalf("query failed: %v %q", err, extractText(res))
	}
	second, err := r.specs.document("petstore")
	if err != nil {
		t.Fatalf("document: %v", err)
	}
	if first != second {
		t.Error("a repeated query re-parsed the stored document instead of using the cache")
	}
}

// A reindex during a session must reach the next query: the cache is keyed by
// the stored fingerprint, never by name alone.
func TestQuery_ReindexIsVisibleInSameSession(t *testing.T) {
	projectDir(t)
	indexSpec(t, "petstore", tinySpec(t, "Petstore", "/pets"))
	r := newRegistry(t)

	res, err := r.handleListEndpoints(context.Background(), makeRequest(map[string]interface{}{}))
	if err != nil || res.IsError || !strings.Contains(extractText(res), "/pets") {
		t.Fatalf("first query should read /pets: %v %q", err, extractText(res))
	}

	indexSpec(t, "petstore", tinySpec(t, "Petstore", "/animals"))

	res, err = r.handleListEndpoints(context.Background(), makeRequest(map[string]interface{}{}))
	if err != nil || res.IsError {
		t.Fatalf("second query failed: %v %q", err, extractText(res))
	}
	text := extractText(res)
	if !strings.Contains(text, "/animals") {
		t.Errorf("a reindexed spec must be re-read, got %q", text)
	}
	if strings.Contains(text, "/pets") {
		t.Errorf("the cached copy of the old document must be dropped, got %q", text)
	}
}

// The cache is a session convenience, not a copy of the whole index.
func TestQuery_CacheIsBounded(t *testing.T) {
	projectDir(t)
	specs := make([]string, 0, specCacheSize+1)
	for i := 0; i <= specCacheSize; i++ {
		name := "spec" + string(rune('a'+i))
		indexSpec(t, name, tinySpec(t, name, "/"+name))
		specs = append(specs, name)
	}
	r := newRegistry(t)

	for _, name := range specs {
		if _, err := r.specs.document(name); err != nil {
			t.Fatalf("document(%q): %v", name, err)
		}
	}
	if got := len(r.specs.cache); got > specCacheSize {
		t.Errorf("cache holds %d documents, want at most %d", got, specCacheSize)
	}
}

func TestSpecStatus_ReportsStoredMetadata(t *testing.T) {
	projectDir(t)
	indexSpec(t, "petstore", tinySpec(t, "Petstore", "/pets", "/pets/{id}"))
	r := newRegistry(t)

	res, err := r.handleSpecStatus(context.Background(), makeRequest(map[string]interface{}{}))
	if err != nil || res.IsError {
		t.Fatalf("spec_status failed: %v %q", err, extractText(res))
	}
	text := extractText(res)
	for _, want := range []string{`"name": "petstore"`, `"source": "fixture://petstore"`, `"etag": "\"etag-petstore\""`, "fetched_at", "endpoint_count"} {
		if !strings.Contains(text, want) {
			t.Errorf("status should report %q, got %q", want, text)
		}
	}
}

// indexSpecFrom is indexSpec with an explicit canonical source, so a test can
// build a spec whose stored name differs from the name derived from its source.
func indexSpecFrom(t *testing.T, name, source string, raw []byte) {
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

	doc, err := loader.ParseDocument(raw)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	loader.PrepareDocument(doc)

	rec := store.NewRecord(doc, raw)
	rec.Name = name
	rec.Source = source
	rec.FetchedAt = time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := st.Put(rec); err != nil {
		t.Fatalf("store fixture %q: %v", name, err)
	}
}

// A query must never write: opening a store that holds no schema reports the
// problem and leaves the file exactly as it was.
func TestQuery_EmptyDatabaseIsNotMigratedByAQuery(t *testing.T) {
	dir := projectDir(t)
	storeDir := filepath.Join(dir, store.DirName)
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatalf("create store directory: %v", err)
	}
	path := filepath.Join(storeDir, store.DBFileName)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("create empty database file: %v", err)
	}

	res, err := newRegistry(t).handleListEndpoints(context.Background(), makeRequest(map[string]interface{}{}))
	if err != nil {
		t.Fatalf("handleListEndpoints error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("a query against an empty database must fail, got %q", extractText(res))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat database: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("the query wrote %d bytes into the empty database", info.Size())
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		t.Fatalf("open database for inspection: %v", err)
	}
	defer db.Close()
	var tables int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master").Scan(&tables); err != nil {
		t.Fatalf("count tables: %v", err)
	}
	if tables != 0 {
		t.Fatalf("the query created %d schema objects", tables)
	}
}

// An old client that still sends url must be told that addressing changed,
// instead of quietly getting an answer about a different spec.
func TestToolCalls_RejectURLEarned(t *testing.T) {
	projectDir(t)
	indexSpec(t, "petstore", tinySpec(t, "Petstore", "/pets"))
	r := newRegistry(t)
	ctx := context.Background()

	for _, def := range r.definitions() {
		t.Run(def.tool.Name, func(t *testing.T) {
			res, err := def.handler(ctx, makeRequest(map[string]interface{}{
				"url":    "https://example.com/openapi.json",
				"method": "get",
				"path":   "/pets",
				"name":   "Pet",
				"query":  "pets",
				"tag":    "pets",
				"schema": "Pet",
			}))
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			if !res.IsError {
				t.Fatalf("a call carrying url must be an error, got %q", extractText(res))
			}
			text := extractText(res)
			for _, want := range []string{"url", "swagger-mcp index", "spec=<name>"} {
				if !strings.Contains(text, want) {
					t.Errorf("error %q should mention %q", text, want)
				}
			}
		})
	}
}

// The refresh command spec_status prints must be copy-pasteable: when the stored
// name is not the name derived from the source, it has to carry --name.
func TestSpecStatus_RefreshCommandPinsCustomName(t *testing.T) {
	projectDir(t)
	indexSpecFrom(t, "billing", "https://api.example.com/v1/api.json", tinySpec(t, "Billing", "/invoices"))
	r := newRegistry(t)

	res, err := r.handleSpecStatus(context.Background(), makeRequest(map[string]interface{}{}))
	if err != nil || res.IsError {
		t.Fatalf("spec_status failed: %v %q", err, extractText(res))
	}
	want := `"refresh": "swagger-mcp index --refresh --name billing https://api.example.com/v1/api.json"`
	if text := extractText(res); !strings.Contains(text, want) {
		t.Errorf("status should report %q, got %q", want, text)
	}
}
