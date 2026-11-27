// The import rules, checked through every dependency; depguard sees only direct imports.

package rules_test

import (
	"maps"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// rule keeps the packages from matches, but not except, from depending on any package under deny.
type rule struct {
	name         string
	from, except *regexp.Regexp
	deny         []string
}

var rules = []rule{
	{
		name: "the shared kernel is below every module and knows no transport or database",
		from: regexp.MustCompile(`^finga/internal/shared/(kernel|gamedata|sim|progression)(/|$)`),
		deny: []string{
			"finga/internal/application", "finga/internal/model", "finga/internal/repository", "finga/internal/handler", "finga/internal/metrics", "finga/internal/adapter", "finga/internal/dependencies", "finga/internal/migration",
			"finga/api/gen", "google.golang.org/grpc", "net/http", "github.com/jackc/pgx", "finga/pkg/dbx",
		},
	},
	{
		name:   "a module's use cases and model know no transport, contract or database; its other layers do: the gateway's handler, the adapters (other modules' facades bring the host), the repository, and metrics, whose Prometheus client brings net/http",
		from:   regexp.MustCompile(`^finga/internal/(application/gateway|application/[^/]+/[^/]+|model/[^/]+)(/|$)`),
		except: regexp.MustCompile(`^finga/internal/application/world/worldtest$`), // test support, which builds Worlds with tracing, outside the rule before the move too
		deny:   []string{"finga/api/gen", "google.golang.org/grpc", "net/http", "github.com/jackc/pgx", "finga/pkg/dbx"},
	},
}

// layered is a package in a module's layer, the module named after it; a name with no application package is no module. The handlers are the top layer: they call the modules' use cases and each other as handlers.go's rule allows, so the module graph leaves them out.
var layered = regexp.MustCompile(`^finga/internal/(application|model|repository|metrics|adapter)/([^/]+)(/|$)`)

// moduleOf is the module p belongs to, or "" for none.
func moduleOf(pkgs map[string]pkg, p string) string {
	m := layered.FindStringSubmatch(p)
	if m == nil {
		return ""
	}
	if _, ok := pkgs["finga/internal/application/"+m[2]]; !ok {
		return ""
	}
	return m[2]
}

// pkg is one package of the module as go list reports it.
type pkg struct {
	path    string
	imports []string
	deps    map[string]bool
}

func TestNoLayerReachesWhatItMustNotThroughAnotherPackage(t *testing.T) {
	pkgs := list(t)
	for _, p := range pkgs {
		for _, r := range rules {
			if !r.from.MatchString(p.path) || r.except != nil && r.except.MatchString(p.path) {
				continue
			}
			for _, d := range r.deny {
				if dep, ok := reaches(p, d); ok {
					t.Errorf("%s depends on %s through %s: %s", p.path, dep, through(pkgs, p, dep), r.name)
				}
			}
		}
	}
}

// A module imports another only at its root, the facade, and no modules call each other round a cycle.
func TestModulesMeetAtFacades(t *testing.T) {
	calls := map[string]map[string]bool{} // each module's, the modules it imports
	pkgs := list(t)
	for _, p := range pkgs {
		own := moduleOf(pkgs, p.path)
		if own == "" {
			continue
		}
		for _, imp := range p.imports {
			other := moduleOf(pkgs, imp)
			if other == "" || other == own {
				continue
			}
			if facade := "finga/internal/application/" + other; imp != facade {
				t.Errorf("%s imports %s: a module meets another at its facade, %s", p.path, imp, facade)
			}
			if calls[own] == nil {
				calls[own] = map[string]bool{}
			}
			calls[own][other] = true
		}
	}
	if len(calls["gateway"]) == 0 {
		t.Fatal("the gateway imports no module: is the check broken?")
	}
	if c := cycleIn(calls); c != nil {
		t.Errorf("the modules call each other round a cycle: %s", strings.Join(c, " → "))
	}
}

// cycleIn is a cycle in the graph, from a node back to itself, or nil for none.
func cycleIn(graph map[string]map[string]bool) []string {
	done, on := map[string]bool{}, map[string]bool{}
	var path []string
	var visit func(n string) []string
	visit = func(n string) []string {
		if on[n] {
			return append(slices.Clone(path[slices.Index(path, n):]), n)
		}
		if done[n] {
			return nil
		}
		on[n], path = true, append(path, n)
		for _, next := range slices.Sorted(maps.Keys(graph[n])) {
			if c := visit(next); c != nil {
				return c
			}
		}
		on[n], path, done[n] = false, path[:len(path)-1], true
		return nil
	}
	for _, n := range slices.Sorted(maps.Keys(graph)) {
		if c := visit(n); c != nil {
			return c
		}
	}
	return nil
}

func TestTheCycleCheckFindsACycle(t *testing.T) {
	graph := map[string]map[string]bool{"world": {"item": true, "social": true}, "social": {"gateway": true}, "gateway": {"world": true}, "item": {}}
	if got, want := cycleIn(graph), []string{"gateway", "world", "social", "gateway"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	delete(graph["gateway"], "world")
	if got := cycleIn(graph); got != nil {
		t.Fatalf("a cycle %v in a graph without one", got)
	}
}

// reaches is the first of p's dependencies at or under prefix, if any.
func reaches(p pkg, prefix string) (string, bool) {
	deps := slices.Sorted(maps.Keys(p.deps))
	i := slices.IndexFunc(deps, func(dep string) bool { return dep == prefix || strings.HasPrefix(dep, prefix+"/") })
	if i < 0 {
		return "", false
	}
	return deps[i], true
}

// through is the direct import of p that dep comes in by.
func through(pkgs map[string]pkg, p pkg, dep string) string {
	for _, i := range p.imports {
		if i == dep || pkgs[i].deps[dep] {
			return i
		}
	}
	return "?"
}

// list is every package of the module with its imports and all its dependencies, tests left out.
func list(t *testing.T) map[string]pkg {
	t.Helper()
	cmd := exec.Command("go", "list", "-f", `{{.ImportPath}}|{{join .Imports " "}}|{{join .Deps " "}}`, "./...")
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	pkgs := map[string]pkg{}
	for line := range strings.Lines(string(out)) {
		parts := strings.Split(strings.TrimSpace(line), "|")
		p := pkg{path: parts[0], imports: strings.Fields(parts[1]), deps: map[string]bool{}}
		for d := range strings.FieldsSeq(parts[2]) {
			p.deps[d] = true
		}
		pkgs[p.path] = p
	}
	if len(pkgs) == 0 {
		t.Fatal("go list found no packages")
	}
	return pkgs
}
