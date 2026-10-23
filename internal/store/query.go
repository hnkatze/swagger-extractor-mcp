package store

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"

	"modernc.org/sqlite" // pure-Go SQLite driver: the build stays CGO_ENABLED=0
)

// The discovery queries answer from the catalog tables, never from the stored
// document (S9). They compare text the way the analyzer does — Go's
// strings.ToLower — so a tag filter or a search query cannot disagree with the
// document-parsed answer: SQLite's own lower() folds ASCII only, so these two
// functions are registered on the driver instead. Registration is global to the
// driver and happens once, before any store is opened.
func init() {
	sqlite.MustRegisterDeterministicScalarFunction("swa_contains_ci", 2, containsCI)
	sqlite.MustRegisterDeterministicScalarFunction("swa_equal_ci", 2, equalCI)
}

// containsCI reports whether the first argument contains the second, comparing
// with strings.ToLower on both sides.
func containsCI(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	haystack, needle, ok := textPair(args)
	if !ok {
		return int64(0), nil
	}
	if strings.Contains(strings.ToLower(haystack), strings.ToLower(needle)) {
		return int64(1), nil
	}
	return int64(0), nil
}

// equalCI reports whether the two arguments are equal after strings.ToLower.
func equalCI(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	a, b, ok := textPair(args)
	if !ok {
		return int64(0), nil
	}
	if strings.ToLower(a) == strings.ToLower(b) {
		return int64(1), nil
	}
	return int64(0), nil
}

// textPair extracts two non-null text arguments.
func textPair(args []driver.Value) (string, string, bool) {
	if len(args) != 2 {
		return "", "", false
	}
	a, okA := args[0].(string)
	b, okB := args[1].(string)
	return a, b, okA && okB
}

// EndpointFilter selects the endpoints a discovery listing returns. Deprecated
// nil means "both current and deprecated", which is what an unfiltered listing
// shows. The fields mean exactly what they mean in analyzer.ListFilter: the
// method is upper-cased before it is compared, the tag is matched without case,
// and the path pattern is a case-sensitive substring.
type EndpointFilter struct {
	Tag         string
	Method      string
	PathPattern string
	Deprecated  *bool
}

// specRow is what every catalog query needs from the specs table: the catalog
// rows of a spec are addressed by its id, and its two fingerprints are what
// prove the catalog is not stale.
type specRow struct {
	id            int64
	sha256        string
	catalogSHA256 string
	endpointCount int
	schemaCount   int
}

func (r specRow) catalogReady() bool { return r.catalogSHA256 == r.sha256 }

// Endpoints returns one page of the spec's endpoints and the total number that
// match, both read from the catalog: list_endpoints never parses the document.
// limit <= 0 means every remaining row.
func (s *Store) Endpoints(name string, f EndpointFilter, limit, offset int) ([]Endpoint, int, error) {
	row, err := s.specRow(name)
	if err != nil {
		return nil, 0, err
	}
	if !row.catalogReady() {
		return nil, 0, fmt.Errorf("spec %q: %w", name, ErrCatalogIncomplete)
	}

	conditions := []string{"e.spec_id = ?"}
	args := []any{row.id}
	if method := strings.ToUpper(f.Method); method != "" {
		conditions = append(conditions, "e.method = ?")
		args = append(args, method)
	}
	if f.PathPattern != "" {
		conditions = append(conditions, "instr(e.path, ?) > 0")
		args = append(args, f.PathPattern)
	}
	if f.Deprecated != nil {
		conditions = append(conditions, "e.deprecated = ?")
		args = append(args, boolInt(*f.Deprecated))
	}
	if f.Tag != "" {
		conditions = append(conditions, `EXISTS (
			SELECT 1 FROM endpoint_tags t
			WHERE t.spec_id = e.spec_id AND t.method = e.method AND t.path = e.path
			  AND swa_equal_ci(t.tag, ?))`)
		args = append(args, f.Tag)
	}
	where := " WHERE " + strings.Join(conditions, " AND ")

	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM endpoints e"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count endpoints of spec %q: %w", name, err)
	}

	page, pageArgs := limitClause(limit, offset)
	rows, err := s.db.Query(
		"SELECT e.method, e.path, e.summary, e.description, e.deprecated FROM endpoints e"+
			where+" ORDER BY e.path, e.method"+page,
		append(args, pageArgs...)...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list endpoints of spec %q: %w", name, err)
	}
	defer rows.Close()

	var out []Endpoint
	for rows.Next() {
		var ep Endpoint
		if err := rows.Scan(&ep.Method, &ep.Path, &ep.Summary, &ep.Description, &ep.Deprecated); err != nil {
			return nil, 0, fmt.Errorf("list endpoints of spec %q: %w", name, err)
		}
		out = append(out, ep)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list endpoints of spec %q: %w", name, err)
	}
	if len(out) == 0 {
		return nil, total, nil
	}
	tags, err := s.endpointTags(row.id)
	if err != nil {
		return nil, 0, err
	}
	for i := range out {
		out[i].Tags = tags[endpointKey{method: out[i].Method, path: out[i].Path}]
	}
	return out, total, nil
}

