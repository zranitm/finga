// No goroutine the game starts lets a panic end the process: go is for pkg/panics and pkg/runner, and every other way to start one is a reviewed site.

package rules_test

import (
	"errors"
	"go/ast"
	"go/types"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// goroutineStarters are the calls that run a function on a goroutine of its own, as the go keyword does.
var goroutineStarters = []string{
	"context.AfterFunc",
	"time.AfterFunc",
	"(*sync.WaitGroup).Go",
	"(*golang.org/x/sync/errgroup.Group).Go",
	"(*golang.org/x/sync/errgroup.Group).TryGo",
}

// goroutineSites counts, by the function they are in, the go statements and goroutineStarters uses in the packages the main packages mains link, but allowed ones.
func goroutineSites(dir string, mains, allowed []string) (map[string]int, error) {
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports, Dir: dir}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, err
	}
	var errs []error
	byPath := map[string]*packages.Package{}
	for _, p := range pkgs {
		for _, e := range p.Errors {
			errs = append(errs, e)
		}
		byPath[p.PkgPath] = p
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	linked := map[string]bool{}
	var link func(p *packages.Package)
	link = func(p *packages.Package) {
		if linked[p.PkgPath] {
			return
		}
		linked[p.PkgPath] = true
		for path := range p.Imports {
			if q, ok := byPath[path]; ok {
				link(q)
			}
		}
	}
	for _, p := range pkgs {
		if p.Name == "main" && slices.Contains(mains, p.PkgPath) {
			link(p)
		}
	}
	sites := map[string]int{}
	for _, path := range slices.Sorted(maps.Keys(linked)) {
		if slices.Contains(allowed, path) {
			continue
		}
		p := byPath[path]
		for _, f := range p.Syntax {
			if ast.IsGenerated(f) {
				continue
			}
			for _, d := range f.Decls {
				name := path + ".init" // a package-level initialiser runs as the package initialises
				if fd, ok := d.(*ast.FuncDecl); ok {
					name = funcName(path, fd)
				}
				ast.Inspect(d, func(n ast.Node) bool {
					switch n := n.(type) {
					case *ast.GoStmt:
						sites[name]++
					case *ast.Ident:
						if startsGoroutine(p.TypesInfo, n) {
							sites[name]++
						}
					}
					return true
				})
			}
		}
	}
	return sites, nil
}

// startsGoroutine reports whether id names one of goroutineStarters, called or taken as a value.
func startsGoroutine(info *types.Info, id *ast.Ident) bool {
	fn, ok := info.Uses[id].(*types.Func)
	return ok && slices.Contains(goroutineStarters, fn.FullName())
}

// funcName names fd as types.Func.FullName does: pkg.Func, or pkg.(*Recv).Method.
func funcName(pkg string, fd *ast.FuncDecl) string {
	if fd.Recv == nil {
		return pkg + "." + fd.Name.Name
	}
	return pkg + ".(" + types.ExprString(fd.Recv.List[0].Type) + ")." + fd.Name.Name
}

// The check counts every way to start a goroutine, in the binaries' packages only, and skips the allowed ones.
func TestTheCheckFindsEveryGoroutineABinaryStarts(t *testing.T) {
	got, err := goroutineSites("testdata/goroutines", []string{"finga/test/rules/testdata/goroutines"}, []string{"finga/test/rules/testdata/goroutines/allowed"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"finga/test/rules/testdata/goroutines/linked.Start":       2,
		"finga/test/rules/testdata/goroutines/linked.(*pump).Run": 2,
		"finga/test/rules/testdata/goroutines/linked.(timer).arm": 2,
		"finga/test/rules/testdata/goroutines/linked.values":      2,
		"finga/test/rules/testdata/goroutines/linked.init":        1,
	}
	if !maps.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// reviewedGoroutines are the places in the game's binaries, outside pkg/panics and pkg/runner, that start goroutines, by how many; each recovers its panics on that goroutine.
var reviewedGoroutines = map[string]int{
	"finga/internal/host.Main":                                                  1, // AfterFunc: stop and cancel, which cannot panic, before any recorder exists
	"finga/pkg/grpcx.(interceptors).endOnDrain":                                 1, // AfterFunc: the cancel runs under the server's recorder
	"finga/pkg/wsconn.run":                                                      4, // the reader, the writer and the pinger under guard, and the goodbye timer under the recorder
	"finga/services/gateway/internal/application/push.(*Gateway).Run":           1, // errgroup: each pump captures its panic, and the task restarts
	"finga/services/gateway/internal/application/push.(*Gateway).registerRound": 1, // errgroup: each player's call under the recorder
	"finga/services/gateway/internal/application/push.(*Gateway).checkSessions": 1, // errgroup: each player's check under the recorder
	"finga/services/gateway/internal/handler/ws/receive.(*Handler).Open":        1, // AfterFunc: the Hello deadline under the recorder
	"finga/services/social.(*Module).GatewayLeft":                               2, // AfterFunc: each sweep under the recorder
	"finga/services/world/internal/application/itemcalls.(*Pool).Run":           1, // errgroup: each worker captures its panic, and the vital task fails
	"finga/services/world/internal/application/party.(*Copy).Run":               2, // errgroup: both loops capture their panics, and the task restarts
	"finga/services/world/internal/application/world.(*World).Run":              1, // errgroup: each map captures its panic, and the vital task fails
	"finga/services/world/internal/application/world.(*World).Transfer":         1, // WaitGroup: each transfer under the recorder
	"finga/services/world/internal/application/world.(*World).persistence":      1, // WaitGroup: each flusher captures its panic, which Save returns
}

// binaries are the game's, which the rule covers; the tools are left out.
var binaries = []string{"finga/cmd/account", "finga/cmd/character", "finga/cmd/gateway", "finga/cmd/item", "finga/cmd/social", "finga/cmd/world"}

// A new place that starts a goroutine is looked at before it joins reviewedGoroutines: an unrecovered panic there ends the game for everyone.
func TestEveryGoroutineRecoversItsPanics(t *testing.T) {
	t.Parallel() // each of the module-wide loads runs beside the others
	got, err := goroutineSites("../..", binaries, []string{"finga/pkg/panics", "finga/pkg/runner"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range slices.Sorted(maps.Keys(got)) {
		if want, ok := reviewedGoroutines[name]; !ok || got[name] != want {
			t.Errorf("%s starts %d goroutines, %d reviewed: check each recovers its panic on its own goroutine, then update reviewedGoroutines", name, got[name], want)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(reviewedGoroutines)) {
		if _, ok := got[name]; !ok {
			t.Errorf("%s starts no goroutine now: drop it from reviewedGoroutines", name)
		}
	}
}

// tools are the binaries that run beside the game, never in it, so the rule leaves them out.
var tools = []string{"finga/cmd/bot", "finga/cmd/migrate", "finga/cmd/testers"}

// Every binary, wherever its main package is, is one the rule covers or a tool, so a new binary can't slip past it.
func TestEveryBinaryIsCoveredOrATool(t *testing.T) {
	cmd := exec.Command("go", "list", "-f", `{{if eq .Name "main"}}{{.ImportPath}}{{end}}`, "./...")
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	mains := strings.Fields(string(out))
	for _, m := range mains {
		if !slices.Contains(binaries, m) && !slices.Contains(tools, m) {
			t.Errorf("%s is a new binary: add it to binaries, so the rule covers it, or to tools", m)
		}
	}
	for _, b := range slices.Concat(binaries, tools) {
		if !slices.Contains(mains, b) {
			t.Errorf("%s is no binary now: drop it from the lists", b)
		}
	}
}
