// Every race-only test runs somewhere: the coverage run skips each name in RACE_ONLY_TESTS in every package, and the race-only step runs those names only in RACE_ONLY_PKGS.

package rules_test

import (
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// raceOnly is the Makefile's race-only test names and package directories, the directories relative to the server and cleaned.
func raceOnly(makefile string) (tests, pkgs []string, err error) {
	for line := range strings.Lines(makefile) {
		if v, ok := strings.CutPrefix(line, "RACE_ONLY_TESTS :="); ok {
			tests = strings.Split(strings.TrimSpace(v), "|")
		}
		if v, ok := strings.CutPrefix(line, "RACE_ONLY_PKGS :="); ok {
			for p := range strings.FieldsSeq(v) {
				pkgs = append(pkgs, filepath.Clean(p))
			}
		}
	}
	if len(tests) == 0 || len(pkgs) == 0 {
		return nil, nil, fmt.Errorf("the Makefile sets no RACE_ONLY_TESTS or RACE_ONLY_PKGS")
	}
	return tests, pkgs, nil
}

// unrun lists each race-only name that runs nowhere: one no package defines, and each package defining it that the race-only step doesn't visit. defined is each test function's package directories by name.
func unrun(tests, pkgs []string, defined map[string][]string) []string {
	var out []string
	for _, name := range tests {
		dirs := defined[name]
		if len(dirs) == 0 {
			out = append(out, name+": no package defines it")
		}
		for _, dir := range dirs {
			if !slices.Contains(pkgs, dir) {
				out = append(out, fmt.Sprintf("%s: %s defines it, but RACE_ONLY_PKGS doesn't list it", name, dir))
			}
		}
	}
	slices.Sort(out)
	return out
}

// The check finds a name no package defines, and a package defining a name that the race-only step doesn't visit, while one listed passes.
func TestTheCheckFindsEveryRaceOnlyTestThatRunsNowhere(t *testing.T) {
	defined := map[string][]string{
		"TestListed":     {"internal/a"},
		"TestUnlisted":   {"internal/b"},
		"TestInBoth":     {"internal/a", "internal/c"},
		"TestNotRaceOne": {"internal/d"},
	}
	got := unrun([]string{"TestListed", "TestUnlisted", "TestInBoth", "TestGone"}, []string{"internal/a"}, defined)
	want := []string{
		"TestGone: no package defines it",
		"TestInBoth: internal/c defines it, but RACE_ONLY_PKGS doesn't list it",
		"TestUnlisted: internal/b defines it, but RACE_ONLY_PKGS doesn't list it",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A race-only test left out of both runs would pass CI unseen.
func TestEveryRaceOnlyTestRunsInTheRaceOnlyStep(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	tests, pkgs, err := raceOnly(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	all, err := wholeModule()
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	defined := map[string][]string{}
	for _, p := range all {
		for _, f := range p.Syntax {
			file := p.Fset.File(f.Pos()).Name()
			if !strings.HasSuffix(file, "_test.go") {
				continue
			}
			dir, err := filepath.Rel(root, filepath.Dir(file))
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range f.Decls {
				if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") && !slices.Contains(defined[fn.Name.Name], dir) {
					defined[fn.Name.Name] = append(defined[fn.Name.Name], dir)
				}
			}
		}
	}
	if gaps := unrun(tests, pkgs, defined); len(gaps) > 0 {
		t.Fatalf("race-only tests that run nowhere; list each one's package in RACE_ONLY_PKGS, or drop the name:\n%s", strings.Join(gaps, "\n"))
	}
}
