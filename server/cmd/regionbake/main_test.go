package main

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"finga/internal/shared/regiongen"
)

const tinyPlan = "testdata/tiny.yaml"

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
