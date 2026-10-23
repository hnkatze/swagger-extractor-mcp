package loader

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// S1: a query must never touch the network. This package is what a query path
// uses to turn stored bytes into a document, so it holds no HTTP client and
// offers no entry point that takes a URL. These checks read the package source
// instead of its behavior, because a reintroduced fetch would otherwise only
// show up as a tool call that quietly reaches out.

// forbiddenImports are the packages that would let loader code fetch.
var forbiddenImports = []string{"net/http", "net/http/httptest", "net"}

// parseLoaderFiles parses every non-test .go file of this package.
func parseLoaderFiles(t *testing.T) ([]*ast.File, *token.FileSet) {
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

// TestLoaderImportsNoHTTPClient fails if any loader source imports a package
// that can perform an HTTP request (S1: only the indexer fetches).
func TestLoaderImportsNoHTTPClient(t *testing.T) {
	files, fset := parseLoaderFiles(t)

	for _, file := range files {
		for _, spec := range file.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			for _, forbidden := range forbiddenImports {
				if path == forbidden {
					t.Errorf("%s imports %q: the loader must not be able to fetch (S1)",
						fset.Position(spec.Pos()).Filename, path)
				}
			}
		}
	}
}

// TestNoExportedLoaderFunctionTakesURL fails if the package grows an exported
// function or method that accepts a URL, the shape every fetch entry point had.
func TestNoExportedLoaderFunctionTakesURL(t *testing.T) {
	files, _ := parseLoaderFiles(t)

	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !isAPISurface(fn) {
				continue
			}
			for _, param := range funcParams(fn.Type.Params) {
				if takesURL(param) {
					t.Errorf("%s(%s) takes a URL: a query path must not accept a URL (S1, S4a)",
						fn.Name.Name, param)
				}
			}
		}
	}
}

// isAPISurface reports whether fn is reachable from outside the package.
func isAPISurface(fn *ast.FuncDecl) bool {
	if fn.Recv == nil {
		return fn.Name.IsExported()
	}
	for _, field := range fn.Recv.List {
		if ast.IsExported(receiverName(field.Type)) {
			return fn.Name.IsExported()
		}
	}
	return false
}

func receiverName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return receiverName(e.X)
	case *ast.Ident:
		return e.Name
	case *ast.IndexExpr:
		return receiverName(e.X)
	}
	return ""
}

// funcParams flattens a parameter list into "name type" strings.
func funcParams(params *ast.FieldList) []string {
	if params == nil {
		return nil
	}
	var out []string
	for _, field := range params.List {
		typeName := exprName(field.Type)
		if len(field.Names) == 0 {
			out = append(out, typeName)
			continue
		}
		for _, name := range field.Names {
			out = append(out, name.Name+" "+typeName)
		}
	}
	return out
}

func exprName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return "*" + exprName(e.X)
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprName(e.X) + "." + e.Sel.Name
	case *ast.ArrayType:
		return "[]" + exprName(e.Elt)
	}
	return ""
}

// takesURL reports whether a flattened parameter is URL-shaped by name or type.
func takesURL(param string) bool {
	lower := strings.ToLower(param)
	if strings.Contains(lower, "url") || strings.Contains(lower, "uri") {
		return true
	}
	return false
}
