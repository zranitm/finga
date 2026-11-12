// Every context.Context is named as one: ctx, or a name holding Ctx, such as retryCtx.

package rules_test

import (
	"errors"
	"fmt"
	"go/ast"
	"go/types"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// misnamed lists each context.Context variable, parameter, result or field whose name holds no ctx, in the packages matching pattern, tests included.
func misnamed(dir, pattern string) ([]string, error) {
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo, Dir: dir, Tests: true}
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		return nil, err
	}
	var errs []error
	for _, p := range pkgs {
		for _, e := range p.Errors {
			errs = append(errs, e)
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{} // a package's files come again in its test variant
	var out []string
	for _, p := range pkgs {
		generated := map[string]bool{}
		for _, f := range p.Syntax {
			if ast.IsGenerated(f) {
				generated[p.Fset.File(f.Pos()).Name()] = true
			}
		}
		for id, obj := range p.TypesInfo.Defs {
			v, ok := obj.(*types.Var)
			if !ok || v.Name() == "_" || !isContext(v.Type()) || strings.Contains(strings.ToLower(v.Name()), "ctx") {
				continue
			}
			pos := p.Fset.Position(id.Pos())
			if generated[pos.Filename] {
				continue
			}
			rel, err := filepath.Rel(root, pos.Filename)
			if err != nil {
				rel = pos.Filename
			}
			at := fmt.Sprintf("%s:%d %s", filepath.ToSlash(rel), pos.Line, v.Name())
			if !seen[at] {
				seen[at] = true
				out = append(out, at)
			}
		}
	}
	slices.Sort(out)
	return out, nil
}

func isContext(t types.Type) bool {
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "context" && n.Obj().Name() == "Context"
}

// The check finds a context however it is declared, and only those whose names hold no ctx.
func TestTheCheckFindsEveryMisnamedContext(t *testing.T) {
	got, err := misnamed("testdata/ctxnames", "./...")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"named.go:11 parent", "named.go:16 retrying", "named.go:21 jobs", "named.go:9 hurried"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A context named for what it is reads as one at every use: retryCtx, not retrying.
func TestEveryContextIsNamedCtx(t *testing.T) {
	t.Parallel() // each of the three module-wide loads runs beside the others
	got, err := misnamed("../..", "./...")
	if err != nil {
		t.Fatal(err)
	}
	for _, at := range got {
		t.Errorf("%s: a context.Context's name holds no ctx", at)
	}
}
