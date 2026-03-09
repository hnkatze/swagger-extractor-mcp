package formatter

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hnkatze/swagger-mcp-go/internal/types"
)

// Format dispatches to JSON or TOON based on the format parameter.
func Format(data interface{}, format types.OutputFormat) (string, error) {
	switch format {
	case types.FormatJSON:
		return FormatJSON(data)
	case types.FormatTOON:
		return FormatTOON(data), nil
	default:
		return "", fmt.Errorf("unsupported format: %s", format)
	}
}

// FormatTOON is a generic TOON formatter for any data structure.
// It recursively formats maps and slices with indentation.
func FormatTOON(data interface{}) string {
	return toonValue(data, 0)
}

// FormatEndpointTOON formats an endpoint detail in TOON notation.
func FormatEndpointTOON(detail *types.EndpointDetail) string {
	var b strings.Builder

	// First line: METHOD /path
	b.WriteString(detail.Method)
	b.WriteString(" ")
	b.WriteString(detail.Path)
	b.WriteString("\n")

	if detail.Summary != "" {
		b.WriteString("summary: ")
		b.WriteString(toonString(detail.Summary))
		b.WriteString("\n")
	}

	if detail.Description != "" {
		b.WriteString("description: ")
		b.WriteString(toonString(detail.Description))
		b.WriteString("\n")
	}

	if len(detail.Tags) > 0 {
		b.WriteString("tags: ")
		b.WriteString(strings.Join(detail.Tags, ", "))
		b.WriteString("\n")
	}

	if detail.Deprecated {
		b.WriteString("deprecated: true\n")
	}

	// Parameters as compact list
	if len(detail.Parameters) > 0 {
		b.WriteString("parameters:\n")
		for _, p := range detail.Parameters {
			b.WriteString("  - ")
			b.WriteString(p.Name)
			reqStr := "optional"
			if p.Required {
				reqStr = "required"
			}
			b.WriteString(" (")
			b.WriteString(p.In)
			b.WriteString(", ")
			b.WriteString(reqStr)
			b.WriteString(")")
			schemaType := extractSchemaType(p.Schema)
			if schemaType != "" {
				b.WriteString(": ")
				b.WriteString(schemaType)
			}
			if p.Description != "" {
				b.WriteString(" — ")
				b.WriteString(p.Description)
			}
			b.WriteString("\n")
		}
	}

	// Request body
	if detail.RequestBody != nil {
		reqLabel := "optional"
		if detail.RequestBody.Required {
			reqLabel = "required"
		}
		b.WriteString("request_body (")
		b.WriteString(reqLabel)
		b.WriteString("):\n")
		if detail.RequestBody.Description != "" {
			b.WriteString("  description: ")
			b.WriteString(toonString(detail.RequestBody.Description))
			b.WriteString("\n")
		}
		for contentType, media := range detail.RequestBody.Content {
			b.WriteString("  ")
			b.WriteString(contentType)
			b.WriteString(":\n")
			schemaStr := toonSchema(media.Schema, 4)
			if schemaStr != "" {
				b.WriteString(schemaStr)
			}
		}
	}

	// Responses
	if len(detail.Responses) > 0 {
		b.WriteString("responses:\n")
		for _, r := range detail.Responses {
			b.WriteString("  ")
			b.WriteString(r.StatusCode)
			b.WriteString(":")
			if r.Description != "" {
				b.WriteString(" ")
				b.WriteString(r.Description)
			}
			b.WriteString("\n")
			for contentType, media := range r.Content {
				b.WriteString("    ")
				b.WriteString(contentType)
				b.WriteString(":\n")
				schemaStr := toonSchema(media.Schema, 6)
				if schemaStr != "" {
					b.WriteString(schemaStr)
				}
			}
		}
	}

	// Security
	if len(detail.Security) > 0 {
		b.WriteString("security:\n")
		for _, sec := range detail.Security {
			for name, scopes := range sec {
				b.WriteString("  - ")
				b.WriteString(name)
				if len(scopes) > 0 {
					b.WriteString(": ")
					b.WriteString(strings.Join(scopes, ", "))
				}
				b.WriteString("\n")
			}
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// FormatEndpointsTOON formats an endpoint list in TOON notation.
// One line per endpoint: "METHOD /path — summary [tag1, tag2]"
func FormatEndpointsTOON(endpoints []types.EndpointSummary) string {
	var b strings.Builder

	for i, ep := range endpoints {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(ep.Method)
		b.WriteString(" ")
		b.WriteString(ep.Path)
		if ep.Summary != "" {
			b.WriteString(" — ")
			b.WriteString(ep.Summary)
		}
		if len(ep.Tags) > 0 {
			b.WriteString(" [")
			b.WriteString(strings.Join(ep.Tags, ", "))
			b.WriteString("]")
		}
		if ep.Deprecated {
			b.WriteString(" (deprecated)")
		}
	}

	return b.String()
}

// FormatTagsTOON formats tags analysis in TOON notation.
func FormatTagsTOON(tags []types.TagSummary) string {
	var b strings.Builder

	for i, tag := range tags {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(tag.Name)
		if tag.Description != "" {
			b.WriteString(": ")
			b.WriteString(toonString(tag.Description))
		}
		b.WriteString("\n")
		b.WriteString("  endpoints: ")
		b.WriteString(fmt.Sprintf("%d", tag.EndpointCount))
		b.WriteString("\n")

		if len(tag.MethodBreakdown) > 0 {
			b.WriteString("  methods:\n")
			// Sort methods for deterministic output
			methods := make([]string, 0, len(tag.MethodBreakdown))
			for m := range tag.MethodBreakdown {
				methods = append(methods, m)
			}
			sort.Strings(methods)
			for _, m := range methods {
				b.WriteString("    ")
				b.WriteString(m)
				b.WriteString(": ")
				b.WriteString(fmt.Sprintf("%d", tag.MethodBreakdown[m]))
				b.WriteString("\n")
			}
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// FormatSchemaTOON formats a schema in TOON with simplified type notation.
func FormatSchemaTOON(schema *types.SchemaDetail) string {
	var b strings.Builder

	b.WriteString(schema.Name)
	b.WriteString(":\n")
	schemaStr := toonSchema(schema.Schema, 2)
	if schemaStr != "" {
		b.WriteString(schemaStr)
	}

	return strings.TrimRight(b.String(), "\n")
}

// FormatDiffTOON formats a diff result in TOON with +/- prefixes.
func FormatDiffTOON(diff *types.DiffResult) string {
	var b strings.Builder

	if len(diff.Added) > 0 {
		b.WriteString("added:\n")
		for _, ep := range diff.Added {
			b.WriteString("  + ")
			b.WriteString(ep.Method)
			b.WriteString(" ")
			b.WriteString(ep.Path)
			if ep.Summary != "" {
				b.WriteString(" — ")
				b.WriteString(ep.Summary)
			}
			b.WriteString("\n")
		}
	}

	if len(diff.Removed) > 0 {
		b.WriteString("removed:\n")
		for _, ep := range diff.Removed {
			b.WriteString("  - ")
			b.WriteString(ep.Method)
			b.WriteString(" ")
			b.WriteString(ep.Path)
			if ep.Summary != "" {
				b.WriteString(" — ")
				b.WriteString(ep.Summary)
			}
			b.WriteString("\n")
		}
	}

	if len(diff.Changed) > 0 {
		b.WriteString("changed:\n")
		for _, ch := range diff.Changed {
			b.WriteString("  ~ ")
			b.WriteString(ch.Method)
			b.WriteString(" ")
			b.WriteString(ch.Path)
			b.WriteString("\n")
			for _, c := range ch.Changes {
				b.WriteString("    ")
				b.WriteString(c)
				b.WriteString("\n")
			}
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// FormatListHeaderTOON returns a metadata header for truncated list results.
// Example: "# showing 50 of 790 — use tag/method/path_pattern filters to narrow results"
func FormatListHeaderTOON(total, showing int, truncated bool) string {
	if !truncated {
		return fmt.Sprintf("# %d endpoints", total)
	}
	return fmt.Sprintf("# showing %d of %d — use tag/method/path_pattern filters to narrow results", showing, total)
}

// FormatListResultTOON formats endpoints with a metadata header when truncated.
func FormatListResultTOON(result *types.ListResult) string {
	var b strings.Builder
	b.WriteString(FormatListHeaderTOON(result.Total, result.Showing, result.Truncated))
	b.WriteString("\n")
	b.WriteString(FormatEndpointsTOON(result.Endpoints))
	return b.String()
}

// FormatRefreshResultTOON formats a refresh result in TOON notation.
func FormatRefreshResultTOON(result *types.RefreshResult) string {
	var b strings.Builder

	b.WriteString("refresh: ")
	b.WriteString(result.URL)
	b.WriteString("\n")

	if result.Changed {
		b.WriteString("status: changed\n")
	} else {
		b.WriteString("status: unchanged\n")
	}

	if result.OldFingerprint != "" {
		b.WriteString("old_fingerprint: ")
		b.WriteString(result.OldFingerprint)
		b.WriteString("\n")
	}
	if result.NewFingerprint != "" {
		b.WriteString("new_fingerprint: ")
		b.WriteString(result.NewFingerprint)
		b.WriteString("\n")
	}

	b.WriteString(fmt.Sprintf("fetch_duration_ms: %d\n", result.FetchDurationMs))

	b.WriteString("spec: ")
	b.WriteString(result.Summary.Title)
	b.WriteString(" v")
	b.WriteString(result.Summary.Version)
	b.WriteString(fmt.Sprintf(" (%d endpoints, %d schemas)", result.Summary.EndpointCount, result.Summary.SchemaCount))

	return b.String()
}

// StripDescriptions removes the Description field from endpoint summaries
// to reduce token output in list views where summary is sufficient.
func StripDescriptions(endpoints []types.EndpointSummary) []types.EndpointSummary {
	stripped := make([]types.EndpointSummary, len(endpoints))
	for i, ep := range endpoints {
		stripped[i] = ep
		stripped[i].Description = ""
	}
	return stripped
}

// --- Helper functions ---

// toonValue formats a single value recursively in TOON notation.
func toonValue(v interface{}, indent int) string {
	if v == nil {
		return ""
	}

	switch val := v.(type) {
	case map[string]interface{}:
		return toonMap(val, indent)
	case []interface{}:
		return toonSlice(val, indent)
	case string:
		if val == "" {
			return ""
		}
		return toonString(val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case int:
		return fmt.Sprintf("%d", val)
	case int64:
		return fmt.Sprintf("%d", val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// toonMap formats a map as TOON with the given indent level.
func toonMap(m map[string]interface{}, indent int) string {
	if len(m) == 0 {
		return ""
	}

	prefix := strings.Repeat(" ", indent)
	var b strings.Builder

	// Sort keys for deterministic output
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := m[k]
		if v == nil {
			continue
		}

		switch val := v.(type) {
		case map[string]interface{}:
			if len(val) == 0 {
				continue
			}
			b.WriteString(prefix)
			b.WriteString(k)
			b.WriteString(":\n")
			b.WriteString(toonMap(val, indent+2))
		case []interface{}:
			if len(val) == 0 {
				continue
			}
			b.WriteString(prefix)
			b.WriteString(k)
			b.WriteString(":\n")
			b.WriteString(toonSlice(val, indent+2))
		case string:
			if val == "" {
				continue
			}
			b.WriteString(prefix)
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(toonString(val))
			b.WriteString("\n")
		case bool:
			b.WriteString(prefix)
			b.WriteString(k)
			b.WriteString(": ")
			if val {
				b.WriteString("true")
			} else {
				b.WriteString("false")
			}
			b.WriteString("\n")
		case float64:
			b.WriteString(prefix)
			b.WriteString(k)
			b.WriteString(": ")
			if val == float64(int64(val)) {
				b.WriteString(fmt.Sprintf("%d", int64(val)))
			} else {
				b.WriteString(fmt.Sprintf("%g", val))
			}
			b.WriteString("\n")
		default:
			formatted := toonValue(val, 0)
			if formatted == "" {
				continue
			}
			b.WriteString(prefix)
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(formatted)
			b.WriteString("\n")
		}
	}

	return b.String()
}

// toonSlice formats a slice as TOON with "- " prefix for each item.
func toonSlice(items []interface{}, indent int) string {
	if len(items) == 0 {
		return ""
	}

	prefix := strings.Repeat(" ", indent)
	var b strings.Builder

	for _, item := range items {
		if item == nil {
			continue
		}

		switch val := item.(type) {
		case map[string]interface{}:
			// First key-value on same line as "- ", rest indented
			keys := make([]string, 0, len(val))
			for k := range val {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			if len(keys) == 0 {
				continue
			}

			first := true
			for _, k := range keys {
				v := val[k]
				if v == nil {
					continue
				}
				if first {
					b.WriteString(prefix)
					b.WriteString("- ")
					b.WriteString(k)
					b.WriteString(": ")
					b.WriteString(toonValue(v, 0))
					b.WriteString("\n")
					first = false
				} else {
					b.WriteString(prefix)
					b.WriteString("  ")
					b.WriteString(k)
					b.WriteString(": ")
					formatted := toonValue(v, 0)
					b.WriteString(formatted)
					b.WriteString("\n")
				}
			}
		default:
			b.WriteString(prefix)
			b.WriteString("- ")
			b.WriteString(toonValue(item, 0))
			b.WriteString("\n")
		}
	}

	return b.String()
}

// toonSchema formats a schema object in simplified TOON notation.
func toonSchema(schema interface{}, indent int) string {
	if schema == nil {
		return ""
	}

	prefix := strings.Repeat(" ", indent)

	m, ok := schema.(map[string]interface{})
	if !ok {
		return prefix + fmt.Sprintf("%v", schema) + "\n"
	}

	// Determine the schema type
	schemaType, _ := m["type"].(string)

	// Handle enum
	if enumVals, hasEnum := m["enum"]; hasEnum {
		if enumSlice, ok := enumVals.([]interface{}); ok {
			parts := make([]string, 0, len(enumSlice))
			for _, e := range enumSlice {
				parts = append(parts, fmt.Sprintf("%v", e))
			}
			return prefix + "enum(" + strings.Join(parts, ", ") + ")\n"
		}
	}

	// Handle array type
	if schemaType == "array" {
		items, hasItems := m["items"]
		if hasItems {
			itemType := extractSchemaType(items)
			if itemType != "" {
				return prefix + "[]" + itemType + "\n"
			}
			// Complex item type
			result := prefix + "[]:\n"
			result += toonSchema(items, indent+2)
			return result
		}
		return prefix + "[]\n"
	}

	// Handle object type with properties
	if schemaType == "object" || hasKey(m, "properties") {
		props, hasProps := m["properties"]
		if !hasProps {
			return prefix + "object\n"
		}

		propsMap, ok := props.(map[string]interface{})
		if !ok {
			return prefix + "object\n"
		}

		// Get required fields
		requiredSet := make(map[string]bool)
		if req, hasReq := m["required"]; hasReq {
			if reqSlice, ok := req.([]interface{}); ok {
				for _, r := range reqSlice {
					if s, ok := r.(string); ok {
						requiredSet[s] = true
					}
				}
			}
		}

		// Sort property keys for deterministic output
		propKeys := make([]string, 0, len(propsMap))
		for k := range propsMap {
			propKeys = append(propKeys, k)
		}
		sort.Strings(propKeys)

		var b strings.Builder
		for _, k := range propKeys {
			v := propsMap[k]
			keyLabel := k
			if requiredSet[k] {
				keyLabel += "*"
			}

			propType := extractSchemaType(v)
			if propType != "" {
				b.WriteString(prefix)
				b.WriteString(keyLabel)
				b.WriteString(": ")
				b.WriteString(propType)
				b.WriteString("\n")
			} else {
				// Complex nested schema
				b.WriteString(prefix)
				b.WriteString(keyLabel)
				b.WriteString(":\n")
				b.WriteString(toonSchema(v, indent+2))
			}
		}
		return b.String()
	}

	// Simple type
	if schemaType != "" {
		format, _ := m["format"].(string)
		if format != "" {
			return prefix + schemaType + "(" + format + ")\n"
		}
		return prefix + schemaType + "\n"
	}

	// Fallback: handle $ref or other structures
	if ref, hasRef := m["$ref"]; hasRef {
		refStr, _ := ref.(string)
		// Extract the schema name from the ref path
		parts := strings.Split(refStr, "/")
		name := parts[len(parts)-1]
		return prefix + "$ref(" + name + ")\n"
	}

	// allOf, oneOf, anyOf
	for _, combo := range []string{"allOf", "oneOf", "anyOf"} {
		if items, has := m[combo]; has {
			if itemSlice, ok := items.([]interface{}); ok {
				var b strings.Builder
				b.WriteString(prefix)
				b.WriteString(combo)
				b.WriteString(":\n")
				for _, item := range itemSlice {
					b.WriteString(toonSchema(item, indent+2))
				}
				return b.String()
			}
		}
	}

	// Generic map fallback
	return toonMapSchema(m, indent)
}

// toonMapSchema formats an arbitrary map in TOON at the given indent level.
func toonMapSchema(m map[string]interface{}, indent int) string {
	prefix := strings.Repeat(" ", indent)
	var b strings.Builder

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := m[k]
		if v == nil {
			continue
		}
		b.WriteString(prefix)
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(toonValue(v, 0))
		b.WriteString("\n")
	}

	return b.String()
}

// extractSchemaType extracts a simple type string from a schema.
// Returns empty string if the schema is complex (object with properties, etc).
func extractSchemaType(schema interface{}) string {
	if schema == nil {
		return ""
	}

	m, ok := schema.(map[string]interface{})
	if !ok {
		return ""
	}

	// Check for enum
	if enumVals, hasEnum := m["enum"]; hasEnum {
		if enumSlice, ok := enumVals.([]interface{}); ok {
			parts := make([]string, 0, len(enumSlice))
			for _, e := range enumSlice {
				parts = append(parts, fmt.Sprintf("%v", e))
			}
			return "enum(" + strings.Join(parts, ", ") + ")"
		}
	}

	schemaType, _ := m["type"].(string)

	// Check for $ref
	if ref, hasRef := m["$ref"]; hasRef {
		refStr, _ := ref.(string)
		parts := strings.Split(refStr, "/")
		return "$ref(" + parts[len(parts)-1] + ")"
	}

	// Array with simple items
	if schemaType == "array" {
		if items, has := m["items"]; has {
			itemType := extractSchemaType(items)
			if itemType != "" {
				return "[]" + itemType
			}
		}
		return "[]"
	}

	// Object with properties is complex — return empty to signal nesting
	if schemaType == "object" && hasKey(m, "properties") {
		return ""
	}

	// Simple type with optional format
	if schemaType != "" {
		format, _ := m["format"].(string)
		if format != "" {
			return schemaType + "(" + format + ")"
		}
		return schemaType
	}

	return ""
}

// needsQuotes checks if a string value needs quoting in TOON.
func needsQuotes(s string) bool {
	if s == "" {
		return false
	}
	if strings.ContainsAny(s, ":\n") {
		return true
	}
	if s[0] == ' ' || s[len(s)-1] == ' ' {
		return true
	}
	return false
}

// quote wraps a string in double quotes, escaping internal quotes.
func quote(s string) string {
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	escaped = strings.ReplaceAll(escaped, "\n", `\n`)
	return `"` + escaped + `"`
}

// toonString returns a TOON-safe representation of a string value.
func toonString(s string) string {
	if needsQuotes(s) {
		return quote(s)
	}
	return s
}

// hasKey checks if a map has a given key.
func hasKey(m map[string]interface{}, key string) bool {
	_, ok := m[key]
	return ok
}
