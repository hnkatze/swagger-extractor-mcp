package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/hnkatze/swagger-mcp-go/internal/formatter"
	"github.com/hnkatze/swagger-mcp-go/internal/store"
	"github.com/hnkatze/swagger-mcp-go/internal/types"
)

// staleAfter is how old a stored copy may be before spec_status calls it stale
// (S6). A week is a product choice, not a technical one: refreshing stays an
// explicit act, so the only job of this threshold is to make silence visible.
const staleAfter = 7 * 24 * time.Hour

// --- catalog rows to the shapes the formatters render (S9) ---

// endpointSummary projects one catalog row onto the summary a listing prints.
func endpointSummary(ep store.Endpoint) types.EndpointSummary {
	return types.EndpointSummary{
		Method:      ep.Method,
		Path:        ep.Path,
		Summary:     ep.Summary,
		Description: ep.Description,
		Tags:        ep.Tags,
		Deprecated:  ep.Deprecated,
	}
}

// endpointSummaries projects a catalog page. No rows means no matches, which the
// listings report as a nil slice (never an empty one).
func endpointSummaries(rows []store.Endpoint) []types.EndpointSummary {
	if len(rows) == 0 {
		return nil
	}
	out := make([]types.EndpointSummary, 0, len(rows))
	for _, ep := range rows {
		out = append(out, endpointSummary(ep))
	}
	return out
}

// schemaInfos projects a catalog page. A nil page means the spec has no component
// schemas at all; an empty page means the filter excluded them.
func schemaInfos(rows []store.Schema) []types.SchemaInfo {
	if rows == nil {
		return nil
	}
	out := make([]types.SchemaInfo, 0, len(rows))
	for _, schema := range rows {
		out = append(out, types.SchemaInfo{
			Name:        schema.Name,
			FieldCount:  schema.FieldCount,
			UsedBy:      schema.UsedBy,
			Description: schema.Description,
		})
	}
	return out
}

// tagSummaries projects the catalog's tag analysis.
func tagSummaries(tags []store.Tag) []types.TagSummary {
	out := make([]types.TagSummary, 0, len(tags))
	for _, tag := range tags {
		breakdown := make(map[string]int, len(tag.MethodBreakdown))
		for method, count := range tag.MethodBreakdown {
			breakdown[method] = count
		}
		out = append(out, types.TagSummary{
			Name:            tag.Name,
			Description:     tag.Description,
			EndpointCount:   tag.EndpointCount,
			MethodBreakdown: breakdown,
		})
	}
	return out
}

// --- list_specs (S5) ---

// specListEntry is one row of list_specs: the store metadata that identifies an
// indexed spec, without its document.
type specListEntry struct {
	Name          string `json:"name"`
	Title         string `json:"title"`
	Version       string `json:"version"`
	Source        string `json:"source"`
	FetchedAt     string `json:"fetched_at"`
	EndpointCount int    `json:"endpoint_count"`
	SchemaCount   int    `json:"schema_count"`
}

// specListReport is the whole list_specs answer in JSON form.
type specListReport struct {
	Total int             `json:"total"`
	Specs []specListEntry `json:"specs"`
}

// handleListSpecs answers from the `specs` table alone: no document is loaded and
// there is no spec argument, because listing is how a caller learns the names
// every other tool takes (S5).
func (r *Registry) handleListSpecs(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	metas, err := r.specs.metas()
	if err != nil {
		return specToolError(err), nil
	}
	if len(metas) == 0 {
		return specToolError(noSpecsIndexed(r.specs.Path())), nil
	}

	if r.getFormat(req) == types.FormatJSON {
		report := &specListReport{Total: len(metas), Specs: make([]specListEntry, 0, len(metas))}
		for _, meta := range metas {
			report.Specs = append(report.Specs, specListEntry{
				Name:          meta.Name,
				Title:         meta.Title,
				Version:       meta.Version,
				Source:        meta.Source,
				FetchedAt:     meta.FetchedAt.UTC().Format(time.RFC3339),
				EndpointCount: meta.EndpointCount,
				SchemaCount:   meta.SchemaCount,
			})
		}
		output, fmtErr := formatter.FormatJSON(report)
		if fmtErr != nil {
			return toolError(types.ErrInternalError, fmtErr.Error()), nil
		}
		return mcp.NewToolResultText(output), nil
	}

	return mcp.NewToolResultText(formatSpecListTOON(metas)), nil
}

