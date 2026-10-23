package specsource

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// S13: this package owns the indexer's read path. The tests pin what the index
// command and the index_spec tool both rely on: the bytes, the resolved source,
// the etag when the source sent one, the size cap, and the parse/validate rules.

const validSpec = `openapi: 3.0.3
info:
  title: Tiny
  version: 1.0.0
paths: {}
`

func writeSourceFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
	return path
}

func TestReadFileReportsTheAbsolutePathAndNoETag(t *testing.T) {
	path := writeSourceFile(t, "api.yaml", []byte(validSpec))

	src, err := NewReader(1<<20, time.Second).Read(context.Background(), path)
	if err != nil {
		t.Fatalf("Read(%s): %v", path, err)
	}
	if string(src.Raw) != validSpec {
		t.Errorf("Raw = %q, want the file bytes", src.Raw)
	}
	if src.Canonical != path {
		t.Errorf("Canonical = %q, want the absolute fixture path %q", src.Canonical, path)
	}
	if src.ETag != "" {
		t.Errorf("ETag = %q, want empty for a file source", src.ETag)
	}
}

func TestReadFileResolvesARelativePath(t *testing.T) {
	path := writeSourceFile(t, "api.yaml", []byte(validSpec))
	t.Chdir(filepath.Dir(path))

	src, err := NewReader(1<<20, time.Second).Read(context.Background(), "api.yaml")
	if err != nil {
		t.Fatalf("Read(api.yaml): %v", err)
	}
	if src.Canonical != path {
		t.Errorf("Canonical = %q, want the same file reached by an absolute path (%q)", src.Canonical, path)
	}
}

func TestReadURLReportsTheURLAndETag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"abc123"`)
		_, _ = w.Write([]byte(validSpec))
	}))
	defer srv.Close()

	src, err := NewReader(1<<20, 5*time.Second).Read(context.Background(), srv.URL+"/api.yaml")
	if err != nil {
		t.Fatalf("Read(%s): %v", srv.URL, err)
	}
	if string(src.Raw) != validSpec {
		t.Errorf("Raw = %q, want the served body", src.Raw)
	}
	if src.Canonical != srv.URL+"/api.yaml" {
		t.Errorf("Canonical = %q, want the source URL", src.Canonical)
	}
	if src.ETag != `"abc123"` {
		t.Errorf("ETag = %q, want %q", src.ETag, `"abc123"`)
	}
}

func TestReadURLReportsAnHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	_, err := NewReader(1<<20, 5*time.Second).Read(context.Background(), srv.URL+"/missing.yaml")
	if err == nil {
		t.Fatal("Read accepted a 404 response")
	}
	if !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("error %q does not report the status", err)
	}
}

func TestReadRefusesAFileOverTheSizeCap(t *testing.T) {
	path := writeSourceFile(t, "big.json", bytes.Repeat([]byte(" "), 4096))

	_, err := NewReader(512, time.Second).Read(context.Background(), path)
	if err == nil {
		t.Fatal("Read accepted a file larger than the cap")
	}
	for _, want := range []string{"512", "spec size limit"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestReadRefusesAURLBodyOverTheSizeCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte(" "), 4096))
	}))
	defer srv.Close()

	_, err := NewReader(512, 5*time.Second).Read(context.Background(), srv.URL+"/big.json")
	if err == nil {
		t.Fatal("Read accepted a body larger than the cap")
	}
	if !strings.Contains(err.Error(), "512") {
		t.Errorf("error %q does not report the cap", err)
	}
}

func TestParseAcceptsAValidDocument(t *testing.T) {
	doc, err := Parse([]byte(validSpec))
	if err != nil {
		t.Fatalf("Parse(valid): %v", err)
	}
	if doc.Info == nil || doc.Info.Title != "Tiny" {
		t.Fatalf("parsed document lost its info: %+v", doc.Info)
	}
}

func TestParseRejectsUnparsableBytes(t *testing.T) {
	_, err := Parse([]byte("{not a spec"))
	if err == nil {
		t.Fatal("Parse accepted garbage")
	}
	if !strings.Contains(err.Error(), "parse OpenAPI spec") {
		t.Errorf("error %q is not the actionable parse message", err)
	}
}

func TestParseRejectsAnInvalidDocument(t *testing.T) {
	// info.title is required by the OpenAPI schema; the indexer refuses it.
	raw := []byte(`{"openapi":"3.0.0","info":{"version":"1.0.0"},"paths":{}}`)
	_, err := Parse(raw)
	if err == nil {
		t.Fatal("Parse accepted a document the server would reject")
	}
	if !strings.Contains(err.Error(), "spec validation failed") {
		t.Errorf("error %q is not the actionable validation message", err)
	}
}

func TestParseNormalizesBeforeItValidates(t *testing.T) {
	// The loader lowercases an HTTP scheme; the indexer validates after that
	// normalization, so a spec the server would serve must index.
	raw := []byte(`{"openapi":"3.0.0",
		"info":{"title":"Upper","version":"1.0.0"},
		"components":{"securitySchemes":{"bearerAuth":{"type":"http","scheme":"Bearer"}}},
		"paths":{}}`)
	if _, err := Parse(raw); err != nil {
		t.Fatalf("Parse rejected a spec the server normalizes: %v", err)
	}
}
