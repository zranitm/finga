package load

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/mapfile"
	"finga/internal/shared/nav"
)

const (
	mapsDir = "maps"
	mapExt  = ".yaml"
)

// loadMaps builds and checks the maps, so a broken one stops the game starting; the reach check is nav's test, not a load step.
func (l *loader) loadMaps(dir string) error {
	entries, err := os.ReadDir(filepath.Join(dir, mapsDir))
	if err != nil {
		return fmt.Errorf("%s: %w", mapsDir, err)
	}
	for _, e := range entries { // ReadDir sorts by name
		if e.IsDir() || filepath.Ext(e.Name()) != mapExt {
			continue
		}
		var f mapfile.File
		file := filepath.Join(mapsDir, e.Name())
		if err := readYAML(dir, file, &f); err != nil {
			return err
		}
		m, err := l.loadMap(strings.TrimSuffix(e.Name(), mapExt), f)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		l.maps.add(m.ID, m)
	}
	if len(l.maps.list) == 0 {
		return fmt.Errorf("%s: no %s maps", mapsDir, mapExt)
	}
	return l.linkMaps()
}

func (l *loader) loadMap(id string, f mapfile.File) (*mapfile.Map, error) {
	m, err := mapfile.Build(id, f, l.movement)
	if err != nil {
		return nil, err
	}
	if err := l.mapRules(m); err != nil {
		return nil, err
	}
	if err := l.spots(m); err != nil {
		return nil, err
	}
	return m, nil
}

// mapRules are a map's rules of kind, town, levels, spawns and boss.
func (l *loader) mapRules(m *mapfile.Map) error {
	if m.Name == "" {
		return errors.New("needs a name, the one players see")
	}
	if gamedata.MapKind(m.Kind) != gamedata.Pvp && (len(m.Sides) > 0 || len(m.PrepWall) > 0) {
		return errors.New("only a pvp map has sides and a prep_wall")
	}
	switch gamedata.MapKind(m.Kind) {
	case gamedata.Town:
		if m.Town != "" || m.Levels != [2]int{} {
			return errors.New("a town is its own respawn town and has no levels")
		}
		if len(m.Spawns) > 0 || m.Boss != nil {
			return errors.New("a town has no mob spawns and no field boss")
		}
		m.Town = m.ID
	case gamedata.Hunting:
		if m.Town == "" {
			return errors.New("a hunting map names its respawn town")
		}
		if m.Levels[0] < 1 || m.Levels[1] < m.Levels[0] {
			return fmt.Errorf("levels %v: a hunting map's mob levels need 1 <= lowest <= highest", m.Levels)
		}
		if m.Boss == nil {
			return errors.New("a hunting map has a field boss")
		}
	case gamedata.Pvp:
		switch {
		case m.Town != "" || m.Levels != [2]int{} || m.Start:
			return errors.New("a pvp map has no town, levels or start")
		case len(m.Portals) > 0 || len(m.Spawns) > 0 || m.Boss != nil || len(m.NPCs) > 0:
			return errors.New("a pvp map has no portals, mob spawns, field boss or NPCs")
		case m.PlayerSpawn != (mapfile.Place{}):
			return errors.New("a pvp map's fighters start on its sides, so it has no player_spawn")
		case len(m.Sides) != 2 || len(m.Sides[0]) == 0 || len(m.Sides[1]) == 0:
			return errors.New("a pvp map has two sides, each with its starting spots")
		case len(m.PrepWall) == 0:
			return errors.New("a pvp map has a prep_wall, standing before each round")
		}
	default:
		return fmt.Errorf("unknown kind %q", m.Kind)
	}
	for i, p := range m.Portals {
		if slices.ContainsFunc(m.Portals[:i], func(q mapfile.Portal) bool { return q.Name == p.Name }) {
			return fmt.Errorf("duplicate portal %q", p.Name)
		}
	}
	normal, elite := false, ""
	for _, s := range m.Spawns {
		mob, ok := l.mobs.get(s.Mob)
		switch {
		case !ok:
			return fmt.Errorf("spawn %q: unknown mob %q", s.Name, s.Mob)
		case mob.Boss:
			return fmt.Errorf("spawn %q: %s is a field boss, which stands at the map's boss spot", s.Name, s.Mob)
		case mob.Level < m.Levels[0] || mob.Level > m.Levels[1]:
			return fmt.Errorf("spawn %q: %s is level %d, outside the map's levels %d to %d", s.Name, s.Mob, mob.Level, m.Levels[0], m.Levels[1])
		case s.Max < 1 || s.Respawn < 0:
			return fmt.Errorf("spawn %q: max must be positive and respawn not negative", s.Name)
		case mob.Elite:
			elite = mob.ID
		default:
			normal = true
		}
	}
	if elite != "" && !normal {
		return fmt.Errorf("elite %s needs a normal mob on its map to stand out from", elite)
	}
	if b := m.Boss; b != nil {
		boss, ok := l.mobs.get(b.Mob)
		switch {
		case !ok || !boss.Boss:
			return fmt.Errorf("boss %q is not a boss in %s", b.Mob, mobsFile)
		case boss.Level < m.Levels[1]:
			return fmt.Errorf("boss %s is level %d, below the map's top level %d", b.Mob, boss.Level, m.Levels[1])
		case b.Radius < boss.Radius:
			return fmt.Errorf("boss %s: its circle's radius %g is narrower than its body's %g", b.Mob, b.Radius, boss.Radius)
		}
	}
	return nil
}

