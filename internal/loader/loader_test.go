package loader

import (
	"os"
	"sort"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/hnkatze/swagger-extractor-mcp/internal/types"
)

// loadPetstoreData is a test helper that reads the petstore.json fixture.
func loadPetstoreData(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/petstore.json")
	if err != nil {
		t.Fatalf("failed to read petstore.json: %v", err)
	}
	return data
}

func TestParseDocument_ValidJSON(t *testing.T) {
	data := loadPetstoreData(t)

	doc, err := ParseDocument(data)
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

func TestParseDocument_InvalidJSON(t *testing.T) {
	_, err := ParseDocument([]byte(`this is not json or yaml at all`))
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

// A real-world spec that writes "Bearer" instead of "bearer" would fail
// validation, so PrepareDocument normalizes the scheme first. The indexer relies
// on this to accept no less than the server would.
func TestPrepareDocument_LowercasesHTTPScheme(t *testing.T) {
	const spec = `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{},"components":{"securitySchemes":{"bearerAuth":{"type":"http","scheme":"Bearer"}}}}`

	doc := mustLoadDoc(t, spec)
	if len(ValidateDocument(doc)) == 0 {
		t.Fatal("expected the spec as written to violate validation")
	}

	PrepareDocument(doc)

	if got := doc.Components.SecuritySchemes["bearerAuth"].Value.Scheme; got != "bearer" {
		t.Errorf("scheme = %q, want %q", got, "bearer")
	}
	if violations := ValidateDocument(doc); len(violations) != 0 {
		t.Errorf("ValidateDocument = %v, want none after normalization", violations)
	}
}

func mustLoadDoc(t *testing.T, spec string) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData([]byte(spec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return doc
}

const validSpecJSON = `{"openapi":"3.0.0","info":{"title":"t","version":"1"},"paths":{"/pets/{id}":{"get":{"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"ok"}}}}}}`

// the path template names {id} but the operation declares no such parameter.
const brokenSpecJSON = `{"openapi":"3.0.0","info":{"title":"t","version":"1"},"paths":{"/pets/{id}":{"get":{"responses":{"200":{"description":"ok"}}}},"/owners/{oid}":{"get":{"responses":{"200":{"description":"ok"}}}}}}`

func TestValidateDocument_ValidHasNoViolations(t *testing.T) {
	got := ValidateDocument(mustLoadDoc(t, validSpecJSON))
	if got == nil || len(got) != 0 {
		t.Fatalf("ValidateDocument = %#v, want empty non-nil slice", got)
	}
}

func TestValidateDocument_ReportsRealViolation(t *testing.T) {
	got := ValidateDocument(mustLoadDoc(t, brokenSpecJSON))
	if len(got) == 0 {
		t.Fatal("expected at least one violation for a missing path parameter")
	}
}

func TestValidateDocument_DeterministicOrder(t *testing.T) {
	first := ValidateDocument(mustLoadDoc(t, brokenSpecJSON))
	for i := 0; i < 20; i++ {
		next := ValidateDocument(mustLoadDoc(t, brokenSpecJSON))
		if len(next) != len(first) {
			t.Fatalf("run %d: %d violations, want %d", i, len(next), len(first))
		}
		for j := range first {
			if next[j] != first[j] {
				t.Fatalf("run %d: order differs at %d: %q vs %q", i, j, next[j], first[j])
			}
		}
	}
	if !sort.StringsAreSorted(first) {
		t.Errorf("violations not sorted: %v", first)
	}
}
