package load

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
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
	return shared([sha256.Size]byte(h.Sum(nil)), func() (sim.Geometry, string, error) {
		r, err := regiongen.Generate(p, kit, maxSlope)
		if err != nil {
			return sim.Geometry{}, "", err
		}
		b, err := r.Bake()
		if err != nil {
			return sim.Geometry{}, "", err
		}
		return r.Geometry, b.Folder(), nil
	})
}

// shared is build's ground and folder for key, built once in the process; a panic in build is its error, for that load and every later one.
func shared(key [sha256.Size]byte, build func() (sim.Geometry, string, error)) (sim.Geometry, string, error) {
	v, _ := generated.LoadOrStore(key, &generation{})
	g := v.(*generation)
	g.once.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				g.err = fmt.Errorf("generating it panicked: %v", r)
			}
		}()
		g.geometry, g.folder, g.err = build()
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
	return l.uniqueWaypoints()
}

// uniqueWaypoints refuses one waypoint id in two regions: a teleport, a find and a character's row name a waypoint by its id alone.
func (l *loader) uniqueWaypoints() error {
	in := map[string]string{}
	for _, id := range slices.Sorted(maps.Keys(l.regions)) {
		for _, w := range l.regions[id].waypoints {
			if other, ok := in[w.ID]; ok {
				return fmt.Errorf("%s: waypoint %s is in %s too; a waypoint's id names it across every region", regionsDir, w.ID, other)
			}
			in[w.ID] = id
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
	grid, bosses, err := l.regionRules(p, m)
	if err != nil {
		return err
	}
	if err := l.spots(m, bosses); err != nil {
		return err
	}
	waypoints, err := l.waypointsOf(p, m, grid)
	if err != nil {
		return err
	}
	l.maps.add(id, m)
	if l.regions == nil {
		l.regions = map[string]regionParts{}
	}
	l.regions[id] = partsOf(p, grid, folder, waypoints)
	return nil
}

// arriveAt is how far from its stone a waypoint's arrival stands, in metres.
const arriveAt = 2

// around is the eight ways from a stone, south first, the camera's side, and north last: the camera looks north, so a stone south of one who arrives would stand between them.
var around = [8][2]float64{{0, -1}, {math.Sqrt2 / 2, -math.Sqrt2 / 2}, {-math.Sqrt2 / 2, -math.Sqrt2 / 2}, {1, 0}, {-1, 0}, {math.Sqrt2 / 2, math.Sqrt2 / 2}, {-math.Sqrt2 / 2, math.Sqrt2 / 2}, {0, 1}}

// waypointsOf is a region's waypoints as players use them: each one's arrival, the first of the spots arriveAt round its stone, in around's order, that a player fits on in the stone's own area, facing north, the camera's way; and those in its main town, the town holding its start, which a region with towns starts in.
func (l *loader) waypointsOf(p regiongen.Plan, m *mapfile.Map, areas regiongen.AreaGrid) ([]gamedata.Waypoint, error) {
	main := areas.Holding(p.Start.At[0], p.Start.At[1])
	if main >= 0 && p.Areas[main].Kind != regiongen.Town {
		main = -1
	}
	if main < 0 && slices.ContainsFunc(p.Areas, func(a regiongen.Area) bool { return a.Kind == regiongen.Town }) {
		return nil, fmt.Errorf("the start at %v stands in no town; a region with towns starts in its main town, which anyone may teleport to", p.Start.At)
	}
	out := make([]gamedata.Waypoint, 0, len(p.Waypoints))
	for _, w := range p.Waypoints {
		area := areas.Holding(w.At[0], w.At[1])
		k := slices.IndexFunc(around[:], func(d [2]float64) bool {
			at := [2]float64{w.At[0] + arriveAt*d[0], w.At[1] + arriveAt*d[1]}
			return areas.Holding(at[0], at[1]) == area && nav.FitsAt(m.Geometry, l.movement.Step, mapfile.Point(at), l.movement.Radius)
		})
		if k < 0 {
			return nil, fmt.Errorf("waypoint %s at %v has no spot %d m round it in its area that a player fits on, to arrive at", w.ID, w.At, arriveAt)
		}
		d := around[k]
		out = append(out, gamedata.Waypoint{
			ID: w.ID, Name: w.Name, At: gamedata.Point{X: w.At[0], Z: w.At[1]},
			Arrive: gamedata.Point{X: w.At[0] + arriveAt*d[0], Z: w.At[1] + arriveAt*d[1]}, Yaw: 0,
			Main: main >= 0 && area == main,
		})
	}
	return out, nil
}

// regionParts is what a region adds to its map: its bake folder, its areas and where they lie, and its waypoints.
type regionParts struct {
	bake      string
	areas     []gamedata.Area
	grid      regiongen.AreaGrid
	waypoints []gamedata.Waypoint
}

func partsOf(p regiongen.Plan, grid regiongen.AreaGrid, bake string, waypoints []gamedata.Waypoint) regionParts {
	out := regionParts{bake: bake, grid: grid, waypoints: waypoints}
	for _, a := range p.Areas {
		area := gamedata.Area{ID: a.ID, Name: a.Name, Kind: gamedata.MapKind(a.Kind), MinLevel: a.Levels[0], MaxLevel: a.Levels[1]}
		if b := a.Boss; b != nil {
			area.Boss = &gamedata.BossSpot{Mob: b.Mob, At: gamedata.Point{X: b.At[0], Z: b.At[1]}, Radius: b.Radius}
		}
		out.areas = append(out.areas, area)
	}
	return out
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

// regionRules are a region's areas and what stands in them, with where its areas lie and its bosses for the spot checks: every square inside the edge in one area; a hunting area with levels and its own boss, a town with neither and a waypoint, where its dead get up; camps wholly in their hunting area, of its levels; waypoints where a player fits; no spot in the ring.
func (l *loader) regionRules(p regiongen.Plan, m *mapfile.Map) (regiongen.AreaGrid, []mapfile.Boss, error) {
	areas, err := p.AreaGrid()
	if err != nil {
		return areas, nil, err
	}
	bosses, err := l.regionSpots(p, m, areas)
	return areas, bosses, err
}

func (l *loader) regionSpots(p regiongen.Plan, m *mapfile.Map, areas regiongen.AreaGrid) ([]mapfile.Boss, error) {
	if p.Name == "" {
		return nil, errors.New("needs a name, the one players see")
	}
	if err := uniquePortals(m.Portals); err != nil {
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
		if err := l.areaRules(p, a, i, areas); err != nil {
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
		}
		if err := within(p, areas, c.At, c.Radius, i); err != nil {
			return nil, fmt.Errorf("camp %q: %w", c.Name, err)
		}
		camps[i] = append(camps[i], m.Spawns[k])
	}
	for i, a := range p.Areas {
		if err := l.spawnRules(camps[i], a.Levels, "area"); err != nil {
			return nil, fmt.Errorf("area %s: %w", a.ID, err)
		}
	}
	type spot struct {
		what string
		at   [2]float64
	}
	spots := []spot{{"the start", p.Start.At}}
	for _, w := range p.Waypoints {
		spots = append(spots, spot{"waypoint " + w.ID, w.At})
	}
	for _, n := range p.NPCs {
		spots = append(spots, spot{"NPC " + n.ID, n.At})
	}
	for _, g := range p.Gates {
		spots = append(spots, spot{"gate " + g.Name, g.At})
	}
	for _, s := range spots {
		if areas.Holding(s.at[0], s.at[1]) < 0 {
			return nil, fmt.Errorf("%s at %v stands in the ring, which no player reaches", s.what, s.at)
		}
	}
	seen := map[string]bool{}
	for _, w := range p.Waypoints {
		if seen[w.ID] {
			return nil, fmt.Errorf("two waypoints are %s", w.ID)
		}
		seen[w.ID] = true // its stone's solid stands on its spot; its arrival beside it must fit (waypointsOf)
	}
	for i, a := range p.Areas {
		if a.Kind == regiongen.Town && !slices.ContainsFunc(p.Waypoints, func(w regiongen.Waypoint) bool { return areas.Holding(w.At[0], w.At[1]) == i }) {
			return nil, fmt.Errorf("town %s holds no waypoint; the region's dead get up at its waypoints", a.ID)
		}
	}
	return bosses, nil
}

// areaRules are an area's own: a name, its straight edges on the squares' lines, and a hunting area's levels and boss, its circle wholly in it, or a town's none.
func (l *loader) areaRules(p regiongen.Plan, a regiongen.Area, i int, areas regiongen.AreaGrid) error {
	if a.Name == "" {
		return errors.New("needs a name, the one players see")
	}
	if e, off := offLines(a.Area); off {
		return fmt.Errorf("its edge from %v to %v is off the %d m lines its squares play on, so it would play up to a square from where it's drawn", e[0], e[1], regiongen.AreaCell)
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
	}
	if err := within(p, areas, b.At, b.Radius, i); err != nil {
		return fmt.Errorf("its boss %s: %w", b.Mob, err)
	}
	return l.bossRules(mapfile.Boss{At: b.At, Radius: b.Radius, Mob: b.Mob}, a.Levels[1], "area")
}

// offLines is a polygon's first east-west or north-south edge whose line isn't a multiple of AreaCell.
func offLines(poly [][2]float64) ([2][2]float64, bool) {
	on := func(v float64) bool { return math.Mod(v, regiongen.AreaCell) == 0 }
	for k, a := range poly {
		if b := poly[(k+1)%len(poly)]; a[0] == b[0] && !on(a[0]) || a[1] == b[1] && !on(a[1]) {
			return [2][2]float64{a, b}, true
		}
	}
	return [2][2]float64{}, false
}

// within is nil when the circle of radius r at at lies wholly in area i: every square it overlaps is i's, so none of it reaches another area's band or the ring.
func within(p regiongen.Plan, areas regiongen.AreaGrid, at [2]float64, r float64, i int) error {
	for _, a := range areas.Under(at[0], at[1], r) {
		switch {
		case a < 0:
			return fmt.Errorf("its circle at %v, %g m round, reaches the ring, outside area %s", at, r, p.Areas[i].ID)
		case a != i:
			return fmt.Errorf("its circle at %v, %g m round, reaches area %s, outside area %s", at, r, p.Areas[a].ID, p.Areas[i].ID)
		}
	}
	return nil
}
