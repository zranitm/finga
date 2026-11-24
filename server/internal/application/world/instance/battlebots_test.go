package instance_test

import (
	"math"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
)

// botBattle is a battle of bots, n a side, but for Ayla on side 1, who stays in her base, where the other side's bots leave her be, and watches the whole battlefield. Its fight lasts 40 s.
type botBattle struct {
	field   *gamedata.Map
	sink    *worldtest.Sink
	side    map[kernel.CharacterID]int // each bot's, 0 or 1
	results *worldtest.Results
	fight   uint64 // the fight's first tick
}

func runBotBattle(t *testing.T, n [2]int) *botBattle {
	t.Helper()
	data := worldtest.LoadDataWith(t, map[string]func(string) string{
		"pvp.yaml": func(s string) string {
			return battleLine.ReplaceAllString(s, strings.Replace(shippedBattle, "length: 12m", "length: 40s", 1))
		},
		"world.yaml": func(s string) string { // one player sees the whole battlefield
			return edit(t, "\n  side: 3\n", "\n  side: 4\n")(edit(t, "\n  behind: 1\n", "\n  behind: 2\n")(s))
		},
	})
	field, _ := data.Map("battlefield")
	ayla := fighterFrom(t, data, "Ayla")
	spec := model.MatchSpec{ID: kernel.NewMatchID(), Mode: "battle"}
	spec.Sides[0] = append(spec.Sides[0], model.Fighter{Character: ayla.ID})
	bb := &botBattle{field: field, side: map[kernel.CharacterID]int{}, results: &worldtest.Results{}}
	classes := []kernel.Class{"warrior", "bowman", "magician", "thief"}
	for s := range 2 {
		for k := range n[s] {
			b := botOf(data, string(rune('A'+8*s+k)))
			c := classes[k%len(classes)]
			b.Class, b.Build.Bar = c, data.Pvp().Bots.Builds[c][0].Bar
			spec.Sides[s] = append(spec.Sides[s], model.Fighter{Bot: b})
			bb.side[b.ID] = s
		}
	}
	inst, stop := worldtest.RunMatch(t, data, spec, nil, worldtest.Options{Results: bb.results})
	t.Cleanup(stop)
	bb.sink = joinAll(t, inst, ayla)[ayla.ID]
	sleep(data.Pvp().Battle.Prep + 40*time.Second + time.Second)
	for _, st := range worldtest.Of[model.MatchState](bb.sink) {
		if st.Phase == model.PhaseFight {
			bb.fight = st.Tick
			break
		}
	}
	return bb
}

// in is the point p stands in, -1 for none.
func (bb *botBattle) in(p model.PlayerView) int {
	for k, pt := range bb.field.Points {
		if pt.Holds(p.State.X, p.State.Z) {
			return k
		}
	}
	return -1
}

// pvp.md section 8, as built in step 6c: a battle's bots go out to the points, half a side at most to each, so three spread one to a point, and take them; walking their side's grid, none ever stands in the other side's base or lingers at its edge, pressing into it.
func TestABattlesBotsTakePointsAndKeepOutOfTheOtherBase(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bb := runBotBattle(t, [2]int{3, 4})
		edge := map[kernel.CharacterID]uint64{} // since when each bot has stood at the other base's edge
		frames := map[kernel.CharacterID]int{}
		east := map[kernel.CharacterID]float64{} // how far east each of side 1's bots got in the fight's first 8 s, as the sides meet at the middle
		for _, d := range bb.sink.Deltas() {
			for _, p := range d.Players {
				s, ok := bb.side[p.ID]
				if !ok {
					continue
				}
				frames[p.ID]++
				if s == 0 && d.Tick < bb.fight+ticks(8*time.Second) {
					east[p.ID] = max(east[p.ID], p.State.X)
				}
				base := bb.field.Bases[1-s]
				gap := math.Hypot(p.State.X-base.At.X, p.State.Z-base.At.Z) - base.Radius
				switch {
				case gap < 0:
					t.Fatalf("at tick %d a bot of side %d stood at (%.1f, %.1f), in the other side's base", d.Tick, s+1, p.State.X, p.State.Z)
				case gap > 1.5:
					delete(edge, p.ID)
				case edge[p.ID] == 0:
					edge[p.ID] = d.Tick
				case d.Tick-edge[p.ID] > ticks(3*time.Second):
					t.Fatalf("a bot of side %d stood at the other base's edge from tick %d to %d, at (%.1f, %.1f); want it gone within 3 s", s+1, edge[p.ID], d.Tick, p.State.X, p.State.Z)
				}
			}
		}
		for id, s := range bb.side {
			if frames[id] < 20 {
				t.Fatalf("Ayla saw a bot of side %d in %d frames; want it seen moving a second at least", s+1, frames[id])
			}
		}
		var reach []float64
		for _, x := range east {
			reach = append(reach, x)
		}
		slices.Sort(reach)
		if len(reach) != 3 || reach[0] > 45 || reach[1] < 50 {
			t.Fatalf("side 1's three bots got as far east as x = %v in the fight's first 8 s; want one stopping at the west point, by x = 38, and two going on past it, as half of three is one", reach)
		}
		var held [3]bool
		for _, st := range worldtest.Of[model.MatchState](bb.sink) {
			for _, pt := range st.Points {
				held[pt.Holder] = true
			}
		}
		got := bb.results.All()
		if !held[1] || !held[2] || len(got) != 1 || got[0].Score[0] == 0 || got[0].Score[1] == 0 {
			t.Fatalf("held by side 1 %v, side 2 %v, outcomes %+v; want each side's bots taking a point and scoring", held[1], held[2], got)
		}
	})
}

// pvp.md section 8: once its side holds a point, with no enemy near, two of its bots there stay and the rest go on. Side 2's six send three to the east point, nearest them, and once it's theirs one of the three leaves it for another.
func TestABattlesBotsGoOnFromAPointTheirSideHolds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bb := runBotBattle(t, [2]int{1, 6})
		const east = 2
		var heldSince uint64 // the tick side 2 first held the east point
		for _, st := range worldtest.Of[model.MatchState](bb.sink) {
			if len(st.Points) > east && st.Points[east].Holder == 2 {
				heldSince = st.Tick
				break
			}
		}
		if heldSince == 0 {
			t.Fatal("side 2's bots never took the east point")
		}
		was := map[kernel.CharacterID]bool{} // side 2's bots that stood in the east point once it was theirs
		for _, d := range bb.sink.Deltas() {
			for _, p := range d.Players {
				if bb.side[p.ID] != 1 || d.Tick < heldSince {
					continue
				}
				switch k := bb.in(p); {
				case k == east:
					was[p.ID] = true
				case k >= 0 && was[p.ID]:
					t.Logf("a bot went on from the east point, held from tick %d, to point %d at tick %d", heldSince, k, d.Tick)
					return
				}
			}
		}
		t.Fatalf("%d of side 2's bots stood in the east point once it was theirs from tick %d, and none went on to another", len(was), heldSince)
	})
}
