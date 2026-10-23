package tools

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
)

// S1: a query must never touch the network. This package is the whole query
// surface, so it holds no HTTP client and no entry point that builds one. The
// checks read the package source instead of its behavior, because a reintroduced
// fetch would otherwise only show up as a tool call that quietly reached out.

// forbiddenImports are the packages that would let a tool handler fetch.
var forbiddenImports = []string{"net/http", "net/http/httptest", "net"}

// forbiddenCalls are the client constructors and request helpers a fetch needs.
// A handler that only formats output never names them.
var forbiddenCalls = []string{
	"http.Client", "http.DefaultClient", "http.Get", "http.Head", "http.Post",
	"http.NewRequest", "http.NewRequestWithContext", "http.DefaultTransport",
	"net.Dial", "net.DialTimeout", "net.Dialer", "net.ResolveTCPAddr",
}

// parsePackageSources parses every non-test .go file of this package.
func parsePackageSources(t *testing.T) ([]*ast.File, *token.FileSet) {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		t.Fatal("no package sources found")
	}
	return files, fset
}

// networkViolations reports every import and call in files that would let the
// query path fetch. It is a plain function so the guard can be tested against a
// synthetic source too.
func networkViolations(files []*ast.File, fset *token.FileSet) []string {
	var out []string
	for _, file := range files {
		for _, spec := range file.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			for _, forbidden := range forbiddenImports {
				if path == forbidden {
					out = append(out, fset.Position(spec.Pos()).Filename+" imports "+path)
				}
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			var name string
			pos := token.NoPos
			switch e := node.(type) {
			case *ast.CallExpr:
				name, pos = exprString(e.Fun), e.Pos()
			case *ast.CompositeLit:
				// &http.Client{} is a literal, not a call.
				name, pos = exprString(e.Type), e.Pos()
			default:
				return true
			}
			for _, forbidden := range forbiddenCalls {
				if name == forbidden {
					out = append(out, fset.Position(pos).Filename+" uses "+name)
				}
			}
			return true
		})
	}
	return out
}

// exprString renders a call target as "pkg.Func" or "Func".
func exprString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprString(e.X) + "." + e.Sel.Name
	case *ast.StarExpr:
		return exprString(e.X)
	case *ast.ParenExpr:
		return exprString(e.X)
	}
	return ""
}

// specSourceImport is the package that reads a spec from its source: a local file
// or a URL. It is the only way a file read or a network read can enter this
// package.
const specSourceImport = "github.com/hnkatze/swagger-extractor-mcp/internal/specsource"

// indexSpecHandlerFile is the one file in this package allowed to import it.
//
// S13, stated exactly: in package tools, specsource is imported by exactly one
// non-test file, and that file is indexspec.go. No query handler file imports it,
// because every query handler — list_specs, list_endpoints, get_endpoint,
// get_schema, search_spec, list_schemas, analyze_tags, diff_endpoints,
// spec_status, generate_types, security_schemes, validate_spec and usage_guide —
// lives in tools.go, discovery.go or specs.go, none of which may import it. The
// guard checks the import path, not an identifier: a file cannot reference the
// package without importing it, so an import is the whole surface.
const indexSpecHandlerFile = "indexspec.go"

// specSourceViolations reports every non-test file that breaks that rule. It is a
// plain function so the guard can be tested against a synthetic source too.
func specSourceViolations(files []*ast.File, fset *token.FileSet) []string {
	importers := map[string]bool{}
	for _, file := range files {
		for _, spec := range file.Imports {
			if strings.Trim(spec.Path.Value, `"`) == specSourceImport {
				importers[fset.Position(spec.Pos()).Filename] = true
				break
			}
		}
	}
	if len(importers) == 0 {
		return []string{"no file imports " + specSourceImport + ": index_spec must read sources through the shared read path"}
	}
	var out []string
	for name := range importers {
		if name != indexSpecHandlerFile {
			out = append(out, name+" imports "+specSourceImport+" (only "+indexSpecHandlerFile+" may read a source)")
		}
	}
	sort.Strings(out)
	return out
}

// TestToolsImportNoHTTPClient fails if any tools source imports a package that
// can perform an HTTP request (S1: only the indexer fetches).
func TestToolsImportNoHTTPClient(t *testing.T) {
	files, fset := parsePackageSources(t)
	for _, violation := range networkViolations(files, fset) {
		t.Errorf("%s: the query path must not be able to fetch (S1)", violation)
	}
}

// TestOnlyIndexSpecHandlerReadsSources is the refined S13 guard on the real tree.
func TestOnlyIndexSpecHandlerReadsSources(t *testing.T) {
	files, fset := parsePackageSources(t)
	for _, violation := range specSourceViolations(files, fset) {
		t.Errorf("%s: queries stay network-free and read-only (S13)", violation)
	}
}

// TestSourceReadingGuardDetectsAQueryImport proves the guard fires in both
// directions: a query file importing the source reader is a violation, and the
// index_spec handler file importing it is not. Without this, a passing guard
// could just mean the detector looks for nothing.
func TestSourceReadingGuardDetectsAQueryImport(t *testing.T) {
	parse := func(name, source string) (*ast.File, *token.FileSet) {
		t.Helper()
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, source, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse synthetic %s: %v", name, err)
		}
		return file, fset
	}
	const source = `package tools

import "github.com/hnkatze/swagger-extractor-mcp/internal/specsource"

func readIt() { _ = specsource.Source{} }
`

	query, querySet := parse("specs.go", source)
	violations := specSourceViolations([]*ast.File{query}, querySet)
	if len(violations) != 1 {
		t.Fatalf("a query file importing the source reader produced %d violations, want 1: %v", len(violations), violations)
	}
	for _, want := range []string{"specs.go", specSourceImport} {
		if !strings.Contains(violations[0], want) {
			t.Errorf("violation %q does not mention %q", violations[0], want)
		}
	}

	handler, handlerSet := parse(indexSpecHandlerFile, source)
	if violations := specSourceViolations([]*ast.File{handler}, handlerSet); len(violations) != 0 {
		t.Errorf("the index_spec handler file must be allowed to read sources, got %v", violations)
	}

	none, noneSet := parse("discovery.go", "package tools\n")
	if violations := specSourceViolations([]*ast.File{none}, noneSet); len(violations) != 1 {
		t.Errorf("a package with no source reader must be reported, got %v", violations)
	}
}

// TestToolsNetworkGuardDetectsAFetch proves the guard actually fires: without
// this, a passing guard could just mean the detector looks for nothing.
func TestToolsNetworkGuardDetectsAFetch(t *testing.T) {
	const source = `package tools

import "net/http"

func fetchIt(url string) {
	client := &http.Client{}
	req, _ := http.NewRequest("GET", url, nil)
	_, _ = client.Do(req)
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "synthetic.go", source, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse synthetic source: %v", err)
	}
	violations := networkViolations([]*ast.File{file}, fset)
	want := map[string]bool{"imports net/http": false, "uses http.Client": false, "uses http.NewRequest": false}
	for _, violation := range violations {
		for key := range want {
			if strings.Contains(violation, key) {
				want[key] = true
			}
		}
	}
	for key, found := range want {
		if !found {
			t.Errorf("guard missed %q in a source that fetches (violations: %v)", key, violations)
		}
	}
}
