// What the module handlers register on the transport, checked in their source: every push has exactly one encoder and every client message exactly one route, so no module's registration can go missing without a test saying which.

package rules_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	clientv1 "finga/api/gen/client/v1"
)

// goFiles parses every non-test Go file of the directories' packages.
func goFiles(t *testing.T, dirs ...string) []*ast.File {
	t.Helper()
	fset := token.NewFileSet()
	var out []*ast.File
	for _, dir := range dirs {
		paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range paths {
			if strings.HasSuffix(p, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, p, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, f)
		}
	}
	return out
}

// handlerDirs are the module handlers' directories, with the transport's.
func handlerDirs(t *testing.T) []string {
	t.Helper()
	dirs, err := filepath.Glob("../../internal/handler/*")
	if err != nil {
		t.Fatal(err)
	}
	return dirs
}

// A push without an encoder is dropped, and a second encoder for one is refused at startup; either would be found in a game, so this finds it first. A push is an event of a module's facade (the world's events, the social and pvp deliveries), the item handler's own piece event, but the frame and the detach, which the adapter turns into one delivery per recipient and a re-join.
func TestEveryPushHasExactlyOneEncoder(t *testing.T) {
	pushes := map[string]int{"world.Framed": 0, "piecesAdded": 0}
	for _, f := range goFiles(t, "../../internal/application/world") {
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv != nil && fn.Name.Name == "worldEvent" && len(fn.Recv.List) == 1 {
				if id, ok := fn.Recv.List[0].Type.(*ast.Ident); ok && id.Name != "Frame" && id.Name != "Detached" {
					pushes["world."+id.Name] = 0
				}
			}
		}
	}
	for pkg, marker := range map[string]string{"social": "delivery", "pvp": "delivery"} {
		for _, f := range goFiles(t, "../../internal/application/"+pkg) {
			for _, d := range f.Decls {
				if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv != nil && fn.Name.Name == marker && len(fn.Recv.List) == 1 {
					if id, ok := fn.Recv.List[0].Type.(*ast.Ident); ok {
						pushes[pkg+"."+id.Name] = 0
					}
				}
			}
		}
	}
	if len(pushes) < 3 {
		t.Fatal("no push found: is the scan broken?")
	}
	extra := map[string]int{}
	for _, f := range goFiles(t, handlerDirs(t)...) {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Encode" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "transport" {
				return true
			}
			lit, ok := call.Args[1].(*ast.FuncLit)
			if !ok || len(lit.Type.Params.List) != 1 {
				return true
			}
			if id, ok := lit.Type.Params.List[0].Type.(*ast.Ident); ok { // a handler's own event
				if _, known := pushes[id.Name]; known {
					pushes[id.Name]++
				} else {
					extra[id.Name]++
				}
			}
			if t, ok := lit.Type.Params.List[0].Type.(*ast.SelectorExpr); ok {
				if pkg, ok := t.X.(*ast.Ident); ok {
					key := pkg.Name + "." + t.Sel.Name
					if _, known := pushes[key]; known {
						pushes[key]++
					} else {
						extra[key]++
					}
				}
			}
			return true
		})
	}
	for name, n := range pushes {
		if n != 1 {
			t.Errorf("the push %s has %d encoders registered by transport.Encode, want exactly 1", name, n)
		}
	}
	for name := range extra {
		t.Errorf("transport.Encode registers an encoder for %s, which is no push", name)
	}
}

// A client message without a route is refused as not implemented, and a second route for one is refused at startup.
func TestEveryClientMessageHasExactlyOneRoute(t *testing.T) {
	body := (&clientv1.ClientMessage{}).ProtoReflect().Descriptor().Oneofs().ByName("body").Fields()
	routes := map[string]int{}
	for i := range body.Len() {
		routes[string(body.Get(i).Message().Name())] = 0
	}
	extra := map[string]int{}
	for _, f := range goFiles(t, handlerDirs(t)...) {
		inTransport := f.Name.Name == "transport"
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 3 {
				return true
			}
			switch fun := call.Fun.(type) {
			case *ast.SelectorExpr: // transport.Handle(r, "Name", …) in a module's handler
				if pkg, ok := fun.X.(*ast.Ident); !ok || pkg.Name != "transport" || fun.Sel.Name != "Handle" {
					return true
				}
			case *ast.Ident: // Handle(h, "Ping", …) in the transport itself
				if !inTransport || fun.Name != "Handle" {
					return true
				}
			default:
				return true
			}
			lit, ok := call.Args[1].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			name, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if _, known := routes[name]; known {
				routes[name]++
			} else {
				extra[name]++
			}
			return true
		})
	}
	names := make([]string, 0, len(routes))
	for name := range routes {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if routes[name] != 1 {
			t.Errorf("the client message %s has %d routes registered by Handle, want exactly 1", name, routes[name])
		}
	}
	for name := range extra {
		t.Errorf("Handle routes %q, which is no client message", name)
	}
}
