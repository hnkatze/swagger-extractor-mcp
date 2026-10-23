package tools

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
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

// TestToolsImportNoHTTPClient fails if any tools source imports a package that
// can perform an HTTP request (S1: only the indexer fetches).
func TestToolsImportNoHTTPClient(t *testing.T) {
	files, fset := parsePackageSources(t)
	for _, violation := range networkViolations(files, fset) {
		t.Errorf("%s: the query path must not be able to fetch (S1)", violation)
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
