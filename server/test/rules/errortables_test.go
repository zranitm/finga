// Every error a module's model declares has a row in its facade's table, and each of the module's kernel rules and contract reasons one in its handler's; every kernel error has one in the gateway's PublicOf, which names it as errmap named it over gRPC.

package rules_test

import (
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

// unmapped lists each Err var of a table's package that its file never names, and each reason no constant in the file holds; read from pkgs, loaded from dir, statically, as test/rules can't import a service's internals.
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
	got, err := unmapped(".", testTables(t), errorTable{
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

// A model's error without a row reaches the caller as an internal error, and a reason without one is never sent.
func TestEveryErrorHasARow(t *testing.T) {
	t.Parallel()
	facade := func(module string) errorTable { // the model's errors become kernel errors
		return errorTable{errs: "finga/services/" + module + "/internal/model", table: "services/" + module + "/errors.go"}
	}
	handler := func(module string, reasons protoreflect.Enum) errorTable { // the module's kernel rules become gRPC codes and reasons
		return errorTable{errs: "finga/internal/kernel", declared: in("errors_" + module + ".go"), table: "services/" + module + "/internal/handler/errors.go", reasons: reasons}
	}
	pkgs, err := wholeModule()
	if err != nil {
		t.Fatal(err)
	}
	got, err := unmapped("../..", built(pkgs),
		facade("account"), handler("account", accountv1.Reason(0)),
		facade("character"), handler("character", characterv1.Reason(0)),
		facade("item"), handler("item", itemv1.Reason(0)),
		facade("social"), handler("social", socialv1.Reason(0)),
		facade("world"), handler("world", worldv1.Reason(0)),
		errorTable{errs: "finga/internal/kernel", table: "services/gateway/internal/model/public.go"},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range got {
		t.Error(problem)
	}
}

// overridden are the failures a gateway adapter names itself, by module and kernel error, with the Public it gives.
var overridden = map[[2]string]string{
	{"world", "ErrClassUnavailable"}: "Internal", // local.worldErr: a class the data lacks is the world's own fault, as errmap read world's reason
}

// A client meets a module's failure in process as it did over gRPC: for each row of a handler's table, PublicOf names the row's kernel error as errmap named the status the row sends.
func TestTheGatewayNamesAModuleFailureAsTheWireDid(t *testing.T) {
	t.Parallel()
	modules := []string{"account", "character", "item", "social", "world"}
	pkgs, err := wholeModule()
	if err != nil {
		t.Fatal(err)
	}
	pkgs = built(pkgs)
	rows := func(file, name string) ([]ast.Expr, *types.Info) {
		t.Helper()
		f, info, err := tableFile("../..", pkgs, file)
		if err != nil {
			t.Fatal(err)
		}
		lit := varValue(f, name)
		if lit == nil {
			t.Fatalf("%s declares no %s", file, name)
		}
		return lit.Elts, info
	}
	named := func(info *types.Info, e ast.Expr) string {
		t.Helper()
		obj := objectOf(info, e)
		if obj == nil {
			t.Fatalf("%s names nothing the type checker resolved", types.ExprString(e))
		}
		return obj.Name()
	}
	wire := map[string]string{} // a reason, by its Go constant: the Public errmap gives it
	elts, info := rows("services/gateway/internal/clients/errmap/errmap.go", "reasons")
	for _, e := range elts {
		kv := e.(*ast.KeyValueExpr)
		wire[nameOf(info, kv.Key.(*ast.CallExpr).Args[0])] = named(info, kv.Value)
	}
	inProcess := map[string]string{} // a kernel error, by name: the Public PublicOf gives it
	elts, info = rows("services/gateway/internal/model/public.go", "ownErrors")
	for _, e := range elts {
		row := e.(*ast.CompositeLit).Elts
		inProcess[named(info, row[0])] = named(info, row[1])
	}
	retryable := retryableRules(t)
	checked := 0
	for _, m := range modules {
		elts, info := rows("services/"+m+"/internal/handler/errors.go", "Errors")
		for _, e := range elts {
			var kernelErr, code, reason string
			for _, f := range e.(*ast.CompositeLit).Elts {
				kv := f.(*ast.KeyValueExpr)
				switch kv.Key.(*ast.Ident).Name {
				case "Err":
					kernelErr = named(info, kv.Value)
				case "Code":
					code = named(info, kv.Value)
				case "Reason":
					reason = nameOf(info, kv.Value)
				}
			}
			want, ok := wire[reason]
			switch {
			case ok:
			case code == "Unavailable":
				want = "Unavailable"
			default:
				want = "Internal"
			}
			got, ok := overridden[[2]string{m, kernelErr}]
			if !ok {
				got = inProcess[kernelErr]
			}
			if got != want {
				t.Errorf("%s's %s: the client saw %s over gRPC, and sees %q in process", m, kernelErr, want, got)
			}
			if m == "world" { // the gateway acts on the kind of a world call only
				if wireKind, kind := kindOverGRPC(reason, code), kindInProcess(kernelErr, retryable); wireKind != kind {
					t.Errorf("world's %s: the gateway acted on %q over gRPC, and on %q in process", kernelErr, wireKind, kind)
				}
			}
			checked++
		}
	}
	if checked < len(inProcess) {
		t.Fatalf("checked %d rows, fewer than PublicOf's %d: is the check broken?", checked, len(inProcess))
	}
}

// kindOverGRPC is the kind errmap.Wrap gave a status: the world not holding the stream, or a service unavailable, reason or not.
func kindOverGRPC(reason, code string) string {
	switch {
	case reason == "finga/api/gen/world/v1.Reason_REASON_NOT_SUBSCRIBED":
		return "not subscribed"
	case code == "Unavailable":
		return "unavailable"
	}
	return ""
}

// kindInProcess is the kind the gateway's adapters give a kernel error: ErrNotSubscribed, or a rule a retry may pass.
func kindInProcess(kernelErr string, retryable map[string]string) string {
	switch {
	case kernelErr == "ErrNotSubscribed":
		return "not subscribed"
	case retryable[kernelErr] != "":
		return "unavailable"
	}
	return ""
}

// varValue is the composite literal the file's package-level var name is declared with, or nil.
func varValue(f *ast.File, name string) *ast.CompositeLit {
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			for k, n := range vs.Names {
				if n.Name == name && k < len(vs.Values) {
					lit, _ := vs.Values[k].(*ast.CompositeLit)
					return lit
				}
			}
		}
	}
	return nil
}

// objectOf is what an identifier or a selector names.
func objectOf(info *types.Info, e ast.Expr) types.Object {
	switch e := e.(type) {
	case *ast.Ident:
		return info.Uses[e]
	case *ast.SelectorExpr:
		return info.Uses[e.Sel]
	}
	return nil
}

// nameOf is the package path and name of what e names, or "" for nothing.
func nameOf(info *types.Info, e ast.Expr) string {
	obj := objectOf(info, e)
	if obj == nil || obj.Pkg() == nil {
		return ""
	}
	return obj.Pkg().Path() + "." + obj.Name()
}
