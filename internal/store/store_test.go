package store

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
)

// petstoreRaw loads the repository fixture used by the whole suite.
func petstoreRaw(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "petstore.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return raw
}

func parseDoc(t *testing.T, raw []byte) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData(raw)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return doc
}

func openTempStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), DirName))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// newRecord builds a record for the petstore fixture under name/source.
func newRecord(t *testing.T, name, source string) Record {
	t.Helper()
	raw := petstoreRaw(t)
	rec := NewRecord(parseDoc(t, raw), raw)
	rec.Name = name
	rec.Source = source
	rec.FetchedAt = time.Now().UTC()
	return rec
}

func mustPut(t *testing.T, st *Store, rec Record) {
	t.Helper()
	if err := st.Put(rec); err != nil {
		t.Fatalf("Put(%q): %v", rec.Name, err)
	}
}

// countRows is test-only evidence that reindexing does not duplicate the
// normalized catalog rows behind the public API.
func countRows(t *testing.T, st *Store, table string) int {
	t.Helper()
	var n int
	if err := st.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// TestOpenIsIdempotentAndVersioned covers "created by the indexer, versioned
// with PRAGMA user_version, idempotent creation".
func TestOpenIsIdempotentAndVersioned(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)

	st, err := Open(dir)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if want := filepath.Join(dir, DBFileName); st.Path() != want {
		t.Fatalf("Path() = %q, want %q", st.Path(), want)
	}
	if _, err := os.Stat(st.Path()); err != nil {
		t.Fatalf("db file missing: %v", err)
	}
	if _, err := st.List(); err != nil {
		t.Fatalf("first List: %v", err)
	}
	var version int
	if err := st.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != SchemaVersion {
		t.Fatalf("user_version = %d, want %d", version, SchemaVersion)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Re-opening an existing store must not fail or wipe it.
	again, err := Open(dir)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer again.Close()
	mustPut(t, again, newRecord(t, "petstore", "testdata/petstore.json"))
	if err := again.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	third, err := Open(dir)
	if err != nil {
		t.Fatalf("third Open: %v", err)
	}
	defer third.Close()
	specs, err := third.List()
	if err != nil {
		t.Fatalf("third List: %v", err)
	}
	if len(specs) != 1 || specs[0].Name != "petstore" {
		t.Fatalf("reopen lost data: %+v", specs)
	}
}

// TestOpenRejectsNewerSchemaVersion keeps an old binary from writing into a
// store layout it does not understand.
func TestOpenRejectsNewerSchemaVersion(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := st.db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatalf("bump user_version: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := Open(dir); err == nil {
		t.Fatal("Open accepted a store written by a newer schema version")
	}
}

// TestPutAndListRoundTrip covers the read path: metadata comes back without the
// document, and the document comes back byte-identical.
func TestPutAndListRoundTrip(t *testing.T) {
	st := openTempStore(t)
	rec := newRecord(t, "petstore", "testdata/petstore.json")
	rec.ETag = `W/"v1"`
	mustPut(t, st, rec)

	specs, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("List returned %d specs, want 1", len(specs))
	}
	got := specs[0]
	if got.Name != "petstore" {
		t.Errorf("Name = %q, want petstore", got.Name)
	}
	if got.Source != "testdata/petstore.json" {
		t.Errorf("Source = %q", got.Source)
	}
	if got.ETag != `W/"v1"` {
		t.Errorf("ETag = %q", got.ETag)
	}
	wantSum := sha256.Sum256(petstoreRaw(t))
	if got.SHA256 != hex.EncodeToString(wantSum[:]) {
		t.Errorf("SHA256 = %q, want sha256 of the source bytes", got.SHA256)
	}
	if got.FetchedAt.IsZero() {
		t.Error("FetchedAt is zero")
	}
	if got.Title != "Petstore" || got.Version != "1.0.0" {
		t.Errorf("title/version = %q/%q, want Petstore/1.0.0", got.Title, got.Version)
	}
	if got.EndpointCount != 5 {
		t.Errorf("EndpointCount = %d, want 5", got.EndpointCount)
	}
	if got.SchemaCount != 4 {
		t.Errorf("SchemaCount = %d, want 4", got.SchemaCount)
	}

	full, err := st.Get("petstore")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(full.Document, petstoreRaw(t)) {
		t.Fatal("Get returned a document that differs from the indexed bytes")
	}
}

