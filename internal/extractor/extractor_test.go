package extractor

import (
	"errors"
	"os"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

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
// GetEndpoint tests
// ---------------------------------------------------------------------------

func TestGetEndpoint_ListPets(t *testing.T) {
	doc := loadTestDoc(t)
	detail, err := GetEndpoint(doc, "GET", "/pets", -1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if detail.Method != "GET" {
		t.Errorf("method = %q, want %q", detail.Method, "GET")
	}
	if detail.Path != "/pets" {
		t.Errorf("path = %q, want %q", detail.Path, "/pets")
	}
	if detail.Summary != "List all pets" {
		t.Errorf("summary = %q, want %q", detail.Summary, "List all pets")
	}
	if len(detail.Tags) != 1 || detail.Tags[0] != "pets" {
		t.Errorf("tags = %v, want [pets]", detail.Tags)
	}
	if len(detail.Parameters) != 2 {
		t.Fatalf("parameters count = %d, want 2", len(detail.Parameters))
	}

	// Verify parameter names exist (order may vary due to map iteration)
	paramNames := make(map[string]bool)
	for _, p := range detail.Parameters {
		paramNames[p.Name] = true
	}
	if !paramNames["limit"] {
		t.Error("expected parameter 'limit' not found")
	}
	if !paramNames["status"] {
		t.Error("expected parameter 'status' not found")
	}
}

func TestGetEndpoint_CreatePet(t *testing.T) {
	doc := loadTestDoc(t)
	detail, err := GetEndpoint(doc, "POST", "/pets", -1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if detail.RequestBody == nil {
		t.Fatal("requestBody is nil, expected non-nil")
	}
	if !detail.RequestBody.Required {
		t.Error("requestBody.Required = false, want true")
	}
	if detail.RequestBody.Content == nil {
		t.Fatal("requestBody.Content is nil")
	}
	if _, ok := detail.RequestBody.Content["application/json"]; !ok {
		t.Error("requestBody.Content missing 'application/json' key")
	}
}

func TestGetEndpoint_GetPet(t *testing.T) {
	doc := loadTestDoc(t)
	detail, err := GetEndpoint(doc, "GET", "/pets/{petId}", -1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify parameter
	if len(detail.Parameters) != 1 {
		t.Fatalf("parameters count = %d, want 1", len(detail.Parameters))
	}
	p := detail.Parameters[0]
	if p.Name != "petId" {
		t.Errorf("parameter name = %q, want %q", p.Name, "petId")
	}
	if p.In != "path" {
		t.Errorf("parameter in = %q, want %q", p.In, "path")
	}
	if !p.Required {
		t.Error("parameter required = false, want true")
	}

	// Verify responses
	if len(detail.Responses) != 2 {
		t.Fatalf("responses count = %d, want 2", len(detail.Responses))
	}

	statusCodes := make(map[string]bool)
	for _, r := range detail.Responses {
		statusCodes[r.StatusCode] = true
	}
	if !statusCodes["200"] {
		t.Error("expected response '200' not found")
	}
	if !statusCodes["404"] {
		t.Error("expected response '404' not found")
	}
}

func TestGetEndpoint_DeletePet(t *testing.T) {
	doc := loadTestDoc(t)
	detail, err := GetEndpoint(doc, "DELETE", "/pets/{petId}", -1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !detail.Deprecated {
		t.Error("deprecated = false, want true")
	}

	if len(detail.Responses) != 1 {
		t.Fatalf("responses count = %d, want 1", len(detail.Responses))
	}
	if detail.Responses[0].StatusCode != "204" {
		t.Errorf("response status = %q, want %q", detail.Responses[0].StatusCode, "204")
	}
}

func TestGetEndpoint_NotFound(t *testing.T) {
	doc := loadTestDoc(t)
	_, err := GetEndpoint(doc, "GET", "/nonexistent", -1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrEndpointNotFound) {
		t.Errorf("error = %v, want ErrEndpointNotFound", err)
	}
}

func TestGetEndpoint_WrongMethod(t *testing.T) {
	doc := loadTestDoc(t)
	_, err := GetEndpoint(doc, "PUT", "/pets", -1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrEndpointNotFound) {
		t.Errorf("error = %v, want ErrEndpointNotFound", err)
	}
}

func TestGetEndpoint_NilDoc(t *testing.T) {
	_, err := GetEndpoint(nil, "GET", "/pets", -1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrEndpointNotFound) {
		t.Errorf("error = %v, want ErrEndpointNotFound", err)
	}
}

func TestGetEndpoint_CaseInsensitive(t *testing.T) {
	doc := loadTestDoc(t)
	detail, err := GetEndpoint(doc, "get", "/pets", -1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Method != "GET" {
		t.Errorf("method = %q, want %q (uppercase)", detail.Method, "GET")
	}
	if detail.Summary != "List all pets" {
		t.Errorf("summary = %q, want %q", detail.Summary, "List all pets")
	}
}

// ---------------------------------------------------------------------------
// GetSchema tests
// ---------------------------------------------------------------------------

func TestGetSchema_Pet(t *testing.T) {
	doc := loadTestDoc(t)
	detail, err := GetSchema(doc, "Pet", -1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if detail.Name != "Pet" {
		t.Errorf("name = %q, want %q", detail.Name, "Pet")
	}

	schema, ok := detail.Schema.(map[string]interface{})
	if !ok {
		t.Fatalf("schema is %T, want map[string]interface{}", detail.Schema)
	}

	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("schema.properties is not a map")
	}

	expectedProps := []string{"id", "name", "tag", "status", "owner"}
	for _, prop := range expectedProps {
		if _, exists := props[prop]; !exists {
			t.Errorf("expected property %q not found in Pet schema", prop)
		}
	}
}

func TestGetSchema_Error(t *testing.T) {
	doc := loadTestDoc(t)
	detail, err := GetSchema(doc, "Error", -1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	schema, ok := detail.Schema.(map[string]interface{})
	if !ok {
		t.Fatalf("schema is %T, want map[string]interface{}", detail.Schema)
	}

	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("schema.properties is not a map")
	}

	if _, exists := props["code"]; !exists {
		t.Error("expected property 'code' not found in Error schema")
	}
	if _, exists := props["message"]; !exists {
		t.Error("expected property 'message' not found in Error schema")
	}
}

func TestGetSchema_Owner(t *testing.T) {
	doc := loadTestDoc(t)
	detail, err := GetSchema(doc, "Owner", -1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	schema, ok := detail.Schema.(map[string]interface{})
	if !ok {
		t.Fatalf("schema is %T, want map[string]interface{}", detail.Schema)
	}

	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("schema.properties is not a map")
	}

	emailProp, exists := props["email"]
	if !exists {
		t.Fatal("expected property 'email' not found in Owner schema")
	}

	emailMap, ok := emailProp.(map[string]interface{})
	if !ok {
		t.Fatalf("email property is %T, want map[string]interface{}", emailProp)
	}

	if emailMap["format"] != "email" {
		t.Errorf("email format = %v, want %q", emailMap["format"], "email")
	}
}

func TestGetSchema_NotFound(t *testing.T) {
	doc := loadTestDoc(t)
	_, err := GetSchema(doc, "NonExistent", -1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrSchemaNotFound) {
		t.Errorf("error = %v, want ErrSchemaNotFound", err)
	}
}

func TestGetSchema_NilDoc(t *testing.T) {
	_, err := GetSchema(nil, "Pet", -1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrSchemaNotFound) {
		t.Errorf("error = %v, want ErrSchemaNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// resolveSchema tests (unexported, accessible within same package)
// ---------------------------------------------------------------------------

func TestResolveSchema_SimpleString(t *testing.T) {
	schema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"string"},
		},
	}

	result := resolveSchema(schema, 0, defaultMaxDepth, map[string]bool{})
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is %T, want map[string]interface{}", result)
	}
	if m["type"] != "string" {
		t.Errorf("type = %v, want %q", m["type"], "string")
	}
}

func TestResolveSchema_WithFormat(t *testing.T) {
	schema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:   &openapi3.Types{"integer"},
			Format: "int64",
		},
	}

	result := resolveSchema(schema, 0, defaultMaxDepth, map[string]bool{})
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is %T, want map[string]interface{}", result)
	}
	if m["type"] != "integer" {
		t.Errorf("type = %v, want %q", m["type"], "integer")
	}
	if m["format"] != "int64" {
		t.Errorf("format = %v, want %q", m["format"], "int64")
	}
}

func TestResolveSchema_Nil(t *testing.T) {
	result := resolveSchema(nil, 0, defaultMaxDepth, map[string]bool{})
	if result != nil {
		t.Errorf("result = %v, want nil", result)
	}
}

func TestResolveSchema_DepthLimit(t *testing.T) {
	schema := &openapi3.SchemaRef{
		Ref: "#/components/schemas/Circular",
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
		},
	}

	// At/over the depth limit a named schema collapses to a short $ref marker
	// (the name is preserved so it can be fetched via get_schema).
	result := resolveSchema(schema, 10, defaultMaxDepth, map[string]bool{})
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is %T, want map[string]interface{}", result)
	}
	if m["$ref"] != "Circular" {
		t.Errorf("$ref = %v, want %q", m["$ref"], "Circular")
	}
}

