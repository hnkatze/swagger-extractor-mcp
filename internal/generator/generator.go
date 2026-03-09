package generator

import (
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// NamedSchema pairs a schema with its name for code generation.
type NamedSchema struct {
	Name   string
	Schema *openapi3.SchemaRef
}

// CollectSchemaByName finds a named schema in components and collects it
// along with all transitively referenced schemas.
func CollectSchemaByName(doc *openapi3.T, name string) ([]*NamedSchema, error) {
	if doc == nil || doc.Components == nil || doc.Components.Schemas == nil {
		return nil, fmt.Errorf("schema %q not found", name)
	}

	schemaRef, ok := doc.Components.Schemas[name]
	if !ok || schemaRef == nil {
		return nil, fmt.Errorf("schema %q not found", name)
	}

	visited := make(map[string]bool)
	var result []*NamedSchema
	collectRefs(doc, name, schemaRef, visited, &result)
	return result, nil
}

// CollectEndpointSchemas collects all schemas referenced by an endpoint's
// request body and responses, including transitive references.
func CollectEndpointSchemas(doc *openapi3.T, method, path string) ([]*NamedSchema, error) {
	if doc == nil || doc.Paths == nil {
		return nil, fmt.Errorf("endpoint %s %s not found", strings.ToUpper(method), path)
	}

	pathItem := doc.Paths.Value(path)
	if pathItem == nil {
		return nil, fmt.Errorf("endpoint %s %s not found", strings.ToUpper(method), path)
	}

	op := getOperationByMethod(pathItem, strings.ToUpper(method))
	if op == nil {
		return nil, fmt.Errorf("endpoint %s %s not found", strings.ToUpper(method), path)
	}

	visited := make(map[string]bool)
	var result []*NamedSchema

	// Collect from request body
	if op.RequestBody != nil && op.RequestBody.Value != nil {
		for _, mt := range op.RequestBody.Value.Content {
			if mt == nil || mt.Schema == nil {
				continue
			}
			name := refName(mt.Schema)
			if name == "" {
				name = syntheticName(op, method, path, "RequestBody")
			}
			collectRefs(doc, name, mt.Schema, visited, &result)
		}
	}

	// Collect from responses
	if op.Responses != nil {
		for code, ref := range op.Responses.Map() {
			if ref == nil || ref.Value == nil {
				continue
			}
			for _, mt := range ref.Value.Content {
				if mt == nil || mt.Schema == nil {
					continue
				}
				name := refName(mt.Schema)
				if name == "" {
					name = syntheticName(op, method, path, "Response"+code)
				}
				collectRefs(doc, name, mt.Schema, visited, &result)
			}
		}
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no schemas found for %s %s", strings.ToUpper(method), path)
	}

	return result, nil
}

// collectRefs recursively collects a schema and its transitive $ref dependencies.
func collectRefs(doc *openapi3.T, name string, schema *openapi3.SchemaRef, visited map[string]bool, result *[]*NamedSchema) {
	if schema == nil {
		return
	}

	// Use ref as key for dedup; for anonymous schemas use the name
	key := schema.Ref
	if key == "" {
		key = "anon:" + name
	}
	if visited[key] {
		return
	}
	visited[key] = true

	// Add this schema
	*result = append(*result, &NamedSchema{Name: name, Schema: schema})

	s := schema.Value
	if s == nil {
		return
	}

	// Walk properties
	for _, propRef := range s.Properties {
		if propRef == nil {
			continue
		}
		if rn := refName(propRef); rn != "" {
			collectRefs(doc, rn, propRef, visited, result)
		} else if propRef.Value != nil && propRef.Value.Items != nil {
			// Array with items that might be a ref
			if rn := refName(propRef.Value.Items); rn != "" {
				collectRefs(doc, rn, propRef.Value.Items, visited, result)
			}
		}
	}

	// Walk items (array)
	if s.Items != nil {
		if rn := refName(s.Items); rn != "" {
			collectRefs(doc, rn, s.Items, visited, result)
		}
	}

	// Walk allOf, oneOf, anyOf
	for _, refs := range []openapi3.SchemaRefs{s.AllOf, s.OneOf, s.AnyOf} {
		for _, ref := range refs {
			if ref == nil {
				continue
			}
			if rn := refName(ref); rn != "" {
				collectRefs(doc, rn, ref, visited, result)
			}
		}
	}

	// Walk additionalProperties
	if s.AdditionalProperties.Schema != nil {
		if rn := refName(s.AdditionalProperties.Schema); rn != "" {
			collectRefs(doc, rn, s.AdditionalProperties.Schema, visited, result)
		}
	}
}

// refName extracts the schema name from a $ref like "#/components/schemas/Pet".
func refName(schema *openapi3.SchemaRef) string {
	if schema == nil || schema.Ref == "" {
		return ""
	}
	parts := strings.Split(schema.Ref, "/")
	return parts[len(parts)-1]
}

// syntheticName generates a name for anonymous schemas based on context.
func syntheticName(op *openapi3.Operation, method, path, suffix string) string {
	if op.OperationID != "" {
		return toPascalCase(op.OperationID) + suffix
	}
	// Build from method + path segments
	clean := strings.NewReplacer("/", " ", "{", "", "}", "", "-", " ", "_", " ").Replace(path)
	return toPascalCase(strings.ToLower(method)+" "+clean) + suffix
}

// toPascalCase converts "get users list" to "GetUsersList".
func toPascalCase(s string) string {
	words := strings.Fields(s)
	var b strings.Builder
	for _, w := range words {
		if len(w) == 0 {
			continue
		}
		b.WriteString(strings.ToUpper(w[:1]))
		if len(w) > 1 {
			b.WriteString(w[1:])
		}
	}
	return b.String()
}

// getOperationByMethod returns the operation for a given uppercase HTTP method.
func getOperationByMethod(item *openapi3.PathItem, method string) *openapi3.Operation {
	switch method {
	case "GET":
		return item.Get
	case "POST":
		return item.Post
	case "PUT":
		return item.Put
	case "DELETE":
		return item.Delete
	case "PATCH":
		return item.Patch
	case "HEAD":
		return item.Head
	case "OPTIONS":
		return item.Options
	default:
		return nil
	}
}
