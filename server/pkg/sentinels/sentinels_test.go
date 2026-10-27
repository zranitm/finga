package sentinels_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"finga/pkg/sentinels"
)

func write(t *testing.T, dir, name, src string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDeclared(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "shop/api.go", `package shop
import "errors"
var (
	ErrFull = errors.New("full")
	ErrGone = errors.New("gone")
	errQuiet = errors.New("unexported")
	Other = 1
)
`)
	write(t, dir, "shop/more.go", "package shop\nimport \"errors\"\nvar ErrLate = errors.New(\"late\")\n")
	write(t, dir, "shop/shop_test.go", "package shop\nimport \"errors\"\nvar ErrTestOnly = errors.New(\"test\")\n")
	write(t, dir, "shop/internal/db/models.go", "package db\nimport \"errors\"\nvar ErrGenerated = errors.New(\"generated\")\n")
	write(t, dir, "till/till.go", "package till\nimport \"errors\"\nvar ErrClosed = errors.New(\"closed\")\n")
	got, err := sentinels.Declared(filepath.Join(dir, "shop"), filepath.Join(dir, "till"))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	want := []string{"shop.ErrFull", "shop.ErrGone", "shop.ErrLate", "till.ErrClosed"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if _, err := sentinels.Declared(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("a missing directory is not an error")
	}
}

func TestReferenced(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "table.go", `package table
var rows = []any{shop.ErrFull, till.ErrClosed, shop.Other}
func f() error { return shop.ErrGone }
`)
	got, err := sentinels.Referenced(filepath.Join(dir, "table.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"shop.ErrFull", "till.ErrClosed", "shop.ErrGone"} {
		if !got[name] {
			t.Fatalf("%s not found in %v", name, got)
		}
	}
	if got["shop.Other"] {
		t.Fatal("a non-error selector was listed")
	}
}
