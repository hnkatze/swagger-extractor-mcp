package analyzer

import (
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/hnkatze/swagger-extractor-mcp/internal/types"
)

// forEachOperation iterates over all non-nil operations on a PathItem.
func forEachOperation(path string, item *openapi3.PathItem, fn func(method, path string, op *openapi3.Operation)) {
	ops := map[string]*openapi3.Operation{
		"GET":     item.Get,
		"POST":    item.Post,
		"PUT":     item.Put,
		"DELETE":  item.Delete,
		"PATCH":   item.Patch,
		"HEAD":    item.Head,
		"OPTIONS": item.Options,
	}
	for method, op := range ops {
		if op != nil {
			fn(method, path, op)
		}
	}
}

// ListEndpoints returns a filtered list of endpoint summaries from the spec.
// ListFilter selects which endpoints a listing returns.
// Deprecated nil means "both current and deprecated", which is what an
// unfiltered listing should show.
type ListFilter struct {
	Tag         string
	Method      string
	PathPattern string
	Deprecated  *bool
}

func ListEndpoints(doc *openapi3.T, f ListFilter) []types.EndpointSummary {
	if doc == nil || doc.Paths == nil {
		return nil
	}

	var results []types.EndpointSummary

	methodUpper := strings.ToUpper(f.Method)

	for path, item := range doc.Paths.Map() {
		if item == nil {
			continue
		}
		if f.PathPattern != "" && !strings.Contains(path, f.PathPattern) {
			continue
		}
		forEachOperation(path, item, func(m, p string, op *openapi3.Operation) {
			if methodUpper != "" && m != methodUpper {
				return
			}
			if f.Tag != "" && !hasTag(op, f.Tag) {
				return
			}
			if f.Deprecated != nil && op.Deprecated != *f.Deprecated {
				return
			}
			results = append(results, types.EndpointSummary{
				Method:      m,
				Path:        p,
				Summary:     op.Summary,
				Description: op.Description,
				Tags:        op.Tags,
				Deprecated:  op.Deprecated,
			})
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Path != results[j].Path {
			return results[i].Path < results[j].Path
		}
		return results[i].Method < results[j].Method
	})

	return results
}

// hasTag checks whether an operation contains the specified tag (case-insensitive).
func hasTag(op *openapi3.Operation, tag string) bool {
	tagLower := strings.ToLower(tag)
	for _, t := range op.Tags {
		if strings.ToLower(t) == tagLower {
			return true
		}
	}
	return false
}

// AnalyzeTags groups all endpoints by tag and returns tag summaries.
func AnalyzeTags(doc *openapi3.T) []types.TagSummary {
	if doc == nil || doc.Paths == nil {
		return nil
	}

	// tagName -> TagSummary accumulator
	tagMap := make(map[string]*types.TagSummary)

	for path, item := range doc.Paths.Map() {
		if item == nil {
			continue
		}
		forEachOperation(path, item, func(method, _ string, op *openapi3.Operation) {
			tags := op.Tags
			if len(tags) == 0 {
				tags = []string{"untagged"}
			}
			for _, t := range tags {
				ts, ok := tagMap[t]
				if !ok {
					ts = &types.TagSummary{
						Name:            t,
						MethodBreakdown: make(map[string]int),
					}
					tagMap[t] = ts
				}
				ts.EndpointCount++
				ts.MethodBreakdown[method]++
			}
		})
	}

	// Enrich descriptions from doc.Tags
	for _, docTag := range doc.Tags {
		if ts, ok := tagMap[docTag.Name]; ok {
			ts.Description = docTag.Description
		}
	}

	results := make([]types.TagSummary, 0, len(tagMap))
	for _, ts := range tagMap {
		results = append(results, *ts)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})
	return results
}

