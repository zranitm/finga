// Every error a module's model declares has a row in its facade's table; every kernel error has one in the gateway's PublicOf, and one in the gateway's test that pins the name and kind each reaches a client with.

package rules_test

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// errorTable is where errors get their rows: the package whose Err vars need one, and the file holding the rows.
type errorTable struct {
	errs     string
	declared func(file string) bool // the package's files whose Err vars need a row; nil is every file
	table    string
}

// unmapped lists each Err var of a table's package that its file never names; read from pkgs, loaded from dir, statically, as test/rules can't import a module's internals.
func unmapped(dir string, pkgs []*packages.Package, tables ...errorTable) ([]string, error) {
	var out []string
	for _, tt := range tables {
		declared := declaredErrs(pkgs, tt.errs, tt.declared)
		if len(declared) == 0 {
			return nil, fmt.Errorf("%s declares no Err var: is the load broken?", tt.errs)
		}
		file, info, err := tableFile(dir, pkgs, tt.table)
		if err != nil {
			return nil, err
		}
		named := map[string]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if obj, ok := info.Uses[id].(*types.Var); ok && obj.Pkg() != nil {
					named[obj.Pkg().Path()+"."+obj.Name()] = true
				}
			}
			return true
		})
		for _, v := range declared {
			if !named[v.Pkg().Path()+"."+v.Name()] {
				out = append(out, fmt.Sprintf("%s.%s has no row in %s", v.Pkg().Name(), v.Name(), tt.table))
			}
		}
	}
	slices.Sort(out)
	return out, nil
}

// declaredErrs are the package's exported package-level error vars named Err*, declared in the files in reports, or all of them if it is nil.
func declaredErrs(pkgs []*packages.Package, path string, in func(file string) bool) []*types.Var {
	errType := types.Universe.Lookup("error").Type()
	var out []*types.Var
	for _, p := range pkgs {
		if p.PkgPath != path {
			continue
		}
		scope := p.Types.Scope()
		for _, name := range scope.Names() {
			v, ok := scope.Lookup(name).(*types.Var)
			if !ok || !token.IsExported(name) || !strings.HasPrefix(name, "Err") || !types.Implements(v.Type(), errType.Underlying().(*types.Interface)) {
				continue
			}
			if in == nil || in(filepath.Base(p.Fset.Position(v.Pos()).Filename)) {
				out = append(out, v)
			}
		}
	}
	return out
}

// tableFile finds the file's syntax among the loaded packages, with its package's type information.
func tableFile(dir string, pkgs []*packages.Package, file string) (*ast.File, *types.Info, error) {
	abs, err := filepath.Abs(filepath.Join(dir, file))
	if err != nil {
		return nil, nil, err
	}
	for _, p := range pkgs {
		for _, f := range p.Syntax {
			if p.Fset.File(f.Pos()).Name() == abs {
				return f, p.TypesInfo, nil
			}
		}
	}
	return nil, nil, fmt.Errorf("%s was not loaded", file)
}

// The check finds an error without a row, and passes over what isn't an exported error.
func TestTheCheckFindsEveryErrorWithoutARow(t *testing.T) {
	got, err := unmapped(".", testTables(t), errorTable{
		errs:  "finga/test/rules/testdata/errortables/shop",
		table: "testdata/errortables/till/table.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"shop.ErrClosed has no row in testdata/errortables/till/table.go",
		"shop.ErrLate has no row in testdata/errortables/till/table.go",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A check limited to some of the package's files passes over the errors the others declare.
func TestTheCheckKeepsToItsFiles(t *testing.T) {
	got, err := unmapped(".", testTables(t), errorTable{errs: "finga/test/rules/testdata/errortables/shop", declared: in("shop.go"), table: "testdata/errortables/till/table.go"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"shop.ErrClosed has no row in testdata/errortables/till/table.go"}; !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// testTables loads the testdata's errors and the table checked against them.
func testTables(t *testing.T) []*packages.Package {
	t.Helper()
	pkgs, err := load(".", false, "finga/test/rules/testdata/errortables/shop", "file=testdata/errortables/till/table.go")
	if err != nil {
		t.Fatal(err)
	}
	return pkgs
}

// in is the file named name.
func in(name string) func(file string) bool { return func(file string) bool { return file == name } }

// A model's error without a row reaches the caller as itself, which a client reads as internal; a kernel error without a PublicOf row does too, and one without a row in the gateway's pin goes unchecked.
func TestEveryErrorHasARow(t *testing.T) {
	t.Parallel()
	facade := func(module string) errorTable { // the model's errors become kernel errors
		return errorTable{errs: "finga/modules/" + module + "/internal/model", table: "modules/" + module + "/errors.go"}
	}
	pkgs, err := wholeModule()
	if err != nil {
		t.Fatal(err)
	}
	pinned := slices.IndexFunc(pkgs, func(p *packages.Package) bool {
		return p.ForTest == "finga/modules/gateway/internal/local" && p.PkgPath == p.ForTest
	})
	if pinned < 0 {
		t.Fatal("the gateway's local adapters were not loaded with their tests")
	}
	got, err := unmapped("../..", append(built(pkgs), pkgs[pinned]),
		facade("account"), facade("character"), facade("item"), facade("social"), facade("world"),
		errorTable{errs: "finga/internal/kernel", table: "modules/gateway/internal/model/public.go"},
		errorTable{errs: "finga/internal/kernel", table: "modules/gateway/internal/local/public_test.go"},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range got {
		t.Error(problem)
	}
}
