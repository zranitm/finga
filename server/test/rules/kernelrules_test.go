// Every kernel rule has a reason of its own: the call metrics and the gRPC and client codes tell failures apart by it.

package rules_test

import (
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// callCodes are the codes a facade call ends with besides a rule's reason (internal/facade), so no rule takes one.
var callCodes = []string{"ok", "unavailable", "canceled", "internal"}

var snakeCase = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// ruleProblems lists each call to newRule, in the packages patterns name, outside home or with a reason that is not a constant, not snake case, a call code or another rule's.
func ruleProblems(dir string, patterns []string, newRule, home string) ([]string, error) {
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo, Dir: dir}
	pkgs, err := packages.Load(cfg, patterns...)
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
	var out []string
	named := map[string]string{} // reason: where the first rule named it
	for _, p := range pkgs {
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || !calls(p.TypesInfo, call.Fun, newRule) {
					return true
				}
				at := p.Fset.Position(call.Pos())
				where := fmt.Sprintf("%s:%d", at.Filename[strings.LastIndex(at.Filename, "/")+1:], at.Line)
				if p.PkgPath != home {
					out = append(out, fmt.Sprintf("%s (%s) declares a rule: rules live in %s, where their reasons are checked together", p.PkgPath, where, home))
				}
				tv := p.TypesInfo.Types[call.Args[0]]
				if tv.Value == nil || tv.Value.Kind() != constant.String {
					out = append(out, where+": a rule's reason is a constant string")
					return true
				}
				reason := constant.StringVal(tv.Value)
				switch first, taken := named[reason]; {
				case taken:
					out = append(out, fmt.Sprintf("%s: %s is already the reason of the rule at %s", where, reason, first))
				case slices.Contains(callCodes, reason):
					out = append(out, fmt.Sprintf("%s: %s is a code a call ends with", where, reason))
				case !snakeCase.MatchString(reason):
					out = append(out, fmt.Sprintf("%s: %q is not snake case", where, reason))
				default:
					named[reason] = where
				}
				return true
			})
		}
	}
	slices.Sort(out)
	return out, nil
}

// calls reports whether fun names the function fullName.
func calls(info *types.Info, fun ast.Expr, fullName string) bool {
	var id *ast.Ident
	switch f := fun.(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	default:
		return false
	}
	fn, ok := info.Uses[id].(*types.Func)
	return ok && fn.FullName() == fullName
}

// The check reports a reason taken twice, a call code, a reason that isn't snake case or a constant, and a rule declared outside its package.
func TestTheCheckFindsEveryBadRule(t *testing.T) {
	const k = "finga/test/rules/testdata/kernelrules/k"
	got, err := ruleProblems(".", []string{"./testdata/kernelrules/..."}, k+".NewRule", k)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`finga/test/rules/testdata/kernelrules/other (other.go:7) declares a rule: rules live in finga/test/rules/testdata/kernelrules/k, where their reasons are checked together`,
		`k.go:15: sold_out is already the reason of the rule at k.go:14`,
		`k.go:16: ok is a code a call ends with`,
		`k.go:17: "Bad Name" is not snake case`,
		`k.go:18: a rule's reason is a constant string`,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestEachRuleHasAReasonOfItsOwn(t *testing.T) {
	t.Parallel() // a module-wide load, beside the others
	got, err := ruleProblems("../..", []string{"./..."}, "finga/internal/kernel.NewRule", "finga/internal/kernel")
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range got {
		t.Error(problem)
	}
}
