package main

import (
	"bytes"
	"flag"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"finga/internal/shared/regiongen"
)

const tinyPlan = "testdata/tiny.yaml"

var update = flag.Bool("update", false, "rewrite the web client's fixture from the tiny bake")

// clientFixture is the tiny bake the web client's tests read, its tiles aside.
const clientFixture = "../../../web/src/testing/regions/tiny"

// A plan's bake lands under <id>/<hash>/, each file as the region bakes it, a tile as its pixels; a stale bake goes and nothing else under out is touched; the preview lands as <id>.png.
func TestABakeIsWrittenUnderItsHash(t *testing.T) {
	out, preview := t.TempDir(), t.TempDir()
	stale := filepath.Join(out, "gone", "0123456789abcdef")
	keep := filepath.Join(out, "notes.txt")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{filepath.Join(stale, "region.json"), keep} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := run("../../data", out, preview, tinyPlan); err != nil {
		t.Fatal(err)
	}
	b := tinyBake(t)
	dir := filepath.Join(out, "tiny", b.Hash[:hashLen])
	for _, f := range b.Files {
		raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
		if err != nil {
			t.Fatal(err)
		}
		if f.Tile == nil {
			if !bytes.Equal(raw, f.Data) {
				t.Errorf("%s differs from the bake's", f.Path)
			}
			continue
		}
		img, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		rgba, ok := img.(*image.RGBA) // an opaque image, which PNG keeps as RGB
		if !ok || rgba.Bounds() != f.Tile.Bounds() || !bytes.Equal(rgba.Pix, f.Tile.Pix) {
			t.Errorf("%s isn't the bake's tile", f.Path)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "gone")); !os.IsNotExist(err) {
		t.Errorf("the stale bake is still there: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("a file that isn't a bake went: %v", err)
	}
	if _, err := os.Stat(filepath.Join(preview, "tiny.png")); err != nil {
		t.Errorf("no preview: %v", err)
	}
}

// Without somewhere to write, or with bad data or a bad plan, it writes nothing.
func TestNothingIsWrittenWithoutAPlaceOrWithBadData(t *testing.T) {
	if err := run("../../data", "", "", tinyPlan); err == nil {
		t.Error("no error without -out or -preview")
	}
	out := filepath.Join(t.TempDir(), "regions")
	if err := run(t.TempDir(), out, "", tinyPlan); err == nil {
		t.Error("no error from an empty data folder")
	}
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(bad, []byte("name: bad\nhills: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run("../../data", out, "", bad); err == nil {
		t.Error("no error from a bad plan")
	}
	if _, err := os.Stat(filepath.Join(out, "bad")); !os.IsNotExist(err) {
		t.Errorf("wrote a bad plan's bake: %v", err)
	}
}

func tinyBake(t *testing.T) regiongen.Bake {
	t.Helper()
	raw, err := os.ReadFile(tinyPlan)
	if err != nil {
		t.Fatal(err)
	}
	p, err := regiongen.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	kraw, err := os.ReadFile("../../data/kit.yaml")
	if err != nil {
		t.Fatal(err)
	}
	k, err := regiongen.DecodeKit(kraw)
	if err != nil {
		t.Fatal(err)
	}
	r, err := regiongen.Generate(p, k, 0.7)
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Bake()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A bake is written beside its folder and renamed into it whole, so a crash leaves no folder that looks complete; a half-written one goes at the next run.
func TestABakeIsRenamedIntoPlaceWhole(t *testing.T) {
	out := t.TempDir()
	half := filepath.Join(out, "tiny", "0123456789abcdef"+partSuffix)
	if err := os.MkdirAll(filepath.Join(half, "chunks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := run("../../data", out, "", tinyPlan); err != nil {
		t.Fatal(err)
	}
	left, err := filepath.Glob(filepath.Join(out, "*", "*"+partSuffix))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("half-written bakes left: %v", left)
	}
	if _, err := os.Stat(filepath.Join(out, "tiny", tinyBake(t).Hash[:hashLen], "region.json")); err != nil {
		t.Errorf("the bake isn't in place: %v", err)
	}
}

// The web client's tests read the bake Go writes, so its reader is checked against these bytes and not a copy of the format: run with -update after a change to the format or the generator.
func TestTheClientsFixtureIsTheTinyBake(t *testing.T) {
	b := tinyBake(t)
	var want []string
	for _, f := range b.Files {
		if f.Tile != nil {
			continue
		}
		want = append(want, f.Path)
		at := filepath.Join(clientFixture, filepath.FromSlash(f.Path))
		if *update {
			if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(at, f.Data, 0o644); err != nil { //nolint:gosec // a fixture in the repo
				t.Fatal(err)
			}
		}
		raw, err := os.ReadFile(at)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(raw, f.Data) {
			t.Errorf("%s differs from the tiny bake's (go test ./cmd/regionbake -run TestTheClientsFixtureIsTheTinyBake -update)", f.Path)
		}
	}
	var got []string
	err := filepath.WalkDir(clientFixture, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(clientFixture, path)
			got = append(got, filepath.ToSlash(rel))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("the fixture holds %v; the bake %v", got, want)
	}
}
