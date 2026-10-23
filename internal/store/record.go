package store

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/hnkatze/swagger-extractor-mcp/internal/analyzer"
	"github.com/hnkatze/swagger-extractor-mcp/internal/loader"
)

// The categories search_spec matches within, in the priority order the analyzer
// scores them: operation parameters first, then request-body properties, then
// response-body properties.
const (
	fieldParameter    = "parameter"
	fieldRequestBody  = "request_body"
	fieldResponseBody = "response_body"
)

// Endpoint is one normalized discovery row: an operation of the spec, with the
// fields list_endpoints displays and the names search_spec matches on. A row read
// back from the catalog fills only the display fields; the name lists are used
// when the catalog is written.
type Endpoint struct {
	Method      string
	Path        string
	Summary     string
	Description string
	OperationID string
	Tags        []string
	Deprecated  bool
	// ParameterNames, RequestBodyFields and ResponseBodyFields are the names
	// search_spec matches on, per category.
	ParameterNames     []string
	RequestBodyFields  []string
	ResponseBodyFields []string
}

// fields returns the searchable names grouped by the category they score in.
func (e Endpoint) fields() map[string][]string {
	return map[string][]string{
		fieldParameter:    e.ParameterNames,
		fieldRequestBody:  e.RequestBodyFields,
		fieldResponseBody: e.ResponseBodyFields,
	}
}

// Schema is one normalized discovery row: a component schema with the counts
// discovery needs without parsing the document.
type Schema struct {
	Name        string
	FieldCount  int
	UsedBy      int
	Description string
}

// Tag is one normalized discovery row: a tag, its declared description and how
// many operations use it, broken down by method.
type Tag struct {
	Name            string
	Description     string
	EndpointCount   int
	MethodBreakdown map[string]int
}

// Record is everything written for one spec: the source metadata, the document
// itself (stored compressed) and the normalized catalog rows derived from it.
// EndpointCount and SchemaCount in the read models are len(Endpoints) and
// len(Schemas), so they can never drift from the catalog.
type Record struct {
	Name      string
	Source    string
	ETag      string
	SHA256    string
	FetchedAt time.Time
	Title     string
	Version   string
	Document  []byte
	Endpoints []Endpoint
	Schemas   []Schema
	Tags      []Tag
}

// SpecMeta is a record without its document: what listing needs.
type SpecMeta struct {
	Name          string
	Source        string
	ETag          string
	SHA256        string
	FetchedAt     time.Time
	Title         string
	Version       string
	EndpointCount int
	SchemaCount   int
}

// NewRecord builds a record for raw (the source bytes exactly as read) and its
// parsed document. Callers set Name, Source, ETag and FetchedAt.
//
// The catalog is derived with the analyzer's own functions rather than a second
// implementation of the same rules: a discovery answer read back from the
// catalog must equal the answer the analyzer produced from the document, and two
// derivations would eventually disagree.
func NewRecord(doc *openapi3.T, raw []byte) Record {
	sum := sha256.Sum256(raw)
	rec := Record{
		SHA256:   hex.EncodeToString(sum[:]),
		Document: raw,
	}
	if doc == nil {
		return rec
	}
	if doc.Info != nil {
		rec.Title = doc.Info.Title
		rec.Version = doc.Info.Version
	}
	rec.Endpoints = endpointsFromDoc(doc)
	rec.Schemas = schemasFromDoc(doc)
	rec.Tags = tagsFromDoc(doc)
	return rec
}

// reparse turns a stored document back into the record its catalog is derived
// from. It is what the v1 migration runs, and it normalizes exactly like the
// indexer so an upgraded store's catalog equals a fresh reindex's.
func reparse(name string, blob []byte) (Record, error) {
	raw, err := decompress(blob)
	if err != nil {
		return Record{}, err
	}
	doc, err := loader.ParseDocument(raw)
	if err != nil {
		return Record{}, err
	}
	loader.PrepareDocument(doc)
	rec := NewRecord(doc, raw)
	rec.Name = name
	return rec, nil
}

// endpointsFromDoc walks the operations the analyzer lists, which is the same set
// list_endpoints reports (that is what makes the stored endpoint_count agree with
// the listing), and attaches the names search_spec matches on.
func endpointsFromDoc(doc *openapi3.T) []Endpoint {
	summaries := analyzer.ListEndpoints(doc, analyzer.ListFilter{})
	if len(summaries) == 0 {
		return nil
	}
	fields := searchFields(doc)
	out := make([]Endpoint, 0, len(summaries))
	for _, summary := range summaries {
		ep := Endpoint{
			Method:      summary.Method,
			Path:        summary.Path,
			Summary:     summary.Summary,
			Description: summary.Description,
			Tags:        summary.Tags,
			Deprecated:  summary.Deprecated,
		}
		if names, ok := fields[summary.Method+" "+summary.Path]; ok {
			ep.OperationID = names.operationID
			ep.ParameterNames = names.byCategory[fieldParameter]
			ep.RequestBodyFields = names.byCategory[fieldRequestBody]
			ep.ResponseBodyFields = names.byCategory[fieldResponseBody]
		}
		out = append(out, ep)
	}
	return out
}

