package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hnkatze/swagger-mcp-go/internal/store"
)

// fixtureDir is resolved once at package init, before any test changes the
// working directory.
var fixtureDir = func() string {
	abs, err := filepath.Abs(filepath.Join("..", "..", "testdata"))
	if err != nil {
		panic(err)
	}
	return abs
}()

// fixturePath returns the absolute path of a repository testdata fixture so a
// test can index it after changing the working directory.
func fixturePath(t *testing.T, name string) string {
	t.Helper()
	abs := filepath.Join(fixtureDir, name)
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("fixture %s missing: %v", abs, err)
	}
	return abs
}

// workdir prepares an isolated working directory with no store above it.
func workdir(t *testing.T) string {
	t.Helper()
	t.Setenv(store.EnvDir, "")
	dir := t.TempDir()
	t.Chdir(dir)
	return dir
}

// runIndexOut runs the index subcommand against the current working directory.
func runIndexOut(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	if err := runIndex(args, &out); err != nil {
		t.Fatalf("runIndex(%v): %v", args, err)
	}
	return out.String()
}

func openStoreFromDir(t *testing.T, dir string) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(dir, store.DirName))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// writeYAMLFixture writes a second, distinct spec so two specs can coexist.
func writeYAMLFixture(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "tiny.yaml")
	const spec = `openapi: 3.0.3
info:
  title: Tiny API
  version: 2.0.0
paths:
  /ping:
    get:
      summary: Ping
      responses:
        '200':
          description: ok
components:
  schemas:
    Ping:
      type: object
      properties:
        ok:
          type: boolean
`
	if err := os.WriteFile(path, []byte(spec), 0o644); err != nil {
		t.Fatalf("write yaml fixture: %v", err)
	}
	return path
}

// TestDeriveName covers name derivation from files and URLs, which is the
// addressing key for every later task.
func TestDeriveName(t *testing.T) {
	cases := []struct {
		source string
		want   string
	}{
		{"testdata/petstore.json", "petstore"},
		{"/tmp/specs/billing-api.yaml", "billing-api"},
		{`C:\work\specs\payments.yml`, "payments"},
		{"https://example.com/openapi/petstore.json", "petstore"},
		{"https://example.com/openapi/petstore.json?version=2", "petstore"},
		{"https://example.com/", "example.com"},
		{"https://example.com", "example.com"},
		{"", "spec"},
	}
	for _, tc := range cases {
		if got := deriveName(tc.source); got != tc.want {
			t.Errorf("deriveName(%q) = %q, want %q", tc.source, got, tc.want)
		}
	}
}

// TestRunIndexFromLocalFixture covers the core S3 write: one file in, one row
// out plus the metadata needed by later tasks.
func TestRunIndexFromLocalFixture(t *testing.T) {
	dir := workdir(t)
	fixture := fixturePath(t, "petstore.json")

	out := runIndexOut(t, fixture)
	if !strings.Contains(out, "petstore") {
		t.Fatalf("output %q does not report the indexed spec name", out)
	}

	st := openStoreFromDir(t, dir)
	if _, err := os.Stat(st.Path()); err != nil {
		t.Fatalf("store file missing at %s: %v", st.Path(), err)
	}
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
	if got.Source != fixture {
		t.Errorf("Source = %q, want %q", got.Source, fixture)
	}
	if got.Title != "Petstore" || got.Version != "1.0.0" {
		t.Errorf("title/version = %q/%q", got.Title, got.Version)
	}
	if got.EndpointCount != 5 || got.SchemaCount != 4 {
		t.Errorf("counts = %d endpoints / %d schemas, want 5/4", got.EndpointCount, got.SchemaCount)
	}
	if got.SHA256 == "" || got.FetchedAt.IsZero() {
		t.Errorf("missing source metadata: sha256=%q fetched_at=%v", got.SHA256, got.FetchedAt)
	}
}

// TestRunIndexReindexIsIdempotent covers "reindexing the same source updates in
// place" and the --refresh flag: without it an identical source is skipped,
// with it the stored copy is rewritten.
func TestRunIndexReindexIsIdempotent(t *testing.T) {
	dir := workdir(t)
	fixture := fixturePath(t, "petstore.json")

	runIndexOut(t, fixture)
	st := openStoreFromDir(t, dir)
	first, err := st.Meta("petstore")
	if err != nil {
		t.Fatalf("Meta after first index: %v", err)
	}
	unchangedOut := runIndexOut(t, fixture)
	if !strings.Contains(unchangedOut, "unchanged") {
		t.Fatalf("second index of the same source printed %q, want an unchanged report", unchangedOut)
	}
	second, err := st.Meta("petstore")
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if !second.FetchedAt.Equal(first.FetchedAt) {
		t.Errorf("unchanged reindex moved FetchedAt from %v to %v", first.FetchedAt, second.FetchedAt)
	}

	runIndexOut(t, "--refresh", fixture)
	third, err := st.Meta("petstore")
	if err != nil {
		t.Fatalf("Meta after refresh: %v", err)
	}
	if !third.FetchedAt.After(first.FetchedAt) {
		t.Errorf("--refresh did not rewrite FetchedAt: %v then %v", first.FetchedAt, third.FetchedAt)
	}
	if third.SHA256 != first.SHA256 {
		t.Errorf("--refresh changed the content hash for identical bytes: %q vs %q", first.SHA256, third.SHA256)
	}
	specs, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("reindexing left %d rows, want 1", len(specs))
	}
}

