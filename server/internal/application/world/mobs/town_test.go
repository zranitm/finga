package mobs_test

import (
	"math"
	"slices"
	"testing"

	"finga/internal/application/world/mobs"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/regiongen"
)

// withTown is m made a region of a town, on the 4 m squares whose south-west corner town holds, and a hunting area on the rest.
func withTown(m *gamedata.Map, town func(x, z float64) bool) *gamedata.Map {
	nx, nz := int(m.Geometry.SizeX/regiongen.AreaCell), int(m.Geometry.SizeZ/regiongen.AreaCell)
	grid := regiongen.AreaGrid{NX: nx, NZ: nz, Of: make([]int16, nx*nz)}
	for s := range grid.Of {
		if !town(float64(s%nx*regiongen.AreaCell), float64(s/nx*regiongen.AreaCell)) {
			grid.Of[s] = 1
		}
	}
	m.Kind, m.AreaGrid = gamedata.Region, grid
	m.Areas = []gamedata.Area{{ID: "town", Name: "Town", Kind: gamedata.Town}, {ID: "field", Name: "Field", Kind: gamedata.Hunting, MinLevel: 1, MaxLevel: 20}}
	return m
}

// southOf16 is a town on the ground south of z 16.
func southOf16(_, z float64) bool { return z < 16 }

// A chaser follows a player to a town's line and no further, its centre never in the town; with the player in, it gives up and walks home. Without the town, it follows them past where the line was.
func TestAChaserStopsAtATownsLineAndWalksHome(t *testing.T) {
	for _, town := range []bool{true, false} {
		m := meadow(t, open(), spawn("wild_boar", 1, 20, 24, 0.01))
		if town {
			m = withTown(m, southOf16)
		}
		a := newArena(t, m, worldtest.FixedRoll{V: 0.5})
		home := a.Views()[0]
		p, nearest := player(20, 21), math.Inf(1)
		for n := range uint64(600) {
			p.Z = max(21-float64(float64(n)*tick.Seconds()), 9) // walking south at 1 m/s, into the town and 7 m on
			a.Step(n+1, []mobs.Body{p}, nil)
			v := a.Views()[0]
			if town && v.Z < 16 {
				t.Fatalf("tick %d: the boar at (%.3f, %.3f), in the town", n+1, v.X, v.Z)
			}
			nearest = min(nearest, v.Z)
		}
		switch v := a.Views()[0]; {
		case !town && nearest > 14:
			t.Fatalf("without a town the boar came no further south than z %.2f; want it past z 14, so the line is what holds it", nearest)
		case town && nearest > 19:
			t.Fatalf("the boar came no nearer than z %.2f; want it to have followed the player toward the line", nearest)
		case town && distance(v, home.X, home.Z) > 0.5:
			t.Fatalf("the boar at (%.2f, %.2f) 30 s on; want it home at (%.2f, %.2f)", v.X, v.Z, home.X, home.Z)
		}
	}
}

// A mob leaves a player standing in a town be, however near: it doesn't come after them or attack, and wanders on as if nobody were there; even one who struck it, it lets go and walks home. Without the town it goes for them.
func TestAMobLeavesAPlayerInATownBe(t *testing.T) {
	for _, c := range []struct {
		name         string
		town, struck bool
		circle       float64
		roll         model.Roll
		wanders      bool
	}{
		{"near", true, false, 3, &cycle{v: []float64{0.1, 0.7, 0.4, 0.9, 0.25}}, true},
		{"who struck it", true, true, 0.01, worldtest.FixedRoll{V: 0.5}, false},
		{"without the town", false, false, 0.01, worldtest.FixedRoll{V: 0.5}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := meadow(t, open(), spawn("wild_boar", 1, 20, 20, c.circle))
			if c.town {
				m = withTown(m, southOf16)
			}
			a := newArena(t, m, c.roll)
			p := player(20, 15.9) // just in, in sight
			a.Step(1, []mobs.Body{p}, nil)
			start := a.Views()[0]
			if c.struck {
				s := nudge(p.X, p.Z)
				s.By, s.Shape.Radius = p.ID, 5
				one(t, a.Strike(s))
			}
			went, attacked := 0.0, false
			for n := range uint64(400) {
				a.Step(n+2, []mobs.Body{p}, nil)
				went = max(went, distance(a.Views()[0], start.X, start.Z))
				attacked = attacked || len(a.Started()) > 0
			}
			end := a.Views()[0]
			switch {
			case !c.town && !attacked:
				t.Fatal("without the town the boar never attacked the player; want it to, so the town is what stops it")
			case c.town && attacked:
				t.Fatal("the boar attacked a player in the town")
			case c.town && c.wanders && went < 0.5:
				t.Fatalf("the boar went at most %.2f m in 20 s; want it wandering its circle as if nobody were there", went)
			case c.town && end.Z < 16:
				t.Fatalf("the boar at (%.2f, %.2f), in the town", end.X, end.Z)
			case c.struck && distance(end, 20, 20) > 0.5:
				t.Fatalf("the boar at (%.2f, %.2f) 20 s on; want it home", end.X, end.Z)
			}
		})
	}
}

