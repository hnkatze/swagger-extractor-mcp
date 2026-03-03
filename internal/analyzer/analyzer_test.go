package analyzer

import (
	"os"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// loadTestDoc reads and parses the petstore.json fixture.
func loadTestDoc(t *testing.T) *openapi3.T {
	t.Helper()
	data, err := os.ReadFile("../../testdata/petstore.json")
	if err != nil {
		t.Fatalf("failed to read test fixture: %v", err)
	}
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(data)
	if err != nil {
		t.Fatalf("failed to parse test fixture: %v", err)
	}
	return doc
}

// ---------------------------------------------------------------------------
// ListEndpoints
// ---------------------------------------------------------------------------

func TestListEndpoints_All(t *testing.T) {
	doc := loadTestDoc(t)

	results := ListEndpoints(doc, "", "", "")
	if len(results) != 5 {
		t.Fatalf("expected 5 endpoints, got %d", len(results))
	}

	// Verify sorted by path first, then method
	for i := 1; i < len(results); i++ {
		prevPath := results[i-1].Path
		currPath := results[i].Path
		if prevPath > currPath {
			t.Errorf("results not sorted by path: %q > %q", prevPath, currPath)
		}
		if prevPath == currPath && results[i-1].Method > results[i].Method {
			t.Errorf("results not sorted by method within same path: %q > %q", results[i-1].Method, results[i].Method)
		}
	}
}

func TestListEndpoints_FilterByTag(t *testing.T) {
	doc := loadTestDoc(t)

	pets := ListEndpoints(doc, "pets", "", "")
	if len(pets) != 4 {
		t.Fatalf("expected 4 'pets' endpoints, got %d", len(pets))
	}

	store := ListEndpoints(doc, "store", "", "")
	if len(store) != 1 {
		t.Fatalf("expected 1 'store' endpoint, got %d", len(store))
	}
}

func TestListEndpoints_FilterByMethod(t *testing.T) {
	doc := loadTestDoc(t)

	gets := ListEndpoints(doc, "", "GET", "")
	if len(gets) != 3 {
		t.Fatalf("expected 3 GET endpoints, got %d", len(gets))
	}
	for _, ep := range gets {
		if ep.Method != "GET" {
			t.Errorf("expected method GET, got %s", ep.Method)
		}
	}
}

func TestListEndpoints_FilterByPath(t *testing.T) {
	doc := loadTestDoc(t)

	results := ListEndpoints(doc, "", "", "/pets")
	if len(results) != 4 {
		t.Fatalf("expected 4 endpoints matching /pets, got %d", len(results))
	}
	for _, ep := range results {
		if ep.Path != "/pets" && ep.Path != "/pets/{petId}" {
			t.Errorf("unexpected path %s", ep.Path)
		}
	}
}

func TestListEndpoints_CombinedFilters(t *testing.T) {
	doc := loadTestDoc(t)

	results := ListEndpoints(doc, "pets", "POST", "")
	if len(results) != 1 {
		t.Fatalf("expected 1 endpoint (createPet), got %d", len(results))
	}
	if results[0].Path != "/pets" || results[0].Method != "POST" {
		t.Errorf("expected POST /pets, got %s %s", results[0].Method, results[0].Path)
	}
}

func TestListEndpoints_NilDoc(t *testing.T) {
	results := ListEndpoints(nil, "", "", "")
	if results != nil {
		t.Fatalf("expected nil for nil doc, got %v", results)
	}
}

func TestListEndpoints_NoMatch(t *testing.T) {
	doc := loadTestDoc(t)

	results := ListEndpoints(doc, "nonexistent", "", "")
	if len(results) != 0 {
		t.Fatalf("expected 0 endpoints for nonexistent tag, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// AnalyzeTags
// ---------------------------------------------------------------------------

func TestAnalyzeTags_Petstore(t *testing.T) {
	doc := loadTestDoc(t)

	tags := AnalyzeTags(doc)
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}

	// Sorted alphabetically: pets, store
	if tags[0].Name != "pets" {
		t.Errorf("expected first tag 'pets', got %q", tags[0].Name)
	}
	if tags[1].Name != "store" {
		t.Errorf("expected second tag 'store', got %q", tags[1].Name)
	}

	if tags[0].EndpointCount != 4 {
		t.Errorf("expected 'pets' to have 4 endpoints, got %d", tags[0].EndpointCount)
	}
	if tags[1].EndpointCount != 1 {
		t.Errorf("expected 'store' to have 1 endpoint, got %d", tags[1].EndpointCount)
	}
}

func TestAnalyzeTags_MethodBreakdown(t *testing.T) {
	doc := loadTestDoc(t)

	tags := AnalyzeTags(doc)

	// Find the "pets" tag
	var petsTag *struct {
		breakdown map[string]int
	}
	for _, tag := range tags {
		if tag.Name == "pets" {
			petsTag = &struct {
				breakdown map[string]int
			}{breakdown: tag.MethodBreakdown}
			break
		}
	}
	if petsTag == nil {
		t.Fatal("'pets' tag not found")
	}

	expected := map[string]int{
		"GET":    2,
		"POST":   1,
		"DELETE": 1,
	}
	for method, count := range expected {
		if petsTag.breakdown[method] != count {
			t.Errorf("pets tag: expected %s:%d, got %s:%d", method, count, method, petsTag.breakdown[method])
		}
	}
}

func TestAnalyzeTags_NilDoc(t *testing.T) {
	results := AnalyzeTags(nil)
	if results != nil {
		t.Fatalf("expected nil for nil doc, got %v", results)
	}
}

// ---------------------------------------------------------------------------
// SearchSpec
// ---------------------------------------------------------------------------

func TestSearchSpec_ByPath(t *testing.T) {
	doc := loadTestDoc(t)

	results := SearchSpec(doc, "inventory")
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'inventory'")
	}
	if results[0].Path != "/store/inventory" {
		t.Errorf("expected /store/inventory as top result, got %s", results[0].Path)
	}
}

func TestSearchSpec_BySummary(t *testing.T) {
	doc := loadTestDoc(t)

	results := SearchSpec(doc, "list")
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'list'")
	}

	found := false
	for _, r := range results {
		if r.Summary == "List all pets" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find 'List all pets' in results")
	}
}

func TestSearchSpec_ByOperationId(t *testing.T) {
	doc := loadTestDoc(t)

	results := SearchSpec(doc, "createPet")
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'createPet'")
	}

	found := false
	for _, r := range results {
		if r.Method == "POST" && r.Path == "/pets" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find POST /pets in results")
	}
}

