package load

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"finga/internal/gamedata"
	"finga/internal/sim"
	"finga/pkg/tiled"
)

const (
	mapsDir = "maps"
	mapExt  = ".tmj"

	layerSolids      = "solids"
	layerPlatforms   = "platforms"
	layerRopes       = "ropes"
	layerPortals     = "portals"
	layerSpawns      = "spawns"
	layerPlayerSpawn = "player_spawn"
	layerNPCs        = "npcs"
	layerBoss        = "boss"

	propKind         = "kind"
	propName         = "name"
	propStart        = "start"
	propTown         = "town"
	propMinLevel     = "min_level"
	propMaxLevel     = "max_level"
	propTargetMap    = "target_map"
	propTargetPortal = "target_portal"
	propMob          = "mob"
	propMax          = "max"
	propRespawn      = "respawn_seconds"
	propKills        = "kills"
)

var requiredLayers = []string{layerSolids, layerPlatforms, layerRopes, layerPortals, layerSpawns, layerPlayerSpawn, layerNPCs, layerBoss}

func (l *loader) loadMaps(dir string) error {
	entries, err := os.ReadDir(filepath.Join(dir, mapsDir))
	if err != nil {
		return fmt.Errorf("%s: %w", mapsDir, err)
	}
	for _, e := range entries { // ReadDir sorts by name
		if e.IsDir() || filepath.Ext(e.Name()) != mapExt {
			continue
		}
		m, err := l.loadMap(filepath.Join(dir, mapsDir, e.Name()))
		if err != nil {
			return err
		}
		l.maps.add(m.ID, m)
	}
	if len(l.maps.list) == 0 {
		return fmt.Errorf("%s: no %s maps", mapsDir, mapExt)
	}
	// Portals can only be checked once every map is loaded.
	for _, m := range l.maps.list {
		file := m.ID + mapExt
		for _, p := range m.Portals {
			target, ok := l.maps.get(p.TargetMap)
			if !ok {
				return fmt.Errorf("%s: layer %q: object %q: target map %q does not exist", file, layerPortals, p.Name, p.TargetMap)
			}
			if !slices.ContainsFunc(target.Portals, func(q gamedata.Portal) bool { return q.Name == p.TargetPortal }) {
				return fmt.Errorf("%s: layer %q: object %q: target portal %q not in map %q", file, layerPortals, p.Name, p.TargetPortal, p.TargetMap)
			}
		}
	}
	bossOn := map[string]string{}
	for _, m := range l.maps.list {
		if m.Boss == nil {
			continue
		}
		if other, ok := bossOn[m.Boss.Mob]; ok {
			return fmt.Errorf("maps: %s and %s both have the field boss %s; each map has its own", other+mapExt, m.ID+mapExt, m.Boss.Mob)
		}
		bossOn[m.Boss.Mob] = m.ID
	}
	for _, mob := range l.mobs.list {
		if _, ok := bossOn[mob.ID]; mob.Boss && !ok {
			return fmt.Errorf("%s: boss %s: no map's layer %q places it", mobsFile, mob.ID, layerBoss)
		}
	}
	for _, m := range l.maps.list {
		town, ok := l.maps.get(m.Town)
		if !ok || town.Kind != gamedata.Town {
			return fmt.Errorf("%s%s: map property %q names %q, which is not a town", m.ID, mapExt, propTown, m.Town)
		}
	}
	for _, m := range l.maps.list {
		if !m.Start {
			continue
		}
		if l.start != nil {
			return fmt.Errorf("maps: %s and %s both have the property %q; new characters start in one town", l.start.ID+mapExt, m.ID+mapExt, propStart)
		}
		if m.Kind != gamedata.Town {
			return fmt.Errorf("%s%s: the map property %q is for a town, where new characters start", m.ID, mapExt, propStart)
		}
		l.start = m
	}
	if l.start == nil {
		return fmt.Errorf("maps: no town has the property %q, where new characters start", propStart)
	}
	return nil
}

