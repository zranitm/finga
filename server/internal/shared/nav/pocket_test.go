package nav_test

import (
	"testing"

	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/mapfile"
	"finga/internal/shared/nav"
	"finga/internal/shared/regiongen"
)

// No region has a one-way pocket (from 4b-2b's playtest, whose riverbanks a player walks down but not up): every cell a player reaches from a shipped region's start leads back to it. A pit would fail it, as one here does.
func TestNoRegionHasAOneWayPocket(t *testing.T) {
	data := gamedatatest.Shipped(t)
	p := shipped(t).params
	rise := fastRise(data, p)
	for id, plan := range shippedRegions(t) {
		t.Run(id, func(t *testing.T) {
			m, _ := data.Map(id)
			if got := m.Grid().Stranded(mapfile.Point(plan.Start.At), p.Radius, rise); len(got) > 0 {
				t.Fatalf("%d cells reached from the start lead back to it by no steps, such as %v", len(got), got[:min(len(got), 5)])
			}
		})
	}
	banked := func(banks string) regiongen.Plan {
		plan := flatRegion()
		plan.Features = []regiongen.Feature{
			{Plateau: &regiongen.Plateau{Area: [][2]float64{{0, 0}, {128, 0}, {128, 40}, {0, 40}}, Height: 6, Edge: regiongen.Ramp}},
			{River: &regiongen.River{Path: [][2]float64{{0, 70}, {64, 80}, {128, 70}}, Width: 8, Depth: 2, Banks: banks}},
			{Lake: &regiongen.Lake{Area: [][2]float64{{80, 20}, {110, 20}, {110, 45}, {80, 45}}, Depth: 3, Banks: banks}},
		}
		return plan
	}
	stranded := func(t *testing.T, plan regiongen.Plan) int {
		r, err := regiongen.Generate(plan, shippedKit(t), p)
		if err != nil {
			t.Fatal(err)
		}
		return len(nav.New(r.Geometry, p.Step).Stranded(mapfile.Point(plan.Start.At), p.Radius, rise))
	}
	t.Run("steep banks", func(t *testing.T) {
		if n := stranded(t, banked("")); n == 0 {
			t.Fatal("no cell is stranded at steep banks")
		}
	})
	t.Run("gentle banks", func(t *testing.T) {
		if n := stranded(t, banked(regiongen.GentleBanks)); n != 0 {
			t.Fatalf("%d cells reached from the start lead back to it by no steps, with gentle banks", n)
		}
	})
	t.Run("a pit", func(t *testing.T) {
		plan := flatRegion()
		plan.Features = []regiongen.Feature{{Plateau: &regiongen.Plateau{Area: [][2]float64{{40, 40}, {56, 40}, {56, 56}, {40, 56}}, Height: -4, Edge: regiongen.Cliff}}}
		r, err := regiongen.Generate(plan, shippedKit(t), p)
		if err != nil {
			t.Fatal(err)
		}
		grid := nav.New(r.Geometry, p.Step)
		start, floor := mapfile.Point(plan.Start.At), nav.Point{X: 48.25, Z: 48.25}
		if !grid.Reach([]nav.Point{start}, p.Radius, rise).Has(floor) {
			t.Fatal("the pit's floor isn't reached")
		}
		got := grid.Stranded(start, p.Radius, rise)
		found := false
		for _, at := range got {
			found = found || at == floor
		}
		if !found || len(got) < 32*32 {
			t.Fatalf("%d cells stranded, the pit's floor among them %v; want at least its floor's 32 by 32", len(got), found)
		}
	})
}
