package gamedata

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"finga/internal/application/world/sim"
)

// The object layers every map must have (page 5), even when empty.
const (
	layerSolids      = "solids"
	layerPlatforms   = "platforms"
	layerRopes       = "ropes"
	layerPortals     = "portals"
	layerSpawns      = "spawns"
	layerPlayerSpawn = "player_spawn"

	propKind         = "kind"
	propTown         = "town"
	propTargetMap    = "target_map"
	propTargetPortal = "target_portal"
	propMob          = "mob"
	propMax          = "max"
	propRespawn      = "respawn_seconds"
)

var requiredLayers = []string{layerSolids, layerPlatforms, layerRopes, layerPortals, layerSpawns, layerPlayerSpawn}

// The subset of Tiled's JSON map format the server reads. Tiled puts the
// origin top-left with y growing downward, the same as sim, so rectangles
// pass through unchanged.
type tiledMap struct {
	Type       string       `json:"type"`
	Width      int          `json:"width"` // tiles
	Height     int          `json:"height"`
	TileWidth  int          `json:"tilewidth"`
	TileHeight int          `json:"tileheight"`
	Properties []tiledProp  `json:"properties"`
	Layers     []tiledLayer `json:"layers"`
}

type tiledLayer struct {
	Name    string        `json:"name"`
	Type    string        `json:"type"`
	Objects []tiledObject `json:"objects"`
}

type tiledObject struct {
	ID         int         `json:"id"`
	Name       string      `json:"name"`
	X          float64     `json:"x"`
	Y          float64     `json:"y"`
	Width      float64     `json:"width"`
	Height     float64     `json:"height"`
	Properties []tiledProp `json:"properties"`
}

type tiledProp struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

func (d *Data) loadMap(path string) (*Map, error) {
	file := filepath.Base(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	var tm tiledMap
	if err := json.Unmarshal(data, &tm); err != nil {
		return nil, fmt.Errorf("%s: parse: %w", file, err)
	}
	if tm.Type != "map" || tm.TileWidth <= 0 || tm.TileHeight <= 0 || tm.Width <= 0 || tm.Height <= 0 {
		return nil, fmt.Errorf("%s: not a Tiled map with a tile size", file)
	}
	m := &Map{
		ID:     strings.TrimSuffix(file, mapExt),
		Width:  float64(tm.Width * tm.TileWidth),
		Height: float64(tm.Height * tm.TileHeight),
	}
	kind, ok := propString(tm.Properties, propKind)
	if !ok {
		return nil, fmt.Errorf("%s: missing map property %q", file, propKind)
	}
	m.Kind = MapKind(kind)
	if m.Kind != Town && m.Kind != Hunting {
		return nil, fmt.Errorf("%s: unknown %s %q", file, propKind, kind)
	}
	// A town is its own town; a hunting map names the town its players
	// respawn in (page 5), checked against the loaded maps in loadMaps.
	if m.Kind == Town {
		m.Town = m.ID
	} else if m.Town, ok = propString(tm.Properties, propTown); !ok {
		return nil, fmt.Errorf("%s: missing map property %q", file, propTown)
	}

	layers := map[string]tiledLayer{}
	for _, l := range tm.Layers {
		if l.Type == "objectgroup" {
			layers[l.Name] = l
		}
	}
	for _, name := range requiredLayers {
		if _, ok := layers[name]; !ok {
			return nil, fmt.Errorf("%s: missing layer %q", file, name)
		}
	}
	m.Geometry = sim.Geometry{
		Solids:    rects(layers[layerSolids]),
		Platforms: rects(layers[layerPlatforms]),
		Ropes:     rects(layers[layerRopes]),
	}

	for _, o := range layers[layerPortals].Objects {
		targetMap, ok := propString(o.Properties, propTargetMap)
		if !ok {
			return nil, fmt.Errorf("%s: layer %q: object %q: missing property %q", file, layerPortals, o.Name, propTargetMap)
		}
		targetPortal, ok := propString(o.Properties, propTargetPortal)
		if !ok {
			return nil, fmt.Errorf("%s: layer %q: object %q: missing property %q", file, layerPortals, o.Name, propTargetPortal)
		}
		for _, p := range m.Portals {
			if p.Name == o.Name {
				return nil, fmt.Errorf("%s: duplicate portal %q", file, o.Name)
			}
		}
		m.Portals = append(m.Portals, Portal{Name: o.Name, Area: rect(o), TargetMap: targetMap, TargetPortal: targetPortal})
	}

	for _, o := range layers[layerSpawns].Objects {
		if m.Kind == Town {
			return nil, fmt.Errorf("%s: a town has no mobs, but layer %q has spawn %q", file, layerSpawns, o.Name)
		}
		mob, ok := propString(o.Properties, propMob)
		if !ok {
			return nil, fmt.Errorf("%s: layer %q: object %q: missing property %q", file, layerSpawns, o.Name, propMob)
		}
		if _, ok := d.Mob(mob); !ok {
			return nil, fmt.Errorf("%s: layer %q: object %q: unknown mob %q", file, layerSpawns, o.Name, mob)
		}
		maxMobs, ok := propInt(o.Properties, propMax)
		if !ok || maxMobs < 1 {
			return nil, fmt.Errorf("%s: layer %q: object %q: property %q must be a positive integer", file, layerSpawns, o.Name, propMax)
		}
		respawn, ok := propInt(o.Properties, propRespawn)
		if !ok || respawn < 0 {
			return nil, fmt.Errorf("%s: layer %q: object %q: property %q must be a non-negative integer", file, layerSpawns, o.Name, propRespawn)
		}
		m.Spawns = append(m.Spawns, Spawn{Mob: mob, Max: maxMobs, Respawn: time.Duration(respawn) * time.Second, Area: rect(o)})
	}

	spawns := layers[layerPlayerSpawn].Objects
	if len(spawns) != 1 {
		return nil, fmt.Errorf("%s: layer %q needs exactly one object, has %d", file, layerPlayerSpawn, len(spawns))
	}
	m.PlayerSpawn = Point{X: spawns[0].X + spawns[0].Width/2, Y: spawns[0].Y + spawns[0].Height}
	return m, nil
}

func rects(l tiledLayer) []sim.Rect {
	out := make([]sim.Rect, 0, len(l.Objects))
	for _, o := range l.Objects {
		out = append(out, rect(o))
	}
	return out
}

func rect(o tiledObject) sim.Rect {
	return sim.Rect{X: o.X, Y: o.Y, W: o.Width, H: o.Height}
}

func propString(props []tiledProp, name string) (string, bool) {
	for _, p := range props {
		if p.Name == name {
			s, ok := p.Value.(string)
			return s, ok
		}
	}
	return "", false
}

func propInt(props []tiledProp, name string) (int, bool) {
	for _, p := range props {
		if p.Name == name {
			f, ok := p.Value.(float64) // JSON numbers
			if !ok || f != float64(int(f)) {
				return 0, false
			}
			return int(f), true
		}
	}
	return 0, false
}