// spots checks every spot stands on ground a body fits on inside the map, and that mobs keep off portals.
func (l *loader) spots(m *mapfile.Map) error {
	player := l.movement.Radius
	circle := func(what string, at [2]float64, radius, body float64) error {
		if !(radius > 0) || at[0]-radius < 0 || at[1]-radius < 0 || at[0]+radius > m.Size[0] || at[1]+radius > m.Size[1] {
			return fmt.Errorf("%s: its circle must have a positive radius and lie inside the map", what)
		}
		if !nav.FitsAt(m.Geometry, l.movement.Step, mapfile.Point(at), body) {
			return fmt.Errorf("%s: its centre %v isn't ground a body %g m across fits on", what, at, 2*body)
		}
		return nil
	}
	offPortals := func(what string, at [2]float64, radius float64) error {
		for _, p := range m.Portals {
			if nav.Distance(mapfile.Point(at), mapfile.Point(p.At)) < radius+p.Radius {
				return fmt.Errorf("%s overlaps portal %q; mobs keep off portals", what, p.Name)
			}
		}
		return nil
	}
	for _, p := range m.Portals {
		if err := circle(fmt.Sprintf("portal %q", p.Name), p.At, p.Radius, player); err != nil {
			return err
		}
	}
	for _, s := range m.Spawns {
		mob, _ := l.mobs.get(s.Mob)
		what := fmt.Sprintf("spawn %q", s.Name)
		if err := circle(what, s.At, s.Radius, mob.Radius); err != nil {
			return err
		}
		if err := offPortals(what, s.At, s.Radius); err != nil {
			return err
		}
	}
	if b := m.Boss; b != nil {
		boss, _ := l.mobs.get(b.Mob)
		what := "boss " + b.Mob
		if err := circle(what, b.At, b.Radius, boss.Radius); err != nil {
			return err
		}
		if err := offPortals(what, b.At, b.Radius); err != nil {
			return err
		}
	}
	if gamedata.MapKind(m.Kind) != gamedata.Pvp && !nav.FitsAt(m.Geometry, l.movement.Step, mapfile.Point(m.PlayerSpawn.At), player) {
		return fmt.Errorf("player_spawn %v isn't ground a player fits on", m.PlayerSpawn.At)
	}
	for s, side := range m.Sides {
		for _, at := range side {
			if !nav.FitsAt(m.Walled, l.movement.Step, mapfile.Point(at.At), player) {
				return fmt.Errorf("side %d's spot %v isn't ground a player fits on, the prep wall standing", s+1, at.At)
			}
		}
	}
	for _, n := range m.NPCs {
		if !l.npcs.has(n.ID) {
			return fmt.Errorf("npc %q is not in %s", n.ID, npcsFile)
		}
		if !nav.FitsAt(m.Geometry, l.movement.Step, mapfile.Point(n.At), player) {
			return fmt.Errorf("npc %s at %v isn't ground a player fits on", n.ID, n.At)
		}
	}
	return nil
}