// TestRunIndexKeepsTwoSpecsApart covers one store holding several specs, from
// both a JSON and a YAML source.
func TestRunIndexKeepsTwoSpecsApart(t *testing.T) {
	dir := workdir(t)
	fixture := fixturePath(t, "petstore.json")
	yaml := writeYAMLFixture(t, t.TempDir())

	runIndexOut(t, fixture, yaml)

	specs, err := openStoreFromDir(t, dir).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("List returned %d specs, want 2: %+v", len(specs), specs)
	}
	if specs[0].Name != "petstore" || specs[1].Name != "tiny" {
		t.Fatalf("names = %q/%q, want petstore/tiny", specs[0].Name, specs[1].Name)
	}
	if specs[1].Title != "Tiny API" || specs[1].Version != "2.0.0" {
		t.Errorf("yaml spec metadata = %q/%q", specs[1].Title, specs[1].Version)
	}
	if specs[1].EndpointCount != 1 || specs[1].SchemaCount != 1 {
		t.Errorf("yaml counts = %d/%d, want 1/1", specs[1].EndpointCount, specs[1].SchemaCount)
	}
}

// TestRunIndexNameOverride covers --name and its single-source restriction.
func TestRunIndexNameOverride(t *testing.T) {
	dir := workdir(t)
	fixture := fixturePath(t, "petstore.json")

	runIndexOut(t, "--name", "pets-v1", fixture)
	specs, err := openStoreFromDir(t, dir).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(specs) != 1 || specs[0].Name != "pets-v1" {
		t.Fatalf("--name was ignored: %+v", specs)
	}

	err = runIndex([]string{"--name", "one-name", fixture, writeYAMLFixture(t, t.TempDir())}, io.Discard)
	if err == nil {
		t.Fatal("--name with several sources was accepted")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error %q does not explain the --name restriction", err.Error())
	}
}

// TestRunIndexDiscoversStoreFromNestedDirectory covers S2 discovery from the
// index command: an existing store above the working directory wins.
func TestRunIndexDiscoversStoreFromNestedDirectory(t *testing.T) {
	t.Setenv(store.EnvDir, "")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, store.DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "services", "api")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	runIndexOut(t, fixturePath(t, "petstore.json"))

	if _, err := os.Stat(filepath.Join(root, store.DirName, store.DBFileName)); err != nil {
		t.Fatalf("store was not created at the project root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(nested, store.DirName)); err == nil {
		t.Fatal("index created a second store in the nested directory")
	}
	specs, err := openStoreFromDir(t, root).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(specs) != 1 || specs[0].Name != "petstore" {
		t.Fatalf("root store content = %+v", specs)
	}
}

// TestRunIndexRequiresSources covers the usage error.
func TestRunIndexRequiresSources(t *testing.T) {
	workdir(t)
	if err := runIndex(nil, io.Discard); err == nil {
		t.Fatal("index with no sources succeeded")
	}
}

// TestRunIndexRejectsUnparsableSource covers a source that is not an OpenAPI
// document: nothing may be written to the store.
func TestRunIndexRejectsUnparsableSource(t *testing.T) {
	dir := workdir(t)
	bad := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runIndex([]string{bad}, io.Discard); err == nil {
		t.Fatal("index accepted an unparsable source")
	}
	st := openStoreFromDir(t, dir)
	specs, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(specs) != 0 {
		t.Fatalf("failed index wrote %d rows: %+v", len(specs), specs)
	}
}

// A spec the server would happily serve must not be rejected by the indexer just
// because the loader normalizes a field before validating it.
func TestIndex_AcceptsSpecsTheServerNormalizes(t *testing.T) {
	dir := workdir(t)
	source := filepath.Join(dir, "upper-scheme.json")
	spec := `{"openapi":"3.0.0",
		"info":{"title":"Upper","version":"1.0.0"},
		"components":{"securitySchemes":{"bearerAuth":{"type":"http","scheme":"Bearer"}}},
		"paths":{"/ping":{"get":{"responses":{"200":{"description":"ok"}}}}}}`
	if err := os.WriteFile(source, []byte(spec), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	runIndexOut(t, source)
}
