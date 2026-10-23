package tools

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/hnkatze/swagger-mcp-go/internal/analyzer"
	"github.com/hnkatze/swagger-mcp-go/internal/loader"
	"github.com/hnkatze/swagger-mcp-go/internal/store"
)

// indexForBenchmark writes raw into a throwaway store and opens it for queries,
// the same way a session does.
func indexForBenchmark(b *testing.B, raw []byte) *store.Store {
	b.Helper()
	dir := filepath.Join(b.TempDir(), store.DirName)
	wst, err := store.Open(dir)
	if err != nil {
		b.Fatalf("open store: %v", err)
	}
	doc, err := loader.ParseDocument(raw)
	if err != nil {
		b.Fatalf("parse fixture: %v", err)
	}
	rec := store.NewRecord(doc, raw)
	rec.Name, rec.Source, rec.FetchedAt = "fixture", "fixture://fixture", time.Now().UTC()
	if err := wst.Put(rec); err != nil {
		b.Fatalf("store fixture: %v", err)
	}
	if err := wst.Close(); err != nil {
		b.Fatalf("close writer: %v", err)
	}
	st, err := store.OpenReadOnly(dir)
	if err != nil {
		b.Fatalf("open read-only store: %v", err)
	}
	b.Cleanup(func() { _ = st.Close() })
	return st
}

// BenchmarkListEndpoints measures the two answers S9 is about: parsing the stored
// document for every listing (the behavior before the catalog) and reading the
// same listing out of the catalog. Run it with
//
//	go test ./internal/tools -run '^$' -bench BenchmarkListEndpoints -benchtime 20x
func BenchmarkListEndpoints(b *testing.B) {
	fixtures := []struct {
		name string
		raw  []byte
	}{
		{"petstore", petstoreFixture(b)},
		{"fixture-130", buildFixtureSpec()},
	}
	for _, fixture := range fixtures {
		st := indexForBenchmark(b, fixture.raw)

		b.Run(fixture.name+"/document", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				rec, err := st.Get("fixture")
				if err != nil {
					b.Fatalf("Get: %v", err)
				}
				doc, err := loader.ParseDocument(rec.Document)
				if err != nil {
					b.Fatalf("parse: %v", err)
				}
				loader.PrepareDocument(doc)
				if got := len(analyzer.ListEndpoints(doc, analyzer.ListFilter{})); got == 0 {
					b.Fatal("no endpoints")
				}
			}
		})

		b.Run(fixture.name+"/catalog", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				rows, _, err := st.Endpoints("fixture", store.EndpointFilter{}, 0, 0)
				if err != nil {
					b.Fatalf("Endpoints: %v", err)
				}
				if len(rows) == 0 {
					b.Fatal("no endpoints")
				}
			}
		})
	}
}