// operationCatalog is everything the catalog records about one operation beyond
// what EndpointSummary carries.
type operationCatalog struct {
	operationID string
	byCategory  map[string][]string
}

// searchFields collects the operation id and the names search_spec matches on,
// per operation. The names mirror analyzer.matchScore: operation parameters, plus
// the top-level properties of each request and response media schema with one
// array level unwrapped — exactly the places the document-parsed search looked
// at, so nested model properties stay unmatchable, as before.
func searchFields(doc *openapi3.T) map[string]operationCatalog {
	out := make(map[string]operationCatalog)
	if doc == nil || doc.Paths == nil {
		return out
	}
	for path, item := range doc.Paths.Map() {
		if item == nil {
			continue
		}
		for method, op := range map[string]*openapi3.Operation{
			"GET":     item.Get,
			"POST":    item.Post,
			"PUT":     item.Put,
			"DELETE":  item.Delete,
			"PATCH":   item.Patch,
			"HEAD":    item.Head,
			"OPTIONS": item.Options,
		} {
			if op == nil {
				continue
			}
			out[method+" "+path] = operationCatalog{
				operationID: op.OperationID,
				byCategory: map[string][]string{
					fieldParameter:    parameterNames(op),
					fieldRequestBody:  requestBodyFields(op),
					fieldResponseBody: responseBodyFields(op),
				},
			}
		}
	}
	return out
}

// parameterNames lists the operation's own parameter names.
func parameterNames(op *openapi3.Operation) []string {
	var names []string
	for _, ref := range op.Parameters {
		if ref == nil || ref.Value == nil {
			continue
		}
		names = append(names, ref.Value.Name)
	}
	return sortedUnique(names)
}

// requestBodyFields lists the top-level property names of every request media
// schema, unwrapping one array level.
func requestBodyFields(op *openapi3.Operation) []string {
	if op.RequestBody == nil || op.RequestBody.Value == nil {
		return nil
	}
	var names []string
	for _, media := range op.RequestBody.Value.Content {
		if media != nil {
			names = append(names, schemaFieldNames(media.Schema)...)
		}
	}
	return sortedUnique(names)
}

// responseBodyFields lists the top-level property names of every response media
// schema, unwrapping one array level.
func responseBodyFields(op *openapi3.Operation) []string {
	if op.Responses == nil {
		return nil
	}
	var names []string
	for _, ref := range op.Responses.Map() {
		if ref == nil || ref.Value == nil {
			continue
		}
		for _, media := range ref.Value.Content {
			if media != nil {
				names = append(names, schemaFieldNames(media.Schema)...)
			}
		}
	}
	return sortedUnique(names)
}

// schemaFieldNames returns the property names of a schema and of its array items,
// one level deep, which is how analyzer.matchSchemaProperties matches.
func schemaFieldNames(ref *openapi3.SchemaRef) []string {
	if ref == nil || ref.Value == nil {
		return nil
	}
	schema := ref.Value
	var names []string
	for name := range schema.Properties {
		names = append(names, name)
	}
	if schema.Items != nil && schema.Items.Value != nil {
		for name := range schema.Items.Value.Properties {
			names = append(names, name)
		}
	}
	return sortedUnique(names)
}

// schemasFromDoc records every component schema the way list_schemas reports it,
// including the endpoint-usage count and the description.
func schemasFromDoc(doc *openapi3.T) []Schema {
	infos := analyzer.ListSchemas(doc, "")
	if len(infos) == 0 {
		return nil
	}
	out := make([]Schema, 0, len(infos))
	for _, info := range infos {
		out = append(out, Schema{
			Name:        info.Name,
			FieldCount:  info.FieldCount,
			UsedBy:      info.UsedBy,
			Description: info.Description,
		})
	}
	return out
}

// tagsFromDoc records the tag analysis the way analyze_tags reports it, including
// the "untagged" bucket and the declared descriptions.
func tagsFromDoc(doc *openapi3.T) []Tag {
	summaries := analyzer.AnalyzeTags(doc)
	if len(summaries) == 0 {
		return nil
	}
	out := make([]Tag, 0, len(summaries))
	for _, summary := range summaries {
		breakdown := make(map[string]int, len(summary.MethodBreakdown))
		for method, count := range summary.MethodBreakdown {
			breakdown[method] = count
		}
		out = append(out, Tag{
			Name:            summary.Name,
			Description:     summary.Description,
			EndpointCount:   summary.EndpointCount,
			MethodBreakdown: breakdown,
		})
	}
	return out
}

// sortedUnique returns values sorted and deduplicated, so the catalog rows do not
// depend on map iteration order.
func sortedUnique(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	sort.Strings(values)
	out := values[:0:0]
	for i, value := range values {
		if i == 0 || value != values[i-1] {
			out = append(out, value)
		}
	}
	return out
}
