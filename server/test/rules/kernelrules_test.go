// Every kernel rule has a reason of its own: the call metrics and the client codes tell failures apart by it.

package rules_test

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// callCodes are the codes a facade call ends with besides a rule's reason (internal/facade), so no rule takes one.
var callCodes = []string{"ok", "unavailable", "canceled", "internal"}

var snakeCase = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// ruleProblems lists each call to newRule, in the packages patterns name, outside home or with a reason that is not a constant, not snake case, a call code or another rule's; home must declare one.
func ruleProblems(pkgs []*packages.Package, newRule, home string) ([]string, error) {
	var out []string
	named := map[string]string{} // reason: where the first rule named it
	loaded, rules := false, 0    // whether home loaded, and the rules it declares
	for _, p := range pkgs {
		loaded = loaded || p.PkgPath == home
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
				} else {
					rules++
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
	switch {
	case !loaded:
		return nil, fmt.Errorf("%s isn't among the packages loaded", home)
	case rules == 0:
		return nil, fmt.Errorf("%s declares no rule with %s: was it renamed?", home, newRule)
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
	pkgs, err := load(".", false, "./testdata/kernelrules/...")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ruleProblems(pkgs, k+".NewRule", k)
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

// A load that misses the rules' package, or finds no rule in it, fails rather than passing with nothing checked.
func TestTheCheckFailsOnAnEmptyLoad(t *testing.T) {
	const k = "finga/test/rules/testdata/kernelrules/k"
	for name, tt := range map[string]struct {
		patterns []string
		newRule  string
	}{
		"home not loaded": {[]string{"./testdata/kernelrules/other"}, k + ".NewRule"},
		"newRule renamed": {[]string{"./testdata/kernelrules/..."}, k + ".NewReason"},
	} {
		pkgs, err := load(".", false, tt.patterns...)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ruleProblems(pkgs, tt.newRule, k); err == nil {
			t.Errorf("%s: the check passed", name)
		}
	}
}

func TestEachRuleHasAReasonOfItsOwn(t *testing.T) {
	t.Parallel()
	pkgs, err := wholeModule()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ruleProblems(built(pkgs), "finga/internal/kernel.NewRule", "finga/internal/kernel")
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range got {
		t.Error(problem)
	}
}

// retryableRules are the kernel's rules declared Retryable, each with the file declaring it.
func retryableRules(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	files, err := filepath.Glob("../../internal/kernel/*.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for k, v := range vs.Values {
				if call, ok := v.(*ast.CallExpr); ok {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Retryable" {
						out[vs.Names[k].Name] = filepath.Base(file)
					}
				}
			}
			return true
		})
	}
	if len(out) == 0 {
		t.Fatal("found no retryable rule: is the check broken?")
	}
	return out
}

// A retryable item or character rule would be retried forever by world's loot and character's starter grant, which retry anything but kernel.IsRule, and would not refuse world's saves, refused by it; each module means its rules as a final answer.
func TestItemAndCharacterDeclareNoRetryableRule(t *testing.T) {
	for name, file := range retryableRules(t) {
		if file == "errors_item.go" || file == "errors_character.go" {
			t.Errorf("%s (%s) is retryable: revisit world's Loot.Retryable and Saver.Refused and character's Starter.Retryable first", name, file)
		}
	}
}
