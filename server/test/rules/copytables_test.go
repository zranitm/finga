// Every event a map sends, every event the world module's stream brings and every push the account module sends has a row in the test that checks its conversion keeps each field: one without a row could lose a field unseen.

package rules_test

import (
	"fmt"
	"go/ast"
	"go/types"
	"slices"
	"testing"

	"golang.org/x/tools/go/packages"
)

// copyTable is where a sum type's members get their rows: the package declaring them, the method marking each, and the test functions, in file, whose bodies name each.
type copyTable struct {
	members string
	marker  string
	file    string
	tests   []string
}

// unrowed lists each member of a table's sum type that none of its test functions names.
func unrowed(dir string, pkgs []*packages.Package, tables ...copyTable) ([]string, error) {
	var out []string
	for _, tt := range tables {
		members := marked(pkgs, tt.members, tt.marker)
		if len(members) == 0 {
			return nil, fmt.Errorf("%s has no type with %s(): is the load broken?", tt.members, tt.marker)
		}
		file, info, err := tableFile(dir, pkgs, tt.file)
		if err != nil {
			return nil, err
		}
		named := map[*types.TypeName]bool{}
		found := 0
		for _, d := range file.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && slices.Contains(tt.tests, fn.Name.Name) {
				found++
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if id, ok := n.(*ast.Ident); ok {
						if tn, ok := info.Uses[id].(*types.TypeName); ok {
							named[tn] = true
						}
					}
					return true
				})
			}
		}
		if found != len(tt.tests) {
			return nil, fmt.Errorf("%s holds %d of the functions %v", tt.file, found, tt.tests)
		}
		for _, m := range members {
			if !named[m] {
				out = append(out, fmt.Sprintf("%s.%s has no row in %s", m.Pkg().Name(), m.Name(), tt.file))
			}
		}
	}
	slices.Sort(out)
	return out, nil
}

// marked is the named types of the package at path with the method marker, on the value or the pointer.
func marked(pkgs []*packages.Package, path, marker string) []*types.TypeName {
	var out []*types.TypeName
	for _, p := range pkgs {
		if p.PkgPath != path || p.ForTest != "" {
			continue
		}
		scope := p.Types.Scope()
		for _, name := range scope.Names() {
			tn, ok := scope.Lookup(name).(*types.TypeName)
			if !ok {
				continue
			}
			if obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(tn.Type()), true, p.Types, marker); obj != nil {
				if _, isFunc := obj.(*types.Func); isFunc {
					out = append(out, tn)
				}
			}
		}
	}
	return out
}

// The check finds a member its table's functions never name, though the file names it elsewhere, and passes over a type without the mark.
func TestTheCheckFindsEveryMemberWithoutARow(t *testing.T) {
	pkgs, err := load(".", false, "finga/test/rules/testdata/copytables/notes", "file=testdata/copytables/checks/checks.go")
	if err != nil {
		t.Fatal(err)
	}
	got, err := unrowed(".", pkgs, copyTable{members: "finga/test/rules/testdata/copytables/notes", marker: "note", file: "testdata/copytables/checks/checks.go", tests: []string{"keepsEachNote"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"notes.Bye has no row in testdata/copytables/checks/checks.go"}; !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A map's event, the world's stream event or a push without a row in its copy test could drop a field on its way to the client unseen.
func TestEveryEventAndPushHasACopyRow(t *testing.T) {
	t.Parallel()
	pkgs, err := wholeModule()
	if err != nil {
		t.Fatal(err)
	}
	got, err := unrowed("../..", pkgs,
		copyTable{members: "finga/internal/model/world", marker: "event", file: "internal/application/world/events_test.go", tests: []string{"TestEveryModelEventKeepsItsFields"}},
		copyTable{members: "finga/internal/application/world", marker: "worldEvent", file: "internal/adapter/account/deliveries_test.go", tests: []string{"TestEveryWorldEventKeepsItsValues", "TestAFrameKeepsItsValues"}},
		copyTable{members: "finga/internal/model/account", marker: "push", file: "internal/handler/transport/push_outbox_test.go", tests: []string{"TestEachPushReachesTheClientAsItsMessage"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range got {
		t.Error(problem)
	}
}