// SearchSpec performs a case-insensitive search across endpoint metadata.
func SearchSpec(doc *openapi3.T, query string) []types.EndpointSummary {
	if doc == nil || doc.Paths == nil || query == "" {
		return nil
	}

	queryLower := strings.ToLower(query)

	type scored struct {
		endpoint types.EndpointSummary
		score    int // lower = more relevant
	}

	var matches []scored

	for path, item := range doc.Paths.Map() {
		if item == nil {
			continue
		}
		forEachOperation(path, item, func(method, p string, op *openapi3.Operation) {
			score := matchScore(queryLower, p, op)
			if score < 0 {
				return
			}
			matches = append(matches, scored{
				endpoint: types.EndpointSummary{
					Method:      method,
					Path:        p,
					Summary:     op.Summary,
					Description: op.Description,
					Tags:        op.Tags,
					Deprecated:  op.Deprecated,
				},
				score: score,
			})
		})
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score < matches[j].score
		}
		if matches[i].endpoint.Path != matches[j].endpoint.Path {
			return matches[i].endpoint.Path < matches[j].endpoint.Path
		}
		return matches[i].endpoint.Method < matches[j].endpoint.Method
	})

	results := make([]types.EndpointSummary, len(matches))
	for i, m := range matches {
		results[i] = m.endpoint
	}
	return results
}

// matchScore returns the relevance score for an operation matching the query.
// Returns -1 if no match. Lower score = more relevant.
// Priority: 0=path, 1=summary, 2=operationId, 3=description, 4=parameter names,
// 5=request body properties, 6=response body properties.
func matchScore(queryLower, path string, op *openapi3.Operation) int {
	if strings.Contains(strings.ToLower(path), queryLower) {
		return 0
	}
	if strings.Contains(strings.ToLower(op.Summary), queryLower) {
		return 1
	}
	if strings.Contains(strings.ToLower(op.OperationID), queryLower) {
		return 2
	}
	if strings.Contains(strings.ToLower(op.Description), queryLower) {
		return 3
	}
	for _, paramRef := range op.Parameters {
		if paramRef == nil || paramRef.Value == nil {
			continue
		}
		if strings.Contains(strings.ToLower(paramRef.Value.Name), queryLower) {
			return 4
		}
	}
	// Score 5: request body schema property names
	if op.RequestBody != nil && op.RequestBody.Value != nil {
		for _, mt := range op.RequestBody.Value.Content {
			if mt.Schema != nil && matchSchemaProperties(mt.Schema, queryLower) {
				return 5
			}
		}
	}
	// Score 6: response body schema property names
	if op.Responses != nil {
		for _, respRef := range op.Responses.Map() {
			if respRef.Value == nil {
				continue
			}
			for _, mt := range respRef.Value.Content {
				if mt.Schema != nil && matchSchemaProperties(mt.Schema, queryLower) {
					return 6
				}
			}
		}
	}
	return -1
}

// matchSchemaProperties checks if any top-level property name in the schema
// contains the query string. Also unwraps one level of arrays.
func matchSchemaProperties(schema *openapi3.SchemaRef, queryLower string) bool {
	if schema == nil || schema.Value == nil {
		return false
	}
	s := schema.Value
	// Direct object properties
	for name := range s.Properties {
		if strings.Contains(strings.ToLower(name), queryLower) {
			return true
		}
	}
	// Array items properties (unwrap one level)
	if s.Items != nil && s.Items.Value != nil {
		for name := range s.Items.Value.Properties {
			if strings.Contains(strings.ToLower(name), queryLower) {
				return true
			}
		}
	}
	return false
}

