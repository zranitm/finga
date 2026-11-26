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
	if err := l.loadRegions(dir); err != nil {
		return err
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
	var bosses []mapfile.Boss
	if m.Boss != nil {
		bosses = append(bosses, *m.Boss)
	}
	if err := l.spots(m, bosses); err != nil {
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
		if err := battleMapRules(m); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown kind %q", m.Kind)
	}
	if err := uniquePortals(m.Portals); err != nil {
		return err
	}
	if err := l.spawnRules(m.Spawns, m.Levels, "map"); err != nil {
		return err
	}
	if b := m.Boss; b != nil {
		return l.bossRules(*b, m.Levels[1], "map")
	}
	return nil
}

// uniquePortals is an error for two portals of a name.
func uniquePortals(portals []mapfile.Portal) error {
	for i, p := range portals {
		if slices.ContainsFunc(portals[:i], func(q mapfile.Portal) bool { return q.Name == p.Name }) {
			return fmt.Errorf("duplicate portal %q", p.Name)
		}
	}
	return nil
}

// spawnRules are the mobs spawning on a map or in an area, of its levels: known, no boss, and an elite only beside a normal mob.
func (l *loader) spawnRules(spawns []mapfile.Spawn, levels [2]int, where string) error {
	normal, elite := false, ""
	for _, s := range spawns {
		mob, ok := l.mobs.get(s.Mob)
		switch {
		case !ok:
			return fmt.Errorf("spawn %q: unknown mob %q", s.Name, s.Mob)
		case mob.Boss:
			return fmt.Errorf("spawn %q: %s is a field boss, which stands at the %s's boss spot", s.Name, s.Mob, where)
		case mob.Level < levels[0] || mob.Level > levels[1]:
			return fmt.Errorf("spawn %q: %s is level %d, outside the %s's levels %d to %d", s.Name, s.Mob, mob.Level, where, levels[0], levels[1])
		case s.Max < 1 || s.Respawn < 0:
			return fmt.Errorf("spawn %q: max must be positive and respawn not negative", s.Name)
		case mob.Elite:
			elite = mob.ID
		default:
			normal = true
		}
	}
	if elite != "" && !normal {
		return fmt.Errorf("elite %s needs a normal mob on its %s to stand out from", elite, where)
	}
	return nil
}

// bossRules are a map's or an area's boss: a boss mob of its top level or above, its circle as wide as its body.
func (l *loader) bossRules(b mapfile.Boss, top int, where string) error {
	boss, ok := l.mobs.get(b.Mob)
	switch {
	case !ok || !boss.Boss:
		return fmt.Errorf("boss %q is not a boss in %s", b.Mob, mobsFile)
	case boss.Level < top:
		return fmt.Errorf("boss %s is level %d, below the %s's top level %d", b.Mob, boss.Level, where, top)
	case b.Radius < boss.Radius:
		return fmt.Errorf("boss %s: its circle's radius %g is narrower than its body's %g", b.Mob, b.Radius, boss.Radius)
	}
	return nil
}

