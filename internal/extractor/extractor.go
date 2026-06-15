package extractor

import (
	"errors"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/hnkatze/swagger-mcp-go/internal/types"
)

// defaultMaxDepth is the schema resolution depth used when the caller does not
// specify one. Kept low on purpose: deeply nested schemas explode token counts
// while rarely adding interpretive value. Callers that need the full tree can
// pass an explicit resolve_depth up to 10.
const defaultMaxDepth = 3

var (
	ErrEndpointNotFound = errors.New(types.ErrEndpointNotFound)
	ErrSchemaNotFound   = errors.New(types.ErrSchemaNotFound)
)

// effectiveMaxDepth returns the max depth to use for schema resolution.
// resolveDepth < 0 means use default, 0 means no resolution, 1-10 means use that value.
func effectiveMaxDepth(resolveDepth int) int {
	if resolveDepth < 0 {
		return defaultMaxDepth
	}
	return resolveDepth
}

// cleanDescription trims interpretive noise from schema/property descriptions.
// .NET/Swashbuckle emits "Gets or sets the X" for every property, which is pure
// boilerplate; stripping it keeps the meaningful remainder and saves tokens.
func cleanDescription(d string) string {
	d = strings.TrimSpace(d)
	if d == "" {
		return ""
	}
	for _, prefix := range []string{"Gets or sets the ", "Gets or sets ", "Gets or set ", "Gets the ", "Gets "} {
		if len(d) > len(prefix) && strings.HasPrefix(d, prefix) {
			rest := strings.TrimSpace(d[len(prefix):])
			if rest != "" {
				return rest
			}
		}
	}
	return d
}

// schemaName extracts the short component name from a $ref string such as
// "#/components/schemas/User" → "User". Returns "" for inline (un-named) schemas.
func schemaName(ref string) string {
	if ref == "" {
		return ""
	}
	parts := strings.Split(ref, "/")
	return parts[len(parts)-1]
}

// resolveRoot resolves a top-level schema, seeding a fresh dedup set so that any
// named ($ref) schema is expanded once per tree and later repeats collapse to a
// lightweight {"$ref": "Name"} marker. This kills the duplication that dominates
// real-world specs (a shared sub-schema referenced N times) and also bounds
// circular references — without losing information, since the name is preserved.
func resolveRoot(schema *openapi3.SchemaRef, maxDepth int) interface{} {
	return resolveSchema(schema, 0, maxDepth, map[string]bool{})
}

// GetEndpoint returns the full detail for a single endpoint.
// resolveDepth controls schema resolution depth: <0 = default (10), 0 = no resolution, 1-10 = use that value.
func GetEndpoint(doc *openapi3.T, method, path string, resolveDepth int) (*types.EndpointDetail, error) {
	if doc == nil || doc.Paths == nil {
		return nil, ErrEndpointNotFound
	}

	pathItem := doc.Paths.Value(path)
	if pathItem == nil {
		return nil, ErrEndpointNotFound
	}

	op := getOperationByMethod(pathItem, strings.ToUpper(method))
	if op == nil {
		return nil, ErrEndpointNotFound
	}

	maxDepth := effectiveMaxDepth(resolveDepth)

	// One dedup set shared across the whole endpoint. This is what makes the
	// difference on .NET-style specs: a shared error envelope (ProblemDetails &
	// co.) referenced by every status code is expanded once, then collapses to
	// $ref(Name) in the remaining responses instead of being inlined 5+ times.
	seen := map[string]bool{}

	detail := &types.EndpointDetail{
		Method:      strings.ToUpper(method),
		Path:        path,
		OperationID: op.OperationID,
		Summary:     op.Summary,
		Description: op.Description,
		Tags:        op.Tags,
		Deprecated:  op.Deprecated,
	}

	// Collect parameters from both path-level and operation-level
	detail.Parameters = extractParameters(pathItem.Parameters, op.Parameters, maxDepth, seen)

	// Extract request body
	if op.RequestBody != nil && op.RequestBody.Value != nil {
		detail.RequestBody = extractRequestBody(op.RequestBody.Value, maxDepth, seen)
	}

	// Extract responses
	if op.Responses != nil {
		detail.Responses = extractResponses(op.Responses, maxDepth, seen)
	}

	// Extract security
	if op.Security != nil {
		secReqs := make([]map[string][]string, 0, len(*op.Security))
		for _, req := range *op.Security {
			secReqs = append(secReqs, map[string][]string(req))
		}
		detail.Security = secReqs
	}

	return detail, nil
}

