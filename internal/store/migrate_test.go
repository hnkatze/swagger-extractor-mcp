package store

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// schemaV1Frozen is the version-1 layout exactly as version 1 shipped it. It is
// frozen in the test on purpose: the migration must be exercised against a real
// old store, not against a re-creation of whatever the current code writes.
var schemaV1Frozen = []string{
	`CREATE TABLE specs (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		name           TEXT    NOT NULL UNIQUE,
		source         TEXT    NOT NULL UNIQUE,
		etag           TEXT    NOT NULL DEFAULT '',
		sha256         TEXT    NOT NULL,
		fetched_at     TEXT    NOT NULL,
		title          TEXT    NOT NULL DEFAULT '',
		version        TEXT    NOT NULL DEFAULT '',
		endpoint_count INTEGER NOT NULL DEFAULT 0,
		schema_count   INTEGER NOT NULL DEFAULT 0,
		document       BLOB    NOT NULL
	) STRICT`,
	`CREATE TABLE endpoints (
		spec_id    INTEGER NOT NULL REFERENCES specs(id) ON DELETE CASCADE,
		method     TEXT    NOT NULL,
		path       TEXT    NOT NULL,
		summary    TEXT    NOT NULL DEFAULT '',
		tags       TEXT    NOT NULL DEFAULT '',
		deprecated INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (spec_id, method, path)
	) STRICT`,
	`CREATE INDEX idx_endpoints_path ON endpoints(path)`,
	`CREATE INDEX idx_endpoints_method ON endpoints(method)`,
	`CREATE TABLE schemas (
		spec_id     INTEGER NOT NULL REFERENCES specs(id) ON DELETE CASCADE,
		name        TEXT    NOT NULL,
		field_count INTEGER NOT NULL DEFAULT 0,
		usage_count INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (spec_id, name)
	) STRICT`,
	`CREATE TABLE tags (
		spec_id        INTEGER NOT NULL REFERENCES specs(id) ON DELETE CASCADE,
		name           TEXT    NOT NULL,
		endpoint_count INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (spec_id, name)
	) STRICT`,
}

// v1Spec is one spec as version 1 stored it: the source metadata, a gzipped
// document, the counts v1 recorded and the v1 catalog rows that lacked
// descriptions, operation ids and body field names.
type v1Spec struct {
	name          string
	source        string
	raw           []byte
	title         string
	endpointCount int
	schemaCount   int
}

// buildV1Store writes a version-1 store by hand and leaves PRAGMA user_version
// at 1, which is what an installed 0.8.0 wrote.
func buildV1Store(t *testing.T, dir string, specs ...v1Spec) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create store directory: %v", err)
	}
	path := filepath.Join(dir, DBFileName)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open v1 database: %v", err)
	}
	defer db.Close()

	for _, stmt := range schemaV1Frozen {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("create v1 schema: %v", err)
		}
	}
	for _, spec := range specs {
		blob, err := compress(spec.raw)
		if err != nil {
			t.Fatalf("compress fixture: %v", err)
		}
		sum := sha256.Sum256(spec.raw)
		if _, err := db.Exec(`
			INSERT INTO specs (name, source, etag, sha256, fetched_at, title, version,
				endpoint_count, schema_count, document)
			VALUES (?, ?, '"etag-v1"', ?, '2025-01-02T03:04:05Z', ?, '1.0.0', ?, ?, ?)`,
			spec.name, spec.source, hex.EncodeToString(sum[:]), spec.title, spec.endpointCount, spec.schemaCount, blob,
		); err != nil {
			t.Fatalf("insert v1 spec %q: %v", spec.name, err)
		}
		// The v1 catalog: endpoints, schemas and tags with the columns v1 had,
		// so the migration has something real to replace.
		var id int64
		if err := db.QueryRow("SELECT id FROM specs WHERE name = ?", spec.name).Scan(&id); err != nil {
			t.Fatalf("read v1 spec id: %v", err)
		}
		if _, err := db.Exec(
			"INSERT INTO endpoints (spec_id, method, path, summary, tags, deprecated) VALUES (?, ?, ?, ?, ?, ?)",
			id, "GET", "/pets", "List all pets", "pets", 0,
		); err != nil {
			t.Fatalf("insert v1 endpoint: %v", err)
		}
		if _, err := db.Exec(
			"INSERT INTO schemas (spec_id, name, field_count, usage_count) VALUES (?, 'Pet', 5, 9)",
			id,
		); err != nil {
			t.Fatalf("insert v1 schema: %v", err)
		}
		if _, err := db.Exec(
			"INSERT INTO tags (spec_id, name, endpoint_count) VALUES (?, 'pets', 4)", id,
		); err != nil {
			t.Fatalf("insert v1 tag: %v", err)
		}
	}
	if _, err := db.Exec("PRAGMA user_version = 1"); err != nil {
		t.Fatalf("record v1 schema version: %v", err)
	}
	return path
}