// searchBaseSQL is the catalog half of search_spec: it reproduces
// analyzer.matchScore in SQL, scoring each endpoint by the first category the
// query occurs in, and leaves the ones that match nowhere at -1. The query is
// bound once through the one-row CTE so the seven comparisons cannot drift.
const searchBaseSQL = `
WITH q(v) AS (SELECT ?)
SELECT e.method, e.path, e.summary, e.description, e.deprecated,
	CASE
		WHEN swa_contains_ci(e.path, q.v) THEN 0
		WHEN swa_contains_ci(e.summary, q.v) THEN 1
		WHEN swa_contains_ci(e.operation_id, q.v) THEN 2
		WHEN swa_contains_ci(e.description, q.v) THEN 3
		WHEN EXISTS (SELECT 1 FROM endpoint_fields f
			WHERE f.spec_id = e.spec_id AND f.method = e.method AND f.path = e.path
			  AND f.kind = '` + fieldParameter + `' AND swa_contains_ci(f.name, q.v)) THEN 4
		WHEN EXISTS (SELECT 1 FROM endpoint_fields f
			WHERE f.spec_id = e.spec_id AND f.method = e.method AND f.path = e.path
			  AND f.kind = '` + fieldRequestBody + `' AND swa_contains_ci(f.name, q.v)) THEN 5
		WHEN EXISTS (SELECT 1 FROM endpoint_fields f
			WHERE f.spec_id = e.spec_id AND f.method = e.method AND f.path = e.path
			  AND f.kind = '` + fieldResponseBody + `' AND swa_contains_ci(f.name, q.v)) THEN 6
		ELSE -1
	END AS score
FROM endpoints e, q
WHERE e.spec_id = ?`

// SearchEndpoints returns one page of the endpoints whose path, summary,
// operation id, description, parameter names or body field names contain query,
// ranked exactly like analyzer.SearchSpec, plus the total number of matches.
func (s *Store) SearchEndpoints(name, query string, limit, offset int) ([]Endpoint, int, error) {
	row, err := s.specRow(name)
	if err != nil {
		return nil, 0, err
	}
	if !row.catalogReady() {
		return nil, 0, fmt.Errorf("spec %q: %w", name, ErrCatalogIncomplete)
	}

	var total int
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM ("+searchBaseSQL+") WHERE score >= 0", query, row.id,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count search matches in spec %q: %w", name, err)
	}

	page, pageArgs := limitClause(limit, offset)
	args := []any{query, row.id}
	rows, err := s.db.Query(
		searchBaseSQL+" AND score >= 0 ORDER BY score, path, method"+page,
		append(args, pageArgs...)...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("search spec %q: %w", name, err)
	}
	defer rows.Close()

	var out []Endpoint
	for rows.Next() {
		var (
			ep    Endpoint
			score int
		)
		if err := rows.Scan(&ep.Method, &ep.Path, &ep.Summary, &ep.Description, &ep.Deprecated, &score); err != nil {
			return nil, 0, fmt.Errorf("search spec %q: %w", name, err)
		}
		out = append(out, ep)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("search spec %q: %w", name, err)
	}
	if len(out) == 0 {
		return nil, total, nil
	}
	tags, err := s.endpointTags(row.id)
	if err != nil {
		return nil, 0, err
	}
	for i := range out {
		out[i].Tags = tags[endpointKey{method: out[i].Method, path: out[i].Path}]
	}
	return out, total, nil
}

