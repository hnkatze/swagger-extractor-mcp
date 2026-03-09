package extractor

import (
	"errors"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/hnkatze/swagger-mcp-go/internal/types"
)

const defaultMaxDepth = 10

var (
	ErrEndpointNotFound = errors.New(types.ErrEndpointNotFound)
	ErrSchemaNotFound   = errors.New(types.ErrSchemaNotFound)
)

// effectiveMaxDepth returns the max depth to use for schema resolution.
// resolveDepth < 0 means use default (10), 0 means no resolution, 1-10 means use that value.
func effectiveMaxDepth(resolveDepth int) int {
	if resolveDepth < 0 {
		return defaultMaxDepth
	}
	return resolveDepth
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
	detail.Parameters = extractParameters(pathItem.Parameters, op.Parameters, maxDepth)

	// Extract request body
	if op.RequestBody != nil && op.RequestBody.Value != nil {
		detail.RequestBody = extractRequestBody(op.RequestBody.Value, maxDepth)
	}

	// Extract responses
	if op.Responses != nil {
		detail.Responses = extractResponses(op.Responses, maxDepth)
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
	resolved := resolveSchema(schemaRef, 0, maxDepth)
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
func extractParameters(pathParams openapi3.Parameters, opParams openapi3.Parameters, maxDepth int) []types.ParameterDetail {
	// Use a map to allow operation-level params to override path-level ones
	seen := make(map[string]types.ParameterDetail)
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
				pd.Schema = resolveSchema(p.Schema, 0, maxDepth)
			}
			if _, exists := seen[key]; !exists {
				order = append(order, key)
			}
			seen[key] = pd
		}
	}

	addParams(pathParams)
	addParams(opParams)

	results := make([]types.ParameterDetail, 0, len(seen))
	for _, key := range order {
		results = append(results, seen[key])
	}
	return results
}

// extractRequestBody converts an openapi3.RequestBody to types.RequestBodyDetail.
func extractRequestBody(rb *openapi3.RequestBody, maxDepth int) *types.RequestBodyDetail {
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
				md.Schema = resolveSchema(mt.Schema, 0, maxDepth)
			}
			content[mediaType] = md
		}
		detail.Content = content
	}
	return detail
}

// extractResponses converts an openapi3.Responses to a sorted slice of ResponseDetail.
func extractResponses(responses *openapi3.Responses, maxDepth int) []types.ResponseDetail {
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
		rd.Headers = extractHeaders(resp.Headers, maxDepth)
		if resp.Content != nil {
			content := make(map[string]types.MediaDetail, len(resp.Content))
			for mediaType, mt := range resp.Content {
				if mt == nil {
					continue
				}
				md := types.MediaDetail{}
				if mt.Schema != nil {
					md.Schema = resolveSchema(mt.Schema, 0, maxDepth)
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
func extractHeaders(headers openapi3.Headers, maxDepth int) []types.HeaderDetail {
	if len(headers) == 0 {
		return nil
	}

	results := make([]types.HeaderDetail, 0, len(headers))
	for name, ref := range headers {
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
			hd.Schema = resolveSchema(h.Schema, 0, maxDepth)
		}
		results = append(results, hd)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})

	return results
}

// resolveSchema recursively converts a SchemaRef into a clean map representation.
func resolveSchema(schema *openapi3.SchemaRef, depth, maxDepth int) interface{} {
	if schema == nil {
		return nil
	}

	// Prevent infinite loops from circular references
	if depth >= maxDepth {
		refName := schema.Ref
		if refName == "" {
			refName = "unknown"
		}
		return map[string]interface{}{
			"$circular_ref":       refName,
			"depth_limit_reached": true,
		}
	}

	// Access the resolved schema value
	s := schema.Value
	if s == nil {
		return nil
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

	// Description
	if s.Description != "" {
		result["description"] = s.Description
	}

	// Enum
	if len(s.Enum) > 0 {
		result["enum"] = s.Enum
	}

	// Required
	if len(s.Required) > 0 {
		result["required"] = s.Required
	}

	// Properties (object type)
	if s.Properties != nil && len(s.Properties) > 0 {
		props := make(map[string]interface{}, len(s.Properties))
		for name, propRef := range s.Properties {
			props[name] = resolveSchema(propRef, depth+1, maxDepth)
		}
		result["properties"] = props
	}

	// Items (array type)
	if s.Items != nil {
		result["items"] = resolveSchema(s.Items, depth+1, maxDepth)
	}

	// AdditionalProperties
	if s.AdditionalProperties.Schema != nil {
		result["additionalProperties"] = resolveSchema(s.AdditionalProperties.Schema, depth+1, maxDepth)
	}

	// OneOf
	if len(s.OneOf) > 0 {
		oneOf := make([]interface{}, len(s.OneOf))
		for i, ref := range s.OneOf {
			oneOf[i] = resolveSchema(ref, depth+1, maxDepth)
		}
		result["oneOf"] = oneOf
	}

	// AnyOf
	if len(s.AnyOf) > 0 {
		anyOf := make([]interface{}, len(s.AnyOf))
		for i, ref := range s.AnyOf {
			anyOf[i] = resolveSchema(ref, depth+1, maxDepth)
		}
		result["anyOf"] = anyOf
	}

	// AllOf
	if len(s.AllOf) > 0 {
		allOf := make([]interface{}, len(s.AllOf))
		for i, ref := range s.AllOf {
			allOf[i] = resolveSchema(ref, depth+1, maxDepth)
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

	// Default
	if s.Default != nil {
		result["default"] = s.Default
	}

	// Example
	if s.Example != nil {
		result["example"] = s.Example
	}

	return result
}
