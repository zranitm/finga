package errmap

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

var declaration = regexp.MustCompile(`= Public\{"([a-z_]+)"`)

// A row naming a failure the model does not declare would get no HTTP status and miss codes.json.
func TestEveryRowIsAFailureTheModelDeclares(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "model", "failures.go"))
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, m := range declaration.FindAllSubmatch(src, -1) {
		declared[string(m[1])] = true
	}
	if len(declared) == 0 {
		t.Fatal("found no Public in failures.go: is the pattern broken?")
	}
	for r, p := range reasons {
		if !declared[p.Code] {
			t.Errorf("%s maps to %s, which model/failures.go does not declare", r, p.Code)
		}
	}
}
