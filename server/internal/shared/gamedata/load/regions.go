package load

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/mapfile"
	"finga/internal/shared/nav"
	"finga/internal/shared/regiongen"
	"finga/internal/shared/sim"
)

const (
	regionsDir = "regions"
	kitFile    = "kit.yaml"
)

// generated shares each region's ground and bake by its plan, kit and slope across the process's loads, as mapfile shares walk grids, so end-to-end stacks don't each generate every region.
var generated sync.Map // [sha256.Size]byte → *generation

type generation struct {
	once     sync.Once
	geometry sim.Geometry
	folder   string
	err      error
}

// generate is p's ground and its bake's folder, made once in the process for every load of the same plan, kit and slope.
func generate(p regiongen.Plan, raw, kitRaw []byte, kit regiongen.Kit, maxSlope float64) (sim.Geometry, string, error) {
	h := sha256.New()
	for _, part := range [][]byte{raw, kitRaw, binary.LittleEndian.AppendUint64(nil, math.Float64bits(maxSlope))} {
		h.Write(binary.LittleEndian.AppendUint64(nil, uint64(len(part))))
		h.Write(part)
	}
	shared, _ := generated.LoadOrStore([sha256.Size]byte(h.Sum(nil)), &generation{})
	g := shared.(*generation)
	g.once.Do(func() {
		r, err := regiongen.Generate(p, kit, maxSlope)
		if err != nil {
			g.err = err
			return
		}
		b, err := r.Bake()
		if err != nil {
			g.err = err
			return
		}
		g.geometry, g.folder = r.Geometry, b.Folder()
	})
	return g.geometry, g.folder, g.err
}

