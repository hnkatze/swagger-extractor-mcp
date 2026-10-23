package tools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/hnkatze/swagger-mcp-go/internal/loader"
	"github.com/hnkatze/swagger-mcp-go/internal/store"
	"github.com/hnkatze/swagger-mcp-go/internal/types"
)

// specCacheSize bounds how many stored documents one session keeps parsed in
// memory. The cache exists so a conversation does not decompress and re-parse
// the same spec on every call, not to hold the whole index (S4).
const specCacheSize = 4

// Error codes for the store-backed addressing path. They are local to the query
// layer because the failure is about the local index, not about a request.
const (
	codeNoStore       = "NO_STORE"
	codeSpecNotFound  = "SPEC_NOT_FOUND"
	codeSpecAmbiguous = "SPEC_AMBIGUOUS"
	codeStoreError    = "STORE_ERROR"
	// codeDiffNeedsSpecs reports a diff request the store cannot answer: fewer
	// than two specs are indexed, so there is nothing to compare.
	codeDiffNeedsSpecs = "DIFF_NEEDS_SPECS"
)

// indexCommand is the command a user must run to create or extend the index.
const indexCommand = "`swagger-mcp index <url|file>`"

// specError is a query failure with a stable code and a message written for the
// caller (a human or an LLM), never for a log file.
type specError struct {
	code    string
	message string
}

func (e *specError) Error() string { return e.message }

func specErr(code, format string, args ...any) *specError {
	return &specError{code: code, message: fmt.Sprintf(format, args...)}
}

// specSource is the query layer in front of the local index: it owns store
// discovery, the parsed-document cache and every error a tool reports when it
// cannot address a spec. It never fetches anything (S1, S4).
type specSource struct {
	// path is the database file the store discovery resolved, kept even when the
	// file does not exist yet so an error can name the store it looked for.
	path string

	mu sync.Mutex
	st *store.Store
	// cache holds the parsed documents of recently queried specs, and order is
	// their insertion order so eviction is a FIFO of that same bound.
	cache map[string]*cachedSpec
	order []string
}

// newSpecSource resolves the store for workDir. Discovery is the store's own
// walk-up, so the server reads the same index the CLI wrote (S2).
func newSpecSource(workDir string) *specSource {
	dir, err := store.DirFor(workDir)
	if err != nil || dir == "" {
		// Keep the failure for the query that needs a store: the server must
		// still start and report the problem per tool call.
		return &specSource{path: filepath.Join(workDir, store.DirName, store.DBFileName), cache: map[string]*cachedSpec{}}
	}
	return &specSource{path: filepath.Join(dir, store.DBFileName), cache: map[string]*cachedSpec{}}
}

// Path is the database file this session reads.
func (s *specSource) Path() string { return s.path }

// open returns the store, opening it on first use. A missing database file is
// reported as a missing store instead of being created by a read: only the
// indexer creates the store (S2, S3).
func (s *specSource) open() (*store.Store, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.openLocked()
}

func (s *specSource) openLocked() (*store.Store, error) {
	if s.st != nil {
		return s.st, nil
	}
	if _, err := os.Stat(s.path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, specErr(codeNoStore,
				"no specs are indexed in this project: run %s to add one (store: %s)", indexCommand, s.path)
		}
		return nil, specErr(codeStoreError, "cannot read the spec store at %s: %v", s.path, err)
	}
	st, err := store.Open(filepath.Dir(s.path))
	if err != nil {
		return nil, specErr(codeStoreError, "cannot open the spec store at %s: %v", s.path, err)
	}
	s.st = st
	return st, nil
}

// Close releases the store if it was opened.
func (s *specSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st == nil {
		return nil
	}
	st := s.st
	s.st = nil
	return st.Close()
}

// names returns the indexed spec names, ordered, so every error can list them
// in the order a caller will see in `spec_status`.
func (s *specSource) names() ([]string, error) {
	metas, err := s.metas()
	if err != nil {
		return nil, err
	}
	return metaNames(metas), nil
}

// metas returns the store metadata of every indexed spec, ordered by name. It
// reads the `specs` table only: discovery never loads a document (S5).
func (s *specSource) metas() ([]store.SpecMeta, error) {
	st, err := s.open()
	if err != nil {
		return nil, err
	}
	metas, err := st.List()
	if err != nil {
		return nil, specErr(codeStoreError, "cannot list the indexed specs: %v", err)
	}
	return metas, nil
}

// metaNames projects metadata to the names, in store order.
func metaNames(metas []store.SpecMeta) []string {
	out := make([]string, 0, len(metas))
	for _, meta := range metas {
		out = append(out, meta.Name)
	}
	return out
}

// noSpecsIndexed is the error every tool reports when the store exists but holds
// nothing: the fix is always the index command, never a query.
func noSpecsIndexed(path string) error {
	return specErr(codeNoStore,
		"no specs are indexed in this project: run %s to add one (store: %s)", indexCommand, path)
}

// resolve turns the request's spec argument into exactly one indexed name:
// explicit name wins, otherwise the only indexed spec is used. Guessing among
// several is never allowed (S4a).
func (s *specSource) resolve(req mcp.CallToolRequest, arg string) (string, error) {
	want := strings.TrimSpace(getStringArg(req, arg))
	names, err := s.names()
	if err != nil {
		return "", err
	}
	if want != "" {
		for _, name := range names {
			if name == want {
				return name, nil
			}
		}
		return "", specErr(codeSpecNotFound, "spec %q is not indexed; indexed specs: %s",
			want, strings.Join(names, ", "))
	}
	switch len(names) {
	case 0:
		return "", noSpecsIndexed(s.path)
	case 1:
		return names[0], nil
	default:
		return "", specErr(codeSpecAmbiguous,
			"several specs are indexed (%s): pass spec=<name> to choose one", strings.Join(names, ", "))
	}
}

