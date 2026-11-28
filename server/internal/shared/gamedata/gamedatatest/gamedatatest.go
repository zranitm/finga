// Package gamedatatest is the game data the server ships, for tests: loaded once per test binary and shared, read-only.
package gamedatatest

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/load"
)

// Dir is the folder of the game data the server ships.
func Dir() string {
	_, here, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(here), "..", "..", "..", "..", "data")
}

// full loads it once per test binary: with the real regions a load takes seconds under -race.
var full = sync.OnceValues(func() (*gamedata.Data, error) { return load.Dir(Dir()) })

// small loads the shipped data with its three big regions swapped for StandIns.
var small = sync.OnceValues(func() (*gamedata.Data, error) {
	dst, err := os.MkdirTemp("", "gamedatatest")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dst)
	if err := standInsIn(dst); err != nil {
		return nil, err
	}
	return load.Dir(dst)
})

func standInsIn(dst string) error {
	if err := os.CopyFS(dst, os.DirFS(Dir())); err != nil {
		return err
	}
	for name, text := range StandIns {
		if err := os.WriteFile(filepath.Join(dst, name), []byte(text), 0o600); err != nil {
			return err
		}
	}
	return AddLandPlaces(dst)
}

// WithLandPlaces is world.yaml's text with the three land maps of StandIns charted beside the shipped places.
func WithLandPlaces(text string) string {
	return strings.Replace(text, "places:\n", "places:\n  crossroads: [50, 50]\n  frost_trail: [64, 38]\n  icefall: [74, 28]\n", 1)
}

// AddLandPlaces charts the three land maps of StandIns in the world.yaml of the data folder dst.
func AddLandPlaces(dst string) error {
	world := filepath.Join(dst, "world.yaml")
	text, err := os.ReadFile(world)
	if err != nil {
		return err
	}
	return os.WriteFile(world, []byte(WithLandPlaces(string(text))), 0o600)
}

// ShippedDir is a copy of the shipped data folder with the regions swapped for StandIns, for tests that load a folder themselves; it goes with the test.
func ShippedDir(t testing.TB) string {
	t.Helper()
	dst := t.TempDir()
	if err := standInsIn(dst); err != nil {
		t.Fatal(err)
	}
	return dst
}

// Full is the game data the server ships, shared by every test in the binary: it is read-only. Only tests of the shipped regions themselves need it.
func Full(t testing.TB) *gamedata.Data {
	t.Helper()
	d, err := full()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Shipped is the shipped game data with Willowmere, Greyfang Cave and the Sunken Shrine small stand-ins of the same ids, gates, start and waypoints, shared by every test in the binary: it is read-only.
func Shipped(t testing.TB) *gamedata.Data {
	t.Helper()
	d, err := small()
	if err != nil {
		t.Fatal(err)
	}
	return d
}