// DiffSpecs compares two OpenAPI specs and returns the differences.
func DiffSpecs(oldDoc *openapi3.T, newDoc *openapi3.T, filterPath string, filterMethod string) *types.DiffResult {
	if oldDoc == nil || newDoc == nil {
		return &types.DiffResult{}
	}

	filterMethodUpper := strings.ToUpper(filterMethod)

	oldEndpoints := buildEndpointMap(oldDoc)
	newEndpoints := buildEndpointMap(newDoc)

	result := &types.DiffResult{}

	// Added: in new but not in old
	for key, newEp := range newEndpoints {
		if _, exists := oldEndpoints[key]; !exists {
			if matchFilter(newEp.Method, newEp.Path, filterMethodUpper, filterPath) {
				result.Added = append(result.Added, newEp)
			}
		}
	}

	// Removed: in old but not in new
	for key, oldEp := range oldEndpoints {
		if _, exists := newEndpoints[key]; !exists {
			if matchFilter(oldEp.Method, oldEp.Path, filterMethodUpper, filterPath) {
				result.Removed = append(result.Removed, oldEp)
			}
		}
	}

	// Changed: in both but different
	for key, oldEp := range oldEndpoints {
		_, exists := newEndpoints[key]
		if !exists {
			continue
		}
		if !matchFilter(oldEp.Method, oldEp.Path, filterMethodUpper, filterPath) {
			continue
		}

		changes := detectChanges(oldDoc, newDoc, oldEp.Method, oldEp.Path)
		if len(changes) > 0 {
			result.Changed = append(result.Changed, types.EndpointChange{
				Method:  oldEp.Method,
				Path:    oldEp.Path,
				Changes: changes,
			})
		}
	}

	// Sort for deterministic output
	sort.Slice(result.Added, func(i, j int) bool {
		return result.Added[i].Path+result.Added[i].Method < result.Added[j].Path+result.Added[j].Method
	})
	sort.Slice(result.Removed, func(i, j int) bool {
		return result.Removed[i].Path+result.Removed[i].Method < result.Removed[j].Path+result.Removed[j].Method
	})
	sort.Slice(result.Changed, func(i, j int) bool {
		return result.Changed[i].Path+result.Changed[i].Method < result.Changed[j].Path+result.Changed[j].Method
	})

	return result
}

// buildEndpointMap creates a map of "METHOD /path" -> EndpointSummary.
func buildEndpointMap(doc *openapi3.T) map[string]types.EndpointSummary {
	endpoints := make(map[string]types.EndpointSummary)
	if doc == nil || doc.Paths == nil {
		return endpoints
	}
	for path, item := range doc.Paths.Map() {
		if item == nil {
			continue
		}
		forEachOperation(path, item, func(method, p string, op *openapi3.Operation) {
			key := method + " " + p
			endpoints[key] = types.EndpointSummary{
				Method:      method,
				Path:        p,
				Summary:     op.Summary,
				Description: op.Description,
				Tags:        op.Tags,
				Deprecated:  op.Deprecated,
			}
		})
	}
	return endpoints
}

// matchFilter checks whether an endpoint matches the method and path filters.
func matchFilter(method, path, filterMethod, filterPath string) bool {
	if filterMethod != "" && method != filterMethod {
		return false
	}
	if filterPath != "" && !strings.Contains(path, filterPath) {
		return false
	}
	return true
}

// detectChanges compares the same endpoint across two specs and returns change descriptions.
func detectChanges(oldDoc, newDoc *openapi3.T, method, path string) []string {
	var changes []string

	oldItem := oldDoc.Paths.Value(path)
	newItem := newDoc.Paths.Value(path)
	if oldItem == nil || newItem == nil {
		return nil
	}

	oldOp := getOperation(oldItem, method)
	newOp := getOperation(newItem, method)
	if oldOp == nil || newOp == nil {
		return nil
	}

	// Compare parameter count
	if len(oldOp.Parameters) != len(newOp.Parameters) {
		changes = append(changes, "parameters count changed")
	}

	// Compare request body presence
	oldHasBody := oldOp.RequestBody != nil && oldOp.RequestBody.Value != nil
	newHasBody := newOp.RequestBody != nil && newOp.RequestBody.Value != nil
	if oldHasBody != newHasBody {
		changes = append(changes, "request body changed")
	}

	// Compare response codes
	oldCodes := getResponseCodes(oldOp)
	newCodes := getResponseCodes(newOp)
	if !equalStringSlices(oldCodes, newCodes) {
		changes = append(changes, "response codes changed")
	}

	// Compare description
	if oldOp.Description != newOp.Description {
		changes = append(changes, "description changed")
	}

	return changes
}

