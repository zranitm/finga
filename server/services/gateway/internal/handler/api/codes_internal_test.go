package api

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/errors/codes.json")

// codesFile is the catalog the client words its codes from.
var codesFile = filepath.Join("..", "..", "..", "..", "..", "testdata", "errors", "codes.json")

// codeRow is one failure as codes.json writes it.
type codeRow struct {
	Code    int    `json:"code"`
	Name    string `json:"name"`
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// everyCode is every failure a client can receive, sorted by code.
func everyCode() []codeRow {
	out := make([]codeRow, 0, len(statuses))
	for p, status := range statuses {
		out = append(out, codeRow{Code: p.Code, Name: p.Name, Status: status, Message: p.Message})
	}
	slices.SortFunc(out, func(a, b codeRow) int { return cmp.Compare(a.Code, b.Code) })
	return out
}

func TestCodesAreUniqueAndNamedAndMatchTheGoldenList(t *testing.T) {
	codes := everyCode()
	upper := regexp.MustCompile(`^[A-Z]+(_[A-Z]+)*$`)
	seen, names := map[int]bool{}, map[string]bool{}
	for _, p := range codes {
		if !upper.MatchString(p.Name) {
			t.Errorf("%d's name %q is not UPPER_SNAKE_CASE", p.Code, p.Name)
		}
		if seen[p.Code] || names[p.Name] {
			t.Errorf("%d %s is used twice; every failure gets its own code and name", p.Code, p.Name)
		}
		seen[p.Code], names[p.Name] = true, true
		if p.Status < 400 || p.Status > 599 || p.Message == "" {
			t.Errorf("%+v needs an error status and a message", p)
		}
	}
	got, err := json.MarshalIndent(codes, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	committed, err := os.ReadFile(codesFile)
	if err != nil && !*update {
		t.Fatalf("%v (run go test ./services/gateway/internal/handler/api -update)", err)
	}
	var before []codeRow
	if err := json.Unmarshal(committed, &before); err != nil && !*update {
		t.Fatalf("%s: %v", codesFile, err)
	}
	now := map[int]string{}
	for _, p := range codes {
		now[p.Code] = p.Name
	}
	for _, p := range before {
		if now[p.Code] != p.Name {
			t.Errorf("%d was %s: a code is never dropped or reused; keep it declared in failures.go, marked retired", p.Code, p.Name)
		}
	}
	if *update {
		if err := os.MkdirAll(filepath.Dir(codesFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(codesFile, got, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	if !bytes.Equal(bytes.ReplaceAll(committed, []byte("\r\n"), []byte("\n")), got) {
		t.Fatalf("%s is stale; run go test ./services/gateway/internal/handler/api -update", codesFile)
	}
}

// A failure the model declares without a status would answer 500 and be missing from codes.json.
func TestEveryFailureHasAStatus(t *testing.T) {
	codes := map[int]bool{}
	for p := range statuses {
		codes[p.Code] = true
	}
	declared := declaredFailures(t)
	for _, d := range declared {
		if !codes[d.code] {
			t.Errorf("%d %s has no status", d.code, d.name)
		}
	}
	if len(statuses) != len(declared) {
		t.Errorf("%d statuses for %d failures: a status names a failure the model does not declare", len(statuses), len(declared))
	}
}

// Each code sits in its area's block in failures.go, and an area's codes count up from 0001 with no gap.
func TestEachCodeIsNumberedInItsArea(t *testing.T) {
	last := map[int]int{}
	for _, d := range declaredFailures(t) {
		if d.code/10000 != d.area {
			t.Errorf("%d %s is declared under area %d", d.code, d.name, d.area)
		}
		if d.code%10000 != last[d.area]+1 {
			t.Errorf("%d %s follows %d%04d: an area's codes count up from 0001", d.code, d.name, d.area, last[d.area])
		}
		last[d.area] = d.code % 10000
	}
}

var (
	areaHeader  = regexp.MustCompile(`^// (\d\d): `)
	declaration = regexp.MustCompile(`= Public\{(\d{6}), "([A-Z_]+)"`)
)

type declared struct {
	area, code int
	name       string
}

// declaredFailures are the Publics model/failures.go declares, each with the area of the block it sits in.
func declaredFailures(t *testing.T) []declared {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "model", "failures.go"))
	if err != nil {
		t.Fatal(err)
	}
	var out []declared
	area := 0
	lines := bufio.NewScanner(bytes.NewReader(src))
	for lines.Scan() {
		if m := areaHeader.FindStringSubmatch(lines.Text()); m != nil {
			area, _ = strconv.Atoi(m[1])
			continue
		}
		if m := declaration.FindStringSubmatch(lines.Text()); m != nil {
			code, _ := strconv.Atoi(m[1])
			out = append(out, declared{area: area, code: code, name: m[2]})
		}
	}
	if len(out) == 0 {
		t.Fatal("found no Public in failures.go: is the pattern broken?")
	}
	return out
}