func TestSearchSpec_NoMatch(t *testing.T) {
	doc := loadTestDoc(t)

	results := SearchSpec(doc, "zzzznotfound")
	if len(results) != 0 {
		t.Fatalf("expected 0 results for 'zzzznotfound', got %d", len(results))
	}
}

func TestSearchSpec_EmptyQuery(t *testing.T) {
	doc := loadTestDoc(t)

	results := SearchSpec(doc, "")
	if results != nil {
		t.Fatalf("expected nil for empty query, got %v", results)
	}
}

func TestSearchSpec_CaseInsensitive(t *testing.T) {
	doc := loadTestDoc(t)

	results := SearchSpec(doc, "LIST")
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'LIST' (case-insensitive)")
	}

	found := false
	for _, r := range results {
		if r.Summary == "List all pets" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find 'List all pets' in case-insensitive search for 'LIST'")
	}
}

// ---------------------------------------------------------------------------
// DiffSpecs
// ---------------------------------------------------------------------------

func TestDiffSpecs_Identical(t *testing.T) {
	doc := loadTestDoc(t)

	result := DiffSpecs(doc, doc, "", "")
	if len(result.Added) != 0 {
		t.Errorf("expected 0 added, got %d", len(result.Added))
	}
	if len(result.Removed) != 0 {
		t.Errorf("expected 0 removed, got %d", len(result.Removed))
	}
}

func TestDiffSpecs_Added(t *testing.T) {
	oldDoc := loadTestDoc(t)
	newDoc := loadTestDoc(t)

	// Add a new endpoint to the new doc
	newPath := &openapi3.PathItem{
		Get: &openapi3.Operation{
			Summary:     "Health check",
			OperationID: "healthCheck",
			Tags:        []string{"system"},
			Responses:   openapi3.NewResponses(),
		},
	}
	newDoc.Paths.Set("/health", newPath)

	result := DiffSpecs(oldDoc, newDoc, "", "")
	if len(result.Added) != 1 {
		t.Fatalf("expected 1 added endpoint, got %d", len(result.Added))
	}
	if result.Added[0].Path != "/health" {
		t.Errorf("expected added path /health, got %s", result.Added[0].Path)
	}
	if result.Added[0].Method != "GET" {
		t.Errorf("expected added method GET, got %s", result.Added[0].Method)
	}
}

