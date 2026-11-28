// The race run's two shards, bot and rest, and the packages it skips are every package the Go tests run, once each: a package in neither would never run under -race, and one in both would run twice. A skipped package starts no goroutine, so the detector has nothing to find there.

package rules_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// shardGaps lists each package of all that no shard runs, each that two shards run, and each a shard runs that isn't in all. shards is `make race-shards`'s lines: a shard's name, a space, and a package.
func shardGaps(shards string, all []string) []string {
	ran := map[string]string{}
	var out []string
	for line := range strings.Lines(shards) {
		shard, pkg, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		if prev, dup := ran[pkg]; dup {
			out = append(out, fmt.Sprintf("%s: runs in the %s shard and the %s", pkg, prev, shard))
		}
		ran[pkg] = shard
		if !slices.Contains(all, pkg) {
			out = append(out, pkg+": runs in the "+shard+" shard, but is no package the tests run")
		}
	}
	for _, pkg := range all {
		if _, ok := ran[pkg]; !ok {
			out = append(out, pkg+": runs in no shard")
		}
	}
	slices.Sort(out)
	return out
}

// The check finds a package in neither the run nor the skipped, one in both, and one a list names that isn't a package, while a package in one shard passes.
func TestTheCheckFindsEveryPackageThatTheRaceShardsMiss(t *testing.T) {
	got := shardGaps("skipped a\nskipped b\nbot b\nrest c\nrest z\n", []string{"a", "b", "c", "d"})
	want := []string{
		"b: runs in the skipped shard and the bot",
		"d: runs in no shard",
		"z: runs in the rest shard, but is no package the tests run",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Every package the Go tests run is in exactly one race shard.
func TestTheRaceShardsAreEveryPackageOnce(t *testing.T) {
	t.Parallel()
	shards, err := exec.Command("make", "-s", "-C", "../../..", "race-shards").Output()
	if err != nil {
		t.Fatal(err)
	}
	list := exec.Command("go", "list", "./test/...", "./...")
	list.Dir = "../.."
	listed, err := list.Output()
	if err != nil {
		t.Fatal(err)
	}
	var all []string
	for pkg := range strings.FieldsSeq(string(listed)) {
		if pkg != "finga/test/rules" { // the Makefile's GO_PKGS leaves out these, which run plain as rules-go
			all = append(all, pkg)
		}
	}
	if gaps := shardGaps(string(shards), all); len(gaps) > 0 {
		t.Fatalf("the race shards miss or repeat packages; GO_RACE_SKIP in the Makefile names those the run skips, GO_RACE_BOT the bots', and every other package is in rest:\n%s", strings.Join(gaps, "\n"))
	}
}

// goroutineStarts lists each place the Go files in dir start a goroutine, or run a test beside the others: the go keyword, and the calls Go, TryGo, AfterFunc and Parallel.
func goroutineStarts(dir string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	var out []string
	fset := token.NewFileSet()
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			return nil, err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.GoStmt:
				out = append(out, fset.Position(n.Pos()).String())
			case *ast.CallExpr:
				if sel, ok := n.Fun.(*ast.SelectorExpr); ok && slices.Contains([]string{"Go", "TryGo", "AfterFunc", "Parallel"}, sel.Sel.Name) {
					out = append(out, fset.Position(n.Pos()).String()+": "+sel.Sel.Name)
				}
			}
			return true
		})
	}
	return out, nil
}

// The check finds a go statement and each call that starts a goroutine, in a test file as in any other, and nothing in a file that starts none.
func TestTheCheckFindsEveryWayToStartAGoroutine(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("quiet.go", "package p\n\nfunc f() { g() }\nfunc g() {}\n")
	if got, err := goroutineStarts(dir); err != nil || len(got) != 0 {
		t.Fatalf("a package with none: got %q, %v", got, err)
	}
	write("go_test.go", "package p\n\nfunc f() { go g() }\nfunc g() {}\n")
	write("calls_test.go", "package p\n\nfunc h(t T, w W, e E) { t.Parallel(); w.Go(nil); e.TryGo(nil); time.AfterFunc(0, nil) }\n")
	got, err := goroutineStarts(dir)
	if err != nil || len(got) != 5 {
		t.Fatalf("got %d starts, want 5: %q, %v", len(got), got, err)
	}
}

// A package the race run skips starts no goroutine and runs no test beside another, so the detector would find nothing in it; one that starts any goes back in the run.
func TestTheRaceRunSkipsOnlyPackagesThatStartNoGoroutine(t *testing.T) {
	t.Parallel()
	shards, err := exec.Command("make", "-s", "-C", "../../..", "race-shards").Output()
	if err != nil {
		t.Fatal(err)
	}
	skipped := 0
	for line := range strings.Lines(string(shards)) {
		shard, pkg, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || shard != "skipped" {
			continue
		}
		skipped++
		starts, err := goroutineStarts(filepath.Join("../..", strings.TrimPrefix(pkg, "finga/")))
		if err != nil {
			t.Fatal(err)
		}
		if len(starts) > 0 {
			t.Errorf("%s starts goroutines, so take it out of GO_RACE_SKIP in the Makefile:\n%s", pkg, strings.Join(starts, "\n"))
		}
	}
	if skipped == 0 {
		t.Fatal("the race run skips no package: make race-shards names none")
	}
}
