package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hnkatze/swagger-extractor-mcp/internal/store"
)

// runUnindexOut runs the unindex subcommand against the current working
// directory and returns its output.
func runUnindexOut(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	if err := runUnindex(args, &out); err != nil {
		t.Fatalf("runUnindex(%v): %v", args, err)
	}
	return out.String()
}

// TestRunUnindexRemovesOneSpec covers S10 end to end: two indexed specs, one
// unindexed, the report names what went away and the other spec still answers.
func TestRunUnindexRemovesOneSpec(t *testing.T) {
	dir := workdir(t)
	yaml := writeYAMLFixture(t, t.TempDir())
	runIndexOut(t, fixturePath(t, "petstore.json"), yaml)

	out := runUnindexOut(t, "petstore")
	for _, want := range []string{"removed petstore", "Petstore", "1.0.0", "5 endpoints", "4 schemas", "store: "} {
		if !strings.Contains(out, want) {
			t.Errorf("unindex output %q does not report %q", out, want)
		}
	}

	st := openStoreFromDir(t, dir)
	metas, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 || metas[0].Name != "tiny" {
		t.Fatalf("index after unindex = %+v, want only tiny", metas)
	}
	if _, err := st.Meta("petstore"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Meta(petstore) after unindex = %v, want ErrNotFound", err)
	}
	// The catalog of the survivor is intact, so discovery still answers from SQL.
	_, total, err := st.Endpoints("tiny", store.EndpointFilter{}, 0, 0)
	if err != nil {
		t.Fatalf("Endpoints(tiny) after unindex: %v", err)
	}
	if total != 1 {
		t.Errorf("tiny has %d endpoints after unindex, want 1", total)
	}
}

// TestRunUnindexUnknownNameListsIndexedNames covers the failure half of S10: the
// error lists the names that exist, exits non-zero, and writes nothing.
func TestRunUnindexUnknownNameListsIndexedNames(t *testing.T) {
	dir := workdir(t)
	runIndexOut(t, fixturePath(t, "petstore.json"), writeYAMLFixture(t, t.TempDir()))

	st := openStoreFromDir(t, dir)
	before, err := st.List()
	if err != nil {
		t.Fatalf("List before: %v", err)
	}

	err = runUnindex([]string{"pets"}, io.Discard)
	if err == nil {
		t.Fatal("unindex of an unknown name succeeded")
	}
	for _, want := range []string{"pets", "not indexed", "petstore", "tiny"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err.Error(), want)
		}
	}

	after, err := st.List()
	if err != nil {
		t.Fatalf("List after: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("an unknown name changed the index: %+v then %+v", before, after)
	}
	for i := range before {
		if after[i].Name != before[i].Name || !after[i].FetchedAt.Equal(before[i].FetchedAt) {
			t.Errorf("an unknown name rewrote %q: %+v then %+v", before[i].Name, before[i], after[i])
		}
	}
}

// TestRunUnindexLastSpecLeavesAnEmptyStore covers removing the last spec: the
// store stays on disk, so the tools report an empty store rather than a missing
// one.
func TestRunUnindexLastSpecLeavesAnEmptyStore(t *testing.T) {
	dir := workdir(t)
	runIndexOut(t, fixturePath(t, "petstore.json"))

	runUnindexOut(t, "petstore")

	st := openStoreFromDir(t, dir)
	if _, err := os.Stat(st.Path()); err != nil {
		t.Fatalf("unindexing the last spec removed the store file: %v", err)
	}
	metas, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 0 {
		t.Fatalf("index after unindexing the last spec = %+v, want none", metas)
	}
}

// TestRunUnindexDiscoversStoreFromNestedDirectory covers S2 discovery on the
// unindex path: the nearest store above the working directory is the one edited,
// exactly as `index` and the server resolve it.
func TestRunUnindexDiscoversStoreFromNestedDirectory(t *testing.T) {
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
	runUnindexOut(t, "petstore")

	metas, err := openStoreFromDir(t, root).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 0 {
		t.Fatalf("the project-root store still holds %+v", metas)
	}
	if _, err := os.Stat(filepath.Join(nested, store.DirName)); err == nil {
		t.Error("unindex created a second store in the nested directory")
	}
}

// TestRunUnindexRequiresExactlyOneName covers the usage error and the no-store
// case: neither may create a store as a side effect.
func TestRunUnindexRequiresExactlyOneName(t *testing.T) {
	dir := workdir(t)

	for _, args := range [][]string{nil, {"one", "two"}} {
		err := runUnindex(args, io.Discard)
		if err == nil {
			t.Fatalf("unindex%v was accepted", args)
		}
		if !strings.Contains(err.Error(), "usage: swagger-mcp unindex <name>") {
			t.Errorf("unindex%v error %q does not print the usage", args, err.Error())
		}
	}

	// No store in this project: report it instead of creating an empty one.
	err := runUnindex([]string{"petstore"}, io.Discard)
	if err == nil {
		t.Fatal("unindex without a store succeeded")
	}
	if !strings.Contains(err.Error(), "swagger-mcp index") {
		t.Errorf("error %q does not name the index command", err.Error())
	}
	if _, statErr := os.Stat(filepath.Join(dir, store.DirName)); statErr == nil {
		t.Error("unindex created a store directory as a side effect")
	}
}