// formatSpecListTOON renders one line per spec: name, title/version, counts,
// source and fetch time. Compact by design: a store with a dozen specs must stay
// readable without a document ever being parsed.
func formatSpecListTOON(metas []store.SpecMeta) string {
	var b strings.Builder
	if len(metas) == 1 {
		b.WriteString("# 1 spec indexed — omit spec= to use it")
	} else {
		b.WriteString(fmt.Sprintf("# %d specs — pass spec=<name> to query one, or all_specs=true to sweep them", len(metas)))
	}
	for _, meta := range metas {
		b.WriteString("\n")
		b.WriteString(meta.Name)
		if label := specLabel(meta.Title, meta.Version); label != "" {
			b.WriteString(" — ")
			b.WriteString(label)
		}
		b.WriteString(fmt.Sprintf(" · endpoints %d, schemas %d", meta.EndpointCount, meta.SchemaCount))
		if meta.Source != "" {
			b.WriteString(" · ")
			b.WriteString(meta.Source)
		}
		b.WriteString(" · fetched ")
		b.WriteString(meta.FetchedAt.UTC().Format(time.RFC3339))
	}
	return b.String()
}

// specLabel renders "Title v1.0.0", tolerating a missing title or a version that
// already carries the prefix.
func specLabel(title, version string) string {
	label := title
	if version != "" {
		if !strings.HasPrefix(version, "v") {
			version = "v" + version
		}
		if label != "" {
			label += " "
		}
		label += version
	}
	return label
}

// --- all_specs sweep (S5) ---

// sweepTargets returns the specs a discovery tool must read: every indexed spec
// when all_specs is set. Each result line is then prefixed with its spec name, so
// a sweep stays readable. Callers that did not opt in never get here: they use
// the single-spec S4a resolution unchanged.
func (r *Registry) sweepTargets() ([]store.SpecMeta, *mcp.CallToolResult) {
	metas, err := r.specs.metas()
	if err != nil {
		return nil, specToolError(err)
	}
	if len(metas) == 0 {
		return nil, specToolError(noSpecsIndexed(r.specs.Path()))
	}
	return metas, nil
}

// endpointFilter reads the endpoint filters of a request once, so list_endpoints
// and its sweep cannot drift apart.
func endpointFilter(req mcp.CallToolRequest) store.EndpointFilter {
	return store.EndpointFilter{
		Tag:         getStringArg(req, "tag"),
		Method:      getStringArg(req, "method"),
		PathPattern: getStringArg(req, "path_pattern"),
		Deprecated:  getOptionalBoolArg(req, "deprecated"),
	}
}

// sweptEndpoint is one endpoint line of a cross-spec result: the summary fields
// the single-spec answer reports, plus the spec it came from.
type sweptEndpoint struct {
	Spec string `json:"spec"`
	types.EndpointSummary
}

type sweptEndpointsReport struct {
	Total     int             `json:"total"`
	Showing   int             `json:"showing"`
	Offset    int             `json:"offset,omitempty"`
	Truncated bool            `json:"truncated"`
	Specs     []string        `json:"specs"`
	Endpoints []sweptEndpoint `json:"endpoints"`
}

type sweptSchema struct {
	Spec string `json:"spec"`
	types.SchemaInfo
}

type sweptSchemasReport struct {
	Total     int           `json:"total"`
	Showing   int           `json:"showing"`
	Offset    int           `json:"offset,omitempty"`
	Truncated bool          `json:"truncated"`
	Specs     []string      `json:"specs"`
	Schemas   []sweptSchema `json:"schemas"`
}

// sweepHeaderTOON is the one-line header every sweep shares: how many results
// there are, how many are shown, and how to narrow the sweep. what is the
// singular noun of the count ("endpoint", "match", "schema").
func sweepHeaderTOON(what string, total, showing, offset int, truncated bool, specs int) string {
	results := plural(total, what)
	scope := fmt.Sprintf("across %s", plural(specs, "spec"))
	switch {
	case offset == 0 && !truncated:
		return fmt.Sprintf("# %s %s", results, scope)
	case showing == 0:
		return fmt.Sprintf("# no results at offset %d (total %s %s)", offset, results, scope)
	case offset == 0:
		return fmt.Sprintf("# showing %d of %s %s — pass spec=<name> to narrow, or page with limit/offset",
			showing, results, scope)
	default:
		header := fmt.Sprintf("# showing %d-%d of %s %s", offset+1, offset+showing, results, scope)
		if truncated {
			header += fmt.Sprintf(" — next offset=%d", offset+showing)
		}
		return header
	}
}