// spots checks every spot stands on ground a body fits on inside the map, its bosses' among them, and that mobs keep off portals.
func (l *loader) spots(m *mapfile.Map, bosses []mapfile.Boss) error {
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
	for _, b := range bosses {
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

// linkMaps checks across maps and regions: portals and gates lead somewhere other than themselves, a gate and what it leads to lead to each other, each boss and NPC is on one map, towns are towns, and one town is the start.
func (l *loader) linkMaps() error {
	bossOn, npcOn := map[string]string{}, map[string]string{}
	start := ""
	for _, m := range l.maps.list {
		for _, p := range m.Portals {
			target, ok := l.maps.get(p.To.Map)
			if !ok {
				return fmt.Errorf("%s: portal %q: target map %q does not exist", fileOf(m), p.Name, p.To.Map)
			}
			if !slices.ContainsFunc(target.Portals, func(q mapfile.Portal) bool { return q.Name == p.To.Portal }) {
				return fmt.Errorf("%s: portal %q: target portal %q not in map %q", fileOf(m), p.Name, p.To.Portal, p.To.Map)
			}
			if p.To == (mapfile.Target{Map: m.ID, Portal: p.Name}) {
				return fmt.Errorf("%s: portal %q leads to itself", fileOf(m), p.Name)
			}
		}
	}
	for _, m := range l.maps.list {
		file := fileOf(m)
		for _, p := range m.Portals {
			target, _ := l.maps.get(p.To.Map)
			k := slices.IndexFunc(target.Portals, func(q mapfile.Portal) bool { return q.Name == p.To.Portal })
			here, what := mapfile.Target{Map: m.ID, Portal: p.Name}, "portal"
			if m.Kind == string(gamedata.Region) {
				what = "gate"
			}
			if back := target.Portals[k].To; (m.Kind == string(gamedata.Region) || target.Kind == string(gamedata.Region)) && back != here { // a gate and the portal or gate it leads to lead to each other
				return fmt.Errorf("%s: %s %q leads to %s's %q, which leads to %s's %q, not back", file, what, p.Name, p.To.Map, p.To.Portal, back.Map, back.Portal)
			}
		}
		switch gamedata.MapKind(m.Kind) {
		case gamedata.Pvp, gamedata.Region: // a match's map has no town, and a region is its own
		default:
			if town, ok := l.maps.get(m.Town); !ok || town.Kind != string(gamedata.Town) {
				return fmt.Errorf("%s: town %q is not a town", file, m.Town)
			}
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
			if (m.Kind != string(gamedata.Town) && m.Kind != string(gamedata.Region)) || start != "" {
				return fmt.Errorf("%s: start is for the one town or region new characters start in", file)
			}
			start = m.ID
		}
	}
	placed := map[string]bool{} // the bosses a region's areas place, which a map's boss may be too
	for _, r := range l.regions {
		for _, a := range r.areas {
			if a.Boss != nil {
				placed[a.Boss.Mob] = true
			}
		}
	}
	for _, mob := range l.mobs.list {
		if _, ok := bossOn[mob.ID]; mob.Boss && !ok && !placed[mob.ID] {
			return fmt.Errorf("%s: boss %s: no map places it", mapsDir, mob.ID)
		}
	}
	for _, n := range l.npcs.list {
		if _, ok := npcOn[n.ID]; !ok {
			return fmt.Errorf("%s: npc %s stands on no map", mapsDir, n.ID)
		}
	}
	if start == "" {
		return fmt.Errorf("%s: no town or region has start, where new characters start", mapsDir)
	}
	l.start = start
	return nil
}

// fileOf is the data file m was read from.
func fileOf(m *mapfile.Map) string {
	if m.Kind == string(gamedata.Region) {
		return regionsDir + "/" + m.ID + mapExt
	}
	return mapsDir + "/" + m.ID + mapExt
}

// mapOf is a built and checked map as the world reads it, with a region's bake: yaws in radians.
func mapOf(m *mapfile.Map, r regionParts) *gamedata.Map {
	point := func(at [2]float64) gamedata.Point { return gamedata.Point{X: at[0], Z: at[1]} }
	out := &gamedata.Map{
		ID: m.ID, Name: m.Name, Kind: gamedata.MapKind(m.Kind), Start: m.Start, Town: m.Town, MinLevel: m.Levels[0], MaxLevel: m.Levels[1],
		Geometry: m.Geometry, PlayerSpawn: point(m.PlayerSpawn.At), SpawnYaw: radians(m.PlayerSpawn.Yaw), Grid: m.Grid,
		Bake: r.bake, Areas: r.areas, AreaGrid: r.grid, Waypoints: r.waypoints, Under: r.under,
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
	for s, b := range m.Bases {
		out.Bases[s] = gamedata.Circle{At: point(b.At), Radius: b.Radius}
	}
	for _, p := range m.Points {
		out.Points = append(out.Points, gamedata.CapturePoint{ID: p.ID, Name: p.Name, At: point(p.At), Radius: p.Radius})
	}
	out.Kept = m.Kept
	if len(m.Bases) == 2 {
		out.KeptGrid = m.KeptGrid
	}
	return out
}

// radians is a data angle, written in degrees, as the world turns by it.
func radians(degrees float64) float64 { return degrees * math.Pi / 180 }

// battleMapRules checks a pvp map's bases and points, which a battle map has and a round map doesn't: two bases apart, each holding its side's spots, and points with ids of their own, inside the map and in no base.
func battleMapRules(m *mapfile.Map) error {
	switch {
	case len(m.Bases) == 0 && len(m.Points) == 0:
		return nil
	case len(m.Bases) != 2 || len(m.Points) == 0:
		return errors.New("a battle map has two bases and its points")
	case math.Hypot(m.Bases[0].At[0]-m.Bases[1].At[0], m.Bases[0].At[1]-m.Bases[1].At[1]) <= m.Bases[0].Radius+m.Bases[1].Radius:
		return errors.New("a battle map's bases are apart")
	}
	in := func(c [2]float64, r float64, at [2]float64) bool { return math.Hypot(at[0]-c[0], at[1]-c[1]) <= r }
	for s, b := range m.Bases {
		for k, spot := range m.Sides[s] {
			if !in(b.At, b.Radius, spot.At) {
				return fmt.Errorf("side %d's spot %d is outside its base", s+1, k+1)
			}
		}
	}
	ids := map[string]bool{}
	for k, p := range m.Points {
		switch {
		case p.ID == "" || p.Name == "" || ids[p.ID]:
			return fmt.Errorf("point %d needs an id of its own and a name", k+1)
		case !positive(p.Radius) || p.At[0]-p.Radius < 0 || p.At[1]-p.Radius < 0 || p.At[0]+p.Radius > m.Size[0] || p.At[1]+p.Radius > m.Size[1]:
			return fmt.Errorf("point %s needs a positive radius and its circle inside the map", p.ID)
		case in(m.Bases[0].At, m.Bases[0].Radius+p.Radius, p.At) || in(m.Bases[1].At, m.Bases[1].Radius+p.Radius, p.At):
			return fmt.Errorf("point %s overlaps a base", p.ID)
		}
		ids[p.ID] = true
	}
	return nil
}
