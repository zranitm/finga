// Package gamedatatest is the game data the server ships, for tests: loaded once per test binary and shared, read-only.
package gamedatatest

import (
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"finga/internal/gamedata"
	"finga/internal/gamedata/load"
)

// Dir is the folder of the game data the server ships.
func Dir() string {
	_, here, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(here), "..", "..", "..", "data")
}

// shipped loads it once per test binary: under -race a load takes ~0.1 s, and hundreds of tests use it.
var shipped = sync.OnceValues(func() (*gamedata.Data, error) { return load.Dir(Dir()) })

// Shipped is the game data the server ships, shared by every test in the binary: it is read-only.
func Shipped(t testing.TB) *gamedata.Data {
	t.Helper()
	d, err := shipped()
	if err != nil {
		t.Fatal(err)
	}
	return d
}