// Schemas returns one page of the spec's component schemas, ordered by usage then
// name, plus the total number that match. A spec without component schemas
// returns no rows at all (nil, not an empty page), which is how list_schemas
// tells "this spec has none" from "the filter excluded all of them".
func (s *Store) Schemas(name, filter string, limit, offset int) ([]Schema, int, error) {
	row, err := s.specRow(name)
	if err != nil {
		return nil, 0, err
	}
	if !row.catalogReady() {
		return nil, 0, fmt.Errorf("spec %q: %w", name, ErrCatalogIncomplete)
	}
	if row.schemaCount == 0 {
		return nil, 0, nil
	}

	conditions := []string{"s.spec_id = ?"}
	args := []any{row.id}
	if filter != "" {
		conditions = append(conditions, "swa_contains_ci(s.name, ?)")
		args = append(args, filter)
	}
	where := " WHERE " + strings.Join(conditions, " AND ")

	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM schemas s"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count schemas of spec %q: %w", name, err)
	}

	page, pageArgs := limitClause(limit, offset)
	rows, err := s.db.Query(
		"SELECT s.name, s.field_count, s.used_by, s.description FROM schemas s"+
			where+" ORDER BY s.used_by DESC, s.name ASC"+page,
		append(args, pageArgs...)...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list schemas of spec %q: %w", name, err)
	}
	defer rows.Close()

	out := make([]Schema, 0, 16)
	for rows.Next() {
		var schema Schema
		if err := rows.Scan(&schema.Name, &schema.FieldCount, &schema.UsedBy, &schema.Description); err != nil {
			return nil, 0, fmt.Errorf("list schemas of spec %q: %w", name, err)
		}
		out = append(out, schema)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list schemas of spec %q: %w", name, err)
	}
	return out, total, nil
}

// Tags returns the spec's tag analysis: one entry per tag with its declared
// description, endpoint count and method breakdown, ordered by name. It is always
// a slice (never nil), matching what analyze_tags reports.
func (s *Store) Tags(name string) ([]Tag, error) {
	row, err := s.specRow(name)
	if err != nil {
		return nil, err
	}
	if !row.catalogReady() {
		return nil, fmt.Errorf("spec %q: %w", name, ErrCatalogIncomplete)
	}

	rows, err := s.db.Query(`
		SELECT t.name, t.description, t.endpoint_count,
		       COALESCE(m.method, ''), COALESCE(m.endpoint_count, 0)
		FROM tags t
		LEFT JOIN tag_methods m ON m.spec_id = t.spec_id AND m.tag = t.name
		WHERE t.spec_id = ?
		ORDER BY t.name, m.method`, row.id)
	if err != nil {
		return nil, fmt.Errorf("read tags of spec %q: %w", name, err)
	}
	defer rows.Close()

	tags := []Tag{}
	index := make(map[string]int)
	for rows.Next() {
		var (
			tagName, description, method string
			endpoints, methodCount       int
		)
		if err := rows.Scan(&tagName, &description, &endpoints, &method, &methodCount); err != nil {
			return nil, fmt.Errorf("read tags of spec %q: %w", name, err)
		}
		i, ok := index[tagName]
		if !ok {
			tags = append(tags, Tag{
				Name:            tagName,
				Description:     description,
				EndpointCount:   endpoints,
				MethodBreakdown: map[string]int{},
			})
			i = len(tags) - 1
			index[tagName] = i
		}
		if method != "" {
			tags[i].MethodBreakdown[method] = methodCount
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read tags of spec %q: %w", name, err)
	}
	return tags, nil
}

// endpointKey identifies one endpoint within a spec.
type endpointKey struct {
	method string
	path   string
}

// endpointTags returns the tags of every endpoint of one spec, in the order the
// document listed them, so a discovery listing can render them unchanged.
func (s *Store) endpointTags(specID int64) (map[endpointKey][]string, error) {
	rows, err := s.db.Query(
		"SELECT method, path, tag FROM endpoint_tags WHERE spec_id = ? ORDER BY method, path, pos", specID,
	)
	if err != nil {
		return nil, fmt.Errorf("read endpoint tags: %w", err)
	}
	defer rows.Close()

	out := make(map[endpointKey][]string)
	for rows.Next() {
		var key endpointKey
		var tag string
		if err := rows.Scan(&key.method, &key.path, &tag); err != nil {
			return nil, fmt.Errorf("read endpoint tags: %w", err)
		}
		out[key] = append(out[key], tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read endpoint tags: %w", err)
	}
	return out, nil
}

// specRow loads the catalog identity of one spec, or ErrNotFound.
func (s *Store) specRow(name string) (specRow, error) {
	var row specRow
	err := s.db.QueryRow(
		"SELECT id, sha256, catalog_sha256, endpoint_count, schema_count FROM specs WHERE name = ?", name,
	).Scan(&row.id, &row.sha256, &row.catalogSHA256, &row.endpointCount, &row.schemaCount)
	if errors.Is(err, sql.ErrNoRows) {
		return specRow{}, fmt.Errorf("spec %q: %w", name, ErrNotFound)
	}
	if err != nil {
		return specRow{}, fmt.Errorf("read spec %q: %w", name, err)
	}
	return row, nil
}

// limitClause renders the paging tail of a discovery query. limit <= 0 means
// "every remaining row", which SQLite spells LIMIT -1.
func limitClause(limit, offset int) (string, []any) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		if offset == 0 {
			return "", nil
		}
		return " LIMIT -1 OFFSET ?", []any{offset}
	}
	return " LIMIT ? OFFSET ?", []any{limit, offset}
}
