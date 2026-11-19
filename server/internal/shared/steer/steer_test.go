package steer

import (
	"math"
	"testing"
	"time"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
)

func shippedMap(t *testing.T, id string) (*gamedata.Map, float64) {
	t.Helper()
	data := gamedatatest.Shipped(t)
	m, ok := data.Map(id)
	if !ok {
		t.Fatalf("no map %s", id)
	}
	return m, data.Movement().Radius
}

func TestKeysWalkTheWayNearestTheWayWanted(t *testing.T) {
	for _, c := range []struct {
		dx, dz float64
		want   sim.Input
	}{
		{1, 0, sim.Input{Right: true}},
		{0, -2, sim.Input{Down: true}},
		{1, 1, sim.Input{Right: true, Up: true}},
		{-1, -1, sim.Input{Left: true, Down: true}},
		{1, 0.3, sim.Input{Right: true}},           // 17° off east
		{1, 0.5, sim.Input{Right: true, Up: true}}, // 27° off east, nearer north-east
		{0, 0, sim.Input{}},
	} {
		if got := KeysToward(c.dx, c.dz); got != c.want {
			t.Errorf("toward (%v, %v) presses %+v, want %+v", c.dx, c.dz, got, c.want)
		}
	}
	if a := YawTo(nav.Point{}, nav.Point{X: 0, Z: 1}); a != 0 {
		t.Errorf("north aims %v", a)
	}
	if a := YawTo(nav.Point{}, nav.Point{X: 1, Z: 0}); math.Abs(float64(a)-math.Pi/2) > 1e-6 {
		t.Errorf("east aims %v, want π/2", a)
	}
}

func TestAStallIsAWayPressedOnTheGroundThatGoesNowhere(t *testing.T) {
	right, still := sim.Input{Right: true}, sim.Input{}
	at := nav.Point{X: 5, Z: 5}
	for _, c := range []struct {
		name     string
		onGround bool
		held     sim.Input
		want     bool
	}{
		{"against a wall", true, right, true},
		{"in the air, its updates late", false, right, false},
		{"standing to attack", true, still, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			var s Stall
			s.Stalled(at, c.onGround, c.held) // the way pressed, which counts from the next decision
			for k := range 2 * stallFor {
				if got := s.Stalled(at, c.onGround, c.held); got != (c.want && k%stallFor == stallFor-1) {
					t.Fatalf("decision %d: stalled %v; a stall is told once, then counted again", k+1, got)
				}
			}
		})
	}
	var s Stall
	for range stallFor {
		s.Stalled(at, true, right)
	}
	for k := range stallFor - 1 {
		if s.Stalled(nav.Point{X: 5.1, Z: 5}, true, right) {
			t.Fatalf("decision %d after a step on: stalled; the step starts the count again", k+1)
		}
	}
	s = Stall{}
	for range stallFor {
		s.Stalled(at, true, right)
	}
	if s.Stalled(at, true, sim.Input{Up: true}) {
		t.Fatal("stalled the decision a new way was pressed; its first steps take a round trip to show")
	}
}

func TestAStalledWalkerJumpsThenStepsAsideEachSideInTurn(t *testing.T) {
	m, r := shippedMap(t, "town")
	from, now := nav.Point{X: m.PlayerSpawn.X, Z: m.PlayerSpawn.Z}, 1000*time.Second
	to := nav.Point{X: from.X + 4, Z: from.Z}
	var wk Walker
	ahead := wayOf(wk.Keys(m, r, from, to, now, false))
	jump := wk.Keys(m, r, from, to, now, true)
	if !jump.Jump || wayOf(jump) != ahead {
		t.Fatalf("the first stall presses %+v, want a jump ahead %v", jump, ahead)
	}
	aside := wayOf(wk.Keys(m, r, from, to, now, true))
	if aside == ([2]int{}) || aside[0]*ahead[0]+aside[1]*ahead[1] != 0 {
		t.Fatalf("the second stall steps %v, want across the way ahead %v", aside, ahead)
	}
	for k := 1; k < asideFor; k++ {
		if got := wayOf(wk.Keys(m, r, from, to, now, false)); got != aside {
			t.Fatalf("decision %d of the step aside presses %v, want %v", k+1, got, aside)
		}
	}
	if got := wayOf(wk.Keys(m, r, from, to, now, false)); got != ahead {
		t.Fatalf("after the step aside it presses %v, want %v", got, ahead)
	}
	if !wk.Keys(m, r, from, to, now, true).Jump {
		t.Fatal("the third stall doesn't jump")
	}
	if got := wayOf(wk.Keys(m, r, from, to, now, true)); got != [2]int{-aside[0], -aside[1]} {
		t.Fatalf("the fourth stall steps %v, want the other side of %v", got, aside)
	}
}

func TestAWalkerWithNoWayWalksStraightAndLooksAgainASecondOn(t *testing.T) {
	m, r := shippedMap(t, "forest")
	from, onTop, now := nav.Point{X: 40, Z: 28}, nav.Point{X: 50, Z: 28}, 1000*time.Second // a ledge only a jump gets onto
	var wk Walker
	if got := wayOf(wk.Keys(m, r, from, onTop, now, false)); !wk.Lost() || got != [2]int{1, 0} {
		t.Fatalf("lost %v, pressing %v; want lost, straight east", wk.Lost(), got)
	}
	if wk.Keys(m, r, from, onTop, now+repathEvery/2, true); wk.found != now {
		t.Fatal("a stall with no way looked again within the second")
	}
	if wk.Keys(m, r, from, nav.Point{X: 30, Z: 20}, now+repathEvery/2, false); wk.Lost() {
		t.Fatal("still lost once its end moved to open ground")
	}
}
