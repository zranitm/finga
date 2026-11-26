package load

import (
	"fmt"
	"math"
	"slices"
	"time"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/mapfile"
)

const (
	worldFile = "world.yaml"
	chartSize = 100.0 // places run 0 to 100 across and down
	placeGap  = 6.0   // the least distance between two places, so the chart's names don't overlap

	longestChannel = 10 * time.Second // a teleport's channel at most
)

// worldYAML is the world chart, each map's place, the client's to draw and the server only checks; how long a teleport channels; and the interest cell.
type worldYAML struct {
	Places   map[string][2]float64 `yaml:"places"`
	Teleport struct {
		Channel time.Duration `yaml:"channel"`
	} `yaml:"teleport"`
	Interest struct {
		Cell   float64 `yaml:"cell"`
		Behind int     `yaml:"behind"`
		Ahead  int     `yaml:"ahead"`
		Side   int     `yaml:"side"`
	} `yaml:"interest"`
}

// loadWorld checks world.yaml: every map placed once, on the chart, and no two maps on the same spot; a teleport's channel over 0 and at most longestChannel.
func (l *loader) loadWorld(dir string) error {
	var y worldYAML
	if err := readYAML(dir, worldFile, &y); err != nil {
		return err
	}
	if c := y.Teleport.Channel; c <= 0 || c > longestChannel {
		return fmt.Errorf("%s: teleport.channel %v must be over 0 and at most %v", worldFile, c, longestChannel)
	}
	l.teleportChannel = y.Teleport.Channel
	in := y.Interest
	if !positive(in.Cell) {
		return fmt.Errorf("%s: interest.cell must be positive", worldFile)
	}
	if in.Behind < 1 || in.Ahead < 1 || in.Side < 1 {
		return fmt.Errorf("%s: interest.behind, ahead and side must each be at least 1", worldFile)
	}
	if far, what := l.farthestReach(); far > float64(in.Behind)*in.Cell {
		return fmt.Errorf("%s: %s reaches %g m, past the %g m the rows behind a player cover (interest.behind × cell)", worldFile, what, far, float64(in.Behind)*in.Cell)
	}
	l.interest = gamedata.Interest{Cell: in.Cell, Behind: in.Behind, Ahead: in.Ahead, Side: in.Side}
	for _, m := range l.maps.list {
		if m.Kind == string(gamedata.Region) && l.interest.Whole(l.interest.Cells(m.Size[0], m.Size[1])) {
			return fmt.Errorf("%s: one interest block holds it from every cell, so it would be sent whole; a region is wider than %s's block", fileOf(m), worldFile)
		}
	}
	ids := make([]string, 0, len(y.Places))
	for id, p := range y.Places {
		m, ok := l.maps.get(id)
		if !ok {
			return fmt.Errorf("%s: a place for %s, which is no map", worldFile, id)
		}
		if !charted(m) && !l.onLand(m) {
			return fmt.Errorf("%s: a place for %s, a %s map, which no chart shows", worldFile, id, m.Kind)
		}
		if p[0] < 0 || p[0] > chartSize || p[1] < 0 || p[1] > chartSize {
			return fmt.Errorf("%s: %s's place %v is off the chart, 0 to %g", worldFile, id, p, chartSize)
		}
		ids = append(ids, id)
	}
	for _, m := range l.maps.list {
		if _, ok := y.Places[m.ID]; !ok && charted(m) {
			return fmt.Errorf("%s: map %s has no place", worldFile, m.ID)
		}
	}
	slices.Sort(ids) // so the same file names the same pair
	for i, a := range ids {
		for _, b := range ids[i+1:] {
			pa, pb := y.Places[a], y.Places[b]
			if math.Hypot(pa[0]-pb[0], pa[1]-pb[1]) < placeGap {
				return fmt.Errorf("%s: %s and %s are closer than %g apart", worldFile, a, b, placeGap)
			}
		}
	}
	return nil
}

// farthestReach is the longest a mob sees, or anything a mob or player does reaches, from its body's middle, and what it is: a player is sent only the cells round them, so nothing may act from past the rows behind them.
func (l *loader) farthestReach() (float64, string) {
	far, what := l.mobRules.Sight, "a mob's sight"
	longer := func(d float64, name string) {
		if d > far {
			far, what = d, name
		}
	}
	for _, m := range l.mobs.list {
		for _, a := range m.Attacks {
			name := fmt.Sprintf("mob %s's %s", m.ID, a.Name)
			switch a.Kind {
			case gamedata.Front:
				longer(m.Radius+a.Reach, name)
			case gamedata.Leap:
				longer(a.Distance+m.Radius+a.Spread, name)
			case gamedata.Rush:
				longer(a.Distance+m.Radius, name)
			case gamedata.Shot:
				longer(a.Range+a.Size/2, name)
			case gamedata.Around:
				longer(m.Radius+a.Spread, name)
			}
		}
	}
	for _, c := range l.classes.list {
		longer(shapeReach(c.Attack.Shape), fmt.Sprintf("class %s's attack", c.ID))
	}
	for _, s := range l.skills.list {
		for _, st := range s.Stages {
			longer(shapeReach(st.Shape), fmt.Sprintf("skill %s", s.ID))
		}
	}
	return far, what
}

// shapeReach is how far from its user an action's shape finds a target.
func shapeReach(s gamedata.Shape) float64 {
	switch s.Kind {
	case gamedata.ArcShape:
		return s.Radius
	case gamedata.ShotShape:
		return s.Range
	case gamedata.AreaShape:
		return s.Radius + s.Reach
	}
	return 0
}

// onLand is whether m is a region on the land, which may have a place on the world chart; a place below, a test region and a match's map need none.
func (l *loader) onLand(m *mapfile.Map) bool {
	r, ok := l.regions[m.ID]
	return ok && r.under == ""
}

// charted is whether m must have a place on the world chart: a town or a hunting map, not a match's map or a region.
func charted(m *mapfile.Map) bool {
	return m.Kind == string(gamedata.Town) || m.Kind == string(gamedata.Hunting)
}