// TestGzipRoundTripOfStoredDocument proves the stored blob is compressed and
// still round-trips through the public API.
func TestGzipRoundTripOfStoredDocument(t *testing.T) {
	st := openTempStore(t)
	raw := petstoreRaw(t)
	rec := NewRecord(parseDoc(t, raw), raw)
	rec.Name, rec.Source, rec.FetchedAt = "petstore", "testdata/petstore.json", time.Now().UTC()
	mustPut(t, st, rec)

	var blob []byte
	if err := st.db.QueryRow("SELECT document FROM specs WHERE name = ?", "petstore").Scan(&blob); err != nil {
		t.Fatalf("read blob: %v", err)
	}
	if !bytes.HasPrefix(blob, []byte{0x1f, 0x8b}) {
		t.Fatal("stored document is not gzip-encoded")
	}
	if len(blob) >= len(raw) {
		t.Fatalf("stored blob %d bytes is not smaller than the raw document %d bytes", len(blob), len(raw))
	}

	got, err := st.Get("petstore")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got.Document, raw) {
		t.Fatal("gzip round-trip changed the document")
	}
}

// TestPutIndexesCatalogRows covers the normalized discovery tables.
func TestPutIndexesCatalogRows(t *testing.T) {
	st := openTempStore(t)
	mustPut(t, st, newRecord(t, "petstore", "testdata/petstore.json"))

	if n := countRows(t, st, "endpoints"); n != 5 {
		t.Errorf("endpoint rows = %d, want 5", n)
	}
	if n := countRows(t, st, "schemas"); n != 4 {
		t.Errorf("schema rows = %d, want 4", n)
	}
	if n := countRows(t, st, "tags"); n < 2 {
		t.Errorf("tag rows = %d, want at least 2 (pets, store)", n)
	}

	var method, path, tag string
	if err := st.db.QueryRow(
		"SELECT method, path, tag FROM endpoint_tags WHERE path = ? AND method = ? ORDER BY pos", "/pets", "POST",
	).Scan(&method, &path, &tag); err != nil {
		t.Fatalf("read endpoint tags: %v", err)
	}
	if tag != "pets" {
		t.Errorf("endpoint tags = %q, want pets", tag)
	}

	var fields, usage int
	if err := st.db.QueryRow("SELECT field_count, used_by FROM schemas WHERE name = ?", "Pet").Scan(&fields, &usage); err != nil {
		t.Fatalf("read schema row: %v", err)
	}
	if fields != 5 {
		t.Errorf("Pet field_count = %d, want 5", fields)
	}
	if usage < 1 {
		t.Errorf("Pet used_by = %d, want at least 1", usage)
	}
}

// TestPutReindexReplacesRowInPlace covers S3: reindexing the same source updates
// one row instead of appending a second one.
func TestPutReindexReplacesRowInPlace(t *testing.T) {
	st := openTempStore(t)
	first := newRecord(t, "petstore", "testdata/petstore.json")
	mustPut(t, st, first)
	firstID := specID(t, st, "petstore")

	second := newRecord(t, "petstore", "testdata/petstore.json")
	second.Title = "Petstore v2"
	second.ETag = `W/"v2"`
	second.FetchedAt = first.FetchedAt.Add(time.Minute)
	mustPut(t, st, second)

	specs, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("reindex produced %d rows, want 1: %+v", len(specs), specs)
	}
	if specs[0].Title != "Petstore v2" {
		t.Errorf("Title = %q, want the reindexed title", specs[0].Title)
	}
	if specs[0].ETag != `W/"v2"` {
		t.Errorf("ETag = %q, want the reindexed etag", specs[0].ETag)
	}
	if !specs[0].FetchedAt.Equal(second.FetchedAt) {
		t.Errorf("FetchedAt = %v, want %v", specs[0].FetchedAt, second.FetchedAt)
	}
	if got := specID(t, st, "petstore"); got != firstID {
		t.Errorf("row id = %d, want the original %d (in-place update)", got, firstID)
	}
	if n := countRows(t, st, "endpoints"); n != 5 {
		t.Errorf("endpoint rows after reindex = %d, want 5 (no duplicates)", n)
	}
	if n := countRows(t, st, "schemas"); n != 4 {
		t.Errorf("schema rows after reindex = %d, want 4 (no duplicates)", n)
	}
}

func specID(t *testing.T, st *Store, name string) int64 {
	t.Helper()
	var id int64
	if err := st.db.QueryRow("SELECT id FROM specs WHERE name = ?", name).Scan(&id); err != nil {
		t.Fatalf("read id for %q: %v", name, err)
	}
	return id
}

// TestSameSourceUnderNewNameDoesNotDuplicate covers the rename case: the source
// is still unique, so indexing it under another name replaces the old row.
func TestSameSourceUnderNewNameDoesNotDuplicate(t *testing.T) {
	st := openTempStore(t)
	mustPut(t, st, newRecord(t, "petstore", "testdata/petstore.json"))
	mustPut(t, st, newRecord(t, "pets", "testdata/petstore.json"))

	specs, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("rename produced %d rows, want 1: %+v", len(specs), specs)
	}
	if specs[0].Name != "pets" {
		t.Errorf("Name = %q, want the newest name", specs[0].Name)
	}
}

