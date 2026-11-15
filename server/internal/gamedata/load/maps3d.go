package load

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"finga/internal/gamedata"
	"finga/internal/maps3d"
	"finga/internal/nav"
)

const (
	maps3dDir = "maps3d"
	map3dExt  = ".yaml"

	pxPerMetre = 32 // mobs.yaml is still in px
)

// loadMaps3D builds and checks the 3D maps, so a broken one stops the game starting; the reach check is nav's test, not a load step.
func (l *loader) loadMaps3D(dir string) error {
	entries, err := os.ReadDir(filepath.Join(dir, maps3dDir))
	if err != nil {
		return fmt.Errorf("%s: %w", maps3dDir, err)
	}
	for _, e := range entries { // ReadDir sorts by name
		if e.IsDir() || filepath.Ext(e.Name()) != map3dExt {
			continue
		}
		var f maps3d.File
		file := filepath.Join(maps3dDir, e.Name())
		if err := readYAML(dir, file, &f); err != nil {
			return err
		}
		m, err := l.loadMap3D(strings.TrimSuffix(e.Name(), map3dExt), f)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		l.maps3d.add(m.ID, m)
	}
	if len(l.maps3d.list) == 0 {
		return fmt.Errorf("%s: no %s maps", maps3dDir, map3dExt)
	}
	return l.linkMaps3D()
}

func (l *loader) loadMap3D(id string, f maps3d.File) (*maps3d.Map, error) {
	m, err := maps3d.Build(id, f, l.movement3d)
	if err != nil {
		return nil, err
	}
	if err := l.mapRules3D(m); err != nil {
		return nil, err
	}
	if err := l.spots3D(m); err != nil {
		return nil, err
	}
	return m, nil
}

// mapRules3D are the 2D maps' rules of kind, town, levels, spawns and boss, as loadMap checks them.
func (l *loader) mapRules3D(m *maps3d.Map) error {
	if m.Name == "" {
		return errors.New("needs a name, the one players see")
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
	default:
		return fmt.Errorf("unknown kind %q", m.Kind)
	}
	for i, p := range m.Portals {
		if slices.ContainsFunc(m.Portals[:i], func(q maps3d.Portal) bool { return q.Name == p.Name }) {
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
			return fmt.Errorf("spawn %q: %s is a field boss, which kills bring", s.Name, s.Mob)
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
		case b.Kills < 1:
			return fmt.Errorf("boss %s: kills must be positive", b.Mob)
		case b.Radius < bodyRadius(boss):
			return fmt.Errorf("boss %s: its circle's radius %g is narrower than its body's %g", b.Mob, b.Radius, bodyRadius(boss))
		}
	}
	return nil
}

// bodyRadius is a mob's cylinder: width ÷ 64, half its width in metres.
func bodyRadius(m gamedata.Mob) float64 { return m.Width / pxPerMetre / 2 }

// spots3D checks every spot stands on ground a body fits on inside the map, and that mobs keep off portals.
func (l *loader) spots3D(m *maps3d.Map) error {
	player := l.movement3d.Radius
	circle := func(what string, at [2]float64, radius, body float64) error {
		if !(radius > 0) || at[0]-radius < 0 || at[1]-radius < 0 || at[0]+radius > m.Size[0] || at[1]+radius > m.Size[1] {
			return fmt.Errorf("%s: its circle must have a positive radius and lie inside the map", what)
		}
		if !nav.FitsAt(m.Geometry, l.movement3d.Step, maps3d.Point(at), body) {
			return fmt.Errorf("%s: its centre %v isn't ground a body %g m across fits on", what, at, 2*body)
		}
		return nil
	}
	offPortals := func(what string, at [2]float64, radius float64) error {
		for _, p := range m.Portals {
			if nav.Distance(maps3d.Point(at), maps3d.Point(p.At)) < radius+p.Radius {
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
		if err := circle(what, s.At, s.Radius, bodyRadius(mob)); err != nil {
			return err
		}
		if err := offPortals(what, s.At, s.Radius); err != nil {
			return err
		}
	}
	if b := m.Boss; b != nil {
		boss, _ := l.mobs.get(b.Mob)
		what := "boss " + b.Mob
		if err := circle(what, b.At, b.Radius, bodyRadius(boss)); err != nil {
			return err
		}
		if err := offPortals(what, b.At, b.Radius); err != nil {
			return err
		}
	}
	if !nav.FitsAt(m.Geometry, l.movement3d.Step, maps3d.Point(m.PlayerSpawn.At), player) {
		return fmt.Errorf("player_spawn %v isn't ground a player fits on", m.PlayerSpawn.At)
	}
	for _, n := range m.NPCs {
		if !l.npcs.has(n.ID) {
			return fmt.Errorf("npc %q is not in %s", n.ID, npcsFile)
		}
		if !nav.FitsAt(m.Geometry, l.movement3d.Step, maps3d.Point(n.At), player) {
			return fmt.Errorf("npc %s at %v isn't ground a player fits on", n.ID, n.At)
		}
	}
	return nil
}

// linkMaps3D checks across maps: portals lead somewhere, each boss and NPC is on one map, towns are towns, and one town is the start.
func (l *loader) linkMaps3D() error {
	bossOn, npcOn := map[string]string{}, map[string]string{}
	start := ""
	for _, m := range l.maps3d.list {
		file := maps3dDir + "/" + m.ID + map3dExt
		for _, p := range m.Portals {
			target, ok := l.maps3d.get(p.To.Map)
			if !ok {
				return fmt.Errorf("%s: portal %q: target map %q does not exist", file, p.Name, p.To.Map)
			}
			if !slices.ContainsFunc(target.Portals, func(q maps3d.Portal) bool { return q.Name == p.To.Portal }) {
				return fmt.Errorf("%s: portal %q: target portal %q not in map %q", file, p.Name, p.To.Portal, p.To.Map)
			}
		}
		if town, ok := l.maps3d.get(m.Town); !ok || town.Kind != string(gamedata.Town) {
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
			return fmt.Errorf("%s: boss %s: no map places it", maps3dDir, mob.ID)
		}
	}
	for _, n := range l.npcs.list {
		if _, ok := npcOn[n.ID]; !ok {
			return fmt.Errorf("%s: npc %s stands on no map", maps3dDir, n.ID)
		}
	}
	if start == "" {
		return fmt.Errorf("%s: no town has start, where new characters start", maps3dDir)
	}
	return nil
}
