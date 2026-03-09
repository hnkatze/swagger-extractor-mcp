package generator

import (
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestCollectSchemaByName_NotFound(t *testing.T) {
	doc := &openapi3.T{
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{},
		},
	}
	_, err := CollectSchemaByName(doc, "Missing")
	if err == nil {
		t.Fatal("expected error for missing schema")
	}
}

func TestCollectSchemaByName_Simple(t *testing.T) {
	doc := buildTestDoc()
	schemas, err := CollectSchemaByName(doc, "Pet")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(schemas) == 0 {
		t.Fatal("expected at least one schema")
	}
	if schemas[0].Name != "Pet" {
		t.Errorf("first schema name = %q, want Pet", schemas[0].Name)
	}
}

func TestCollectEndpointSchemas_NotFound(t *testing.T) {
	doc := &openapi3.T{}
	_, err := CollectEndpointSchemas(doc, "GET", "/missing")
	if err == nil {
		t.Fatal("expected error for missing endpoint")
	}
}

func TestCollectEndpointSchemas_WithResponse(t *testing.T) {
	doc := buildTestDoc()
	schemas, err := CollectEndpointSchemas(doc, "GET", "/pets")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(schemas) == 0 {
		t.Fatal("expected at least one schema from endpoint")
	}
}

func TestToPascalCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"get users list", "GetUsersList"},
		{"hello world", "HelloWorld"},
		{"a", "A"},
		{"", ""},
	}
	for _, tt := range tests {
		got := toPascalCase(tt.input)
		if got != tt.want {
			t.Errorf("toPascalCase(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRefName(t *testing.T) {
	s := &openapi3.SchemaRef{Ref: "#/components/schemas/User"}
	if got := refName(s); got != "User" {
		t.Errorf("refName = %q, want User", got)
	}

	s2 := &openapi3.SchemaRef{}
	if got := refName(s2); got != "" {
		t.Errorf("refName = %q, want empty", got)
	}
}

func TestGenerateTypeScript_Simple(t *testing.T) {
	doc := buildTestDoc()
	schemas, err := CollectSchemaByName(doc, "Pet")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := GenerateTypeScript(schemas)
	if !strings.Contains(output, "export interface Pet") {
		t.Errorf("TypeScript output missing 'export interface Pet':\n%s", output)
	}
	if !strings.Contains(output, "name: string") {
		t.Errorf("TypeScript output missing 'name: string':\n%s", output)
	}
	if !strings.Contains(output, "// Generated from OpenAPI spec") {
		t.Error("TypeScript output missing header comment")
	}
}

func TestGenerateGo_Simple(t *testing.T) {
	doc := buildTestDoc()
	schemas, err := CollectSchemaByName(doc, "Pet")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := GenerateGo(schemas)
	if !strings.Contains(output, "type Pet struct") {
		t.Errorf("Go output missing 'type Pet struct':\n%s", output)
	}
	if !strings.Contains(output, "json:\"name\"") {
		t.Errorf("Go output missing json tag for name:\n%s", output)
	}
	if !strings.Contains(output, "// Generated from OpenAPI spec") {
		t.Error("Go output missing header comment")
	}
}

func TestGenerateTypeScript_Enum(t *testing.T) {
	schema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"string"},
			Enum: []interface{}{"active", "inactive", "suspended"},
		},
	}
	schemas := []*NamedSchema{{Name: "Status", Schema: schema}}
	output := GenerateTypeScript(schemas)
	if !strings.Contains(output, `"active"`) {
		t.Errorf("TypeScript enum output missing 'active':\n%s", output)
	}
	if !strings.Contains(output, "export type Status") {
		t.Errorf("TypeScript enum output missing type declaration:\n%s", output)
	}
}

func TestGenerateGo_Enum(t *testing.T) {
	schema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"string"},
			Enum: []interface{}{"active", "inactive"},
		},
	}
	schemas := []*NamedSchema{{Name: "Status", Schema: schema}}
	output := GenerateGo(schemas)
	if !strings.Contains(output, "type Status string") {
		t.Errorf("Go enum output missing type declaration:\n%s", output)
	}
	if !strings.Contains(output, "StatusActive") {
		t.Errorf("Go enum output missing const:\n%s", output)
	}
}

func TestGenerateTypeScript_Nullable(t *testing.T) {
	schema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{},
			Properties: openapi3.Schemas{
				"name": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:     &openapi3.Types{"string"},
						Nullable: true,
					},
				},
			},
		},
	}
	schemas := []*NamedSchema{{Name: "User", Schema: schema}}
	output := GenerateTypeScript(schemas)
	if !strings.Contains(output, "| null") {
		t.Errorf("TypeScript output missing nullable union:\n%s", output)
	}
}

func TestGenerateGo_OptionalPointer(t *testing.T) {
	schema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"id"},
			Properties: openapi3.Schemas{
				"id": &openapi3.SchemaRef{
					Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}, Format: "int64"},
				},
				"name": &openapi3.SchemaRef{
					Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
				},
			},
		},
	}
	schemas := []*NamedSchema{{Name: "Item", Schema: schema}}
	output := GenerateGo(schemas)
	if !strings.Contains(output, "*string") {
		t.Errorf("Go output missing pointer for optional field:\n%s", output)
	}
	if !strings.Contains(output, "int64") {
		t.Errorf("Go output missing int64 type:\n%s", output)
	}
}

func TestGoFieldName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"user_name", "UserName"},
		{"id", "ID"},
		{"created_at", "CreatedAt"},
		{"api_url", "APIURL"},
	}
	for _, tt := range tests {
		got := goFieldName(tt.input)
		if got != tt.want {
			t.Errorf("goFieldName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// buildTestDoc creates a minimal OpenAPI doc for testing.
func buildTestDoc() *openapi3.T {
	petSchema := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"name"},
			Properties: openapi3.Schemas{
				"name": &openapi3.SchemaRef{
					Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
				},
				"tag": &openapi3.SchemaRef{
					Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
				},
			},
		},
	}

	description := "A list of pets"
	paths := openapi3.NewPaths()
	paths.Set("/pets", &openapi3.PathItem{
		Get: &openapi3.Operation{
			OperationID: "listPets",
			Responses: openapi3.NewResponses(
				openapi3.WithStatus(200, &openapi3.ResponseRef{
					Value: &openapi3.Response{
						Description: &description,
						Content: openapi3.Content{
							"application/json": &openapi3.MediaType{
								Schema: &openapi3.SchemaRef{
									Ref:   "#/components/schemas/Pet",
									Value: petSchema.Value,
								},
							},
						},
					},
				}),
			),
		},
	})

	return &openapi3.T{
		Paths: paths,
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{
				"Pet": petSchema,
			},
		},
	}
}
