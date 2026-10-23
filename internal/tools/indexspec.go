package tools

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/hnkatze/swagger-extractor-mcp/internal/specsource"
	"github.com/hnkatze/swagger-extractor-mcp/internal/store"
	"github.com/hnkatze/swagger-extractor-mcp/internal/types"
)

// codeIndexFailed reports a write that was refused: an unreadable source, an
// invalid document, a source over the size cap, or a name owned by another
// source. The message always carries the fix.
const codeIndexFailed = "INDEX_FAILED"

// indexSpecTool is the explicit write path for a client without a shell (S13).
// It is the only tool that reads a source or touches the network; every query
// stays a local store read (S1, S4).
func indexSpecTool() mcp.Tool {
	return mcp.NewTool("index_spec",
		mcp.WithDescription("Index a spec into this project's store: the tool equivalent of `swagger-mcp index`, for a client without a shell. Reads a local file or URL once, parses and validates it with the server's own rules, and writes the row the query tools read. The only tool that reads a source or uses the network. Re-indexing the same source updates it in place; a name already owned by a different source is refused."),
		mcp.WithString("source", mcp.Required(), mcp.Description("Spec file path or URL to read, e.g. './openapi.yaml' or 'https://api.example.com/openapi.json'")),
		mcp.WithString("name", mcp.Description("Store name to use instead of the one derived from the source (e.g. 'billing-v2'); only needed when the derived name already belongs to a different source")),
		mcp.WithBoolean("refresh", mcp.Description("true = re-read the source and rewrite the stored copy even when the content is unchanged")),
	)
}

// handleIndexSpec indexes one source and answers with a compact summary: name,
// title, version, endpoint and schema counts, and the store it wrote.
func (r *Registry) handleIndexSpec(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	source := strings.TrimSpace(getStringArg(req, "source"))
	if source == "" {
		return toolError(types.ErrInvalidQuery,
			"index_spec needs a source: pass a spec file path or URL as source=<...> (e.g. source=\"./openapi.yaml\")"), nil
	}
	summary, err := r.indexSource(ctx, source, strings.TrimSpace(getStringArg(req, "name")), getBoolArg(req, "refresh"))
	if err != nil {
		return specToolError(err), nil
	}
	return mcp.NewToolResultText(summary), nil
}

// indexSource performs exactly what `swagger-mcp index` performs for one source:
// read through internal/specsource (same size cap), parse and validate with the
// indexer's rules, derive the name with store.DeriveName, refuse a name owned by
// a different source, and write the store (creating ./swa-mcp/ when writing).
func (r *Registry) indexSource(ctx context.Context, source, nameOverride string, refresh bool) (string, error) {
	reader := specsource.NewReader(r.cfg.MaxSpecSize, r.cfg.FetchTimeout)
	src, err := reader.Read(ctx, source)
	if err != nil {
		return "", specErr(codeIndexFailed, "%s: %v", source, err)
	}
	doc, err := specsource.Parse(src.Raw)
	if err != nil {
		return "", specErr(codeIndexFailed, "%s: %v", source, err)
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

	// The source is read and validated before the store is opened, so a bad
	// source leaves no half-created store behind. The CLI opens the store first
	// only because it indexes several sources in one run.
	storePath := r.specs.Path()
	st, err := store.Open(filepath.Dir(storePath))
	if err != nil {
		if errors.Is(err, store.ErrUnusable) {
			// The message already names the path and the recovery action (S11).
			return "", specErr(codeStoreError, "%v", err)
		}
		return "", specErr(codeStoreError, "cannot open the spec store at %s for writing: %v", storePath, err)
	}
	defer st.Close()

	existing, metaErr := st.Meta(name)
	if metaErr != nil && !errors.Is(metaErr, store.ErrNotFound) {
		return "", specErr(codeStoreError, "cannot read the stored spec %q: %v", name, metaErr)
	}
	if metaErr == nil && existing.Source != src.Canonical {
		// Taking over the name would delete the other spec from the index, so the
		// refusal names the owner and the tool's own parameter to use instead.
		return "", specErr(codeIndexFailed,
			"cannot index %s as %q: that name already belongs to %s; pass name=<another name> to store this spec under a name of its own",
			src.Canonical, name, existing.Source)
	}
	if !refresh && metaErr == nil && existing.SHA256 == rec.SHA256 {
		return fmt.Sprintf("unchanged %s: %s %s (content identical, pass refresh=true to rewrite)\nstore: %s\n",
			name, existing.Title, existing.Version, st.Path()), nil
	}

	if err := st.Put(rec); err != nil {
		return "", specErr(codeIndexFailed, "%v", err)
	}
	return fmt.Sprintf("indexed %s: %s %s (%d endpoints, %d schemas)\nstore: %s\n",
		name, rec.Title, rec.Version, len(rec.Endpoints), len(rec.Schemas), st.Path()), nil
}