// userVersion reads PRAGMA user_version straight from the file.
func userVersion(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	return version
}

// catalogRows counts the rows of a catalog table for one spec, reading the
// database directly: the migration is about on-disk state, not about an API.
func catalogRows(t *testing.T, path, table, name string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM "+table+" WHERE spec_id = (SELECT id FROM specs WHERE name = ?)", name,
	).Scan(&n); err != nil {
		t.Fatalf("count %s rows for %q: %v", table, name, err)
	}
	return n
}

// catalogFieldNames lists one endpoint's catalog field names of a kind, ordered,
// so a test can state exactly which request-body properties were backfilled.
func catalogFieldNames(t *testing.T, path, spec, method, endpointPath, kind string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(`
		SELECT name FROM endpoint_fields
		WHERE spec_id = (SELECT id FROM specs WHERE name = ?)
		  AND method = ? AND path = ? AND kind = ?
		ORDER BY name`, spec, method, endpointPath, kind)
	if err != nil {
		t.Fatalf("read endpoint_fields: %v", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan endpoint_fields: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read endpoint_fields: %v", err)
	}
	return names
}

// describedSpec is a small spec that carries the prose petstore lacks: an
// operation description and a schema description, so the migration test can
// prove those columns are backfilled.
func describedSpec(t *testing.T) []byte {
	t.Helper()
	return []byte(`{
	  "openapi": "3.0.3",
	  "info": {"title": "Billing", "version": "1.0.0"},
	  "paths": {"/invoices": {"get": {
	    "operationId": "listInvoices",
	    "summary": "List invoices",
	    "description": "Returns invoices, newest first.",
	    "tags": ["billing"],
	    "responses": {"200": {"description": "ok", "content": {
	      "application/json": {"schema": {"$ref": "#/components/schemas/Invoice"}}
	    }}}
	  }}},
	  "components": {"schemas": {"Invoice": {
	    "type": "object",
	    "description": "A billable invoice.",
	    "properties": {"id": {"type": "string"}, "amount": {"type": "number"}}
	  }}}
	}`)
}

// TestMigrateV1StoreBackfillsTheCatalog is S9's migration requirement: opening a
// v1 store for writing upgrades it in place, re-deriving the catalog from the
// stored documents, and loses nothing.
func TestMigrateV1StoreBackfillsTheCatalog(t *testing.T) {
	raw := petstoreRaw(t)
	dir := filepath.Join(t.TempDir(), DirName)
	path := buildV1Store(t, dir,
		v1Spec{name: "petstore", source: "/specs/petstore.json", raw: raw, title: "Petstore", endpointCount: 5, schemaCount: 4},
		v1Spec{name: "billing", source: "/specs/billing.json", raw: describedSpec(t), title: "Billing", endpointCount: 1, schemaCount: 1},
	)

	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open(v1 store): %v", err)
	}
	defer st.Close()

	if got := userVersion(t, path); got != SchemaVersion {
		t.Fatalf("user_version after migration = %d, want %d", got, SchemaVersion)
	}

	// Nothing was lost: the document, its metadata and the source stay put.
	rec, err := st.Get("petstore")
	if err != nil {
		t.Fatalf("Get after migration: %v", err)
	}
	if !bytes.Equal(rec.Document, raw) {
		t.Fatal("migration changed the stored document")
	}
	if rec.Source != "/specs/petstore.json" || rec.ETag != `"etag-v1"` || rec.Title != "Petstore" {
		t.Fatalf("migration lost spec metadata: %+v", rec)
	}
	if rec.FetchedAt.IsZero() {
		t.Error("migration lost fetched_at")
	}

	// The catalog is complete, not merely present: the counts match the
	// document the store still holds.
	if n := catalogRows(t, path, "endpoints", "petstore"); n != 5 {
		t.Errorf("endpoint rows after migration = %d, want 5", n)
	}
	if n := catalogRows(t, path, "schemas", "petstore"); n != 4 {
		t.Errorf("schema rows after migration = %d, want 4", n)
	}
	if n := catalogRows(t, path, "tags", "petstore"); n != 2 {
		t.Errorf("tag rows after migration = %d, want 2 (pets, store)", n)
	}

	// What v1 could not answer is now in the catalog: the operation id and the
	// request-body field names search_spec matches on.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	var opID, summary string
	if err := db.QueryRow(
		`SELECT operation_id, summary FROM endpoints
		 WHERE method = 'POST' AND path = '/pets'
		   AND spec_id = (SELECT id FROM specs WHERE name = 'petstore')`,
	).Scan(&opID, &summary); err != nil {
		t.Fatalf("read POST /pets row: %v", err)
	}
	if opID != "createPet" || summary != "Create a pet" {
		t.Errorf("POST /pets row = (%q, %q), want createPet/Create a pet", opID, summary)
	}
	if got, want := catalogFieldNames(t, path, "petstore", "POST", "/pets", "request_body"), []string{"name", "tag"}; !equalStrings(got, want) {
		t.Errorf("POST /pets request_body fields = %v, want %v", got, want)
	}
	if got, want := catalogFieldNames(t, path, "petstore", "GET", "/pets", "parameter"), []string{"limit", "status"}; !equalStrings(got, want) {
		t.Errorf("GET /pets parameter names = %v, want %v", got, want)
	}

	// Prose is backfilled too: endpoint descriptions (which the JSON listing
	// reports) and schema descriptions.
	var endpointDescription, schemaDescription string
	if err := db.QueryRow(`
		SELECT e.description, s.description
		FROM endpoints e, schemas s
		WHERE e.spec_id = (SELECT id FROM specs WHERE name = 'billing')
		  AND e.path = '/invoices'
		  AND s.spec_id = e.spec_id AND s.name = 'Invoice'`,
	).Scan(&endpointDescription, &schemaDescription); err != nil {
		t.Fatalf("read backfilled descriptions: %v", err)
	}
	if endpointDescription != "Returns invoices, newest first." {
		t.Errorf("endpoint description = %q, want the document's description", endpointDescription)
	}
	if schemaDescription != "A billable invoice." {
		t.Errorf("schema description = %q, want the document's description", schemaDescription)
	}

	// Schema descriptions and endpoint-usage counts are backfilled too.
	var usedBy, fieldCount int
	if err := db.QueryRow(
		`SELECT used_by, field_count FROM schemas
		 WHERE name = 'Pet' AND spec_id = (SELECT id FROM specs WHERE name = 'petstore')`,
	).Scan(&usedBy, &fieldCount); err != nil {
		t.Fatalf("read Pet schema row: %v", err)
	}
	if usedBy < 1 {
		t.Errorf("Pet used_by = %d, want at least 1", usedBy)
	}
	if fieldCount != 5 {
		t.Errorf("Pet field_count = %d, want 5", fieldCount)
	}

	// Tag method breakdown, which analyze_tags reports per tag.
	var getCount int
	if err := db.QueryRow(
		`SELECT endpoint_count FROM tag_methods
		 WHERE tag = 'pets' AND method = 'GET'
		   AND spec_id = (SELECT id FROM specs WHERE name = 'petstore')`,
	).Scan(&getCount); err != nil {
		t.Fatalf("read pets/GET tag_methods row: %v", err)
	}
	if getCount != 2 {
		t.Errorf("pets GET endpoint_count = %d, want 2", getCount)
	}

	// No catalog row was left claiming the v1 numbers were current.
	var catalogSHA, sha string
	if err := db.QueryRow("SELECT sha256, catalog_sha256 FROM specs WHERE name = 'petstore'").Scan(&sha, &catalogSHA); err != nil {
		t.Fatalf("read catalog_sha256: %v", err)
	}
	if catalogSHA == "" || catalogSHA != sha {
		t.Errorf("catalog_sha256 = %q, want the document sha256 %q", catalogSHA, sha)
	}
}

