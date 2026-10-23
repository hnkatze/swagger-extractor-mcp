package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/hnkatze/swagger-extractor-mcp/internal/config"
	"github.com/hnkatze/swagger-extractor-mcp/internal/loader"
	"github.com/hnkatze/swagger-extractor-mcp/internal/store"
)

const indexUsage = "usage: swagger-mcp index [--refresh] [--name NAME] <url|file>..."

// maxReportedViolations bounds how much of a failed validation ends up on one
// summary line.
const maxReportedViolations = 3

// indexOptions is the parsed command line of the index subcommand.
type indexOptions struct {
	sources []string
	name    string
	refresh bool
}

// runIndex implements `swagger-mcp index`: the only path that writes the store
// and the only path that may touch the network (S1, S3).
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
		store:   st,
		client:  &http.Client{Timeout: cfg.FetchTimeout},
		maxSize: cfg.MaxSpecSize,
		out:     out,
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
	store   *store.Store
	client  *http.Client
	maxSize int64
	out     io.Writer
}

func (ix *specIndexer) index(ctx context.Context, source, nameOverride string, refresh bool) error {
	raw, etag, canonical, err := ix.readSource(ctx, source)
	if err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}
	doc, err := parseAndValidate(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}

	name := nameOverride
	if name == "" {
		name = store.DeriveName(source)
	}

	rec := store.NewRecord(doc, raw)
	rec.Name = name
	rec.Source = canonical
	rec.ETag = etag
	rec.FetchedAt = time.Now().UTC()

	if !refresh {
		existing, metaErr := ix.store.Meta(name)
		switch {
		case metaErr == nil && existing.Source == canonical && existing.SHA256 == rec.SHA256:
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

// readSource returns the source bytes, its ETag (URLs only) and the canonical
// source key stored with the spec. Files use their absolute path so the same
// file reached by a different relative path is still the same source.
func (ix *specIndexer) readSource(ctx context.Context, source string) (raw []byte, etag, canonical string, err error) {
	if store.IsURL(source) {
		return ix.readURL(ctx, source)
	}
	abs, err := filepath.Abs(source)
	if err != nil {
		return nil, "", "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, "", "", err
	}
	if err := ix.checkSize(info.Size()); err != nil {
		return nil, "", "", err
	}
	raw, err = os.ReadFile(abs)
	if err != nil {
		return nil, "", "", err
	}
	if err := ix.checkSize(int64(len(raw))); err != nil {
		return nil, "", "", err
	}
	return raw, "", abs, nil
}

func (ix *specIndexer) readURL(ctx context.Context, source string) ([]byte, string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, "", "", err
	}
	req.Header.Set("Accept", "application/json, application/yaml, application/x-yaml, text/yaml")

	resp, err := ix.client.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	reader := io.Reader(resp.Body)
	if ix.maxSize > 0 {
		// One byte past the cap: a body that fills the extra byte is over the
		// limit, which the caller reports instead of indexing a cut-off document.
		reader = io.LimitReader(resp.Body, ix.maxSize+1)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil, "", "", err
	}
	if err := ix.checkSize(int64(len(raw))); err != nil {
		return nil, "", "", err
	}
	return raw, resp.Header.Get("ETag"), source, nil
}

// checkSize refuses a source larger than the read cap. The cap used to truncate
// in silence, so a cut-off YAML could validate into a wrong document; failing is
// the only safe answer.
func (ix *specIndexer) checkSize(size int64) error {
	if ix.maxSize > 0 && size > ix.maxSize {
		return fmt.Errorf("source is %d bytes, over the %d-byte spec size limit: it was not indexed (index a smaller document)", size, ix.maxSize)
	}
	return nil
}

// parseAndValidate parses with kin-openapi and applies the same relaxed
// validation the loader applies when it serves a spec, so the index cannot
// accept something the server would reject or reject something it would serve.
func parseAndValidate(raw []byte) (*openapi3.T, error) {
	doc, err := openapi3.NewLoader().LoadFromData(raw)
	if err != nil {
		return nil, fmt.Errorf("parse OpenAPI spec: %w", err)
	}
	// Normalize first: the loader would accept (and serve) a spec the indexer
	// would otherwise reject for a field the server fixes on read.
	loader.PrepareDocument(doc)
	if violations := loader.ValidateDocument(doc); len(violations) > 0 {
		return nil, fmt.Errorf("spec validation failed: %s", summarizeViolations(violations))
	}
	return doc, nil
}

func summarizeViolations(violations []string) string {
	shown := violations
	if len(shown) > maxReportedViolations {
		shown = shown[:maxReportedViolations]
	}
	msg := strings.Join(shown, "; ")
	if rest := len(violations) - len(shown); rest > 0 {
		msg = fmt.Sprintf("%s (and %d more)", msg, rest)
	}
	return msg
}
