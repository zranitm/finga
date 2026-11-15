package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"finga/pkg/tiled"
)

// Every 3D map that isn't designed is exactly what the converter makes of its .tmj, so the placeholders are the converter's, all 12.
func TestPlaceholdersAreTheConvertersOutput(t *testing.T) {
	maps, err := filepath.Glob("../../data/maps/*.tmj")
	if err != nil {
		t.Fatal(err)
	}
	var converted, designed []string
	for _, path := range maps {
		id := strings.TrimSuffix(filepath.Base(path), ".tmj")
		have, err := os.ReadFile(filepath.Join("../../data/maps3d", id+".yaml"))
		if err != nil {
			t.Fatalf("%s has no 3D map: %v", id, err)
		}
		if !bytes.HasPrefix(have, fmt.Appendf(nil, marker, id)) {
			designed = append(designed, id)
			continue
		}
		converted = append(converted, id)
		want, err := convertFile(path, id)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(have, want) {
			t.Errorf("data/maps3d/%s.yaml isn't the converter's: go run ./cmd/tmj2yaml %s, or drop its first line if it's designed now", id, id)
		}
	}
	if !slices.Equal(designed, []string{"forest", "town"}) || len(converted) != 12 {
		t.Errorf("designed %v and %d converted, want forest and town and the other 12", designed, len(converted))
	}
}

func TestAnAreaOnAPortalIsRefused(t *testing.T) {
	tm, err := tiled.Parse([]byte(`{"type":"map","width":40,"height":20,"tilewidth":32,"tileheight":32,
		"properties":[{"name":"kind","type":"string","value":"hunting"}],
		"layers":[
			{"name":"portals","type":"objectgroup","objects":[{"name":"out","x":32,"y":0,"width":32,"height":64,
				"properties":[{"name":"target_map","type":"string","value":"town"},{"name":"target_portal","type":"string","value":"in"}]}]},
			{"name":"spawns","type":"objectgroup","objects":[{"name":"slimes","x":0,"y":0,"width":160,"height":64,
				"properties":[{"name":"mob","type":"string","value":"green_slime"}]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := convert(tm); err == nil || !strings.Contains(err.Error(), "slimes") || !strings.Contains(err.Error(), "portal") {
		t.Fatalf("error %v", err)
	}
}
