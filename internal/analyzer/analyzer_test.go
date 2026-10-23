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

	results := ListEndpoints(doc, ListFilter{})
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

	pets := ListEndpoints(doc, ListFilter{Tag: "pets"})
	if len(pets) != 4 {
		t.Fatalf("expected 4 'pets' endpoints, got %d", len(pets))
	}

	store := ListEndpoints(doc, ListFilter{Tag: "store"})
	if len(store) != 1 {
		t.Fatalf("expected 1 'store' endpoint, got %d", len(store))
	}
}

func TestListEndpoints_FilterByMethod(t *testing.T) {
	doc := loadTestDoc(t)

	gets := ListEndpoints(doc, ListFilter{Method: "GET"})
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

	results := ListEndpoints(doc, ListFilter{PathPattern: "/pets"})
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

	results := ListEndpoints(doc, ListFilter{Tag: "pets", Method: "POST"})
	if len(results) != 1 {
		t.Fatalf("expected 1 endpoint (createPet), got %d", len(results))
	}
	if results[0].Path != "/pets" || results[0].Method != "POST" {
		t.Errorf("expected POST /pets, got %s %s", results[0].Method, results[0].Path)
	}
}

func TestListEndpoints_NilDoc(t *testing.T) {
	results := ListEndpoints(nil, ListFilter{})
	if results != nil {
		t.Fatalf("expected nil for nil doc, got %v", results)
	}
}

