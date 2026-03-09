package generator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// GenerateGo generates Go type definitions from the given schemas.
func GenerateGo(schemas []*NamedSchema) string {
	var b strings.Builder
	b.WriteString("// Generated from OpenAPI spec\n\n")

	for i, ns := range schemas {
		if ns.Schema == nil || ns.Schema.Value == nil {
			continue
		}
		if i > 0 {
			b.WriteString("\n")
		}
		goSchema(&b, ns.Name, ns.Schema, make(map[string]bool))
	}

	return b.String()
}

func goSchema(b *strings.Builder, name string, schema *openapi3.SchemaRef, visited map[string]bool) {
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
		goEnumType(b, name, s)
		return
	}

	// Handle oneOf/anyOf — Go doesn't have union types, use interface{}
	if len(s.OneOf) > 0 || len(s.AnyOf) > 0 {
		fmt.Fprintf(b, "// %s represents a union type — use type assertion at runtime.\n", name)
		fmt.Fprintf(b, "type %s interface{}\n", name)
		return
	}

	// Handle allOf as merged struct
	if len(s.AllOf) > 0 {
		merged := mergeAllOfProperties(s.AllOf)
		if merged != nil {
			goStructType(b, name, merged)
			return
		}
		// If we can't merge (has $refs), emit embedded structs
		goAllOfStruct(b, name, s.AllOf)
		return
	}

	// Handle object types
	if isObjectSchema(s) {
		goStructType(b, name, s)
		return
	}

	// Handle array types
	if isArrayType(s) {
		itemType := goTypeRef(s.Items, false)
		fmt.Fprintf(b, "type %s []%s\n", name, itemType)
		return
	}

	// Handle additionalProperties
	if s.AdditionalProperties.Schema != nil {
		valType := goTypeRef(s.AdditionalProperties.Schema, false)
		fmt.Fprintf(b, "type %s map[string]%s\n", name, valType)
		return
	}

	// Fallback: simple type alias
	fmt.Fprintf(b, "type %s %s\n", name, goPrimitiveType(s, false))
}

func goStructType(b *strings.Builder, name string, s *openapi3.Schema) {
	requiredSet := make(map[string]bool, len(s.Required))
	for _, r := range s.Required {
		requiredSet[r] = true
	}

	fmt.Fprintf(b, "type %s struct {\n", name)

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

		isRequired := requiredSet[pn]
		isNullable := propRef.Value.Nullable
		usePointer := !isRequired || isNullable

		fieldName := goFieldName(pn)
		fieldType := goTypeRef(propRef, usePointer)

		omitempty := ""
		if !isRequired {
			omitempty = ",omitempty"
		}

		fmt.Fprintf(b, "\t%s %s `json:\"%s%s\"`\n", fieldName, fieldType, pn, omitempty)
	}

	b.WriteString("}\n")
}

func goAllOfStruct(b *strings.Builder, name string, refs openapi3.SchemaRefs) {
	fmt.Fprintf(b, "type %s struct {\n", name)
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		if rn := refName(ref); rn != "" {
			fmt.Fprintf(b, "\t%s\n", rn)
		}
	}
	b.WriteString("}\n")
}

func goEnumType(b *strings.Builder, name string, s *openapi3.Schema) {
	baseType := goPrimitiveType(s, false)
	fmt.Fprintf(b, "type %s %s\n\n", name, baseType)
	fmt.Fprintf(b, "const (\n")
	for _, v := range s.Enum {
		constName := name + goFieldName(fmt.Sprint(v))
		fmt.Fprintf(b, "\t%s %s = %q\n", constName, name, fmt.Sprint(v))
	}
	fmt.Fprintf(b, ")\n")
}

// goTypeRef returns the Go type for a schema reference.
func goTypeRef(schema *openapi3.SchemaRef, pointer bool) string {
	if schema == nil {
		return "interface{}"
	}

	// Named reference
	if rn := refName(schema); rn != "" {
		if pointer {
			return "*" + rn
		}
		return rn
	}

	if schema.Value == nil {
		return "interface{}"
	}

	s := schema.Value

	// oneOf/anyOf
	if len(s.OneOf) > 0 || len(s.AnyOf) > 0 {
		return "interface{}"
	}

	// Array
	if isArrayType(s) {
		itemType := goTypeRef(s.Items, false)
		return "[]" + itemType
	}

	// additionalProperties → map
	if s.AdditionalProperties.Schema != nil {
		valType := goTypeRef(s.AdditionalProperties.Schema, false)
		return "map[string]" + valType
	}

	return goPrimitiveType(s, pointer)
}

func goPrimitiveType(s *openapi3.Schema, pointer bool) string {
	if s.Type == nil {
		return "interface{}"
	}
	types := s.Type.Slice()
	if len(types) == 0 {
		return "interface{}"
	}

	var t string
	switch types[0] {
	case "string":
		t = "string"
	case "integer":
		switch s.Format {
		case "int32":
			t = "int32"
		case "int64":
			t = "int64"
		default:
			t = "int"
		}
	case "number":
		switch s.Format {
		case "float":
			t = "float32"
		case "double":
			t = "float64"
		default:
			t = "float64"
		}
	case "boolean":
		t = "bool"
	case "object":
		return "map[string]interface{}"
	case "array":
		if s.Items != nil {
			return "[]" + goTypeRef(s.Items, false)
		}
		return "[]interface{}"
	default:
		return "interface{}"
	}

	if pointer {
		return "*" + t
	}
	return t
}

// goFieldName converts a JSON field name to Go exported field name.
func goFieldName(s string) string {
	// Split on common separators
	words := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})
	var b strings.Builder
	for _, w := range words {
		if len(w) == 0 {
			continue
		}
		// Handle common acronyms
		upper := strings.ToUpper(w)
		if isCommonAcronym(upper) {
			b.WriteString(upper)
		} else {
			b.WriteString(strings.ToUpper(w[:1]))
			if len(w) > 1 {
				b.WriteString(w[1:])
			}
		}
	}
	return b.String()
}

func isCommonAcronym(s string) bool {
	acronyms := map[string]bool{
		"ID": true, "URL": true, "URI": true, "API": true,
		"HTTP": true, "HTTPS": true, "JSON": true, "XML": true,
		"HTML": true, "CSS": true, "SQL": true, "SSH": true,
		"IP": true, "TCP": true, "UDP": true, "DNS": true,
	}
	return acronyms[s]
}
