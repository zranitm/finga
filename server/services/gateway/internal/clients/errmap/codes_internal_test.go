package errmap

import (
	"bytes"
	"cmp"
	"encoding/json"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"testing"

	"finga/services/gateway/internal/model"
)

var update = flag.Bool("update", false, "rewrite testdata/errors/codes.json")

// codesFile is the list the client checks its own code list against.
var codesFile = filepath.Join("..", "..", "..", "..", "..", "testdata", "errors", "codes.json")

// raised are the failures the gateway raises itself; TestEveryRaisedFailureIsListed keeps it complete.
var raised = []model.Public{
	model.MalformedBody, model.MissingField, model.BodyTooLarge, model.NoSuchEndpoint, model.MethodNotAllowed, model.RateLimited,
	model.NotImplemented, model.ChatUnavailable, model.PartiesUnavailable, model.InventoryUnavailable, model.Unauthenticated,
	model.BadID, model.UnknownClass, model.Internal,
}

// codeRow is one failure as codes.json writes it.
type codeRow struct {
	Code    string `json:"code"`
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// everyCode is every failure a client can receive, sorted by code.
func everyCode() []codeRow {
	out := make([]codeRow, 0, len(reasons)+len(raised))
	for _, p := range slices.Concat(slices.Collect(maps.Values(reasons)), raised) {
		out = append(out, codeRow(p))
	}
	slices.SortFunc(out, func(a, b codeRow) int { return cmp.Compare(a.Code, b.Code) })
	// bad_id, unknown_class and unauthenticated each have more than one row.
	return slices.Compact(out)
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
		t.Fatalf("%v (run go test ./services/gateway/internal/clients/errmap -update)", err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Fatalf("%s is stale; run go test ./services/gateway/internal/clients/errmap -update", codesFile)
	}
}

// A Public the gateway declares but raised leaves out would be missing from codes.json.
func TestEveryRaisedFailureIsListed(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "..", "model", "public.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var declared []string
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || len(lit.Elts) == 0 {
			return true
		}
		if id, ok := lit.Type.(*ast.Ident); !ok || id.Name != "Public" {
			return true
		}
		if code, ok := lit.Elts[0].(*ast.BasicLit); ok {
			name, err := strconv.Unquote(code.Value)
			if err != nil {
				t.Fatal(err)
			}
			declared = append(declared, name)
		}
		return true
	})
	if len(declared) == 0 {
		t.Fatal("found no Public in public.go: is the parse broken?")
	}
	for _, code := range declared {
		if !slices.ContainsFunc(raised, func(p model.Public) bool { return p.Code == code }) {
			t.Errorf("%s is declared in model but not in raised", code)
		}
	}
}
