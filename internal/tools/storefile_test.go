package tools

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hnkatze/swagger-extractor-mcp/internal/store"
)

// storeFilePath is where discovery looks for the store in the current project
// directory (the tests run with the working directory chdir'ed into it).
func storeFilePath(t *testing.T) string {
	t.Helper()
	dir, err := store.DirFor(".")
	if err != nil {
		t.Fatalf("locate store: %v", err)
	}
	return filepath.Join(dir, store.DBFileName)
}

// TestQueryOnUnreadableStoreReportsRecovery covers S11 on the tool path: a store
// file SQLite cannot read answers STORE_ERROR with the store path and the action
// that fixes it, and the query leaves the file byte-for-byte alone.
func TestQueryOnUnreadableStoreReportsRecovery(t *testing.T) {
	projectDir(t)
	path := storeFilePath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create store dir: %v", err)
	}
	garbage := []byte("swa-mcp.db (checked out from a dropped LFS pointer)\n")
	if err := os.WriteFile(path, garbage, 0o644); err != nil {
		t.Fatalf("write garbage store: %v", err)
	}

	r := newRegistry(t)
	res, err := r.handleListEndpoints(context.Background(), makeRequest(map[string]interface{}{}))
	if err != nil {
		t.Fatalf("handleListEndpoints error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("a query over an unreadable store must fail, got %q", extractText(res))
	}

	text := extractText(res)
	for _, want := range []string{
		"[STORE_ERROR]",
		path,
		"move or delete the swa-mcp/ directory and reindex",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("error %q does not report %q", text, want)
		}
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store after the failed query: %v", err)
	}
	if !bytes.Equal(after, garbage) {
		t.Errorf("a query rewrote the store file: %q then %q", garbage, after)
	}
}

// TestQueryOnEmptiedStoreReportsNoStore covers what an `unindex` of the last spec
// leaves behind: a store with no rows, which every tool reports as "nothing is
// indexed" with the index command, not as a missing or broken store.
func TestQueryOnEmptiedStoreReportsNoStore(t *testing.T) {
	projectDir(t)
	indexSpec(t, "petstore", tinySpec(t, "Petstore", "/pets"))

	dir, err := store.DirFor(".")
	if err != nil {
		t.Fatalf("locate store: %v", err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := st.Delete("petstore"); err != nil {
		st.Close()
		t.Fatalf("Delete(petstore): %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	res, err := newRegistry(t).handleListEndpoints(context.Background(), makeRequest(map[string]interface{}{}))
	if err != nil {
		t.Fatalf("handleListEndpoints error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("an emptied store must report that nothing is indexed, got %q", extractText(res))
	}
	text := extractText(res)
	for _, want := range []string{"[NO_STORE]", "swagger-mcp index"} {
		if !strings.Contains(text, want) {
			t.Errorf("error %q does not report %q", text, want)
		}
	}
}