func TestDiffSpecs_NilDocs(t *testing.T) {
	doc := loadTestDoc(t)

	result1 := DiffSpecs(nil, doc, "", "")
	if result1 == nil {
		t.Fatal("expected non-nil result for nil old doc")
	}
	if len(result1.Added) != 0 && len(result1.Removed) != 0 && len(result1.Changed) != 0 {
		t.Error("expected empty result for nil old doc")
	}

	result2 := DiffSpecs(doc, nil, "", "")
	if result2 == nil {
		t.Fatal("expected non-nil result for nil new doc")
	}
	if len(result2.Added) != 0 && len(result2.Removed) != 0 && len(result2.Changed) != 0 {
		t.Error("expected empty result for nil new doc")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func TestHasTag(t *testing.T) {
	tests := []struct {
		name     string
		tags     []string
		search   string
		expected bool
	}{
		{
			name:     "Found",
			tags:     []string{"pets"},
			search:   "pets",
			expected: true,
		},
		{
			name:     "CaseInsensitive",
			tags:     []string{"pets"},
			search:   "PETS",
			expected: true,
		},
		{
			name:     "NotFound",
			tags:     []string{"pets"},
			search:   "dogs",
			expected: false,
		},
		{
			name:     "EmptyTags",
			tags:     []string{},
			search:   "pets",
			expected: false,
		},
		{
			name:     "EmptySearch",
			tags:     []string{"pets"},
			search:   "",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			op := &openapi3.Operation{Tags: tc.tags}
			result := hasTag(op, tc.search)
			if result != tc.expected {
				t.Errorf("hasTag(%v, %q) = %v, want %v", tc.tags, tc.search, result, tc.expected)
			}
		})
	}
}

func TestMatchFilter(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		path         string
		filterMethod string
		filterPath   string
		expected     bool
	}{
		{
			name:         "AllMatch_EmptyFilters",
			method:       "GET",
			path:         "/pets",
			filterMethod: "",
			filterPath:   "",
			expected:     true,
		},
		{
			name:         "MethodMatch",
			method:       "GET",
			path:         "/pets",
			filterMethod: "GET",
			filterPath:   "",
			expected:     true,
		},
		{
			name:         "MethodMismatch",
			method:       "POST",
			path:         "/pets",
			filterMethod: "GET",
			filterPath:   "",
			expected:     false,
		},
		{
			name:         "PathMatch",
			method:       "GET",
			path:         "/pets/{petId}",
			filterMethod: "",
			filterPath:   "/pets",
			expected:     true,
		},
		{
			name:         "PathMismatch",
			method:       "GET",
			path:         "/store/inventory",
			filterMethod: "",
			filterPath:   "/pets",
			expected:     false,
		},
		{
			name:         "BothMatch",
			method:       "GET",
			path:         "/pets",
			filterMethod: "GET",
			filterPath:   "/pets",
			expected:     true,
		},
		{
			name:         "MethodMatchPathMismatch",
			method:       "GET",
			path:         "/store/inventory",
			filterMethod: "GET",
			filterPath:   "/pets",
			expected:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := matchFilter(tc.method, tc.path, tc.filterMethod, tc.filterPath)
			if result != tc.expected {
				t.Errorf("matchFilter(%q, %q, %q, %q) = %v, want %v",
					tc.method, tc.path, tc.filterMethod, tc.filterPath, result, tc.expected)
			}
		})
	}
}

func TestEqualStringSlices(t *testing.T) {
	tests := []struct {
		name     string
		a        []string
		b        []string
		expected bool
	}{
		{
			name:     "Equal",
			a:        []string{"a", "b"},
			b:        []string{"a", "b"},
			expected: true,
		},
		{
			name:     "Different",
			a:        []string{"a", "b"},
			b:        []string{"a", "c"},
			expected: false,
		},
		{
			name:     "DifferentLen",
			a:        []string{"a"},
			b:        []string{"a", "b"},
			expected: false,
		},
		{
			name:     "BothEmpty",
			a:        []string{},
			b:        []string{},
			expected: true,
		},
		{
			name:     "BothNil",
			a:        nil,
			b:        nil,
			expected: true,
		},
		{
			name:     "NilVsEmpty",
			a:        nil,
			b:        []string{},
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := equalStringSlices(tc.a, tc.b)
			if result != tc.expected {
				t.Errorf("equalStringSlices(%v, %v) = %v, want %v", tc.a, tc.b, result, tc.expected)
			}
		})
	}
}

func TestMatchScore(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		path      string
		op        *openapi3.Operation
		wantScore int
	}{
		{
			name:  "MatchByPath",
			query: "inventory",
			path:  "/store/inventory",
			op:    &openapi3.Operation{},
			wantScore: 0,
		},
		{
			name:  "MatchBySummary",
			query: "list",
			path:  "/pets",
			op: &openapi3.Operation{
				Summary: "List all pets",
			},
			wantScore: 1,
		},
		{
			name:  "MatchByOperationId",
			query: "createpet",
			path:  "/pets",
			op: &openapi3.Operation{
				OperationID: "createPet",
			},
			wantScore: 2,
		},
		{
			name:  "MatchByDescription",
			query: "detailed",
			path:  "/pets",
			op: &openapi3.Operation{
				Description: "Returns a detailed pet object",
			},
			wantScore: 3,
		},
		{
			name:  "NoMatch",
			query: "zzzznotfound",
			path:  "/pets",
			op: &openapi3.Operation{
				Summary:     "List all pets",
				OperationID: "listPets",
				Description: "Lists pets",
			},
			wantScore: -1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			score := matchScore(tc.query, tc.path, tc.op)
			if score != tc.wantScore {
				t.Errorf("matchScore(%q, %q, op) = %d, want %d", tc.query, tc.path, score, tc.wantScore)
			}
		})
	}
}
