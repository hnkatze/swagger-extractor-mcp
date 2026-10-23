package loader

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/hnkatze/swagger-extractor-mcp/internal/types"
)

// Package loader turns stored spec bytes into a document a query can use. It
// reads no network and keeps no cache (S1): the store holds the bytes, and the
// only path that fetches is `swagger-mcp index`.

// PrepareDocument applies the normalizations the server performs before it
// serves a document, so an indexer can validate with exactly the rules a query
// would hit instead of reimplementing (or missing) them.
func PrepareDocument(doc *openapi3.T) {
	sanitizeSecuritySchemes(doc)
}

// ValidateDocument reports the violations doc.Validate finds, using the same
// relaxed options the server applies when it loads a document, so the index and
// a query can never disagree about whether a spec is valid. It does not
// normalize anything first, so values a load silently fixes are reported as
// written. The result is sorted and is empty (never nil) for a valid document.
func ValidateDocument(doc *openapi3.T) []string {
	violations := []string{}
	err := doc.Validate(context.Background(), openapi3.DisableExamplesValidation(), openapi3.DisableSchemaDefaultsValidation())
	if err == nil {
		return violations
	}
	var multi openapi3.MultiError
	if errors.As(err, &multi) {
		for _, e := range multi {
			violations = append(violations, e.Error())
		}
	} else {
		violations = append(violations, err.Error())
	}
	sort.Strings(violations)
	return violations
}

// sanitizeSecuritySchemes normalizes security scheme fields that real-world specs
// often get wrong (e.g. "Bearer" instead of "bearer").
func sanitizeSecuritySchemes(doc *openapi3.T) {
	if doc.Components == nil || doc.Components.SecuritySchemes == nil {
		return
	}
	for _, ref := range doc.Components.SecuritySchemes {
		if ref == nil || ref.Value == nil {
			continue
		}
		ss := ref.Value
		if ss.Type == "http" && ss.Scheme != "" {
			ss.Scheme = strings.ToLower(ss.Scheme)
		}
	}
}

// ParseDocument parses raw bytes into an OpenAPI document. It reads no network
// and no cache: a query hands it the bytes the store holds, so the same parse
// rules apply whether a document arrived through the loader or the index.
func ParseDocument(data []byte) (*openapi3.T, error) {
	oaLoader := openapi3.NewLoader()
	doc, err := oaLoader.LoadFromData(data)
	if err != nil {
		return nil, &types.ToolError{
			Code:    types.ErrParseError,
			Message: "failed to parse OpenAPI spec",
			Details: err.Error(),
		}
	}
	return doc, nil
}