func TestResolveSchema_DedupRepeatedRef(t *testing.T) {
	// A shared schema referenced twice should expand once, then collapse to
	// a $ref marker on the second occurrence within the same tree.
	address := &openapi3.SchemaRef{
		Ref: "#/components/schemas/Address",
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			Properties: openapi3.Schemas{
				"city": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
			},
		},
	}
	root := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			Properties: openapi3.Schemas{
				"billing":  address,
				"shipping": address,
			},
		},
	}

	result := resolveRoot(root, defaultMaxDepth)
	m := result.(map[string]interface{})
	props := m["properties"].(map[string]interface{})

	// "billing" sorts before "shipping", so billing is expanded, shipping is a ref.
	billing := props["billing"].(map[string]interface{})
	if _, hasProps := billing["properties"]; !hasProps {
		t.Errorf("expected first occurrence (billing) to be expanded, got %v", billing)
	}
	shipping := props["shipping"].(map[string]interface{})
	if shipping["$ref"] != "Address" {
		t.Errorf("expected second occurrence (shipping) to be $ref(Address), got %v", shipping)
	}
}

func TestResolveSchema_OmitsExampleAndDefault(t *testing.T) {
	schema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:    &openapi3.Types{"string"},
			Default: "hello",
			Example: "world",
		},
	}

	result := resolveSchema(schema, 0, defaultMaxDepth, map[string]bool{})
	m := result.(map[string]interface{})
	if _, has := m["example"]; has {
		t.Error("example should be omitted to save tokens")
	}
	if _, has := m["default"]; has {
		t.Error("default should be omitted to save tokens")
	}
}

func TestCleanDescription(t *testing.T) {
	cases := map[string]string{
		"Gets or sets the user identifier": "user identifier",
		"Gets or sets email":               "email",
		"The order total":                  "The order total",
		"":                                 "",
	}
	for in, want := range cases {
		if got := cleanDescription(in); got != want {
			t.Errorf("cleanDescription(%q) = %q, want %q", in, got, want)
		}
	}
}
