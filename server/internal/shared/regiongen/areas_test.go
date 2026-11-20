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

// A circle is under the areas of every square it overlaps, each once; -1 for the ring and past the side; a square it meets only at an edge is out.
func TestACircleIsUnderTheAreasOfEverySquareItOverlaps(t *testing.T) {
	p := small(t)
	g, err := p.AreaGrid()
	if err != nil {
		t.Fatal(err)
	}
	ids := func(under []int) []string {
		var out []string
		for _, a := range under {
			if a < 0 {
				out = append(out, "")
				continue
			}
			out = append(out, p.Areas[a].ID)
		}
		return out
	}
	for _, tt := range []struct {
		x, z, r float64
		want    []string
	}{
		{120, 80, 6, []string{"town"}},
		{91, 64, 2, []string{"west", "town"}},   // the town's west side at x = 90, its squares from 92
		{90, 66, 2, []string{"west"}},           // meets the town's square at x = 92 and the squares at z = 64's lines only at their edges
		{26, 100, 3, []string{"", "west"}},      // into the ring's squares, x below 24
		{2, 2, 5, []string{""}},                 // past the region's side and the ring
		{100, 58, 3, []string{"south", "town"}}, // across the line at z = 60
	} {
		if got := ids(g.Under(tt.x, tt.z, tt.r)); !slices.Equal(got, tt.want) {
			t.Errorf("a circle at (%g, %g), %g m round, is under %q; want %q", tt.x, tt.z, tt.r, got, tt.want)
		}
	}
}

// Diagonal squares count only where the circle reaches into them: on a 3 by 3 grid of one area a square, numbered from the south-west.
func TestACircleIsUnderADiagonalSquareOnlyWhereItReachesIn(t *testing.T) {
	g := AreaGrid{NX: 3, NZ: 3, Of: []int16{0, 1, 2, 3, 4, 5, 6, 7, 8}}
	for _, tt := range []struct {
		x, z, r float64
		want    []int
	}{
		{5, 5, 1.2, []int{1, 3, 4}},                  // 1 m from the south and west squares, √2 m from the south-west one's corner
		{7, 7, 1.2, []int{4, 5, 7}},                  // likewise north-east
		{5, 5, 1.5, []int{0, 1, 3, 4}},               // √2 m < 1.5 m: into the south-west square
		{5, 4, 5, []int{-1, 0, 1, 2, 3, 4, 5, 6, 7}}, // 3 m east and 4 m north of the north-east square's corner: on its edge, not in it
	} {
		if got := g.Under(tt.x, tt.z, tt.r); !slices.Equal(got, tt.want) {
			t.Errorf("a circle at (%g, %g), %g m round, is under %v; want %v", tt.x, tt.z, tt.r, got, tt.want)
		}
	}
}

// A region's size is whole map tiles, so whole area squares too.
func TestATileIsWholeAreaSquares(t *testing.T) {
	if tileM%AreaCell != 0 {
		t.Fatalf("a %d m tile isn't whole %d m squares", tileM, AreaCell)
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