func (r *Registry) handleListEndpointsAllSpecs(req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	metas, errResult := r.sweepTargets()
	if errResult != nil {
		return errResult, nil
	}

	filter := endpointFilter(req)

	var all []sweptEndpoint
	for _, meta := range metas {
		rows, _, err := r.specs.endpoints(meta.Name, filter, 0, 0)
		if err != nil {
			return specToolError(err), nil
		}
		for _, ep := range rows {
			all = append(all, sweptEndpoint{Spec: meta.Name, EndpointSummary: endpointSummary(ep)})
		}
	}

	total := len(all)
	start, end, truncated := r.pageWindow(req, total)
	page := all[start:end]

	if r.getFormat(req) == types.FormatJSON {
		report := &sweptEndpointsReport{
			Total: total, Showing: len(page), Offset: start, Truncated: truncated,
			Specs: metaNames(metas), Endpoints: page,
		}
		output, fmtErr := formatter.FormatJSON(report)
		if fmtErr != nil {
			return toolError(types.ErrInternalError, fmtErr.Error()), nil
		}
		return mcp.NewToolResultText(output), nil
	}

	var b strings.Builder
	b.WriteString(sweepHeaderTOON("endpoint", total, len(page), start, truncated, len(metas)))
	b.WriteString("\n")
	b.WriteString(strings.Join(sweptEndpointLines(page), "\n"))
	return mcp.NewToolResultText(b.String()), nil
}

// sweptEndpointLines renders each endpoint with its spec name in front, reusing
// the single-spec TOON line so the notation does not change (S5).
func sweptEndpointLines(endpoints []sweptEndpoint) []string {
	lines := make([]string, 0, len(endpoints))
	for _, ep := range endpoints {
		summary := formatter.StripDescriptions([]types.EndpointSummary{ep.EndpointSummary})
		line := formatter.FormatEndpointsTOON(summary)
		lines = append(lines, ep.Spec+": "+line)
	}
	return lines
}

func (r *Registry) handleSearchSpecAllSpecs(req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query := getStringArg(req, "query")
	if query == "" {
		return toolError(types.ErrInvalidQuery, "search query is required"), nil
	}

	metas, errResult := r.sweepTargets()
	if errResult != nil {
		return errResult, nil
	}

	var all []sweptEndpoint
	for _, meta := range metas {
		rows, _, err := r.specs.search(meta.Name, query, 0, 0)
		if err != nil {
			return specToolError(err), nil
		}
		for _, ep := range rows {
			all = append(all, sweptEndpoint{Spec: meta.Name, EndpointSummary: endpointSummary(ep)})
		}
	}

	total := len(all)
	start, end, truncated := r.pageWindow(req, total)
	page := all[start:end]

	if r.getFormat(req) == types.FormatJSON {
		report := &sweptEndpointsReport{
			Total: total, Showing: len(page), Offset: start, Truncated: truncated,
			Specs: metaNames(metas), Endpoints: page,
		}
		output, fmtErr := formatter.FormatJSON(report)
		if fmtErr != nil {
			return toolError(types.ErrInternalError, fmtErr.Error()), nil
		}
		return mcp.NewToolResultText(output), nil
	}

	if total == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("No endpoints found matching query: %s (searched %d specs: %s)",
			query, len(metas), strings.Join(metaNames(metas), ", "))), nil
	}

	var b strings.Builder
	b.WriteString(sweepHeaderTOON("match", total, len(page), start, truncated, len(metas)))
	b.WriteString("\n")
	b.WriteString(strings.Join(sweptEndpointLines(page), "\n"))
	return mcp.NewToolResultText(b.String()), nil
}

