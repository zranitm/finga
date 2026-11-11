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
		name: "the shared kernel is below every service and knows no transport or database",
		from: regexp.MustCompile(`^finga/internal/(kernel|gamedata|sim|progression)(/|$)`),
		deny: []string{"finga/services", "finga/api/gen", "google.golang.org/grpc", "net/http", "github.com/jackc/pgx", "finga/pkg/dbx"},
	},
	{
		name:   "a service's modules know no transport, contract or database; its handler, clients and repository do",
		from:   regexp.MustCompile(`^finga/services/[^/]+/internal/`),
		except: regexp.MustCompile(`^finga/services/[^/]+/internal/(handler|clients|repository|db)(/|$)`),
		deny:   []string{"finga/api/gen", "google.golang.org/grpc", "net/http", "github.com/jackc/pgx", "finga/pkg/dbx"},
	},
}

var service = regexp.MustCompile(`^finga/services/([^/]+)/`)

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

func TestNoServiceReachesAnother(t *testing.T) {
	pkgs := list(t)
	for _, p := range pkgs {
		own := service.FindStringSubmatch(p.path)
		if own == nil {
			continue
		}
		for dep := range p.deps {
			if other := service.FindStringSubmatch(dep); other != nil && other[1] != own[1] {
				t.Errorf("%s depends on %s through %s", p.path, dep, through(pkgs, p, dep))
			}
		}
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
