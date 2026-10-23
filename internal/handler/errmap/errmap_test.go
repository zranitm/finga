package errmap_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"finga/internal/application/account"
	"finga/internal/application/character"
	"finga/internal/application/chat"
	"finga/internal/application/item"
	"finga/internal/application/party"
	"finga/internal/application/world"
	"finga/internal/handler/errmap"
	"finga/internal/model"
)

var update = flag.Bool("update", false, "rewrite testdata/errors/codes.json")

// codesFile is the list the client checks its own code list against.
var codesFile = filepath.Join("..", "..", "..", "testdata", "errors", "codes.json")

func TestForGivesEachServiceErrorItsOwnCode(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{account.ErrUsernameTaken, http.StatusConflict, "username_taken"},
		{account.ErrUnauthenticated, http.StatusUnauthorized, "unauthenticated"},
		{account.ErrRateLimited, http.StatusTooManyRequests, "login_rate_limited"},
		{account.ErrBadPassword, http.StatusBadRequest, "bad_password"},
		{account.ErrSessionRace, http.StatusConflict, "session_race"},
		{character.ErrNameTaken, http.StatusConflict, "name_taken"},
		{character.ErrSlotsFull, http.StatusConflict, "character_slots_full"},
		{character.ErrNotFound, http.StatusNotFound, "character_not_found"},
		{chat.ErrRate, http.StatusTooManyRequests, "chat_rate_limited"},
		{chat.ErrNoParties, http.StatusServiceUnavailable, "party_chat_unavailable"},
		{item.ErrInventoryFull, http.StatusConflict, "inventory_full"},
		{item.ErrKeyReused, http.StatusConflict, "idempotency_key_reused"},
		{item.ErrDifferentItem, http.StatusConflict, "slot_holds_other_item"},
		{party.ErrFull, http.StatusConflict, "party_full"},
		{party.ErrNotLeader, http.StatusConflict, "not_party_leader"},
		{party.ErrInviteExpired, http.StatusNotFound, "invite_expired"},
		{model.ErrBadID, http.StatusBadRequest, "bad_id"},
		{world.ErrPickupsUnavailable, http.StatusServiceUnavailable, "pickups_unavailable"},
		{world.ErrPickupBusy, http.StatusServiceUnavailable, "pickup_busy"},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			got := errmap.For(tt.err)
			if got.Status != tt.status || got.Code != tt.code || got.Message == "" {
				t.Fatalf("For(%v) = %+v, want status %d, code %s and a message", tt.err, got, tt.status, tt.code)
			}
		})
	}
}

func TestForFindsTheErrorThroughWrappingAndNeverShowsTheCause(t *testing.T) {
	cause := errors.New(`duplicate key value violates unique constraint "characters_name_key"`)
	err := fmt.Errorf("create character: %w", fmt.Errorf("%w: %w", character.ErrNameTaken, cause))
	got := errmap.For(err)
	if got.Code != "name_taken" || got.Status != http.StatusConflict {
		t.Fatalf("For = %+v, want name_taken 409", got)
	}
	if strings.Contains(got.Message, "duplicate") || strings.Contains(got.Message, "characters_name_key") {
		t.Fatalf("the cause leaked into the message: %q", got.Message)
	}
}

func TestForTurnsAnUnknownErrorIntoInternalWithoutItsText(t *testing.T) {
	got := errmap.For(errors.New("dial tcp 10.0.0.5:5432: connection refused"))
	if got != errmap.Internal {
		t.Fatalf("For(unknown) = %+v, want %+v", got, errmap.Internal)
	}
	if got.Status != http.StatusInternalServerError || strings.Contains(got.Message, "10.0.0.5") {
		t.Fatalf("Internal = %+v", got)
	}
	if got := errmap.For(nil); got != (errmap.Public{}) {
		t.Fatalf("For(nil) = %+v, want the zero value", got)
	}
	if errmap.IsInternal(errmap.For(item.ErrBadSlot)) || !errmap.IsInternal(errmap.For(errors.New("boom"))) {
		t.Fatal("IsInternal disagrees with For")
	}
}

// TestEveryServiceErrorIsMapped reads the source of every application module
// and of model: an exported Err variable the table leaves out would reach a
// player as "something went wrong" with code internal.
func TestEveryServiceErrorIsMapped(t *testing.T) {
	declared := exportedErrors(t, filepath.Join("..", "..", "application"), filepath.Join("..", "..", "model"))
	if len(declared) < 40 {
		t.Fatalf("found only %d errors (%v); is the walk broken?", len(declared), declared)
	}
	mapped := referencedErrors(t, "errmap.go")
	for _, name := range declared {
		if !mapped[name] {
			t.Errorf("%s has no row in the table", name)
		}
	}
}

func TestCodesAreUniqueSnakeCaseAndMatchTheGoldenList(t *testing.T) {
	codes := errmap.Codes()
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
		t.Fatalf("%v (run go test ./internal/handler/errmap -update)", err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Fatalf("%s is stale; run go test ./internal/handler/errmap -update", codesFile)
	}
}

// exportedErrors lists "pkg.ErrName" for every exported package-level
// variable whose name starts with Err, outside tests and generated code.
func exportedErrors(t *testing.T, roots ...string) []string {
	t.Helper()
	var out []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.Contains(filepath.ToSlash(path), "/internal/db/") {
				return nil
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			for _, decl := range f.Decls {
				g, ok := decl.(*ast.GenDecl)
				if !ok || g.Tok != token.VAR {
					continue
				}
				for _, spec := range g.Specs {
					for _, name := range spec.(*ast.ValueSpec).Names {
						if strings.HasPrefix(name.Name, "Err") {
							out = append(out, f.Name.Name+"."+name.Name)
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// referencedErrors is the set of "pkg.ErrName" selectors in a source file.
func referencedErrors(t *testing.T, file string) map[string]bool {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if x, ok := sel.X.(*ast.Ident); ok && strings.HasPrefix(sel.Sel.Name, "Err") {
				out[x.Name+"."+sel.Sel.Name] = true
			}
		}
		return true
	})
	return out
}
