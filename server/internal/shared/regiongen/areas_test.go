package regiongen

import (
	"slices"
	"strings"
	"testing"
)

// The small plan's areas tile the region inside its 24 m edge: every square there is held once, by the area holding its south-west corner.
func TestEachSquareIsHeldByTheAreaHoldingItsCorner(t *testing.T) {
	p := small(t)
	g, err := p.AreaGrid()
	if err != nil {
		t.Fatal(err)
	}
	if g.NX != 64 || g.NZ != 64 {
		t.Fatalf("%d by %d squares; want 64 by 64", g.NX, g.NZ)
	}
	for s, a := range g.Of {
		x, z := float64(s%g.NX*AreaCell), float64(s/g.NX*AreaCell)
		inside := x >= 24 && x < 232 && z >= 24 && z < 232
		if inside != (a >= 0) {
			t.Fatalf("the square at [%g %g] is held by %d", x, z, a)
		}
	}
	id := func(x, z float64) string {
		if a := g.Holding(x, z); a >= 0 {
			return p.Areas[a].ID
		}
		return ""
	}
	for _, tt := range []struct {
		x, z float64
		want string
	}{
		{88, 64, "west"}, {91.9, 64, "west"}, {92, 64, "town"}, // town's west side is at x = 90: the square at 88 has its corner west of it
		{148, 108, "town"}, {148, 112, "north"}, {152, 108, "woods"},
		{100, 56, "south"}, {100, 60, "town"}, // a shared side goes to the area north of it, as a sample on it does
		{-5, -5, ""}, {300, 300, ""}, {231.9, 231.9, "woods"},
	} {
		if got := id(tt.x, tt.z); got != tt.want {
			t.Errorf("(%g, %g) is in %q; want %q", tt.x, tt.z, got, tt.want)
		}
	}
}

func TestTwoAreasHoldingOneSquareAreRefused(t *testing.T) {
	p := small(t)
	p.Areas = slices.Clone(p.Areas)
	p.Areas[0].Area = [][2]float64{{86, 60}, {150, 60}, {150, 110}, {86, 110}} // the town reaches into West Field
	_, err := p.AreaGrid()
	if err == nil || !strings.Contains(err.Error(), "areas town and west both hold the 4 m square at [88 60]") {
		t.Fatalf("error %v", err)
	}
}
