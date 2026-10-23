package store

import (
	"errors"
	"testing"
)

// tinyRaw is a second, structurally distinct document: one endpoint, one schema
// and one tag, so a delete test can tell which catalog rows survived.
const tinyRaw = `{
	"openapi": "3.0.3",
	"info": {"title": "Tiny API", "version": "2.0.0"},
	"paths": {
		"/ping": {
			"get": {
				"summary": "Ping",
				"tags": ["Infra"],
				"responses": {"200": {"description": "ok"}}
			}
		}
	},
	"components": {
		"schemas": {
			"Ping": {"type": "object", "properties": {"ok": {"type": "boolean"}}}
		}
	}
}`

// tinyRecord builds the record of the tiny fixture under name/source.
func tinyRecord(t *testing.T) Record {
	t.Helper()
	raw := []byte(tinyRaw)
	rec := NewRecord(parseDoc(t, raw), raw)
	rec.Name = "tiny"
	rec.Source = "testdata/tiny.json"
	return rec
}

// orphanRows returns, per catalog table, how many rows point at a spec id that no
// longer exists. That is exactly the damage an incomplete delete leaves behind:
// rows nobody can reach, holding counts the tools would still report.
func orphanRows(t *testing.T, st *Store) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, table := range catalogTables {
		var n int
		if err := st.db.QueryRow(
			"SELECT COUNT(*) FROM " + table + " WHERE spec_id NOT IN (SELECT id FROM specs)",
		).Scan(&n); err != nil {
			t.Fatalf("count orphan rows of %s: %v", table, err)
		}
		out[table] = n
	}
	return out
}

// TestDeleteRemovesOneSpecAndItsCatalogRows covers S10: unindexing takes exactly
// one spec, its document and every catalog row derived from it, and reports what
// it removed. The other spec must stay queryable.
func TestDeleteRemovesOneSpecAndItsCatalogRows(t *testing.T) {
	st := openTempStore(t)
	mustPut(t, st, newRecord(t, "petstore", "testdata/petstore.json"))
	mustPut(t, st, tinyRecord(t))

	removed, err := st.Delete("petstore")
	if err != nil {
		t.Fatalf("Delete(petstore): %v", err)
	}
	if removed.Name != "petstore" || removed.Title != "Petstore" || removed.Version != "1.0.0" {
		t.Errorf("Delete reported %q %q %q, want petstore/Petstore/1.0.0", removed.Name, removed.Title, removed.Version)
	}
	if removed.EndpointCount != 5 || removed.SchemaCount != 4 {
		t.Errorf("Delete reported %d endpoints / %d schemas, want 5/4", removed.EndpointCount, removed.SchemaCount)
	}

	metas, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 || metas[0].Name != "tiny" {
		t.Fatalf("List after Delete = %+v, want only tiny", metas)
	}
	if _, err := st.Meta("petstore"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Meta(petstore) after Delete = %v, want ErrNotFound", err)
	}

	for table, n := range orphanRows(t, st) {
		if n != 0 {
			t.Errorf("%s kept %d rows of the deleted spec", table, n)
		}
	}
	// Only the survivor's catalog is left: one endpoint, one schema, one tag.
	for table, want := range map[string]int{"endpoints": 1, "schemas": 1, "tags": 1} {
		if got := countRows(t, st, table); got != want {
			t.Errorf("%s holds %d rows, want %d (only the surviving spec)", table, got, want)
		}
	}

	// The surviving spec still answers, document included.
	rows, total, err := st.Endpoints("tiny", EndpointFilter{}, 0, 0)
	if err != nil {
		t.Fatalf("Endpoints(tiny) after Delete: %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].Path != "/ping" {
		t.Errorf("Endpoints(tiny) = %+v (total %d), want one GET /ping", rows, total)
	}
	rec, err := st.Get("tiny")
	if err != nil {
		t.Fatalf("Get(tiny) after Delete: %v", err)
	}
	if rec.Title != "Tiny API" {
		t.Errorf("Get(tiny).Title = %q, want Tiny API", rec.Title)
	}
}

// TestDeleteUnknownNameChangesNothing covers the other half of S10: an unknown
// name is an error, and nothing else is touched.
func TestDeleteUnknownNameChangesNothing(t *testing.T) {
	st := openTempStore(t)
	mustPut(t, st, newRecord(t, "petstore", "testdata/petstore.json"))

	before, err := st.List()
	if err != nil {
		t.Fatalf("List before: %v", err)
	}
	endpointsBefore := countRows(t, st, "endpoints")

	if _, err := st.Delete("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete(unknown) = %v, want ErrNotFound", err)
	}

	after, err := st.List()
	if err != nil {
		t.Fatalf("List after: %v", err)
	}
	if len(after) != len(before) || after[0].Name != before[0].Name || !after[0].FetchedAt.Equal(before[0].FetchedAt) {
		t.Errorf("Delete of an unknown name changed the index: %+v then %+v", before, after)
	}
	if got := countRows(t, st, "endpoints"); got != endpointsBefore {
		t.Errorf("endpoints went from %d to %d rows on an unknown name", endpointsBefore, got)
	}
}

// TestDeleteLastSpecLeavesAnEmptyStore covers the empty case: the row is gone,
// the store itself is not, so the tools keep answering from a store they can
// open instead of reporting a missing one.
func TestDeleteLastSpecLeavesAnEmptyStore(t *testing.T) {
	st := openTempStore(t)
	mustPut(t, st, tinyRecord(t))

	if _, err := st.Delete("tiny"); err != nil {
		t.Fatalf("Delete(tiny): %v", err)
	}
	metas, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 0 {
		t.Fatalf("List after deleting the last spec = %+v, want none", metas)
	}
	version, err := st.schemaVersion()
	if err != nil {
		t.Fatalf("schemaVersion after deleting the last spec: %v", err)
	}
	if version != SchemaVersion {
		t.Errorf("store schema version = %d after a delete, want %d", version, SchemaVersion)
	}
	for table, n := range orphanRows(t, st) {
		if n != 0 {
			t.Errorf("%s kept %d rows after the last spec was deleted", table, n)
		}
	}
}