// TestTwoSpecsCoexist covers S3's "several specs coexist in one store".
func TestTwoSpecsCoexist(t *testing.T) {
	st := openTempStore(t)
	mustPut(t, st, newRecord(t, "petstore", "testdata/petstore.json"))

	other := newRecord(t, "events", "https://example.com/events.json")
	other.Title = "Events"
	mustPut(t, st, other)

	specs, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("List returned %d specs, want 2", len(specs))
	}
	if specs[0].Name != "events" || specs[1].Name != "petstore" {
		t.Fatalf("List order/content = %+v, want events then petstore", specs)
	}
	if n := countRows(t, st, "endpoints"); n != 10 {
		t.Errorf("endpoint rows = %d, want 10 (5 per spec)", n)
	}
}

// TestGetUnknownSpec covers the read path's not-found contract.
func TestGetUnknownSpec(t *testing.T) {
	st := openTempStore(t)
	if _, err := st.Get("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(nope) error = %v, want ErrNotFound", err)
	}
	if _, err := st.Meta("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Meta(nope) error = %v, want ErrNotFound", err)
	}
}

// TestPutRejectsNameOwnedByAnotherSource covers the data-loss defect: two
// sources whose derived name is the same must never overwrite each other, so the
// second source fails and names the source that already owns the name.
func TestPutRejectsNameOwnedByAnotherSource(t *testing.T) {
	st := openTempStore(t)
	original := newRecord(t, "api", "/specs/v1/api.json")
	original.Title = "V1"
	mustPut(t, st, original)
	originalID := specID(t, st, "api")

	intruder := newRecord(t, "api", "/specs/v2/api.json")
	intruder.Title = "V2"
	err := st.Put(intruder)
	if err == nil {
		t.Fatal("Put overwrote a row owned by a different source")
	}
	for _, want := range []string{"/specs/v1/api.json", "--name"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}

	// The refused write left the original row exactly as it was.
	specs, listErr := st.List()
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}
	if len(specs) != 1 {
		t.Fatalf("refused write left %d rows, want 1: %+v", len(specs), specs)
	}
	if specs[0].Name != "api" || specs[0].Source != "/specs/v1/api.json" || specs[0].Title != "V1" {
		t.Fatalf("row was modified by the refused write: %+v", specs[0])
	}
	if got := specID(t, st, "api"); got != originalID {
		t.Errorf("row id = %d, want the original %d", got, originalID)
	}
	if n := countRows(t, st, "endpoints"); n != 5 {
		t.Errorf("endpoint rows = %d, want the original 5", n)
	}
}

// TestOpenReadOnlyDoesNotCreateSchema covers the read path: a query opens the
// store without creating the file, the directory or the schema.
func TestOpenReadOnlyDoesNotCreateSchema(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create store directory: %v", err)
	}
	path := filepath.Join(dir, DBFileName)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("create empty database file: %v", err)
	}

	if _, err := OpenReadOnly(dir); err == nil {
		t.Fatal("OpenReadOnly accepted a database with no schema")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat database: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("read-only open wrote %d bytes into the empty database", info.Size())
	}

	db, err := sql.Open("sqlite", readOnlyDSN(path))
	if err != nil {
		t.Fatalf("open database for inspection: %v", err)
	}
	defer db.Close()
	var tables int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master").Scan(&tables); err != nil {
		t.Fatalf("count tables: %v", err)
	}
	if tables != 0 {
		t.Fatalf("read-only open created %d schema objects in the empty database", tables)
	}
}

// TestOpenReadOnlyReadsExistingStore keeps the query path working on a store the
// indexer wrote.
func TestOpenReadOnlyReadsExistingStore(t *testing.T) {
	// A store under a directory whose name has a space must open too: project
	// paths are not always shell-safe.
	dir := filepath.Join(t.TempDir(), "my project", DirName)
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	mustPut(t, st, newRecord(t, "petstore", "testdata/petstore.json"))
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ro, err := OpenReadOnly(dir)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer ro.Close()
	specs, err := ro.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(specs) != 1 || specs[0].Name != "petstore" {
		t.Fatalf("read-only store content = %+v", specs)
	}
	rec, err := ro.Get("petstore")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(rec.Document, petstoreRaw(t)) {
		t.Fatal("read-only store returned a different document")
	}
}

// TestOpenReadOnlyRejectsNewerSchemaVersion keeps the upgrade message a query
// must report on a store written by a newer binary.
func TestOpenReadOnlyRejectsNewerSchemaVersion(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := st.db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatalf("bump user_version: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err = OpenReadOnly(dir)
	if err == nil {
		t.Fatal("OpenReadOnly accepted a store written by a newer schema version")
	}
	if !strings.Contains(err.Error(), "upgrade swagger-mcp") {
		t.Errorf("error %q should report the upgrade message", err)
	}
}
