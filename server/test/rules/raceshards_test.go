// The race run's two shards, heavy and rest, are every package the Go tests run, once each: a package in neither would never run under -race, and one in both would run twice.

package rules_test

import (
	"fmt"
	"os/exec"
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

// The check finds a package in no shard, one in two, and one a shard names that isn't a package, while a package in one shard passes.
func TestTheCheckFindsEveryPackageThatTheRaceShardsMiss(t *testing.T) {
	got := shardGaps("heavy a\nheavy b\nrest b\nrest c\nrest z\n", []string{"a", "b", "c", "d"})
	want := []string{
		"b: runs in the heavy shard and the rest",
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
		t.Fatalf("the race shards miss or repeat packages; GO_RACE_HEAVY in the Makefile names the heavy ones, and every other package is in rest:\n%s", strings.Join(gaps, "\n"))
	}
}
