package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hnkatze/swagger-extractor-mcp/internal/store"
)

const unindexUsage = "usage: swagger-mcp unindex <name>"

// runUnindex implements `swagger-mcp unindex <name>`: the write path that removes
// one indexed spec together with the catalog rows derived from it (S10), and
// reports exactly what it removed.
func runUnindex(args []string, out io.Writer) error {
	name, err := parseUnindexArgs(args)
	if err != nil {
		return err
	}
	st, err := openWritableStore()
	if err != nil {
		return err
	}
	defer st.Close()

	meta, err := st.Delete(name)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return unknownSpecError(st, name)
		}
		return err
	}

	fmt.Fprintf(out, "removed %s: %s %s (%d endpoints, %d schemas)\n",
		meta.Name, meta.Title, meta.Version, meta.EndpointCount, meta.SchemaCount)
	fmt.Fprintf(out, "store: %s\n", st.Path())
	return nil
}

// parseUnindexArgs accepts one positional spec name and nothing else: the name is
// the whole request, so an unknown flag is a usage error rather than a spec.
func parseUnindexArgs(args []string) (string, error) {
	if len(args) != 1 {
		return "", errors.New(unindexUsage)
	}
	name := strings.TrimSpace(args[0])
	if name == "" || strings.HasPrefix(name, "-") {
		return "", fmt.Errorf("%q is not a spec name\n%s", args[0], unindexUsage)
	}
	return name, nil
}

// unknownSpecError lists the names that are indexed, so a typo costs one read
// instead of a guess. Nothing was removed when this is returned.
func unknownSpecError(st *store.Store, name string) error {
	metas, err := st.List()
	if err != nil {
		return err
	}
	if len(metas) == 0 {
		return fmt.Errorf("spec %q is not indexed: no specs are indexed in this store (store: %s)", name, st.Path())
	}
	names := make([]string, 0, len(metas))
	for _, meta := range metas {
		names = append(names, meta.Name)
	}
	return fmt.Errorf("spec %q is not indexed; indexed specs: %s", name, strings.Join(names, ", "))
}

// openWritableStore resolves the store exactly as every other subcommand does,
// but never creates one: unindex on a project with no index is an error, not an
// empty database left behind by a mistyped name.
func openWritableStore() (*store.Store, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("determine working directory: %w", err)
	}
	dir, err := store.DirFor(cwd)
	if err != nil {
		return nil, fmt.Errorf("locate store: %w", err)
	}
	path := filepath.Join(dir, store.DBFileName)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no specs are indexed in this project: run `swagger-mcp index <url|file>` to add one (store: %s)", path)
		}
		return nil, fmt.Errorf("read the spec store at %s: %w", path, err)
	}
	return store.Open(dir)
}