// loadRegions generates each region in data/regions from its plan, as a map of kind region, and checks it by the map rules that fit and its areas' own (large-regions.md section 3); a data folder may have none.
func (l *loader) loadRegions(dir string) error {
	entries, err := os.ReadDir(filepath.Join(dir, regionsDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s: %w", regionsDir, err)
	}
	var kit *regiongen.Kit
	var kitRaw []byte
	for _, e := range entries { // ReadDir sorts by name
		if e.IsDir() || filepath.Ext(e.Name()) != mapExt {
			continue
		}
		if kit == nil {
			raw, err := os.ReadFile(filepath.Join(dir, kitFile))
			if err != nil {
				return fmt.Errorf("%s: %w", kitFile, err)
			}
			k, err := regiongen.DecodeKit(raw)
			if err != nil {
				return fmt.Errorf("%s: %w", kitFile, err)
			}
			kit, kitRaw = &k, raw
		}
		file := filepath.Join(regionsDir, e.Name())
		raw, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		if err := l.loadRegion(strings.TrimSuffix(e.Name(), mapExt), raw, kitRaw, *kit); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
	}
	return nil
}

func (l *loader) loadRegion(id string, raw, kitRaw []byte, kit regiongen.Kit) error {
	if l.maps.has(id) {
		return fmt.Errorf("a map has the id %s too", id)
	}
	p, err := regiongen.Decode(raw)
	if err != nil {
		return err
	}
	geometry, folder, err := generate(p, raw, kitRaw, kit, l.movement.MaxSlope)
	if err != nil {
		return err
	}
	m := mapfile.Generated(id, regionFile(id, p), geometry, l.movement)
	bosses, err := l.regionRules(p, m)
	if err != nil {
		return err
	}
	if err := l.spots(m, bosses); err != nil {
		return err
	}
	l.maps.add(id, m)
	if l.bakes == nil {
		l.bakes = map[string]string{}
	}
	l.bakes[id] = folder
	return nil
}

// regionFile is a region's plan as the map rules read a map's file: its gates are portals, its camps spawns and its start the player spawn, and it's its own town.
func regionFile(id string, p regiongen.Plan) mapfile.File {
	f := mapfile.File{
		Name: p.Name, Kind: string(gamedata.Region), Town: id, Look: mapfile.Look{Sky: p.Look.Sky}, Size: p.Size,
		PlayerSpawn: mapfile.Place{At: p.Start.At, Yaw: p.Start.Yaw},
	}
	for _, g := range p.Gates {
		f.Portals = append(f.Portals, mapfile.Portal{Name: g.Name, At: g.At, Radius: g.Radius, To: mapfile.Target{Map: g.To.Region, Portal: g.To.Gate}})
	}
	for _, c := range p.Camps {
		f.Spawns = append(f.Spawns, mapfile.Spawn{Name: c.Name, At: c.At, Radius: c.Radius, Mob: c.Mob, Max: c.Max, Respawn: c.Respawn})
	}
	for _, n := range p.NPCs {
		f.NPCs = append(f.NPCs, mapfile.NPC{ID: n.ID, At: n.At, Yaw: n.Yaw})
	}
	return f
}

// regionRules are a region's areas and what stands in them, and its bosses for the spot checks: every square inside the edge in one area; a hunting area with levels and its own boss, a town with neither; camps in their hunting area, of its levels; waypoints where a player fits.
func (l *loader) regionRules(p regiongen.Plan, m *mapfile.Map) ([]mapfile.Boss, error) {
	if p.Name == "" {
		return nil, errors.New("needs a name, the one players see")
	}
	if err := uniquePortals(m.Portals); err != nil {
		return nil, err
	}
	areas, err := p.AreaGrid()
	if err != nil {
		return nil, err
	}
	edge := p.Edge.Width
	for s, a := range areas.Of {
		x, z := float64(s%areas.NX*regiongen.AreaCell), float64(s/areas.NX*regiongen.AreaCell)
		if a < 0 && x >= edge && x < p.Size[0]-edge && z >= edge && z < p.Size[1]-edge {
			return nil, fmt.Errorf("the %d m square at [%g %g] is in no area; every square inside the edge is in one", regiongen.AreaCell, x, z)
		}
	}
	index := map[string]int{}
	bossOf := map[string]string{}
	var bosses []mapfile.Boss
	for i, a := range p.Areas {
		if _, ok := index[a.ID]; ok {
			return nil, fmt.Errorf("two areas are %s", a.ID)
		}
		index[a.ID] = i
		if err := l.areaRules(a, i, areas); err != nil {
			return nil, fmt.Errorf("area %s: %w", a.ID, err)
		}
		if b := a.Boss; b != nil {
			if other, ok := bossOf[b.Mob]; ok {
				return nil, fmt.Errorf("area %s: boss %s is area %s's already; each area has its own", a.ID, b.Mob, other)
			}
			bossOf[b.Mob] = a.ID
			bosses = append(bosses, mapfile.Boss{At: b.At, Radius: b.Radius, Mob: b.Mob})
		}
	}
	camps := make([][]mapfile.Spawn, len(p.Areas))
	for k, c := range p.Camps {
		i, ok := index[c.Area]
		switch {
		case !ok:
			return nil, fmt.Errorf("camp %q: no area %s", c.Name, c.Area)
		case p.Areas[i].Kind != regiongen.Hunting:
			return nil, fmt.Errorf("camp %q: area %s is a town, which has no camps", c.Name, c.Area)
		case areas.Holding(c.At[0], c.At[1]) != i:
			return nil, fmt.Errorf("camp %q: its centre %v isn't in area %s", c.Name, c.At, c.Area)
		}
		camps[i] = append(camps[i], m.Spawns[k])
	}
	for i, a := range p.Areas {
		if err := l.spawnRules(camps[i], a.Levels, "area"); err != nil {
			return nil, fmt.Errorf("area %s: %w", a.ID, err)
		}
	}
	seen := map[string]bool{}
	for _, w := range p.Waypoints {
		if seen[w.ID] {
			return nil, fmt.Errorf("two waypoints are %s", w.ID)
		}
		seen[w.ID] = true
		if !nav.FitsAt(m.Geometry, l.movement.Step, mapfile.Point(w.At), l.movement.Radius) {
			return nil, fmt.Errorf("waypoint %s at %v isn't ground a player fits on", w.ID, w.At)
		}
	}
	return bosses, nil
}

// areaRules are an area's own: a name, and a hunting area's levels and boss, standing in it, or a town's none.
func (l *loader) areaRules(a regiongen.Area, i int, areas regiongen.AreaGrid) error {
	if a.Name == "" {
		return errors.New("needs a name, the one players see")
	}
	if a.Kind == regiongen.Town {
		if a.Levels != [2]int{} || a.Boss != nil {
			return errors.New("a town has no levels and no boss")
		}
		return nil
	}
	b := a.Boss
	switch {
	case a.Levels[0] < 1 || a.Levels[1] < a.Levels[0]:
		return fmt.Errorf("levels %v: a hunting area's mob levels need 1 <= lowest <= highest", a.Levels)
	case b == nil:
		return errors.New("a hunting area has a boss")
	case areas.Holding(b.At[0], b.At[1]) != i:
		return fmt.Errorf("its boss %s stands at %v, outside it", b.Mob, b.At)
	}
	return l.bossRules(mapfile.Boss{At: b.At, Radius: b.Radius, Mob: b.Mob}, a.Levels[1], "area")
}
