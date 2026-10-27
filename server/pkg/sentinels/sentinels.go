// Package sentinels reads Go source for the sentinel errors packages
// declare and the ones a file names, so a test can check that an error
// table maps every error a package can return. It parses source only and
// never builds or runs it.
package sentinels

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
)

// Declared lists "pkg.ErrName" for every exported package-level variable
// named Err..., in the Go files under each directory, skipping tests and
// generated code in internal/db.
func Declared(dirs ...string) ([]string, error) {
	var out []string
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.Contains(filepath.ToSlash(path), "/internal/db/") {
				return nil
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			for _, decl := range f.Decls {
				g, ok := decl.(*ast.GenDecl)
				if !ok || g.Tok != token.VAR {
					continue
				}
				for _, spec := range g.Specs {
					for _, name := range spec.(*ast.ValueSpec).Names {
						if strings.HasPrefix(name.Name, "Err") {
							out = append(out, f.Name.Name+"."+name.Name)
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Referenced is the set of "pkg.ErrName" selectors in the files.
func Referenced(files ...string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, file := range files {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if x, ok := sel.X.(*ast.Ident); ok && strings.HasPrefix(sel.Sel.Name, "Err") {
					out[x.Name+"."+sel.Sel.Name] = true
				}
			}
			return true
		})
	}
	return out, nil
}