func (l *loader) loadMap(path string) (*gamedata.Map, error) {
	file := filepath.Base(path)
	tm, err := tiled.Load(path)
	if err != nil {
		return nil, err
	}
	m := &gamedata.Map{
		ID:     strings.TrimSuffix(file, mapExt),
		Width:  tm.PixelWidth(),
		Height: tm.PixelHeight(),
	}
	kind, ok := tm.Properties.String(propKind)
	if !ok {
		return nil, fmt.Errorf("%s: missing map property %q", file, propKind)
	}
	m.Kind = gamedata.MapKind(kind)
	if m.Kind != gamedata.Town && m.Kind != gamedata.Hunting {
		return nil, fmt.Errorf("%s: unknown %s %q", file, propKind, kind)
	}
	if m.Name, ok = tm.Properties.String(propName); !ok || m.Name == "" {
		return nil, fmt.Errorf("%s: missing map property %q, the name players see", file, propName)
	}
	m.Start, _ = tm.Properties.Bool(propStart)
	// A hunting map names its respawn town; loadMaps checks it exists.
	if m.Kind == gamedata.Town {
		m.Town = m.ID
	} else if m.Town, ok = tm.Properties.String(propTown); !ok {
		return nil, fmt.Errorf("%s: missing map property %q", file, propTown)
	}
	if m.Kind == gamedata.Hunting {
		lo, okLo := tm.Properties.Int(propMinLevel)
		hi, okHi := tm.Properties.Int(propMaxLevel)
		if !okLo || !okHi || lo < 1 || hi < lo {
			return nil, fmt.Errorf("%s: a hunting map needs map properties %q and %q, its mobs' levels, with 1 <= %s <= %s", file, propMinLevel, propMaxLevel, propMinLevel, propMaxLevel)
		}
		m.MinLevel, m.MaxLevel = lo, hi
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
		m.Portals = append(m.Portals, gamedata.Portal{Name: o.Name, Area: rect(o), TargetMap: targetMap, TargetPortal: targetPortal})
	}

	for _, o := range layers[layerSpawns].Objects {
		if m.Kind == gamedata.Town {
			return nil, fmt.Errorf("%s: a town has no mobs, but layer %q has spawn %q", file, layerSpawns, o.Name)
		}
		mob, ok := o.Properties.String(propMob)
		if !ok {
			return nil, fmt.Errorf("%s: layer %q: object %q: missing property %q", file, layerSpawns, o.Name, propMob)
		}
		mt, ok := l.mobs.get(mob)
		if !ok {
			return nil, fmt.Errorf("%s: layer %q: object %q: unknown mob %q", file, layerSpawns, o.Name, mob)
		}
		if mt.Boss {
			return nil, fmt.Errorf("%s: layer %q: object %q: %s is a field boss, which kills bring in layer %q", file, layerSpawns, o.Name, mob, layerBoss)
		}
		if mt.Level < m.MinLevel || mt.Level > m.MaxLevel {
			return nil, fmt.Errorf("%s: layer %q: object %q: %s is level %d, outside the map's levels %d to %d", file, layerSpawns, o.Name, mob, mt.Level, m.MinLevel, m.MaxLevel)
		}
		maxMobs, ok := o.Properties.Int(propMax)
		if !ok || maxMobs < 1 {
			return nil, fmt.Errorf("%s: layer %q: object %q: property %q must be a positive integer", file, layerSpawns, o.Name, propMax)
		}
		respawn, ok := o.Properties.Int(propRespawn)
		if !ok || respawn < 0 {
			return nil, fmt.Errorf("%s: layer %q: object %q: property %q must be a non-negative integer", file, layerSpawns, o.Name, propRespawn)
		}
		if p, ok := portalIn(m, rect(o)); ok {
			return nil, fmt.Errorf("%s: layer %q: object %q overlaps portal %q; mobs keep off portals", file, layerSpawns, o.Name, p)
		}
		m.Spawns = append(m.Spawns, gamedata.Spawn{Mob: mob, Max: maxMobs, Respawn: time.Duration(respawn) * time.Second, Area: rect(o)})
	}

	if elite, normal := spawnsElite(l, m.Spawns); elite != "" && !normal {
		return nil, fmt.Errorf("%s: layer %q: elite %s needs a normal mob on its map to stand out from", file, layerSpawns, elite)
	}

	spawns := layers[layerPlayerSpawn].Objects
	if len(spawns) != 1 {
		return nil, fmt.Errorf("%s: layer %q needs exactly one object, has %d", file, layerPlayerSpawn, len(spawns))
	}
	m.PlayerSpawn = feet(spawns[0])

	// loadNPCs matches each object to npcs.yaml by its name.
	for _, o := range layers[layerNPCs].Objects {
		m.NPCs = append(m.NPCs, gamedata.Placement{NPC: o.Name, At: feet(o)})
	}
	if m.Boss, err = l.bossSpot(file, m, layers[layerBoss].Objects); err != nil {
		return nil, err
	}
	return m, nil
}

