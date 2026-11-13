// Every error a module's model declares has a row in its facade's table, and each of the module's kernel rules and contract reasons one in its handler's; kernel's shared errors have one in the gateway's PublicOf.

package rules_test

import (
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	accountv1 "finga/api/gen/account/v1"
	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/pkg/grpcx/grpctest"
)

// errorTable is where errors get their rows: the package whose Err vars need one, the file holding the rows, and the contract's reasons, if any.
type errorTable struct {
	errs     string
	declared func(file string) bool // the package's files whose Err vars need a row; nil is every file
	table    string
	reasons  protoreflect.Enum
}

// unmapped lists each Err var of a table's package that its file never names, and each reason no constant in the file holds; read from dir, statically, as test/rules can't import a service's internals.
func unmapped(dir string, tables ...errorTable) ([]string, error) {
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo, Dir: dir}
	var patterns []string
	for _, tt := range tables {
		patterns = append(patterns, tt.errs, "file="+tt.table)
	}
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
	for _, tt := range tables {
		declared := declaredErrs(pkgs, tt.errs, tt.declared)
		if len(declared) == 0 {
			return nil, fmt.Errorf("%s declares no Err var: is the load broken?", tt.errs)
		}
		file, info, err := tableFile(dir, pkgs, tt.table)
		if err != nil {
			return nil, err
		}
		named, held := map[string]bool{}, map[protoreflect.EnumNumber]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			switch obj := info.Uses[id].(type) {
			case *types.Var:
				if obj.Pkg() != nil {
					named[obj.Pkg().Path()+"."+obj.Name()] = true
				}
			case *types.Const:
				if tt.reasons != nil && isEnum(obj.Type(), tt.reasons) {
					n, _ := constant.Int64Val(obj.Val())
					held[protoreflect.EnumNumber(n)] = true
				}
			}
			return true
		})
		for _, v := range declared {
			if !named[v.Pkg().Path()+"."+v.Name()] {
				out = append(out, fmt.Sprintf("%s.%s has no row in %s", v.Pkg().Name(), v.Name(), tt.table))
			}
		}
		if tt.reasons == nil {
			continue
		}
		values := tt.reasons.Descriptor().Values()
		for _, r := range grpctest.Reasons(tt.reasons.Descriptor()) {
			if !held[values.ByName(protoreflect.Name(r.Name)).Number()] {
				out = append(out, fmt.Sprintf("%s has no row in %s", r.Name, tt.table))
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

// isEnum reports whether t is the generated Go type of e.
func isEnum(t types.Type, e protoreflect.Enum) bool {
	n, ok := types.Unalias(t).(*types.Named)
	want := reflect.TypeOf(e)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == want.PkgPath() && n.Obj().Name() == want.Name()
}

// The check finds an error or a reason without a row, and passes over what isn't an exported error.
func TestTheCheckFindsEveryErrorWithoutARow(t *testing.T) {
	got, err := unmapped(".", errorTable{
		errs:    "finga/test/rules/testdata/errortables/shop",
		table:   "testdata/errortables/till/table.go",
		reasons: descriptorpb.FieldDescriptorProto_Label(0),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"LABEL_REPEATED has no row in testdata/errortables/till/table.go",
		"shop.ErrClosed has no row in testdata/errortables/till/table.go",
		"shop.ErrLate has no row in testdata/errortables/till/table.go",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A check limited to some of the package's files passes over the errors the others declare.
func TestTheCheckKeepsToItsFiles(t *testing.T) {
	got, err := unmapped(".", errorTable{errs: "finga/test/rules/testdata/errortables/shop", declared: in("shop.go"), table: "testdata/errortables/till/table.go"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"shop.ErrClosed has no row in testdata/errortables/till/table.go"}; !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// in is the file named name.
func in(name string) func(file string) bool { return func(file string) bool { return file == name } }

// A model's error without a row reaches the caller as an internal error, and a reason without one is never sent.
func TestEveryErrorHasARow(t *testing.T) {
	t.Parallel()
	facade := func(module string) errorTable { // the model's errors become kernel errors
		return errorTable{errs: "finga/services/" + module + "/internal/model", table: "services/" + module + "/errors.go"}
	}
	handler := func(module string, reasons protoreflect.Enum) errorTable { // the module's kernel rules become gRPC codes and reasons
		return errorTable{errs: "finga/internal/kernel", declared: in("errors_" + module + ".go"), table: "services/" + module + "/internal/handler/errors.go", reasons: reasons}
	}
	unwrapped := func(module string, reasons protoreflect.Enum) errorTable { // a module with no facade yet: its handler maps the model's errors
		return errorTable{errs: "finga/services/" + module + "/internal/model", table: "services/" + module + "/internal/handler/errors.go", reasons: reasons}
	}
	shared := func(file string) bool { // a module's rules get PublicOf rows once the gateway calls its facade
		module, _ := filepath.Match("errors*.go", file)
		return !module
	}
	got, err := unmapped("../..",
		facade("account"), handler("account", accountv1.Reason(0)),
		facade("character"), handler("character", characterv1.Reason(0)),
		facade("item"), handler("item", itemv1.Reason(0)),
		facade("social"), handler("social", socialv1.Reason(0)),
		unwrapped("world", worldv1.Reason(0)),
		errorTable{errs: "finga/internal/kernel", declared: shared, table: "services/gateway/internal/model/public.go"},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range got {
		t.Error(problem)
	}
}
