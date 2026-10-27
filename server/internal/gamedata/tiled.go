package gamedata

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"finga/internal/sim"
	"finga/pkg/tiled"
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

func (d *Data) loadMap(path string) (*Map, error) {
	file := filepath.Base(path)
	tm, err := tiled.Load(path)
	if err != nil {
		return nil, err
	}
	m := &Map{
		ID:     strings.TrimSuffix(file, mapExt),
		Width:  tm.PixelWidth(),
		Height: tm.PixelHeight(),
	}
	kind, ok := tm.Properties.String(propKind)
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
	} else if m.Town, ok = tm.Properties.String(propTown); !ok {
		return nil, fmt.Errorf("%s: missing map property %q", file, propTown)
	}

	layers := tm.ObjectGroups()
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
		targetMap, ok := o.Properties.String(propTargetMap)
		if !ok {
			return nil, fmt.Errorf("%s: layer %q: object %q: missing property %q", file, layerPortals, o.Name, propTargetMap)
		}
		targetPortal, ok := o.Properties.String(propTargetPortal)
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
		mob, ok := o.Properties.String(propMob)
		if !ok {
			return nil, fmt.Errorf("%s: layer %q: object %q: missing property %q", file, layerSpawns, o.Name, propMob)
		}
		if _, ok := d.Mob(mob); !ok {
			return nil, fmt.Errorf("%s: layer %q: object %q: unknown mob %q", file, layerSpawns, o.Name, mob)
		}
		maxMobs, ok := o.Properties.Int(propMax)
		if !ok || maxMobs < 1 {
			return nil, fmt.Errorf("%s: layer %q: object %q: property %q must be a positive integer", file, layerSpawns, o.Name, propMax)
		}
		respawn, ok := o.Properties.Int(propRespawn)
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

func rects(l tiled.Layer) []sim.Rect {
	out := make([]sim.Rect, 0, len(l.Objects))
	for _, o := range l.Objects {
		out = append(out, rect(o))
	}
	return out
}

func rect(o tiled.Object) sim.Rect {
	return sim.Rect{X: o.X, Y: o.Y, W: o.Width, H: o.Height}
}