// bossSpot is a hunting map's one field boss area, which a town doesn't have.
func (l *loader) bossSpot(file string, m *gamedata.Map, objects []tiled.Object) (*gamedata.BossSpot, error) {
	if m.Kind == gamedata.Town {
		if len(objects) > 0 {
			return nil, fmt.Errorf("%s: a town has no field boss, but layer %q has %q", file, layerBoss, objects[0].Name)
		}
		return nil, nil
	}
	if len(objects) != 1 {
		return nil, fmt.Errorf("%s: layer %q needs exactly one area on a hunting map, has %d", file, layerBoss, len(objects))
	}
	o := objects[0]
	id, _ := o.Properties.String(propMob)
	boss, ok := l.mobs.get(id)
	if !ok || !boss.Boss {
		return nil, fmt.Errorf("%s: layer %q: object %q: property %q names %q, which is not a boss in %s", file, layerBoss, o.Name, propMob, id, mobsFile)
	}
	if boss.Level < m.MaxLevel {
		return nil, fmt.Errorf("%s: layer %q: object %q: %s is level %d, below the map's top level %d", file, layerBoss, o.Name, id, boss.Level, m.MaxLevel)
	}
	kills, ok := o.Properties.Int(propKills)
	if !ok || kills < 1 {
		return nil, fmt.Errorf("%s: layer %q: object %q: property %q must be a positive integer", file, layerBoss, o.Name, propKills)
	}
	area := rect(o)
	if p, ok := portalIn(m, area); ok {
		return nil, fmt.Errorf("%s: layer %q: object %q overlaps portal %q; mobs keep off portals", file, layerBoss, o.Name, p)
	}
	if g, ok := m.GroundAt(area.X+area.W/2, area.Y+area.H); !ok || min(g.X+g.W, area.X+area.W)-max(g.X, area.X) < boss.Width {
		return nil, fmt.Errorf("%s: layer %q: object %q: the ground under its bottom centre is narrower within it than %s, %g px", file, layerBoss, o.Name, id, boss.Width)
	}
	return &gamedata.BossSpot{Mob: id, Kills: kills, Area: area}, nil
}

// portalIn is the name of a portal of m that area overlaps, if any.
func portalIn(m *gamedata.Map, area sim.Rect) (string, bool) {
	for _, p := range m.Portals {
		if p.Area.Overlaps(area) {
			return p.Name, true
		}
	}
	return "", false
}

// spawnsElite is an elite the spawns keep, if any, and whether they keep a normal mob too.
func spawnsElite(l *loader, spawns []gamedata.Spawn) (elite string, normal bool) {
	for _, s := range spawns {
		if mob, _ := l.mobs.get(s.Mob); mob.Elite {
			elite = mob.ID
		} else {
			normal = true
		}
	}
	return elite, normal
}

// feet is where an object stands: its bottom centre, which for a point object is the point.
func feet(o tiled.Object) gamedata.Point {
	return gamedata.Point{X: o.X + o.Width/2, Y: o.Y + o.Height}
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