func TestListEndpoints_NoMatch(t *testing.T) {
	doc := loadTestDoc(t)

	results := ListEndpoints(doc, ListFilter{Tag: "nonexistent"})
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

// ---------------------------------------------------------------------------
// ListEndpoints — deprecated filter (S5)
// ---------------------------------------------------------------------------

func TestListEndpoints_DeprecatedFilter(t *testing.T) {
	doc := &openapi3.T{OpenAPI: "3.0.0", Paths: &openapi3.Paths{}}
	doc.Paths.Set("/current", &openapi3.PathItem{
		Get: &openapi3.Operation{Summary: "still supported"},
	})
	doc.Paths.Set("/legacy", &openapi3.PathItem{
		Get: &openapi3.Operation{Summary: "going away", Deprecated: true},
	})

	t.Run("nil keeps both", func(t *testing.T) {
		got := ListEndpoints(doc, ListFilter{})
		if len(got) != 2 {
			t.Fatalf("expected 2 endpoints, got %d", len(got))
		}
	})

	t.Run("true keeps only deprecated", func(t *testing.T) {
		only := true
		got := ListEndpoints(doc, ListFilter{Deprecated: &only})
		if len(got) != 1 || got[0].Path != "/legacy" {
			t.Fatalf("expected only /legacy, got %+v", got)
		}
	})

	t.Run("false keeps only current", func(t *testing.T) {
		only := false
		got := ListEndpoints(doc, ListFilter{Deprecated: &only})
		if len(got) != 1 || got[0].Path != "/current" {
			t.Fatalf("expected only /current, got %+v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// ListSchemas (S3)
// ---------------------------------------------------------------------------

func TestListSchemas_SortedWithUsage(t *testing.T) {
	doc := &openapi3.T{OpenAPI: "3.0.0", Paths: &openapi3.Paths{}}

	doc.Components = &openapi3.Components{Schemas: openapi3.Schemas{
		"Zebra": &openapi3.SchemaRef{Value: openapi3.NewObjectSchema().
			WithProperty("stripes", openapi3.NewIntegerSchema())},
		"Alpha": &openapi3.SchemaRef{Value: openapi3.NewObjectSchema().
			WithProperty("id", openapi3.NewStringSchema()).
			WithProperty("name", openapi3.NewStringSchema())},
	}}

	// One endpoint whose request body references Alpha, so usage is not zero.
	doc.Paths.Set("/alpha", &openapi3.PathItem{
		Post: &openapi3.Operation{
			RequestBody: &openapi3.RequestBodyRef{Value: openapi3.NewRequestBody().
				WithContent(openapi3.Content{
					"application/json": &openapi3.MediaType{
						Schema: openapi3.NewSchemaRef("#/components/schemas/Alpha", nil),
					},
				})},
		},
	})

	got := ListSchemas(doc, "")

	if len(got) != 2 {
		t.Fatalf("expected 2 schemas, got %d: %+v", len(got), got)
	}
	// Deterministic order: map iteration in Go is random.
	if got[0].Name != "Alpha" || got[1].Name != "Zebra" {
		t.Fatalf("expected sorted [Alpha Zebra], got [%s %s]", got[0].Name, got[1].Name)
	}
	if got[0].FieldCount != 2 {
		t.Errorf("Alpha FieldCount = %d, want 2", got[0].FieldCount)
	}
	if got[0].UsedBy != 1 {
		t.Errorf("Alpha UsedBy = %d, want 1", got[0].UsedBy)
	}
	if got[1].UsedBy != 0 {
		t.Errorf("Zebra UsedBy = %d, want 0", got[1].UsedBy)
	}
}

func TestListSchemas_Filter(t *testing.T) {
	doc := &openapi3.T{OpenAPI: "3.0.0", Paths: &openapi3.Paths{}}
	doc.Components = &openapi3.Components{Schemas: openapi3.Schemas{
		"UserDto":  &openapi3.SchemaRef{Value: openapi3.NewObjectSchema()},
		"OrderDto": &openapi3.SchemaRef{Value: openapi3.NewObjectSchema()},
	}}

	got := ListSchemas(doc, "user")

	if len(got) != 1 || got[0].Name != "UserDto" {
		t.Fatalf("filter 'user' should match only UserDto, got %+v", got)
	}
}

func TestListSchemas_NilDoc(t *testing.T) {
	if got := ListSchemas(nil, ""); got != nil {
		t.Errorf("expected nil for nil doc, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// ListSecuritySchemes
// ---------------------------------------------------------------------------

func securityTestDoc(t *testing.T) *openapi3.T {
	t.Helper()
	const spec = `{
  "openapi": "3.0.3",
  "info": {"title": "t", "version": "1"},
  "security": [{"bearerAuth": []}],
  "paths": {
    "/a": {"get": {"responses": {"200": {"description": "ok"}}}},
    "/b": {"get": {"responses": {"200": {"description": "ok"}}}},
    "/c": {"get": {"security": [{"apiKeyAuth": []}], "responses": {"200": {"description": "ok"}}}},
    "/d": {"get": {"security": [], "responses": {"200": {"description": "ok"}}}},
    "/e": {"get": {"security": [{"oauth": ["write", "read"]}], "responses": {"200": {"description": "ok"}}}},
    "/f": {"get": {"security": [{"oauth": ["read"]}, {"apiKeyAuth": []}], "responses": {"200": {"description": "ok"}}}}
  },
  "components": {"securitySchemes": {
    "bearerAuth": {"type": "http", "scheme": "bearer", "bearerFormat": "JWT"},
    "apiKeyAuth": {"type": "apiKey", "in": "header", "name": "X-API-Key", "description": "key"},
    "oauth": {"type": "oauth2", "flows": {
      "authorizationCode": {"authorizationUrl": "https://a/auth", "tokenUrl": "https://a/token", "scopes": {"read": "r", "write": "w"}},
      "clientCredentials": {"tokenUrl": "https://a/token", "scopes": {"read": "r", "admin": "a"}}
    }},
    "unused": {"type": "openIdConnect", "openIdConnectUrl": "https://a/.well-known/openid-configuration"}
  }}
}`
	doc, err := openapi3.NewLoader().LoadFromData([]byte(spec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return doc
}

func TestListSecuritySchemes_DefinitionsAndCounts(t *testing.T) {
	got := ListSecuritySchemes(securityTestDoc(t))

	if len(got.Global) != 1 || len(got.Global[0]) != 1 || got.Global[0][0] != "bearerAuth" {
		t.Errorf("Global = %v, want [[bearerAuth]]", got.Global)
	}
	if got.UnauthenticatedOverride != 1 {
		t.Errorf("UnauthenticatedOverride = %d, want 1", got.UnauthenticatedOverride)
	}

	// bearerAuth: /a,/b via global fallback = 2. apiKeyAuth: /c,/f = 2. oauth: /e,/f = 2. unused = 0.
	byName := map[string]int{}
	for _, s := range got.Schemes {
		byName[s.Name] = s.Endpoints
	}
	want := map[string]int{"bearerAuth": 2, "apiKeyAuth": 2, "oauth": 2, "unused": 0}
	for name, n := range want {
		if byName[name] != n {
			t.Errorf("%s endpoints = %d, want %d", name, byName[name], n)
		}
	}

	for _, s := range got.Schemes {
		switch s.Name {
		case "bearerAuth":
			if s.Type != "http" || s.Scheme != "bearer" || s.BearerFormat != "JWT" {
				t.Errorf("bearerAuth = %+v", s)
			}
		case "apiKeyAuth":
			if s.Type != "apiKey" || s.In != "header" || s.ParamName != "X-API-Key" || s.Description != "key" {
				t.Errorf("apiKeyAuth = %+v", s)
			}
		case "oauth":
			wantScopes := []string{"admin", "read", "write"}
			if len(s.Scopes) != 3 || s.Scopes[0] != wantScopes[0] || s.Scopes[1] != wantScopes[1] || s.Scopes[2] != wantScopes[2] {
				t.Errorf("oauth scopes = %v, want %v", s.Scopes, wantScopes)
			}
		case "unused":
			if s.OpenIDConnectURL != "https://a/.well-known/openid-configuration" {
				t.Errorf("unused OIDC URL = %q", s.OpenIDConnectURL)
			}
		}
	}
}

func TestListSecuritySchemes_DeterministicOrder(t *testing.T) {
	doc := securityTestDoc(t)
	want := []string{"apiKeyAuth", "bearerAuth", "oauth", "unused"} // 2,2,2 by name asc, then 0
	for i := 0; i < 20; i++ {
		got := ListSecuritySchemes(doc)
		for j, s := range got.Schemes {
			if s.Name != want[j] {
				t.Fatalf("run %d: order[%d] = %s, want %s", i, j, s.Name, want[j])
			}
		}
	}
}

func TestListSecuritySchemes_EmptyRequirementMapOptsOut(t *testing.T) {
	doc := securityTestDoc(t)
	doc.Paths.Find("/a").Get.Security = &openapi3.SecurityRequirements{openapi3.SecurityRequirement{}}
	got := ListSecuritySchemes(doc)
	if got.UnauthenticatedOverride != 2 {
		t.Errorf("UnauthenticatedOverride = %d, want 2", got.UnauthenticatedOverride)
	}
	for _, s := range got.Schemes {
		if s.Name == "bearerAuth" && s.Endpoints != 1 {
			t.Errorf("bearerAuth endpoints = %d, want 1", s.Endpoints)
		}
	}
}

func TestListSecuritySchemes_NilSafe(t *testing.T) {
	if got := ListSecuritySchemes(nil); got == nil || len(got.Schemes) != 0 {
		t.Errorf("nil doc = %+v", got)
	}
	if got := ListSecuritySchemes(&openapi3.T{}); got == nil || len(got.Schemes) != 0 {
		t.Errorf("empty doc = %+v", got)
	}
}

// The doc-level security LIST is an OR of requirement objects; each object keeps
// its own group so a reader can tell "either of these" from "both of these".
func TestListSecuritySchemes_KeepsAlternativeGroups(t *testing.T) {
	doc := &openapi3.T{
		OpenAPI: "3.0.0",
		Paths:   &openapi3.Paths{},
		Security: openapi3.SecurityRequirements{
			openapi3.SecurityRequirement{"Bearer": []string{}},
			openapi3.SecurityRequirement{"ApiKey": []string{}},
		},
	}

	got := ListSecuritySchemes(doc)

	if len(got.Global) != 2 {
		t.Fatalf("Global = %v, want 2 alternative groups", got.Global)
	}
	if len(got.Global[0]) != 1 || got.Global[0][0] != "Bearer" {
		t.Errorf("Global[0] = %v, want [Bearer]", got.Global[0])
	}
	if len(got.Global[1]) != 1 || got.Global[1][0] != "ApiKey" {
		t.Errorf("Global[1] = %v, want [ApiKey]", got.Global[1])
	}
}