// GetSchema returns the resolved detail for a named schema from components.
// resolveDepth controls schema resolution depth: <0 = default (10), 0 = no resolution, 1-10 = use that value.
func GetSchema(doc *openapi3.T, name string, resolveDepth int) (*types.SchemaDetail, error) {
	if doc == nil || doc.Components == nil || doc.Components.Schemas == nil {
		return nil, ErrSchemaNotFound
	}

	schemaRef, ok := doc.Components.Schemas[name]
	if !ok || schemaRef == nil {
		return nil, ErrSchemaNotFound
	}

	maxDepth := effectiveMaxDepth(resolveDepth)
	resolved := resolveRoot(schemaRef, maxDepth)
	return &types.SchemaDetail{
		Name:   name,
		Schema: resolved,
	}, nil
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

// extractParameters merges path-level and operation-level parameters.
func extractParameters(pathParams openapi3.Parameters, opParams openapi3.Parameters, maxDepth int, seen map[string]bool) []types.ParameterDetail {
	// Use a map to allow operation-level params to override path-level ones
	byKey := make(map[string]types.ParameterDetail)
	var order []string

	addParams := func(params openapi3.Parameters) {
		for _, ref := range params {
			if ref == nil || ref.Value == nil {
				continue
			}
			p := ref.Value
			key := p.In + ":" + p.Name
			pd := types.ParameterDetail{
				Name:        p.Name,
				In:          p.In,
				Required:    p.Required,
				Description: p.Description,
			}
			if p.Schema != nil {
				pd.Schema = resolveSchema(p.Schema, 0, maxDepth, seen)
			}
			if _, exists := byKey[key]; !exists {
				order = append(order, key)
			}
			byKey[key] = pd
		}
	}

	addParams(pathParams)
	addParams(opParams)

	results := make([]types.ParameterDetail, 0, len(byKey))
	for _, key := range order {
		results = append(results, byKey[key])
	}
	return results
}

// extractRequestBody converts an openapi3.RequestBody to types.RequestBodyDetail.
func extractRequestBody(rb *openapi3.RequestBody, maxDepth int, seen map[string]bool) *types.RequestBodyDetail {
	detail := &types.RequestBodyDetail{
		Required:    rb.Required,
		Description: rb.Description,
	}
	if rb.Content != nil {
		content := make(map[string]types.MediaDetail, len(rb.Content))
		for mediaType, mt := range rb.Content {
			if mt == nil {
				continue
			}
			md := types.MediaDetail{}
			if mt.Schema != nil {
				md.Schema = resolveSchema(mt.Schema, 0, maxDepth, seen)
			}
			content[mediaType] = md
		}
		detail.Content = content
	}
	return detail
}

// extractResponses converts an openapi3.Responses to a sorted slice of ResponseDetail.
// Responses are processed in sorted status-code order and share the endpoint's
// dedup set, so a schema reused across status codes is expanded only once.
func extractResponses(responses *openapi3.Responses, maxDepth int, seen map[string]bool) []types.ResponseDetail {
	respMap := responses.Map()
	codes := make([]string, 0, len(respMap))
	for code := range respMap {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	results := make([]types.ResponseDetail, 0, len(codes))
	for _, code := range codes {
		ref := respMap[code]
		if ref == nil || ref.Value == nil {
			continue
		}
		resp := ref.Value
		rd := types.ResponseDetail{
			StatusCode: code,
		}
		if resp.Description != nil {
			rd.Description = *resp.Description
		}
		rd.Headers = extractHeaders(resp.Headers, maxDepth, seen)
		if resp.Content != nil {
			content := make(map[string]types.MediaDetail, len(resp.Content))
			for mediaType, mt := range resp.Content {
				if mt == nil {
					continue
				}
				md := types.MediaDetail{}
				if mt.Schema != nil {
					md.Schema = resolveSchema(mt.Schema, 0, maxDepth, seen)
				}
				content[mediaType] = md
			}
			rd.Content = content
		}
		results = append(results, rd)
	}
	return results
}

// extractHeaders converts openapi3.Headers to a sorted slice of HeaderDetail.
func extractHeaders(headers openapi3.Headers, maxDepth int, seen map[string]bool) []types.HeaderDetail {
	if len(headers) == 0 {
		return nil
	}

	// Iterate header names in sorted order so the shared dedup set is populated
	// deterministically.
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)

	results := make([]types.HeaderDetail, 0, len(headers))
	for _, name := range names {
		ref := headers[name]
		if ref == nil || ref.Value == nil {
			continue
		}
		h := ref.Value
		hd := types.HeaderDetail{
			Name:        name,
			Description: h.Description,
			Required:    h.Required,
			Deprecated:  h.Deprecated,
		}
		if h.Schema != nil {
			hd.Schema = resolveSchema(h.Schema, 0, maxDepth, seen)
		}
		results = append(results, hd)
	}

	return results
}

// resolveSchema recursively converts a SchemaRef into a clean map representation.
//
// seen tracks named ($ref) schemas already expanded in the current tree. The
// first time a named schema is encountered it is fully expanded; any later
// occurrence (a repeat sibling or a circular back-reference) collapses to a
// {"$ref": "Name"} marker. This removes the single biggest source of token
// bloat in real specs without losing meaning — the name still tells the model
// exactly which type it is.
func resolveSchema(schema *openapi3.SchemaRef, depth, maxDepth int, seen map[string]bool) interface{} {
	if schema == nil {
		return nil
	}

	name := schemaName(schema.Ref)

	// Already expanded once in this tree → reference it by name instead of
	// inlining the whole structure again.
	if name != "" && seen[name] {
		return map[string]interface{}{"$ref": name}
	}

	// Depth guard: stop descending but keep the name so the model can fetch
	// the type via get_schema if it needs the rest.
	if depth >= maxDepth {
		if name != "" {
			return map[string]interface{}{"$ref": name}
		}
		return map[string]interface{}{"$ref": "unknown", "truncated": true}
	}

	// Access the resolved schema value
	s := schema.Value
	if s == nil {
		return nil
	}

	// Mark this named schema as expanded before recursing so its own
	// descendants (circular refs) collapse to a marker.
	if name != "" {
		seen[name] = true
	}

	result := make(map[string]interface{})

	// Type
	if s.Type != nil {
		typeSlice := s.Type.Slice()
		if len(typeSlice) == 1 {
			result["type"] = typeSlice[0]
		} else if len(typeSlice) > 1 {
			result["type"] = typeSlice
		}
	}

	// Format
	if s.Format != "" {
		result["format"] = s.Format
	}

	// Description — the primary interpretive signal. Cleaned of boilerplate noise.
	if d := cleanDescription(s.Description); d != "" {
		result["description"] = d
	}

	// Enum
	if len(s.Enum) > 0 {
		result["enum"] = s.Enum
	}

	// Required
	if len(s.Required) > 0 {
		result["required"] = s.Required
	}

	// Properties (object type). Iterate in sorted order so the dedup set is
	// populated deterministically — the alphabetically-first occurrence of a
	// shared schema is the one that gets expanded.
	if len(s.Properties) > 0 {
		propNames := make([]string, 0, len(s.Properties))
		for n := range s.Properties {
			propNames = append(propNames, n)
		}
		sort.Strings(propNames)
		props := make(map[string]interface{}, len(s.Properties))
		for _, n := range propNames {
			props[n] = resolveSchema(s.Properties[n], depth+1, maxDepth, seen)
		}
		result["properties"] = props
	}

	// Items (array type)
	if s.Items != nil {
		result["items"] = resolveSchema(s.Items, depth+1, maxDepth, seen)
	}

	// AdditionalProperties
	if s.AdditionalProperties.Schema != nil {
		result["additionalProperties"] = resolveSchema(s.AdditionalProperties.Schema, depth+1, maxDepth, seen)
	}

	// OneOf
	if len(s.OneOf) > 0 {
		oneOf := make([]interface{}, len(s.OneOf))
		for i, ref := range s.OneOf {
			oneOf[i] = resolveSchema(ref, depth+1, maxDepth, seen)
		}
		result["oneOf"] = oneOf
	}

	// AnyOf
	if len(s.AnyOf) > 0 {
		anyOf := make([]interface{}, len(s.AnyOf))
		for i, ref := range s.AnyOf {
			anyOf[i] = resolveSchema(ref, depth+1, maxDepth, seen)
		}
		result["anyOf"] = anyOf
	}

	// AllOf
	if len(s.AllOf) > 0 {
		allOf := make([]interface{}, len(s.AllOf))
		for i, ref := range s.AllOf {
			allOf[i] = resolveSchema(ref, depth+1, maxDepth, seen)
		}
		result["allOf"] = allOf
	}

	// Nullable
	if s.Nullable {
		result["nullable"] = true
	}

	// ReadOnly / WriteOnly
	if s.ReadOnly {
		result["readOnly"] = true
	}
	if s.WriteOnly {
		result["writeOnly"] = true
	}

	// Deprecated
	if s.Deprecated {
		result["deprecated"] = true
	}

	// Note: `default` and `example` are intentionally omitted. In real-world
	// specs (notably .NET/Swashbuckle) example objects duplicate the entire
	// structure with sample data, dominating token counts while adding little
	// the model can't infer from types + descriptions. `description` is kept —
	// it is the actual interpretive signal.

	return result
}
