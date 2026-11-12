// Every error a service's model declares, and every reason its contract lists, has a row in its handler's error table; every kernel error has one in the gateway's PublicOf.

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
	errs    string
	table   string
	reasons protoreflect.Enum
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
		declared := declaredErrs(pkgs, tt.errs)
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

// declaredErrs are the package's exported package-level error vars named Err*.
func declaredErrs(pkgs []*packages.Package, path string) []*types.Var {
	errType := types.Universe.Lookup("error").Type()
	var out []*types.Var
	for _, p := range pkgs {
		if p.PkgPath != path {
			continue
		}
		scope := p.Types.Scope()
		for _, name := range scope.Names() {
			if v, ok := scope.Lookup(name).(*types.Var); ok && token.IsExported(name) && strings.HasPrefix(name, "Err") && types.Implements(v.Type(), errType.Underlying().(*types.Interface)) {
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
	want := []string{"LABEL_REPEATED has no row in testdata/errortables/till/table.go", "shop.ErrClosed has no row in testdata/errortables/till/table.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A model's error without a row reaches the caller as an internal error, and a reason without one is never sent.
func TestEveryErrorHasARow(t *testing.T) {
	t.Parallel()
	handler := func(service string, reasons protoreflect.Enum) errorTable {
		return errorTable{errs: "finga/services/" + service + "/internal/model", table: "services/" + service + "/internal/handler/errors.go", reasons: reasons}
	}
	got, err := unmapped("../..",
		handler("account", accountv1.Reason(0)),
		handler("character", characterv1.Reason(0)),
		handler("item", itemv1.Reason(0)),
		handler("social", socialv1.Reason(0)),
		handler("world", worldv1.Reason(0)),
		errorTable{errs: "finga/internal/kernel", table: "services/gateway/internal/model/public.go"},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range got {
		t.Error(problem)
	}
}
