package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/hnkatze/swagger-extractor-mcp/internal/config"
	"github.com/hnkatze/swagger-extractor-mcp/internal/specsource"
	"github.com/hnkatze/swagger-extractor-mcp/internal/store"
)

const indexUsage = "usage: swagger-mcp index [--refresh] [--name NAME] <url|file>..."

// indexOptions is the parsed command line of the index subcommand.
type indexOptions struct {
	sources []string
	name    string
	refresh bool
}

// runIndex implements `swagger-mcp index`, the CLI entry point of the write
// path (S1, S3). The byte-level read and the parse rules live in
// internal/specsource, which the index_spec tool uses too (S13), so the two
// entry points cannot disagree.
func runIndex(args []string, out io.Writer) error {
	opts, err := parseIndexArgs(args)
	if err != nil {
		return err
	}
	return indexSources(context.Background(), opts, out)
}

func parseIndexArgs(args []string) (indexOptions, error) {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var opts indexOptions
	fs.BoolVar(&opts.refresh, "refresh", false, "re-read every source and rewrite the stored copy")
	fs.StringVar(&opts.name, "name", "", "store name to use instead of the name derived from the source (single source only)")
	if err := fs.Parse(args); err != nil {
		return indexOptions{}, fmt.Errorf("%v\n%s", err, indexUsage)
	}
	opts.sources = fs.Args()
	if len(opts.sources) == 0 {
		return indexOptions{}, errors.New(indexUsage)
	}
	if opts.name != "" && len(opts.sources) > 1 {
		return indexOptions{}, errors.New("--name names one spec, so it accepts a single source")
	}
	return opts, nil
}

// indexSources indexes every requested source into the store for the current
// project. A failing source does not stop the others; the failures are reported
// together.
func indexSources(ctx context.Context, opts indexOptions, out io.Writer) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("determine working directory: %w", err)
	}
	dir, err := store.DirFor(cwd)
	if err != nil {
		return fmt.Errorf("locate store: %w", err)
	}
	st, err := store.Open(dir)
	if err != nil {
		return err
	}
	defer st.Close()

	cfg := config.Load()
	ix := &specIndexer{
		store:  st,
		reader: specsource.NewReader(cfg.MaxSpecSize, cfg.FetchTimeout),
		out:    out,
	}

	var failures []error
	for _, source := range opts.sources {
		if err := ix.index(ctx, source, opts.name, opts.refresh); err != nil {
			failures = append(failures, err)
		}
	}
	fmt.Fprintf(out, "store: %s\n", st.Path())
	return errors.Join(failures...)
}

// specIndexer reads, parses and stores one spec at a time.
type specIndexer struct {
	store  *store.Store
	reader *specsource.Reader
	out    io.Writer
}

func (ix *specIndexer) index(ctx context.Context, source, nameOverride string, refresh bool) error {
	src, err := ix.reader.Read(ctx, source)
	if err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}
	doc, err := specsource.Parse(src.Raw)
	if err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}

	name := nameOverride
	if name == "" {
		name = store.DeriveName(source)
	}

	rec := store.NewRecord(doc, src.Raw)
	rec.Name = name
	rec.Source = src.Canonical
	rec.ETag = src.ETag
	rec.FetchedAt = time.Now().UTC()

	if !refresh {
		existing, metaErr := ix.store.Meta(name)
		switch {
		case metaErr == nil && existing.Source == src.Canonical && existing.SHA256 == rec.SHA256:
			fmt.Fprintf(ix.out, "unchanged %s: %s %s (content identical, use --refresh to rewrite)\n",
				name, existing.Title, existing.Version)
			return nil
		case metaErr != nil && !errors.Is(metaErr, store.ErrNotFound):
			return fmt.Errorf("read stored spec %q: %w", name, metaErr)
		}
	}

	if err := ix.store.Put(rec); err != nil {
		return err
	}
	fmt.Fprintf(ix.out, "indexed %s: %s %s (%d endpoints, %d schemas)\n",
		name, rec.Title, rec.Version, len(rec.Endpoints), len(rec.Schemas))
	return nil
}