// linkMaps checks across maps: portals lead somewhere, each boss and NPC is on one map, towns are towns, and one town is the start.
func (l *loader) linkMaps() error {
	bossOn, npcOn := map[string]string{}, map[string]string{}
	start := ""
	for _, m := range l.maps.list {
		file := mapsDir + "/" + m.ID + mapExt
		for _, p := range m.Portals {
			target, ok := l.maps.get(p.To.Map)
			if !ok {
				return fmt.Errorf("%s: portal %q: target map %q does not exist", file, p.Name, p.To.Map)
			}
			if !slices.ContainsFunc(target.Portals, func(q mapfile.Portal) bool { return q.Name == p.To.Portal }) {
				return fmt.Errorf("%s: portal %q: target portal %q not in map %q", file, p.Name, p.To.Portal, p.To.Map)
			}
		}
		if town, ok := l.maps.get(m.Town); m.Kind != string(gamedata.Pvp) && (!ok || town.Kind != string(gamedata.Town)) {
			return fmt.Errorf("%s: town %q is not a town", file, m.Town)
		}
		if m.Boss != nil {
			if other, ok := bossOn[m.Boss.Mob]; ok {
				return fmt.Errorf("%s: boss %s is already on %s; each map has its own", file, m.Boss.Mob, other)
			}
			bossOn[m.Boss.Mob] = m.ID
		}
		for _, n := range m.NPCs {
			if other, ok := npcOn[n.ID]; ok {
				return fmt.Errorf("%s: npc %s already stands on %s", file, n.ID, other)
			}
			npcOn[n.ID] = m.ID
			placed := &l.npcs.list[l.npcs.idx[n.ID]]
			placed.MapID, placed.At, placed.Yaw = m.ID, gamedata.Point{X: n.At[0], Z: n.At[1]}, radians(n.Yaw)
		}
		if m.Start {
			if m.Kind != string(gamedata.Town) || start != "" {
				return fmt.Errorf("%s: start is for the one town new characters start in", file)
			}
			start = m.ID
		}
	}
	for _, mob := range l.mobs.list {
		if _, ok := bossOn[mob.ID]; mob.Boss && !ok {
			return fmt.Errorf("%s: boss %s: no map places it", mapsDir, mob.ID)
		}
	}
	for _, n := range l.npcs.list {
		if _, ok := npcOn[n.ID]; !ok {
			return fmt.Errorf("%s: npc %s stands on no map", mapsDir, n.ID)
		}
	}
	if start == "" {
		return fmt.Errorf("%s: no town has start, where new characters start", mapsDir)
	}
	l.start = start
	return nil
}

// mapOf is a built and checked map as the world reads it: yaws in radians.
func mapOf(m *mapfile.Map) *gamedata.Map {
	point := func(at [2]float64) gamedata.Point { return gamedata.Point{X: at[0], Z: at[1]} }
	out := &gamedata.Map{
		ID: m.ID, Name: m.Name, Kind: gamedata.MapKind(m.Kind), Start: m.Start, Town: m.Town, MinLevel: m.Levels[0], MaxLevel: m.Levels[1],
		Geometry: m.Geometry, PlayerSpawn: point(m.PlayerSpawn.At), SpawnYaw: radians(m.PlayerSpawn.Yaw), Grid: m.Grid(),
	}
	for _, p := range m.Portals {
		out.Portals = append(out.Portals, gamedata.Portal{Name: p.Name, At: point(p.At), Radius: p.Radius, TargetMap: p.To.Map, TargetPortal: p.To.Portal})
	}
	for _, s := range m.Spawns {
		out.Spawns = append(out.Spawns, gamedata.Spawn{Mob: s.Mob, Max: s.Max, Respawn: s.Respawn, At: point(s.At), Radius: s.Radius})
	}
	for _, n := range m.NPCs {
		out.NPCs = append(out.NPCs, gamedata.Placement{NPC: n.ID, At: point(n.At), Yaw: radians(n.Yaw)})
	}
	if b := m.Boss; b != nil {
		out.Boss = &gamedata.BossSpot{Mob: b.Mob, At: point(b.At), Radius: b.Radius}
	}
	for s, side := range m.Sides {
		for _, at := range side {
			out.Sides[s] = append(out.Sides[s], gamedata.Spot{At: point(at.At), Yaw: radians(at.Yaw)})
		}
	}
	if gamedata.MapKind(m.Kind) == gamedata.Pvp {
		out.PlayerSpawn, out.SpawnYaw, out.Walled = out.Sides[0][0].At, out.Sides[0][0].Yaw, m.Walled // where one who falls out is caught
	}
	return out
}

// radians is a data angle, written in degrees, as the world turns by it.
func radians(degrees float64) float64 { return degrees * math.Pi / 180 }