// equalStrings compares two string slices element by element (nil equals empty).
func equalStrings(a, b []string) bool {
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

// TestMigrateV1StoreWithUnparseableDocumentKeepsTheStoreWritable covers the one
// case migration cannot backfill: a stored document the parser rejects. The
// upgrade must still finish (otherwise `swagger-mcp index`, the only command
// that can rewrite that document, could never run), and the spec must be marked
// so discovery reports it instead of answering from an empty catalog.
func TestMigrateV1StoreWithUnparseableDocumentKeepsTheStoreWritable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	path := buildV1Store(t, dir, v1Spec{name: "broken", source: "/specs/broken.json", raw: []byte("{not a spec"), title: "Broken", endpointCount: 1, schemaCount: 0})

	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open(v1 store with an unparseable document): %v", err)
	}
	defer st.Close()

	if got := userVersion(t, path); got != SchemaVersion {
		t.Fatalf("user_version after migration = %d, want %d", got, SchemaVersion)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	var sha, catalogSHA string
	if err := db.QueryRow("SELECT sha256, catalog_sha256 FROM specs WHERE name = 'broken'").Scan(&sha, &catalogSHA); err != nil {
		t.Fatalf("read catalog_sha256: %v", err)
	}
	if sha == "" {
		t.Fatal("migration lost the document fingerprint")
	}
	if catalogSHA != "" {
		t.Fatalf("catalog_sha256 = %q, want empty: nothing could be derived from that document", catalogSHA)
	}

	// The catalog holds nothing for that spec, so the missing marker is what
	// tells discovery to report the problem instead of answering "0 endpoints".
	if n := catalogRows(t, path, "endpoints", "broken"); n != 0 {
		t.Errorf("endpoint rows for the unparseable spec = %d, want 0", n)
	}
}

// TestOpenReadOnlyRejectsV1StoreWithTheIndexCommand is the read path half of S9:
// a query may not migrate, so an old store must produce an actionable error
// naming the command to run instead of answering incompletely.
func TestOpenReadOnlyRejectsV1StoreWithTheIndexCommand(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	raw := petstoreRaw(t)
	path := buildV1Store(t, dir, v1Spec{name: "petstore", source: "/specs/petstore.json", raw: raw, title: "Petstore", endpointCount: 5, schemaCount: 4})

	_, err := OpenReadOnly(dir)
	if err == nil {
		t.Fatal("OpenReadOnly accepted a v1 store without migrating it")
	}
	message := err.Error()
	for _, want := range []string{"swagger-mcp index", "1", "2"} {
		if !strings.Contains(message, want) {
			t.Errorf("error %q should mention %q", message, want)
		}
	}

	// The read path wrote nothing: the store is still a v1 store.
	if got := userVersion(t, path); got != 1 {
		t.Fatalf("user_version after a read-only open = %d, want the untouched 1", got)
	}
}