// A charge stops at a town's line, its end as its windup shows it and where it ends up, as a wall would stop it; without the town it runs its 4 m.
func TestAChargeStopsAtATownsLine(t *testing.T) {
	for _, town := range []bool{true, false} {
		m := meadow(t, open(), spawn("orange_mushroom", 1, 20, 19, 0.01))
		if town {
			m = withTown(m, southOf16)
		}
		a := newArena(t, m, worldtest.FixedRoll{V: 0.5})
		v := a.Views()[0]
		p := player(20, 16.4) // just out of the town, south of the mushroom
		at, next := windup(t, a, []mobs.Body{p}, 1)
		p.X = 25 // stepping out of its lane, so it runs to its end
		landed(a, []mobs.Body{p}, next, 24)
		end, want := a.Views()[0], v.Z-4
		if town {
			if at.ToZ < 16 || at.ToZ > 16+4.0/256+1e-9 {
				t.Fatalf("windup %+v; want its end at the town's line, z 16", at)
			}
			want = at.ToZ
		}
		if math.Abs(end.Z-want) > 1e-9 || math.Abs(end.X-20) > 1e-6 {
			t.Fatalf("town %v: windup %+v; the mushroom ended at (%v, %v); want z %v", town, at, end.X, end.Z, want)
		}
	}
}

// A blow's push stops at a town's line: a slime 0.2 m south of a town's edge, pushed 0.25 m north, ends just short of it.
func TestAPushStopsAtATownsLine(t *testing.T) {
	m := withTown(meadow(t, open(), spawn("green_slime", 1, 20, 19.8, 0.01)), func(_, z float64) bool { return z >= 20 })
	a := newArena(t, m, worldtest.FixedRoll{V: 0.5})
	a.Step(1, nil, nil)
	v := theMob(t, a)
	one(t, a.Strike(nudge(20, v.Z-1)))
	if got := theMob(t, a); got.Pose != model.Flinching || got.Z >= 20 || got.Z < 20-0.25/256 {
		t.Fatalf("slime %+v from z %v; want it flinching, pushed up to the town's line at z 20", got, v.Z)
	}
}

// Mobs coming at a player beside a town's line take spots out of the town, and settle there still, each biting or waiting its turn on a ring: none walks for a spot in the town, to be held at the line out of reach. Here the town is east of x 24, a player 1 m short of it, and boars come from the north, their outer ring's spots on the east in the town.
func TestChasersAtATownsLineTakeSpotsOutOfIt(t *testing.T) {
	a := newArena(t, withTown(meadow(t, open(), spawn("wild_boar", 6, 22, 27, 1.5)), func(x, _ float64) bool { return x >= 24 }), &cycle{v: []float64{0.1, 0.7, 0.4, 0.9, 0.25, 0.55, 0.8}})
	p := player(23, 20)
	for n := range uint64(400) {
		a.Step(n+1, []mobs.Body{p}, nil)
		for _, v := range a.Views() {
			if v.X >= 24 {
				t.Fatalf("tick %d: boar %d at (%.3f, %.3f), in the town", n+1, v.ID, v.X, v.Z)
			}
		}
	}
	settle(t, a, p, 0.875, boarReach, 401)
	for _, v := range a.Views() {
		if d := distance(v, p.X, p.Z); !slices.ContainsFunc(boarRings, func(r float64) bool { return math.Abs(d-r) < 1e-6 }) {
			t.Fatalf("boar %d at (%.3f, %.3f), %.3f m off the player; want it on its spot on a ring %v, not held short of one", v.ID, v.X, v.Z, d, boarRings)
		}
	}
}

// boarRings are how far from a player's centre a boar's spots stand: touching, at its bite's edge, then a boar's width further out each.
var boarRings = []float64{playerRadius + 0.875 + 0.05, boarReach - 0.05, playerRadius + 0.875 + 0.05 + 2*0.875, playerRadius + 0.875 + 0.05 + 4*0.875}
