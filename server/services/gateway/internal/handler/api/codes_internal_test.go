package api

import (
	"bytes"
	"cmp"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/errors/codes.json")

// codesFile is the list the client checks its own code list against.
var codesFile = filepath.Join("..", "..", "..", "..", "..", "testdata", "errors", "codes.json")

// codeRow is one failure as codes.json writes it.
type codeRow struct {
	Code    string `json:"code"`
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// everyCode is every failure a client can receive, sorted by code.
func everyCode() []codeRow {
	out := make([]codeRow, 0, len(statuses))
	for p, status := range statuses {
		out = append(out, codeRow{Code: p.Code, Status: status, Message: p.Message})
	}
	slices.SortFunc(out, func(a, b codeRow) int { return cmp.Compare(a.Code, b.Code) })
	return out
}

func TestCodesAreUniqueSnakeCaseAndMatchTheGoldenList(t *testing.T) {
	codes := everyCode()
	snake := regexp.MustCompile(`^[a-z]+(_[a-z]+)*$`)
	seen := map[string]bool{}
	for _, p := range codes {
		if !snake.MatchString(p.Code) {
			t.Errorf("code %q is not snake_case", p.Code)
		}
		if seen[p.Code] {
			t.Errorf("code %q is used twice; every failure gets its own", p.Code)
		}
		seen[p.Code] = true
		if p.Status < 400 || p.Status > 599 || p.Message == "" {
			t.Errorf("%+v needs an error status and a message", p)
		}
	}
	got, err := json.MarshalIndent(codes, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if *update {
		if err := os.MkdirAll(filepath.Dir(codesFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(codesFile, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(codesFile)
	if err != nil {
		t.Fatalf("%v (run go test ./services/gateway/internal/handler/api -update)", err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Fatalf("%s is stale; run go test ./services/gateway/internal/handler/api -update", codesFile)
	}
}

// A failure the model declares without a status would answer 500 and be missing from codes.json.
func TestEveryFailureHasAStatus(t *testing.T) {
	codes := map[string]bool{}
	for p := range statuses {
		codes[p.Code] = true
	}
	declared := declaredFailures(t)
	for _, code := range declared {
		if !codes[code] {
			t.Errorf("%s has no status", code)
		}
	}
	if len(statuses) != len(declared) {
		t.Errorf("%d statuses for %d failures: a status names a failure the model does not declare", len(statuses), len(declared))
	}
}

var declaration = regexp.MustCompile(`= Public\{"([a-z_]+)"`)

// declaredFailures are the codes of the Publics model/failures.go declares.
func declaredFailures(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "model", "failures.go"))
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, m := range declaration.FindAllSubmatch(src, -1) {
		codes = append(codes, string(m[1]))
	}
	if len(codes) == 0 {
		t.Fatal("found no Public in failures.go: is the pattern broken?")
	}
	return codes
}