func (r *Registry) handleListSchemasAllSpecs(req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	metas, errResult := r.sweepTargets()
	if errResult != nil {
		return errResult, nil
	}

	filter := getStringArg(req, "filter")
	var all []sweptSchema
	for _, meta := range metas {
		rows, _, err := r.specs.schemas(meta.Name, filter, 0, 0)
		if err != nil {
			return specToolError(err), nil
		}
		for _, schema := range schemaInfos(rows) {
			all = append(all, sweptSchema{Spec: meta.Name, SchemaInfo: schema})
		}
	}

	total := len(all)
	start, end, truncated := r.pageWindow(req, total)
	page := all[start:end]

	if r.getFormat(req) == types.FormatJSON {
		report := &sweptSchemasReport{
			Total: total, Showing: len(page), Offset: start, Truncated: truncated,
			Specs: metaNames(metas), Schemas: page,
		}
		output, fmtErr := formatter.FormatJSON(report)
		if fmtErr != nil {
			return toolError(types.ErrInternalError, fmtErr.Error()), nil
		}
		return mcp.NewToolResultText(output), nil
	}

	var b strings.Builder
	b.WriteString(sweepHeaderTOON("schema", total, len(page), start, truncated, len(metas)))
	b.WriteString("\n")
	for i, schema := range page {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(schema.Spec)
		b.WriteString(": ")
		b.WriteString(schema.Name)
		b.WriteString(fmt.Sprintf(" (%d fields, used by %d)", schema.FieldCount, schema.UsedBy))
		if schema.Description != "" {
			b.WriteString(" — ")
			b.WriteString(schema.Description)
		}
	}
	return mcp.NewToolResultText(b.String()), nil
}

// sweptTags is one spec's tag analysis in a cross-spec answer.
type sweptTags struct {
	Spec string             `json:"spec"`
	Tags []types.TagSummary `json:"tags"`
}

type sweptTagsReport struct {
	Specs []sweptTags `json:"specs"`
}

func (r *Registry) handleAnalyzeTagsAllSpecs(req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	metas, errResult := r.sweepTargets()
	if errResult != nil {
		return errResult, nil
	}

	type specTags struct {
		name string
		tags []types.TagSummary
	}
	perSpec := make([]specTags, 0, len(metas))
	for _, meta := range metas {
		tags, err := r.specs.tags(meta.Name)
		if err != nil {
			return specToolError(err), nil
		}
		perSpec = append(perSpec, specTags{name: meta.Name, tags: tagSummaries(tags)})
	}

	if r.getFormat(req) == types.FormatJSON {
		report := &sweptTagsReport{Specs: make([]sweptTags, 0, len(perSpec))}
		for _, entry := range perSpec {
			report.Specs = append(report.Specs, sweptTags{Spec: entry.name, Tags: entry.tags})
		}
		output, fmtErr := formatter.FormatJSON(report)
		if fmtErr != nil {
			return toolError(types.ErrInternalError, fmtErr.Error()), nil
		}
		return mcp.NewToolResultText(output), nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("# tags across %s", plural(len(perSpec), "spec")))
	for _, entry := range perSpec {
		block := formatter.FormatTagsTOON(entry.tags)
		if block == "" {
			continue
		}
		b.WriteString("\n")
		b.WriteString(entry.name)
		b.WriteString(":")
		for _, line := range strings.Split(block, "\n") {
			b.WriteString("\n  ")
			b.WriteString(line)
		}
	}
	return mcp.NewToolResultText(b.String()), nil
}

// --- staleness helpers (S6) ---

// humanAge renders an age compactly ("3d", "3d4h", "5h", "12m"), because the
// verdict in spec_status is only useful if the magnitude is readable.
func humanAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case days > 0:
		return fmt.Sprintf("%dd", days)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	case int(d.Minutes()) > 0:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}

// refreshCommand is the exact command that rewrites this spec's stored copy. It
// is printed, never run: refreshing is explicit (S6).
//
// It must be copy-pasteable: the indexer derives a spec's name from its source,
// so a spec stored under a different name would be renamed by the printed
// command (and could collide with another source's row). When the stored name is
// not the derived one, the command carries it explicitly.
func refreshCommand(meta store.SpecMeta) string {
	command := "swagger-mcp index --refresh"
	if store.DeriveName(meta.Source) != meta.Name {
		command += " --name " + shellQuote(meta.Name)
	}
	return command + " " + shellQuote(meta.Source)
}

// shellQuote wraps an argument that a shell would otherwise split.
func shellQuote(value string) string {
	if strings.ContainsAny(value, " \t") {
		return `"` + value + `"`
	}
	return value
}

// indexedCount describes what the store holds, for errors that have to say what
// is indexed rather than just that something is missing.
func indexedCount(n int, names []string) string {
	switch n {
	case 0:
		return "this project indexes no specs"
	case 1:
		return fmt.Sprintf("this project indexes one spec (%s)", names[0])
	default:
		return fmt.Sprintf("this project indexes %d specs (%s)", n, strings.Join(names, ", "))
	}
}

// containsName reports whether name is one of the indexed names.
func containsName(names []string, name string) bool {
	for _, candidate := range names {
		if candidate == name {
			return true
		}
	}
	return false
}

// plural renders "1 spec" / "2 specs" so sweep headers do not read like a bug.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
