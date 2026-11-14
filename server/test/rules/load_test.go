// The rules read the module's packages from one load, as each load type-checks the whole module.

package rules_test

import (
	"errors"
	"slices"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"
)

// load type-checks the packages patterns name from dir, with their test variants if tests, and fails on any package's errors.
func load(dir string, tests bool, patterns ...string) ([]*packages.Package, error) {
	mode := packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedForTest
	pkgs, err := packages.Load(&packages.Config{Mode: mode, Dir: dir, Tests: tests}, patterns...)
	if err != nil {
		return nil, err
	}
	var errs []error
	for _, p := range pkgs {
		for _, e := range p.Errors {
			errs = append(errs, e)
		}
	}
	return pkgs, errors.Join(errs...)
}

// wholeModule is every package of the module with its tests, loaded once for all the rules; the rules only read it.
var wholeModule = sync.OnceValues(func() ([]*packages.Package, error) { return load("../..", true, "./...") })

// built keeps the packages as a binary builds them: no test variant and no test binary.
func built(pkgs []*packages.Package) []*packages.Package {
	return slices.DeleteFunc(slices.Clone(pkgs), func(p *packages.Package) bool {
		return p.ForTest != "" || strings.HasSuffix(p.ID, ".test")
	})
}
