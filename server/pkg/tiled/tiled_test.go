package tiled_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"finga/pkg/tiled"
)

// minimal is a map in the shape Tiled 1.11 exports: one tile layer, one
// object group with a property on an object, and a property on the map.
const minimal = `{
  "type": "map", "version": "1.10", "tiledversion": "1.11.0", "orientation": "orthogonal",
  "width": 40, "height": 20, "tilewidth": 32, "tileheight": 32, "infinite": false,
  "properties": [{"name": "kind", "type": "string", "value": "town"}, {"name": "level", "type": "int", "value": 3}],
  "layers": [
    {"id": 1, "name": "ground", "type": "tilelayer", "width": 40, "height": 20, "data": []},
    {"id": 2, "name": "doors", "type": "objectgroup", "objects": [
      {"id": 7, "name": "front", "type": "", "x": 64, "y": 544, "width": 32, "height": 64,
       "properties": [{"name": "target", "type": "string", "value": "shop"}]}
    ]}
  ]
}`

func TestParse(t *testing.T) {
	m, err := tiled.Parse([]byte(minimal))
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		name string
		ok   bool
	}{
		{"size in tiles", m.Width == 40 && m.Height == 20},
		{"tile size", m.TileWidth == 32 && m.TileHeight == 32},
		{"size in pixels", m.PixelWidth() == 1280 && m.PixelHeight() == 640},
		{"both layers in file order", len(m.Layers) == 2 && m.Layers[0].Name == "ground" && m.Layers[1].Name == "doors"},
		{"layer types", m.Layers[0].Type == "tilelayer" && m.Layers[1].Type == tiled.ObjectGroup},
		{"an object with its rectangle", len(m.Layers[1].Objects) == 1 && m.Layers[1].Objects[0].X == 64 && m.Layers[1].Objects[0].Height == 64 && m.Layers[1].Objects[0].ID == 7 && m.Layers[1].Objects[0].Name == "front"},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("%s: %+v", c.name, m)
		}
	}
	t.Run("object groups by name", func(t *testing.T) {
		groups := m.ObjectGroups()
		if _, ok := groups["doors"]; !ok || len(groups) != 1 {
			t.Fatalf("groups %v", groups)
		}
	})
}

func TestParseRejects(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{"not JSON", `{not json`, "parse"},
		{"a tileset, not a map", `{"type": "tileset", "width": 1, "height": 1, "tilewidth": 32, "tileheight": 32}`, "not a Tiled map"},
		{"no tile size", `{"type": "map", "width": 1, "height": 1, "tilewidth": 0, "tileheight": 32}`, "tile size"},
		{"no size", `{"type": "map", "width": 0, "height": 1, "tilewidth": 32, "tileheight": 32}`, "tile size"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tiled.Parse([]byte(tt.json))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestLoadNamesTheFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "town.tmj")
	bad := filepath.Join(dir, "broken.tmj")
	if err := os.WriteFile(good, []byte(minimal), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if m, err := tiled.Load(good); err != nil || m.Width != 40 {
		t.Fatalf("load: %v", err)
	}
	for _, path := range []string{bad, filepath.Join(dir, "missing.tmj")} {
		_, err := tiled.Load(path)
		if err == nil || !strings.Contains(err.Error(), filepath.Base(path)) {
			t.Fatalf("err = %v, want it to name %s", err, filepath.Base(path))
		}
	}
}

func TestProperties(t *testing.T) {
	m, err := tiled.Parse([]byte(minimal))
	if err != nil {
		t.Fatal(err)
	}
	obj := m.Layers[1].Objects[0]
	frac := tiled.Properties{{Name: "speed", Type: "float", Value: 1.5}}
	tests := []struct {
		name    string
		props   tiled.Properties
		key     string
		wantStr string
		wantInt int
		strOK   bool
		intOK   bool
	}{
		{"a string on the map", m.Properties, "kind", "town", 0, true, false},
		{"an int on the map", m.Properties, "level", "", 3, false, true},
		{"a string on an object", obj.Properties, "target", "shop", 0, true, false},
		{"missing", obj.Properties, "nothing", "", 0, false, false},
		{"a fractional number is not an int", frac, "speed", "", 0, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, ok := tt.props.String(tt.key)
			if s != tt.wantStr || ok != tt.strOK {
				t.Fatalf("String = %q, %v; want %q, %v", s, ok, tt.wantStr, tt.strOK)
			}
			n, ok := tt.props.Int(tt.key)
			if n != tt.wantInt || ok != tt.intOK {
				t.Fatalf("Int = %d, %v; want %d, %v", n, ok, tt.wantInt, tt.intOK)
			}
		})
	}
}
