package nav_test

import (
	"math"
	"testing"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
)

// unreachedGround is ground a shipped map may keep from its mobs, each with why; nothing else may.
var unreachedGround = []struct {
	mapID string
	area  [4]float64 // x, z, width and depth from the south-west corner, edges included
	why   string
}{
	{"forest", [4]float64{43.5, 23.5, 13, 9}, "the mesa, 1.2 m, that only a jump climbs: the scoped ledge rule (Systems → Combat) keeps it until mobs climb"},
}

// band is how far over and under a mob's feet a player's may stand, centre to centre within across, for one of its blows to land.
type band struct{ up, down, across float64 }

// bands are a mob's, as its attacks test reach, from ground it walks: a hop or a charge stops where it can't walk, as a ball does, so a hop lands its circle and a charge its body; a shot's from beside the player, where its lane is short and clear.
func bands(m gamedata.Mob, p sim.Params) []band {
	touch := m.Radius + p.Radius
	out := []band{{m.Height, p.Height, touch}}
	for _, at := range m.Attacks {
		switch at.Kind {
		case gamedata.Front:
			out = append(out, band{m.Height, p.Height, touch + at.Reach})
		case gamedata.Leap:
			out = append(out, band{m.Height + at.Spread, p.Height + at.Spread, touch + at.Spread})
		case gamedata.Around:
			out = append(out, band{m.Height + at.Spread, p.Height + at.Spread, touch + at.Spread})
		case gamedata.Shot:
			out = append(out, band{m.Height/2 + at.Size/2, p.Height - m.Height/2 + at.Size/2, touch + 1})
		}
	}
	return out
}

// lands reports whether, by one of bs, a mob on ground walk reached lands a blow on a player standing at q, py up.
func lands(g *nav.Grid, walk nav.Reached, q nav.Point, py float64, bs []band) bool {
	for _, b := range bs {
		for dz := -math.Floor(b.across/0.5) * 0.5; dz <= b.across; dz += 0.5 {
			for dx := -math.Floor(b.across/0.5) * 0.5; dx <= b.across; dx += 0.5 {
				at := nav.Point{X: q.X + dx, Z: q.Z + dz}
				if nav.Length(dx, dz) > b.across || !walk.Has(at) {
					continue
				}
				if my := g.GroundAt(at); py <= my+b.up && py >= my-b.down {
					return true
				}
			}
		}
	}
	return false
}

// On every hunting map and every shipped region's hunting areas, each mob lands a blow on a player wherever they stand inside its leash: no ledge or nook keeps a player out of its reach while they hit it.
func TestEveryMobReachesAPlayerAnywhereInItsLeash(t *testing.T) {
	data := gamedatatest.Shipped(t)
	p := shipped(t).params
	rise := nav.JumpRise(p) // the highest a jump lifts the feet, at the slowest speed
	for _, m := range huntingGrounds(t, data) {
		t.Run(m.id, func(t *testing.T) {
			g := m.grid()
			player := g.Reach(m.starts, p.Radius, rise)
			for _, h := range m.homes {
				def, ok := data.Mob(h.mob)
				if !ok {
					t.Fatalf("%s: no mob %q", h.what, h.mob)
				}
				c := h.at
				leash := h.r + data.MobRules().Leash
				walk := g.WalkWithin(c, def.Radius, c, leash)
				bs := bands(def, p)
				missed, first := 0, nav.Point{}
				for z := 0.25; z < m.geo.SizeZ; z += 0.5 {
					for x := 0.25; x < m.geo.SizeX; x += 0.5 {
						q := nav.Point{X: x, Z: z}
						if nav.Distance(q, c) > leash || !player.Has(q) || excused(m.id, q) {
							continue
						}
						if !lands(g, walk, q, g.GroundAt(q), bs) {
							if missed == 0 {
								first = q
							}
							missed++
						}
					}
				}
				if missed > 0 {
					t.Errorf("%s (%s): %d spots a player reaches inside its leash where none of its blows lands, the first at (%.2f, %.2f), %.2f m up", h.what, h.mob, missed, first.X, first.Z, g.GroundAt(first))
				}
			}
		})
	}
}

// excused reports whether q lies on ground unreachedGround lets map id keep from its mobs.
func excused(id string, q nav.Point) bool {
	for _, u := range unreachedGround {
		if u.mapID == id && q.X >= u.area[0] && q.X <= u.area[0]+u.area[2] && q.Z >= u.area[1] && q.Z <= u.area[1]+u.area[3] {
			return true
		}
	}
	return false
}
