package generator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// GenerateTypeScript generates TypeScript interface definitions from the given schemas.
func GenerateTypeScript(schemas []*NamedSchema) string {
	var b strings.Builder
	b.WriteString("// Generated from OpenAPI spec\n\n")

	for i, ns := range schemas {
		if ns.Schema == nil || ns.Schema.Value == nil {
			continue
		}
		if i > 0 {
			b.WriteString("\n")
		}
		tsSchema(&b, ns.Name, ns.Schema, make(map[string]bool), 0)
	}

	return b.String()
}

func tsSchema(b *strings.Builder, name string, schema *openapi3.SchemaRef, visited map[string]bool, depth int) {
	if schema == nil || schema.Value == nil {
		return
	}

	// Circular reference protection
	key := schema.Ref
	if key == "" {
		key = "anon:" + name
	}
	if visited[key] {
		fmt.Fprintf(b, "// circular reference: %s\n", name)
		return
	}
	visited[key] = true
	defer func() { delete(visited, key) }()

	s := schema.Value

	// Handle enum types
	if len(s.Enum) > 0 {
		fmt.Fprintf(b, "export type %s = %s;\n", name, tsEnumValues(s.Enum))
		return
	}

	// Handle oneOf/anyOf as union types
	if len(s.OneOf) > 0 {
		fmt.Fprintf(b, "export type %s = %s;\n", name, tsUnionType(s.OneOf))
		return
	}
	if len(s.AnyOf) > 0 {
		fmt.Fprintf(b, "export type %s = %s;\n", name, tsUnionType(s.AnyOf))
		return
	}

	// Handle allOf as intersection
	if len(s.AllOf) > 0 {
		merged := mergeAllOfProperties(s.AllOf)
		if merged != nil {
			tsObjectInterface(b, name, merged, schema)
			return
		}
		fmt.Fprintf(b, "export type %s = %s;\n", name, tsIntersectionType(s.AllOf))
		return
	}

	// Handle object types (or schemas with properties)
	if isObjectSchema(s) {
		tsObjectInterface(b, name, s, schema)
		return
	}

	// Handle array types
	if isArrayType(s) {
		itemType := tsTypeRef(s.Items)
		fmt.Fprintf(b, "export type %s = %s[];\n", name, itemType)
		return
	}

	// Handle additionalProperties (Record type)
	if s.AdditionalProperties.Schema != nil {
		valType := tsTypeRef(s.AdditionalProperties.Schema)
		fmt.Fprintf(b, "export type %s = Record<string, %s>;\n", name, valType)
		return
	}

	// Fallback: simple type alias
	fmt.Fprintf(b, "export type %s = %s;\n", name, tsPrimitiveType(s))
}

func tsObjectInterface(b *strings.Builder, name string, s *openapi3.Schema, schema *openapi3.SchemaRef) {
	requiredSet := make(map[string]bool, len(s.Required))
	for _, r := range s.Required {
		requiredSet[r] = true
	}

	fmt.Fprintf(b, "export interface %s {\n", name)

	// Sort property names for deterministic output
	propNames := make([]string, 0, len(s.Properties))
	for pn := range s.Properties {
		propNames = append(propNames, pn)
	}
	sort.Strings(propNames)

	for _, pn := range propNames {
		propRef := s.Properties[pn]
		if propRef == nil || propRef.Value == nil {
			continue
		}

		optional := ""
		if !requiredSet[pn] {
			optional = "?"
		}

		propType := tsTypeRef(propRef)
		if propRef.Value.Nullable {
			propType += " | null"
		}

		fmt.Fprintf(b, "  %s%s: %s;\n", pn, optional, propType)
	}

	// Handle additionalProperties on object
	if s.AdditionalProperties.Schema != nil {
		valType := tsTypeRef(s.AdditionalProperties.Schema)
		fmt.Fprintf(b, "  [key: string]: %s;\n", valType)
	}

	b.WriteString("}\n")
}