// meta returns one spec's stored metadata without loading its document.
func (s *specSource) meta(name string) (store.SpecMeta, error) {
	st, err := s.open()
	if err != nil {
		return store.SpecMeta{}, err
	}
	meta, err := st.Meta(name)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.SpecMeta{}, specErr(codeSpecNotFound, "%v", err)
		}
		return store.SpecMeta{}, specErr(codeStoreError, "cannot read spec %q: %v", name, err)
	}
	return meta, nil
}

// document returns the stored document parsed and normalized exactly as the
// loader would have served it, so every extractor/analyzer/generator call sees
// the shape it always did (S8).
func (s *specSource) document(name string) (*openapi3.T, error) {
	entry, err := s.entry(name)
	if err != nil {
		return nil, err
	}
	return entry.document()
}

// storedDocument returns the stored document parsed but NOT normalized, which is
// what `validate_spec` must report: values the loader silently fixes have to
// show up as the spec actually wrote them.
func (s *specSource) storedDocument(name string) (*openapi3.T, error) {
	entry, err := s.entry(name)
	if err != nil {
		return nil, err
	}
	return entry.storedDocument()
}

// entry returns the cached entry for name, loading the stored bytes when the
// store holds a different document than the one cached (a reindex in the same
// session must be visible to the next query).
func (s *specSource) entry(name string) (*cachedSpec, error) {
	st, err := s.open()
	if err != nil {
		return nil, err
	}
	// Metadata is one indexed row: reading it per query keeps the cache honest
	// while still avoiding a decompress and a parse.
	meta, metaErr := st.Meta(name)
	if metaErr != nil {
		if errors.Is(metaErr, store.ErrNotFound) {
			return nil, specErr(codeSpecNotFound, "%v", metaErr)
		}
		return nil, specErr(codeStoreError, "cannot read spec %q: %v", name, metaErr)
	}
	if entry := s.cached(name); entry != nil && entry.sha == meta.SHA256 {
		return entry, nil
	}

	rec, err := st.Get(name)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, specErr(codeSpecNotFound, "%v", err)
		}
		return nil, specErr(codeStoreError, "cannot read spec %q: %v", name, err)
	}

	entry := &cachedSpec{name: rec.Name, sha: rec.SHA256, raw: rec.Document}
	return s.storeEntry(entry), nil
}

func (s *specSource) cached(name string) *cachedSpec {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache[name]
}

// storeEntry caches an entry, replacing a stale one and evicting the oldest so
// the cache cannot grow with the index.
func (s *specSource) storeEntry(entry *cachedSpec) *cachedSpec {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.cache[entry.name]; ok {
		if existing.sha == entry.sha {
			return existing
		}
		// Reindexed since the cached copy: drop it and refresh the position.
		s.removeLocked(entry.name)
	}
	s.cache[entry.name] = entry
	s.order = append(s.order, entry.name)
	for len(s.order) > specCacheSize {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.cache, oldest)
	}
	return entry
}

func (s *specSource) removeLocked(name string) {
	delete(s.cache, name)
	for i, n := range s.order {
		if n == name {
			s.order = append(s.order[:i], s.order[i+1:]...)
			return
		}
	}
}

// cachedSpec is one stored document. Both parse forms are produced lazily and at
// most once each, so a session with N calls parses a spec twice in the worst
// case (normalized for queries, verbatim for validate_spec) rather than per call.
type cachedSpec struct {
	name string
	sha  string
	raw  []byte

	mu         sync.Mutex
	normalized *openapi3.T
	verbatim   *openapi3.T
}

func (e *cachedSpec) document() (*openapi3.T, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.normalized != nil {
		return e.normalized, nil
	}
	doc, err := loader.ParseDocument(e.raw)
	if err != nil {
		return nil, specErr(types.ErrParseError, "stored spec %q cannot be parsed: %v", e.name, err)
	}
	loader.PrepareDocument(doc)
	e.normalized = doc
	return doc, nil
}

func (e *cachedSpec) storedDocument() (*openapi3.T, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.verbatim != nil {
		return e.verbatim, nil
	}
	doc, err := loader.ParseDocument(e.raw)
	if err != nil {
		return nil, specErr(types.ErrParseError, "stored spec %q cannot be parsed: %v", e.name, err)
	}
	e.verbatim = doc
	return doc, nil
}

// SpecStatusReport is what spec_status reports for one indexed spec: the store's
// own record of where the spec came from, when it was read, and whether that
// copy is old enough to be worth refreshing (S6).
type SpecStatusReport struct {
	Name           string `json:"name"`
	Source         string `json:"source"`
	Title          string `json:"title"`
	Version        string `json:"version"`
	ETag           string `json:"etag"`
	SHA256         string `json:"sha256"`
	FetchedAt      string `json:"fetched_at"`
	Age            string `json:"age"`
	AgeSeconds     int64  `json:"age_seconds"`
	Stale          bool   `json:"stale"`
	StaleAfterDays int    `json:"stale_after_days"`
	Refresh        string `json:"refresh"`
	EndpointCount  int    `json:"endpoint_count"`
	SchemaCount    int    `json:"schema_count"`
	Store          string `json:"store"`
}
