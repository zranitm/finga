package errmap

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

var declaration = regexp.MustCompile(`= Public\{(\d{6}),`)

// A row naming a failure the model does not declare would get no HTTP status and miss codes.json.
func TestEveryRowIsAFailureTheModelDeclares(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "model", "failures.go"))
	if err != nil {
		t.Fatal(err)
	}
	declared := map[int]bool{}
	for _, m := range declaration.FindAllSubmatch(src, -1) {
		code, _ := strconv.Atoi(string(m[1]))
		declared[code] = true
	}
	if len(declared) == 0 {
		t.Fatal("found no Public in failures.go: is the pattern broken?")
	}
	for r, p := range reasons {
		if !declared[p.Code] {
			t.Errorf("%s maps to %d %s, which model/failures.go does not declare", r, p.Code, p.Name)
		}
	}
}
