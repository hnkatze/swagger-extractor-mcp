// Package specsource is the indexer's read path: it turns a source (a local
// file or a URL) into the bytes the store will hold, and into the validated
// document those bytes must describe.
//
// It exists because indexing has two entry points — the `swagger-mcp index`
// command and the `index_spec` tool — and a second copy of the read path is how
// the two would disagree about the size cap, the resolved source, the parse
// rules or the validation messages. It reads no store and writes nothing: the
// caller owns the write.
package specsource

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/hnkatze/swagger-extractor-mcp/internal/loader"
	"github.com/hnkatze/swagger-extractor-mcp/internal/store"
)

// maxReportedViolations bounds how much of a failed validation ends up in one
// message.
const maxReportedViolations = 3

// Source is what one read produced: the raw bytes, the canonical key the store
// records for the source, and the ETag when the source sent one (URLs only).
type Source struct {
	Raw       []byte
	ETag      string
	Canonical string
}

// Reader reads a spec source with a bounded size and a bounded wait. It is the
// only thing in the codebase that opens a spec over the network (S13).
type Reader struct {
	client  *http.Client
	maxSize int64
}

// NewReader builds a Reader that refuses a source larger than maxSize bytes
// (maxSize <= 0 disables the cap) and gives a URL read at most timeout.
func NewReader(maxSize int64, timeout time.Duration) *Reader {
	return &Reader{
		client:  &http.Client{Timeout: timeout},
		maxSize: maxSize,
	}
}

// Read returns the source bytes, its ETag (URLs only) and the canonical source
// key stored with the spec. Files use their absolute path so the same file
// reached by a different relative path is still the same source.
func (r *Reader) Read(ctx context.Context, source string) (Source, error) {
	if store.IsURL(source) {
		return r.readURL(ctx, source)
	}
	abs, err := filepath.Abs(source)
	if err != nil {
		return Source{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Source{}, err
	}
	if err := r.checkSize(info.Size()); err != nil {
		return Source{}, err
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return Source{}, err
	}
	if err := r.checkSize(int64(len(raw))); err != nil {
		return Source{}, err
	}
	return Source{Raw: raw, Canonical: abs}, nil
}

func (r *Reader) readURL(ctx context.Context, source string) (Source, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return Source{}, err
	}
	req.Header.Set("Accept", "application/json, application/yaml, application/x-yaml, text/yaml")

	resp, err := r.client.Do(req)
	if err != nil {
		return Source{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Source{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	reader := io.Reader(resp.Body)
	if r.maxSize > 0 {
		// One byte past the cap: a body that fills the extra byte is over the
		// limit, which the caller reports instead of indexing a cut-off document.
		reader = io.LimitReader(resp.Body, r.maxSize+1)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		return Source{}, err
	}
	if err := r.checkSize(int64(len(raw))); err != nil {
		return Source{}, err
	}
	return Source{Raw: raw, ETag: resp.Header.Get("ETag"), Canonical: source}, nil
}

// checkSize refuses a source larger than the read cap. The cap used to truncate
// in silence, so a cut-off YAML could validate into a wrong document; failing is
// the only safe answer.
func (r *Reader) checkSize(size int64) error {
	if r.maxSize > 0 && size > r.maxSize {
		return fmt.Errorf("source is %d bytes, over the %d-byte spec size limit: it was not indexed (index a smaller document)", size, r.maxSize)
	}
	return nil
}

// Parse parses with kin-openapi and applies the same relaxed validation a query
// applies to a stored document, so the index cannot accept something the server
// would reject or reject something it would serve. Both write paths call it, so
// the parse and validation messages are identical.
func Parse(raw []byte) (*openapi3.T, error) {
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
