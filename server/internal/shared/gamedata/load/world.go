package load

import (
	"fmt"
	"math"
	"slices"
)

const (
	worldFile = "world.yaml"
	chartSize = 100.0 // places run 0 to 100 across and down
	placeGap  = 6.0   // the least distance between two places, so the chart's names don't overlap
)

// worldYAML is the world chart: each map's place, the client's to draw; the server only checks it.
type worldYAML struct {
	Places map[string][2]float64 `yaml:"places"`
}

// loadWorld checks world.yaml: every map placed once, on the chart, and no two maps on the same spot.
func (l *loader) loadWorld(dir string) error {
	var y worldYAML
	if err := readYAML(dir, worldFile, &y); err != nil {
		return err
	}
	ids := make([]string, 0, len(y.Places))
	for id, p := range y.Places {
		if !l.maps.has(id) {
			return fmt.Errorf("%s: a place for %s, which is no map", worldFile, id)
		}
		if p[0] < 0 || p[0] > chartSize || p[1] < 0 || p[1] > chartSize {
			return fmt.Errorf("%s: %s's place %v is off the chart, 0 to %g", worldFile, id, p, chartSize)
		}
		ids = append(ids, id)
	}
	for _, m := range l.maps.list {
		if _, ok := y.Places[m.ID]; !ok {
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
