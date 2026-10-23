package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
)

// Endpoint is one normalized discovery row: an operation of the spec.
type Endpoint struct {
	Method     string
	Path       string
	Summary    string
	Tags       []string
	Deprecated bool
}

// Schema is one normalized discovery row: a component schema with the counts
// discovery needs without parsing the document.
type Schema struct {
	Name       string
	FieldCount int
	UsageCount int
}

// Tag is one normalized discovery row: a tag and how many operations use it.
type Tag struct {
	Name          string
	EndpointCount int
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
	rec.Tags = tagsFromEndpoints(rec.Endpoints)
	return rec
}

// endpointsFromDoc walks the same method set the loader counts, so the stored
// endpoint_count agrees with what fetch/validate reported before the store.
func endpointsFromDoc(doc *openapi3.T) []Endpoint {
	if doc.Paths == nil {
		return nil
	}
	var out []Endpoint
	for path, item := range doc.Paths.Map() {
		if item == nil {
			continue
		}
		add := func(method string, op *openapi3.Operation) {
			if op == nil {
				return
			}
			out = append(out, Endpoint{
				Method:     method,
				Path:       path,
				Summary:    op.Summary,
				Tags:       append([]string(nil), op.Tags...),
				Deprecated: op.Deprecated,
			})
		}
		add("GET", item.Get)
		add("POST", item.Post)
		add("PUT", item.Put)
		add("DELETE", item.Delete)
		add("PATCH", item.Patch)
		add("HEAD", item.Head)
		add("OPTIONS", item.Options)
		add("TRACE", item.Trace)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

func tagsFromEndpoints(endpoints []Endpoint) []Tag {
	counts := make(map[string]int)
	for _, ep := range endpoints {
		for _, tag := range ep.Tags {
			if tag == "" {
				continue
			}
			counts[tag]++
		}
	}
	tags := make([]Tag, 0, len(counts))
	for name, n := range counts {
		tags = append(tags, Tag{Name: name, EndpointCount: n})
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i].Name < tags[j].Name })
	return tags
}

func schemasFromDoc(doc *openapi3.T) []Schema {
	if doc.Components == nil || doc.Components.Schemas == nil {
		return nil
	}
	usage := schemaUsage(doc)
	out := make([]Schema, 0, len(doc.Components.Schemas))
	for name, ref := range doc.Components.Schemas {
		fields := 0
		if ref != nil && ref.Value != nil {
			fields = len(ref.Value.Properties)
		}
		out = append(out, Schema{Name: name, FieldCount: fields, UsageCount: usage[name]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// schemaUsage counts how often each component schema is referenced. It scans
// the marshalled document for "#/components/schemas/<name>" pointers, which is
// cheap next to the parse that just happened and covers refs from paths,
// parameters, responses and other schemas alike.
func schemaUsage(doc *openapi3.T) map[string]int {
	usage := make(map[string]int)
	blob, err := json.Marshal(doc)
	if err != nil {
		return usage
	}
	const prefix = "#/components/schemas/"
	for idx := 0; ; {
		found := bytes.Index(blob[idx:], []byte(prefix))
		if found < 0 {
			return usage
		}
		start := idx + found + len(prefix)
		end := start
		for end < len(blob) && isSchemaNameByte(blob[end]) {
			end++
		}
		if end > start {
			usage[string(blob[start:end])]++
		}
		idx = start
	}
}

func isSchemaNameByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '_', b == '-', b == '.':
		return true
	}
	return false
}

// tagList renders endpoint tags for the tags column: a comma separated list, so
// a discovery query can match one tag with a LIKE without a join per endpoint.
func tagList(tags []string) string {
	cleaned := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag = strings.TrimSpace(tag); tag != "" {
			cleaned = append(cleaned, tag)
		}
	}
	return strings.Join(cleaned, ",")
}