// getOperation returns the operation for a given HTTP method on a PathItem.
func getOperation(item *openapi3.PathItem, method string) *openapi3.Operation {
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

// getResponseCodes returns sorted status codes from an operation's responses.
func getResponseCodes(op *openapi3.Operation) []string {
	if op.Responses == nil {
		return nil
	}
	codes := make([]string, 0)
	for code := range op.Responses.Map() {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// equalStringSlices checks whether two sorted string slices are equal.
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// ListSchemas
// ---------------------------------------------------------------------------

const schemaRefPrefix = "#/components/schemas/"

// ListSchemas returns every component schema with its field count and how many
// endpoints reference it, sorted by usage then name. Without this, a spec's
// schemas are only discoverable by hitting a $ref inside an endpoint and
// guessing the name.
func ListSchemas(doc *openapi3.T, filter string) []types.SchemaInfo {
	if doc == nil || doc.Components == nil || len(doc.Components.Schemas) == 0 {
		return nil
	}

	usage := make(map[string]int)
	if doc.Paths != nil {
		for _, item := range doc.Paths.Map() {
			if item == nil {
				continue
			}
			forEachOperation("", item, func(_, _ string, op *openapi3.Operation) {
				refs := make(map[string]bool)
				collectOperationRefs(op, refs)
				for name := range refs {
					usage[name]++
				}
			})
		}
	}

	filterLower := strings.ToLower(filter)

	schemas := make([]types.SchemaInfo, 0, len(doc.Components.Schemas))
	for name, ref := range doc.Components.Schemas {
		if filterLower != "" && !strings.Contains(strings.ToLower(name), filterLower) {
			continue
		}
		info := types.SchemaInfo{Name: name, UsedBy: usage[name]}
		if ref != nil && ref.Value != nil {
			info.FieldCount = len(ref.Value.Properties)
			info.Description = ref.Value.Description
		}
		schemas = append(schemas, info)
	}

	// Go map iteration is random: without an explicit order, the same spec
	// returns rows in a different sequence on every call and offset paging
	// silently skips or repeats entries.
	sort.Slice(schemas, func(i, j int) bool {
		if schemas[i].UsedBy != schemas[j].UsedBy {
			return schemas[i].UsedBy > schemas[j].UsedBy
		}
		return schemas[i].Name < schemas[j].Name
	})

	return schemas
}

// collectOperationRefs gathers every component schema name reachable from an
// operation's parameters, request body and responses.
func collectOperationRefs(op *openapi3.Operation, out map[string]bool) {
	if op == nil {
		return
	}
	visited := make(map[*openapi3.Schema]bool)
	for _, paramRef := range op.Parameters {
		if paramRef != nil && paramRef.Value != nil {
			collectSchemaRefs(paramRef.Value.Schema, out, visited)
		}
	}
	if op.RequestBody != nil && op.RequestBody.Value != nil {
		for _, media := range op.RequestBody.Value.Content {
			if media != nil {
				collectSchemaRefs(media.Schema, out, visited)
			}
		}
	}
	if op.Responses != nil {
		for _, respRef := range op.Responses.Map() {
			if respRef == nil || respRef.Value == nil {
				continue
			}
			for _, media := range respRef.Value.Content {
				if media != nil {
					collectSchemaRefs(media.Schema, out, visited)
				}
			}
		}
	}
}

// collectSchemaRefs walks a schema tree recording component names it points at.
// The visited set guards against self-referencing models recursing forever.
func collectSchemaRefs(ref *openapi3.SchemaRef, out map[string]bool, visited map[*openapi3.Schema]bool) {
	if ref == nil {
		return
	}
	if name := strings.TrimPrefix(ref.Ref, schemaRefPrefix); name != ref.Ref {
		out[name] = true
	}
	schema := ref.Value
	if schema == nil || visited[schema] {
		return
	}
	visited[schema] = true

	for _, propRef := range schema.Properties {
		collectSchemaRefs(propRef, out, visited)
	}
	collectSchemaRefs(schema.Items, out, visited)
	for _, sub := range schema.AllOf {
		collectSchemaRefs(sub, out, visited)
	}
	for _, sub := range schema.AnyOf {
		collectSchemaRefs(sub, out, visited)
	}
	for _, sub := range schema.OneOf {
		collectSchemaRefs(sub, out, visited)
	}
	if schema.AdditionalProperties.Schema != nil {
		collectSchemaRefs(schema.AdditionalProperties.Schema, out, visited)
	}
}

// ListSecuritySchemes returns the security scheme definitions of the spec with
// the number of operations requiring each. Operations without their own
// security fall back to the document-level requirement; an explicit empty
// requirement opts out and counts toward UnauthenticatedOverride.
func ListSecuritySchemes(doc *openapi3.T) *types.SecurityOverview {
	out := &types.SecurityOverview{Schemes: []types.SecuritySchemeInfo{}}
	if doc == nil {
		return out
	}

	for _, req := range doc.Security {
		names := make([]string, 0, len(req))
		for name := range req {
			names = append(names, name)
		}
		sort.Strings(names)
		out.Global = append(out.Global, names)
	}

	usage := make(map[string]int)
	if doc.Paths != nil {
		for _, item := range doc.Paths.Map() {
			if item == nil {
				continue
			}
			forEachOperation("", item, func(_, _ string, op *openapi3.Operation) {
				effective := doc.Security
				if op.Security != nil {
					effective = *op.Security
				}
				used := make(map[string]bool)
				for _, req := range effective {
					for name := range req {
						used[name] = true
					}
				}
				if len(used) == 0 {
					if op.Security != nil {
						out.UnauthenticatedOverride++
					}
					return
				}
				for name := range used {
					usage[name]++
				}
			})
		}
	}

	if doc.Components != nil {
		for name, ref := range doc.Components.SecuritySchemes {
			if ref == nil || ref.Value == nil {
				continue
			}
			out.Schemes = append(out.Schemes, buildSecuritySchemeInfo(name, ref.Value, usage[name]))
		}
	}

	sort.Slice(out.Schemes, func(i, j int) bool {
		if out.Schemes[i].Endpoints != out.Schemes[j].Endpoints {
			return out.Schemes[i].Endpoints > out.Schemes[j].Endpoints
		}
		return out.Schemes[i].Name < out.Schemes[j].Name
	})
	return out
}

func buildSecuritySchemeInfo(name string, s *openapi3.SecurityScheme, endpoints int) types.SecuritySchemeInfo {
	info := types.SecuritySchemeInfo{
		Name:             name,
		Type:             s.Type,
		Scheme:           s.Scheme,
		BearerFormat:     s.BearerFormat,
		In:               s.In,
		ParamName:        s.Name,
		OpenIDConnectURL: s.OpenIdConnectUrl,
		Description:      s.Description,
		Endpoints:        endpoints,
	}
	if s.Flows == nil {
		return info
	}

	scopeSet := make(map[string]bool)
	add := func(kind string, flow *openapi3.OAuthFlow) {
		if flow == nil {
			return
		}
		info.Flows = append(info.Flows, types.OAuthFlowInfo{
			Type:             kind,
			AuthorizationURL: flow.AuthorizationURL,
			TokenURL:         flow.TokenURL,
			RefreshURL:       flow.RefreshURL,
		})
		for scope := range flow.Scopes {
			scopeSet[scope] = true
		}
	}
	add("implicit", s.Flows.Implicit)
	add("password", s.Flows.Password)
	add("clientCredentials", s.Flows.ClientCredentials)
	add("authorizationCode", s.Flows.AuthorizationCode)

	for scope := range scopeSet {
		info.Scopes = append(info.Scopes, scope)
	}
	sort.Strings(info.Scopes)
	return info
}

// CollectTagRefs returns the sorted, deduplicated names of the component
// schemas that the endpoints carrying tag reach directly in their parameters,
// request bodies and responses. Nested schemas are the caller's job (the
// generator already expands them via its own collectRefs).
func CollectTagRefs(doc *openapi3.T, tag string) []string {
	if doc == nil || doc.Paths == nil {
		return nil
	}
	refs := make(map[string]bool)
	for path, item := range doc.Paths.Map() {
		if item == nil {
			continue
		}
		forEachOperation(path, item, func(_, _ string, op *openapi3.Operation) {
			if hasTag(op, tag) {
				collectOperationRefs(op, refs)
			}
		})
	}
	names := make([]string, 0, len(refs))
	for name := range refs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