// tsTypeRef returns the TypeScript type for a schema reference.
func tsTypeRef(schema *openapi3.SchemaRef) string {
	if schema == nil {
		return "unknown"
	}

	// Named reference
	if rn := refName(schema); rn != "" {
		return rn
	}

	if schema.Value == nil {
		return "unknown"
	}

	s := schema.Value

	// Enum inline
	if len(s.Enum) > 0 {
		return tsEnumValues(s.Enum)
	}

	// oneOf/anyOf
	if len(s.OneOf) > 0 {
		return tsUnionType(s.OneOf)
	}
	if len(s.AnyOf) > 0 {
		return tsUnionType(s.AnyOf)
	}

	// allOf
	if len(s.AllOf) > 0 {
		return tsIntersectionType(s.AllOf)
	}

	// Array
	if isArrayType(s) {
		itemType := tsTypeRef(s.Items)
		return itemType + "[]"
	}

	// additionalProperties → Record
	if s.AdditionalProperties.Schema != nil {
		valType := tsTypeRef(s.AdditionalProperties.Schema)
		return "Record<string, " + valType + ">"
	}

	return tsPrimitiveType(s)
}

func tsPrimitiveType(s *openapi3.Schema) string {
	if s.Type == nil {
		return "unknown"
	}
	types := s.Type.Slice()
	if len(types) == 0 {
		return "unknown"
	}

	switch types[0] {
	case "string":
		return "string"
	case "integer", "number":
		return "number"
	case "boolean":
		return "boolean"
	case "object":
		return "Record<string, unknown>"
	case "array":
		if s.Items != nil {
			return tsTypeRef(s.Items) + "[]"
		}
		return "unknown[]"
	default:
		return "unknown"
	}
}

func tsEnumValues(values []interface{}) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, fmt.Sprintf("%q", fmt.Sprint(v)))
	}
	return strings.Join(parts, " | ")
}

func tsUnionType(refs openapi3.SchemaRefs) string {
	parts := make([]string, 0, len(refs))
	for _, ref := range refs {
		parts = append(parts, tsTypeRef(ref))
	}
	return strings.Join(parts, " | ")
}

func tsIntersectionType(refs openapi3.SchemaRefs) string {
	parts := make([]string, 0, len(refs))
	for _, ref := range refs {
		parts = append(parts, tsTypeRef(ref))
	}
	return strings.Join(parts, " & ")
}

// mergeAllOfProperties attempts to merge allOf schemas into a single schema.
// Returns nil if the schemas contain non-object types that can't be merged.
func mergeAllOfProperties(refs openapi3.SchemaRefs) *openapi3.Schema {
	merged := &openapi3.Schema{
		Properties: make(openapi3.Schemas),
	}
	requiredSet := make(map[string]bool)

	for _, ref := range refs {
		if ref == nil || ref.Value == nil {
			continue
		}
		s := ref.Value
		// If it's a $ref to a named schema, we can't easily merge inline
		if ref.Ref != "" {
			return nil
		}
		for name, prop := range s.Properties {
			merged.Properties[name] = prop
		}
		for _, r := range s.Required {
			requiredSet[r] = true
		}
	}

	if len(merged.Properties) == 0 {
		return nil
	}

	for r := range requiredSet {
		merged.Required = append(merged.Required, r)
	}
	sort.Strings(merged.Required)

	return merged
}

func isObjectSchema(s *openapi3.Schema) bool {
	if len(s.Properties) > 0 {
		return true
	}
	if s.Type == nil {
		return false
	}
	types := s.Type.Slice()
	for _, t := range types {
		if t == "object" {
			return true
		}
	}
	return false
}

func isArrayType(s *openapi3.Schema) bool {
	if s.Type == nil {
		return false
	}
	types := s.Type.Slice()
	for _, t := range types {
		if t == "array" {
			return true
		}
	}
	return false
}
