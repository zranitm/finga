package instance_test

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

// shippedBattle is pvp.yaml's battle line as shipped, for a test to change one rule of.
const shippedBattle = "battle: {prep: 20s, length: 12m, goal: 1000, per_point: 1, per_down: 5, respawn: 10s, base_heal: 0.10, capture: {time: 8s, per_extra: 0.25, fastest: 2}}"

var battleLine = regexp.MustCompile(`(?m)^battle: \{.*\}$`)

// battleData is the shipped data with the battle's rules swapped as replace says, old for new, and pvp.yaml's scale set.
func battleData(t *testing.T, hp, damage string, replace ...string) *gamedata.Data {
	t.Helper()
	return worldtest.LoadDataWith(t, map[string]func(string) string{"pvp.yaml": func(s string) string {
		if battleLine.FindString(s) != shippedBattle {
			t.Fatalf("pvp.yaml's battle is %q; want %q", battleLine.FindString(s), shippedBattle)
		}
		return scaleOf(t, hp, damage)(battleLine.ReplaceAllString(s, strings.NewReplacer(replace...).Replace(shippedBattle)))
	}})
}

// aBattle runs a battle of these players, each side in its spots' order, and joins them, in prep.
func aBattle(t *testing.T, data *gamedata.Data, results *worldtest.Results, sides [2][]model.Player) (*instance.Instance, map[kernel.CharacterID]*worldtest.Sink, func()) {
	t.Helper()
	spec := model.MatchSpec{ID: kernel.NewMatchID(), Mode: "battle"}
	var all []model.Player
	for s, side := range sides {
		for _, p := range side {
			spec.Sides[s] = append(spec.Sides[s], model.Fighter{Character: p.ID})
			all = append(all, p)
		}
	}
	inst, stop := worldtest.RunMatch(t, data, spec, nil, worldtest.Options{Results: results})
	return inst, joinAll(t, inst, all...), stop
}

// march walks the players east or west together, a tick at a time, until each stands within 0.3 m of x, then stops them; seq is their inputs' last, and the new last comes back.
func march(inst *instance.Instance, sinks map[kernel.CharacterID]*worldtest.Sink, seq uint32, x float64, ids ...kernel.CharacterID) uint32 {
	for range 1200 {
		seq++
		moving := false
		for _, id := range ids {
			own, _ := sinks[id].Own(id)
			in := sim.Input{}
			if d := x - own.State.X; d > 0.3 || d < -0.3 {
				in, moving = sim.Input{Right: d > 0, Left: d < 0}, true
			}
			_ = inst.Input(id, seq, in, 0)
		}
		sleep(worldtest.TickInterval)
		if !moving {
			break
		}
	}
	return seq
}

// enteredAt is the first tick after from the player's own frames have them in c, 0 for none.
func enteredAt(s *worldtest.Sink, id kernel.CharacterID, c gamedata.Circle, from uint64) uint64 {
	return firstFrame(s, id, from, func(v model.PlayerView) bool { return c.Holds(v.State.X, v.State.Z) })
}

// leftAt is the first tick after from the player's own frames have them out of c, 0 for none.
func leftAt(s *worldtest.Sink, id kernel.CharacterID, c gamedata.Circle, from uint64) uint64 {
	return firstFrame(s, id, from, func(v model.PlayerView) bool { return !c.Holds(v.State.X, v.State.Z) })
}

func firstFrame(s *worldtest.Sink, id kernel.CharacterID, from uint64, ok func(model.PlayerView) bool) uint64 {
	for _, d := range s.Deltas() {
		for _, p := range d.Players {
			if p.ID == id && d.Tick > from && ok(p) {
				return d.Tick
			}
		}
	}
	return 0
}

// turned is the first MatchState s heard after from in which point k's holder is holder.
func turned(s *worldtest.Sink, k, holder int, from uint64) (model.MatchState, bool) {
	for _, st := range worldtest.Of[model.MatchState](s) {
		if st.Tick > from && len(st.Points) > k && st.Points[k].Holder == holder {
			return st, true
		}
	}
	return model.MatchState{}, false
}

// lastState is the latest MatchState s heard.
func lastState(s *worldtest.Sink) model.MatchState {
	st := worldtest.Of[model.MatchState](s)
	return st[len(st)-1]
}

// stages are the phases s heard, each once in a row.
func stages(s *worldtest.Sink) []model.MatchPhase {
	return slices.Compact(phases(s))
}

// ticks is how many ticks d runs.
func ticks(d time.Duration) uint64 { return uint64(d / worldtest.TickInterval) }

// pvp.md section 7: a battle's prep holds each side behind its gate, then goes straight into the fight, with no count, the builds sent in prep put on as it starts; the battle shows its score and points, not rounds, each second, and on time the side ahead wins.
func TestABattleGoesFromPrepIntoItsFightAndEndsOnTimeToTheSideAhead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := battleData(t, "1.0", "1.0", "length: 12m", "length: 40s")
		field, _ := data.Map("battlefield")
		results := &worldtest.Results{}
		ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
		inst, sinks, stop := aBattle(t, data, results, [2][]model.Player{{ayla}, {robin}})
		defer stop()
		as := sinks[ayla.ID]
		sleep(worldtest.TickInterval)
		first := lastState(as)
		if !first.Battle || first.Round != 0 || first.Phase != model.PhasePrep || first.EndsIn > 20*time.Second || len(first.Points) != 3 || first.Points[0].ID != "west" || first.Points[2].ID != "east" {
			t.Fatalf("the battle in prep %+v; want no round, its three points in the map's order, and 20 s of prep", first)
		}
		build := model.Build{Bar: arenaBar("strike"), Traits: map[string]string{}, Revision: 1}
		if err := inst.SetBuild(context.Background(), ayla.ID, build); err != nil {
			t.Fatal(err)
		}
		seq := worldtest.Walk(inst, ayla.ID, 0, sim.Input{Right: true}, int(ticks(8*time.Second)))
		seq = worldtest.Walk(inst, ayla.ID, seq, sim.Input{}, 1)
		if own, _ := as.Own(ayla.ID); own.State.X > 24.5 || own.State.X < 23 {
			t.Fatalf("Ayla walked east 8 s in prep to x %.1f; want her held at the gate at 24.5", own.State.X)
		}
		sleep(data.Pvp().Battle.Prep)
		if got := stages(as); !slices.Equal(got, []model.MatchPhase{model.PhasePrep, model.PhaseFight}) {
			t.Fatalf("phases %v; want prep straight into the fight", got)
		}
		fight := slices.IndexFunc(worldtest.Of[model.MatchState](as), func(st model.MatchState) bool { return st.Phase == model.PhaseFight })
		started := worldtest.Of[model.MatchState](as)[fight].Tick
		if bars := worldtest.Of[model.ArenaBar](as); len(bars) != 1 || bars[0].Tick != started || !slices.Equal(bars[0].Bar, build.Bar) {
			t.Fatalf("Ayla's bars %+v; want the build she sent in prep put on as the fight starts, at tick %d", bars, started)
		}
		march(inst, sinks, seq, field.Points[0].At.X, ayla.ID)
		sleep(40 * time.Second)
		got := results.All()
		if len(got) != 1 || got[0].Winner != 1 || got[0].Score[0] == 0 || got[0].Score[1] != 0 || got[0].Wins != [2]int{} {
			t.Fatalf("outcomes %+v; want Ayla's side ahead on its point's score, and no rounds", got)
		}
		if end := lastState(as); end.Phase != model.PhaseResult || end.Score != got[0].Score {
			t.Fatalf("the last state %+v; want the result at the outcome's score %v", end, got[0].Score)
		}
		var seconds int
		for _, st := range worldtest.Of[model.MatchState](as) {
			if st.Phase == model.PhaseFight {
				seconds++
			}
		}
		if seconds < 40 {
			t.Fatalf("%d states in a 40 s fight; want one each second at least", seconds)
		}
	})
}

// pvp.md section 7: a point turns to the side alone in it in capture's time for one fighter, each more adding per_extra of that pace, up to fastest times it: 8 s for one, 6.4 s for two, 4 s for six as for five.
func TestAPointTurnsFasterForEachMoreOfItsSideUpToTheFastest(t *testing.T) {
	for _, tt := range []struct {
		n    int
		want time.Duration
	}{{1, 8 * time.Second}, {2, 6400 * time.Millisecond}, {6, 4 * time.Second}} {
		t.Run(strconv.Itoa(tt.n), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := worldtest.LoadData(t)
				field, _ := data.Map("battlefield")
				west := field.Points[0]
				var side []model.Player
				var ids []kernel.CharacterID
				for k := range tt.n {
					p := fighterFrom(t, data, fmt.Sprint("Fighter", k))
					side, ids = append(side, p), append(ids, p.ID)
				}
				inst, sinks, stop := aBattle(t, data, nil, [2][]model.Player{side, {fighterFrom(t, data, "Robin")}})
				defer stop()
				var seq uint32
				for _, id := range ids { // on the point's line behind the gate, to step in together
					seq = walkTo(inst, sinks[id], id, seq, west.At.Z, 0.2)
				}
				seq = march(inst, sinks, seq, 20, ids...)
				sleep(data.Pvp().Battle.Prep)
				march(inst, sinks, seq, west.At.X, ids...)
				sleep(10 * time.Second)
				var in uint64
				for _, id := range ids {
					in = max(in, enteredAt(sinks[id], id, west.Circle, 0))
				}
				st, ok := turned(sinks[ids[0]], 0, 1, 0)
				if want := in + ticks(tt.want) - 1; !ok || st.Tick+3 < want || st.Tick > want+1 {
					t.Fatalf("%d of side 1 in the west point from tick %d: it turned at %+v; want tick %d", tt.n, in, st, want)
				}
			})
		})
	}
}

// pvp.md section 7: a point both sides stand in holds still, its holder scoring on, whichever side holds it; one the other side holds drains to no one's in capture's time, then turns to the side alone in it in as long again.
func TestAHeldPointHoldsWhileContestedAndDrainsToNoOnesFirst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		field, _ := data.Map("battlefield")
		west := field.Points[0]
		ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
		inst, sinks, stop := aBattle(t, data, nil, [2][]model.Player{{ayla}, {robin}})
		defer stop()
		as := sinks[ayla.ID]
		sleep(data.Pvp().Battle.Prep + worldtest.TickInterval)
		seq := march(inst, sinks, 0, west.At.X, ayla.ID)
		sleep(9 * time.Second)
		held, ok := turned(as, 0, 1, 0)
		if !ok {
			t.Fatal("Ayla alone never took the west point")
		}
		seq = march(inst, sinks, seq, west.At.X+1, robin.ID)
		before := lastState(as)
		sleep(3 * time.Second)
		if st := lastState(as); !st.Points[0].Contested || st.Points[0].Holder != 1 || st.Points[0].Progress != -1 || st.Score[0] < before.Score[0]+3 {
			t.Fatalf("both sides in the west point 3 s: %+v from %+v; want it contested, still side 1's, scoring for it", st, before)
		}
		in := enteredAt(sinks[robin.ID], robin.ID, west.Circle, held.Tick)
		states := worldtest.Of[model.MatchState](as)
		if k := slices.IndexFunc(states, func(st model.MatchState) bool { return st.Tick > held.Tick && st.Points[0].Contested }); k < 0 || states[k].Tick != in {
			t.Fatalf("Robin in the west point at tick %d; Ayla heard it contested at %d (-1 for never) of %+v; want that tick", in, k, states)
		}
		seq = march(inst, sinks, seq, 25, ayla.ID)
		gone := leftAt(as, ayla.ID, west.Circle, held.Tick)
		sleep(17 * time.Second)
		none, okNone := turned(as, 0, 0, gone)
		theirs, okTheirs := turned(as, 0, 2, gone)
		near := func(got, want uint64) bool { return got+1 >= want && got <= want+1 }
		if !okNone || !okTheirs || !near(none.Tick, gone+ticks(8*time.Second)-1) || !near(theirs.Tick, gone+ticks(16*time.Second)-1) {
			t.Fatalf("Robin alone from tick %d: no one's at %+v, his at %+v; want 8 s to drain, 8 s more to turn", gone, none, theirs)
		}
		march(inst, sinks, seq, west.At.X-1, ayla.ID)
		sleep(2 * time.Second)
		if st := lastState(as); !st.Points[0].Contested || st.Points[0].Holder != 2 || st.Points[0].Progress != 1 {
			t.Fatalf("Ayla back in Robin's point 2 s: %+v; want it contested, still his", st.Points[0])
		}
	})
}

// meet walks a east and b west to stand a metre and a bit apart about x.
func meet(inst *instance.Instance, sinks map[kernel.CharacterID]*worldtest.Sink, seq uint32, x float64, a, b kernel.CharacterID) uint32 {
	seq = march(inst, sinks, seq, x, a)
	return march(inst, sinks, seq, x+1.2, b)
}

// aimAt is the yaw from a's body to b's, as s last saw them.
func aimAt(s *worldtest.Sink, a, b kernel.CharacterID) float32 {
	va, _ := s.Own(a)
	vb, _ := s.Own(b)
	return float32(math.Atan2(vb.State.X-va.State.X, vb.State.Z-va.State.Z))
}

// strikeAt has by attack target once, aimed at them.
func strikeAt(inst *instance.Instance, s *worldtest.Sink, by, target kernel.CharacterID) {
	_ = inst.Attack(by, aimAt(s, by, target), model.EntityRef{Player: target})
}

// downs has by attack target until they're down, and gives the tick they went down.
func downs(t *testing.T, inst *instance.Instance, s *worldtest.Sink, by, target kernel.CharacterID) uint64 {
	t.Helper()
	for range 400 {
		strikeAt(inst, s, by, target)
		sleep(worldtest.TickInterval)
		for _, d := range worldtest.Of[model.Died](s) {
			if d.ID == target {
				return d.Tick
			}
		}
	}
	t.Fatal("never down")
	return 0
}

// pvp.md section 7: a down scores per_down for the other side; the downed get up after respawn in their base at its spot farthest from any body, at full HP, wearing the build they sent since.
func TestTheDownedGetUpInTheirBaseAtItsFreestSpot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := battleData(t, "1.0", "100.0") // any blow downs
		field, _ := data.Map("battlefield")
		ayla, cora, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Cora"), fighterFrom(t, data, "Robin")
		inst, sinks, stop := aBattle(t, data, nil, [2][]model.Player{{ayla, cora}, {robin}})
		defer stop()
		as := sinks[ayla.ID]
		sleep(data.Pvp().Battle.Prep + worldtest.TickInterval)
		meet(inst, sinks, 0, 60, ayla.ID, robin.ID)
		build := model.Build{Bar: arenaBar("strike"), Traits: map[string]string{}, Revision: 1}
		if err := inst.SetBuild(context.Background(), ayla.ID, build); err != nil {
			t.Fatal(err)
		}
		down := downs(t, inst, as, robin.ID, ayla.ID)
		if st := lastState(as); st.Score != [2]int{0, 5} || st.Phase != model.PhaseFight || len(worldtest.Of[model.ArenaBar](as)) != 0 {
			t.Fatalf("after Ayla's down %+v; want 5 to side 2, the fight on, and her build not yet on", st)
		}
		sleep(data.Pvp().Battle.Respawn)
		ups := worldtest.Of[model.Respawned](as)
		if len(ups) != 1 || ups[0].ID != ayla.ID || ups[0].Tick != down+ticks(data.Pvp().Battle.Respawn) {
			t.Fatalf("Respawned %+v; want Ayla's, 10 s after her down at tick %d", ups, down)
		}
		var bodies [][2]float64
		for _, id := range []kernel.CharacterID{cora.ID, robin.ID} {
			v, _ := sinks[id].Own(id)
			bodies = append(bodies, [2]float64{v.State.X, v.State.Z})
		}
		want, far := field.Sides[0][0], -1.0
		for _, s := range field.Sides[0] {
			near := math.Inf(1)
			for _, b := range bodies {
				near = min(near, math.Hypot(b[0]-s.At.X, b[1]-s.At.Z))
			}
			if near > far {
				want, far = s, near
			}
		}
		if ups[0].X != want.At.X || ups[0].Z != want.At.Z || want == field.Sides[0][0] {
			t.Fatalf("Ayla got up at (%.1f, %.1f); want side 1's spot farthest from Cora and Robin, %+v", ups[0].X, ups[0].Z, want.At)
		}
		own, _ := as.Own(ayla.ID)
		if own.Dead || own.HP != own.MaxHP {
			t.Fatalf("Ayla up %+v; want full HP", own)
		}
		if bars := worldtest.Of[model.ArenaBar](as); len(bars) != 1 || bars[0].Tick != ups[0].Tick || !slices.Equal(bars[0].Bar, build.Bar) {
			t.Fatalf("Ayla's bars %+v; want the build she sent put on as she got up", bars)
		}
	})
}

// poisonOnWarrior is a Thief's attack under Poison Blade on a Warrior in a battle, its poison's first tick, and the Warrior's max HP there.
func poisonOnWarrior(t *testing.T, data *gamedata.Data) (hit, tick, maxHP int) {
	t.Helper()
	ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
	ayla.Class = "thief"
	inst, sinks, stop := aBattle(t, data, nil, [2][]model.Player{{ayla}, {robin}})
	defer stop()
	as := sinks[ayla.ID]
	sleep(data.Pvp().Battle.Prep + worldtest.TickInterval)
	meet(inst, sinks, 0, 60, ayla.ID, robin.ID)
	if err := useSkill(inst, ayla, "poison_blade", 0); err != nil {
		t.Fatal(err)
	}
	sleep(time.Second)
	strikeAt(inst, as, ayla.ID, robin.ID)
	sleep(2 * time.Second)
	for _, h := range hitsOn(as, robin.ID) {
		switch {
		case h.Status == "" && hit == 0:
			hit = h.Amount
		case h.Status == "poison" && tick == 0:
			tick = h.Amount
		}
	}
	own, _ := sinks[robin.ID].Own(robin.ID)
	if hit == 0 || tick == 0 {
		t.Fatalf("Ayla's hits on Robin %+v; want an attack and a poison tick", hitsOn(as, robin.ID))
	}
	return hit, tick, own.MaxHP
}

// A poison tick downs as a hit does: the down scores for the poisoner's side, and the side it emptied fights on.
func TestAPoisonDownScoresForThePoisonersSide(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hit, tick, maxHP := poisonOnWarrior(t, battleData(t, "1.0", "1.0"))
		hp := (float64(hit) + 1.5*float64(tick)) / float64(maxHP) // one hit leaves a tick and a half of poison
		data := battleData(t, strconv.FormatFloat(hp, 'f', 6, 64), "1.0")
		ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
		ayla.Class = "thief"
		inst, sinks, stop := aBattle(t, data, nil, [2][]model.Player{{ayla}, {robin}})
		defer stop()
		as := sinks[ayla.ID]
		sleep(data.Pvp().Battle.Prep + worldtest.TickInterval)
		meet(inst, sinks, 0, 60, ayla.ID, robin.ID)
		if err := useSkill(inst, ayla, "poison_blade", 0); err != nil {
			t.Fatal(err)
		}
		sleep(time.Second)
		strikeAt(inst, as, ayla.ID, robin.ID)
		sleep(4 * time.Second)
		died := worldtest.Of[model.Died](as)
		hits := hitsOn(as, robin.ID)
		if len(died) != 1 || died[0].ID != robin.ID || len(hits) < 2 || hits[len(hits)-1].Status != "poison" || hits[len(hits)-1].Tick != died[0].Tick {
			t.Fatalf("Died %+v after hits %+v; want Robin down to a poison tick", died, hits)
		}
		if st := lastState(as); st.Score != [2]int{5, 0} || st.Phase != model.PhaseFight {
			t.Fatalf("after Robin's poison down %+v; want 5 to Ayla's side, and side 2, all down, fighting on", st)
		}
	})
}

// The first side to the goal wins there and then.
func TestTheFirstSideToTheGoalWinsThere(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := battleData(t, "1.0", "100.0", "goal: 1000", "goal: 10", "per_down: 5", "per_down: 10")
		results := &worldtest.Results{}
		ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
		inst, sinks, stop := aBattle(t, data, results, [2][]model.Player{{ayla}, {robin}})
		defer stop()
		as := sinks[ayla.ID]
		sleep(data.Pvp().Battle.Prep + worldtest.TickInterval)
		meet(inst, sinks, 0, 60, ayla.ID, robin.ID)
		down := downs(t, inst, as, robin.ID, ayla.ID)
		got := results.All()
		if end := lastState(as); len(got) != 1 || got[0].Winner != 2 || got[0].Score != [2]int{0, 10} || end.Phase != model.PhaseResult || end.Tick != down {
			t.Fatalf("outcomes %+v, last state %+v; want side 2's win at 10, as Ayla went down at tick %d", got, end, down)
		}
	})
}

// pvp.md section 7: a point scores at each whole second from the fight's start, the last at its end, so one held from a tick scores every second from the first at or after it, and the fight lasts its length to the tick; no tick tells the match twice.
func TestAHeldPointScoresEachWholeSecondToTheFightsEnd(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := battleData(t, "1.0", "1.0", "length: 12m", "length: 30s")
		field, _ := data.Map("battlefield")
		results := &worldtest.Results{}
		ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
		inst, sinks, stop := aBattle(t, data, results, [2][]model.Player{{ayla}, {robin}})
		defer stop()
		as := sinks[ayla.ID]
		sleep(data.Pvp().Battle.Prep + worldtest.TickInterval)
		march(inst, sinks, 0, field.Points[0].At.X, ayla.ID)
		sleep(30 * time.Second)
		states := worldtest.Of[model.MatchState](as)
		fight := states[slices.IndexFunc(states, func(st model.MatchState) bool { return st.Phase == model.PhaseFight })].Tick
		held, ok := turned(as, 0, 1, fight)
		second, length := ticks(time.Second), ticks(30*time.Second)
		first := (held.Tick - fight + second - 1) / second // the first whole second at or after the capture
		want := int(length/second - first + 1)
		got := results.All()
		if end := lastState(as); !ok || len(got) != 1 || got[0].Score != [2]int{want, 0} || end.Phase != model.PhaseResult || end.Tick != fight+length {
			t.Fatalf("the west point Ayla's from tick %d (%v) of a 30 s fight from tick %d: outcomes %+v, last state at tick %d; want %d, a point for each second from the %dth to the 30th, and the end at tick %d", held.Tick, ok, fight, got, end.Tick, want, first, fight+length)
		}
		for k := 1; k < len(states); k++ {
			if states[k].Tick == states[k-1].Tick {
				t.Fatalf("two MatchStates at tick %d: %+v and %+v; want one a tick", states[k].Tick, states[k-1], states[k])
			}
			if st := states[k]; st.Score != states[k-1].Score && (st.Tick-fight)%second != 0 {
				t.Fatalf("the score %v heard at tick %d, %d after the fight's start; want it told on the whole second it scored", st.Score, st.Tick, st.Tick-fight)
			}
		}
	})
}

// pvp.md section 7: when both sides reach the goal on one tick, the higher score wins there, and an equal one is a draw.
func TestBothSidesAtTheGoalOnOneTick(t *testing.T) {
	for _, c := range []struct {
		name   string
		hold   bool // Ayla's side holds the west point first, scoring a little
		winner int
	}{
		{"even scores draw", false, 0},
		{"the higher score wins", true, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := battleData(t, "1.0", "100.0", "goal: 1000", "goal: 20", "per_down: 5", "per_down: 20") // any blow downs, and a down reaches the goal
				field, _ := data.Map("battlefield")
				west := field.Points[0]
				results := &worldtest.Results{}
				ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
				inst, sinks, stop := aBattle(t, data, results, [2][]model.Player{{ayla}, {robin}})
				defer stop()
				as := sinks[ayla.ID]
				sleep(data.Pvp().Battle.Prep + worldtest.TickInterval)
				at := west.At.X + west.Radius + 3 // clear of the point
				if c.hold {
					at = west.At.X + west.Radius - 0.3 // just in it, Robin just out
				}
				seq := march(inst, sinks, 0, at+1.2, robin.ID)
				march(inst, sinks, seq, at, ayla.ID)
				for range ticks(20 * time.Second) {
					if !c.hold || lastState(as).Score[0] > 0 {
						break
					}
					sleep(worldtest.TickInterval)
				}
				before := lastState(as).Score
				for range 400 {
					strikeAt(inst, as, ayla.ID, robin.ID)
					strikeAt(inst, as, robin.ID, ayla.ID)
					sleep(worldtest.TickInterval)
					if len(worldtest.Of[model.Died](as)) > 0 {
						break
					}
				}
				died := worldtest.Of[model.Died](as)
				got := results.All()
				if len(died) != 2 || died[0].Tick != died[1].Tick || len(got) != 1 || got[0].Winner != c.winner || got[0].Score != [2]int{before[0] + 20, 20} || lastState(as).Tick != died[0].Tick {
					t.Fatalf("from %v, Died %+v, outcomes %+v; want both down on one tick, side 1 at %d and side 2 at 20, and side %d's win there", before, died, got, before[0]+20, c.winner)
				}
				if c.hold && before[0] == 0 {
					t.Fatal("Ayla's side scored nothing from the point before the downs")
				}
			})
		})
	}
}

// pvp.md section 7: a base keeps the other side's bodies out, walking or knocked back, and heals its own side base_heal of max HP a second, nobody outside it.
func TestABaseKeepsTheOtherSideOutAndHealsItsOwn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := battleData(t, "1.0", "1.0")
		field, _ := data.Map("battlefield")
		base := field.Bases[0]
		ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
		ayla.Class = "bowman"
		inst, sinks, stop := aBattle(t, data, nil, [2][]model.Player{{ayla}, {robin}})
		defer stop()
		as, rs := sinks[ayla.ID], sinks[robin.ID]
		sleep(data.Pvp().Battle.Prep + worldtest.TickInterval)
		edge := base.At.X + base.Radius
		seq := march(inst, sinks, 0, base.At.X, robin.ID)
		if own, _ := rs.Own(robin.ID); own.State.X < edge {
			t.Fatalf("Robin walked into side 1's base to x %.1f; want him held at its edge, %.1f", own.State.X, edge)
		}
		held, _ := rs.Own(robin.ID)
		seq = march(inst, sinks, seq, held.State.X+1.4, ayla.ID)
		leap := make(chan error, 1)
		go func() { // aimed west at Robin, leaping east
			leap <- inst.UseSkill(context.Background(), ayla.ID, ayla.Connection, model.SkillUse{Skill: "disengage", Root: "disengage", Aim: aimAt(as, ayla.ID, robin.ID), Way: sim.Way{X: 1, Length: 6.5}}, seq)
		}()
		sleep(worldtest.TickInterval)
		if err := <-leap; err != nil {
			t.Fatal(err)
		}
		sleep(time.Second)
		if len(hitsOn(as, robin.ID)) == 0 {
			t.Fatal("Ayla's Disengage never reached Robin")
		}
		if own, _ := rs.Own(robin.ID); own.State.X < edge {
			t.Fatalf("Robin knocked back to x %.1f; want him held at side 1's base's edge, %.1f", own.State.X, edge)
		}
		at, _ := as.Own(ayla.ID)
		seq = march(inst, sinks, seq, at.State.X-1.2, robin.ID)
		strikeAt(inst, as, robin.ID, ayla.ID)
		sleep(time.Second)
		hurt, _ := as.Own(ayla.ID)
		sleep(3 * time.Second)
		if own, _ := as.Own(ayla.ID); hurt.HP >= hurt.MaxHP || own.HP != hurt.HP {
			t.Fatalf("Ayla hurt outside her base %+v, 3 s on %+v; want her HP down and staying there", hurt, own)
		}
		outside := as.Last().Tick
		march(inst, sinks, seq, field.Sides[0][0].At.X, ayla.ID)
		in := enteredAt(as, ayla.ID, base, outside)
		sleep(time.Second)
		var healed []int
		for _, d := range as.Deltas() {
			for _, p := range d.Players {
				if p.ID == ayla.ID && d.Tick >= in && d.Tick <= in+ticks(time.Second) {
					healed = append(healed, p.HP)
				}
			}
		}
		if gain := healed[len(healed)-1] - hurt.HP; len(healed) == 0 || gain < hurt.MaxHP/10-1 || gain > hurt.MaxHP/10+1 {
			t.Fatalf("Ayla's HP her first second in her base %v from %d; want a tenth of %d more", healed, hurt.HP, hurt.MaxHP)
		}
	})
}

// A downed leaver's stand-in lies down until the leaver would have got up, then gets up in the base.
func TestADownedLeaversStandInGetsUpWhenTheyWould(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := battleData(t, "1.0", "100.0")
		ayla, cora, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Cora"), fighterFrom(t, data, "Robin")
		inst, sinks, stop := aBattle(t, data, nil, [2][]model.Player{{ayla, cora}, {robin}})
		defer stop()
		cs := sinks[cora.ID]
		sleep(data.Pvp().Battle.Prep + worldtest.TickInterval)
		meet(inst, sinks, 0, 60, ayla.ID, robin.ID)
		down := downs(t, inst, sinks[ayla.ID], robin.ID, ayla.ID)
		sleep(3 * time.Second)
		if err := inst.Forfeit(context.Background(), ayla.ID, ayla.Connection); err != nil {
			t.Fatal(err)
		}
		sleep(data.Pvp().Battle.Respawn)
		bot := lastState(cs).Sides[0][0]
		var ups []model.Respawned
		for _, r := range worldtest.Of[model.Respawned](cs) {
			if r.ID == bot.ID {
				ups = append(ups, r)
			}
		}
		if !bot.Bot || len(ups) != 1 || ups[0].Tick != down+ticks(data.Pvp().Battle.Respawn) {
			t.Fatalf("Ayla's stand-in %+v got up %+v; want it up 10 s after her down at tick %d", bot, ups, down)
		}
	})
}

// pvp.md section 7: a downed body counts for nothing while it lies there: it turns no point it lies in, and its base heals it nothing.
func TestADownedBodyTurnsNoPointAndHealsInNoBase(t *testing.T) {
	t.Run("a point", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := battleData(t, "1.0", "100.0")
			field, _ := data.Map("battlefield")
			west := field.Points[0]
			ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
			inst, sinks, stop := aBattle(t, data, nil, [2][]model.Player{{ayla}, {robin}})
			defer stop()
			as := sinks[ayla.ID]
			sleep(data.Pvp().Battle.Prep + worldtest.TickInterval)
			edge := west.At.X - west.Radius
			seq := march(inst, sinks, 0, edge-0.7, ayla.ID) // just outside the point
			march(inst, sinks, seq, edge+0.5, robin.ID)     // just inside it
			down := downs(t, inst, as, ayla.ID, robin.ID)
			at := lastState(as).Points[0].Progress
			sleep(data.Pvp().Battle.Respawn - time.Second)
			for _, st := range worldtest.Of[model.MatchState](as) {
				if st.Tick > down && st.Points[0].Progress != at {
					t.Fatalf("Robin down in the west point from tick %d: it moved from %v to %+v; want it held still, with no one alive in it", down, at, st.Points[0])
				}
			}
		})
	})
	t.Run("a base", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := battleData(t, "1.0", "100.0")
			field, _ := data.Map("battlefield")
			base := field.Bases[0]
			ayla, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Robin")
			inst, sinks, stop := aBattle(t, data, nil, [2][]model.Player{{ayla}, {robin}})
			defer stop()
			as := sinks[ayla.ID]
			sleep(data.Pvp().Battle.Prep + worldtest.TickInterval)
			seq := march(inst, sinks, 0, base.At.X, robin.ID) // held at the base's edge
			held, _ := sinks[robin.ID].Own(robin.ID)
			march(inst, sinks, seq, held.State.X-1.3, ayla.ID) // inside her base, in his reach
			if own, _ := as.Own(ayla.ID); !base.Holds(own.State.X, own.State.Z) {
				t.Fatalf("Ayla at x %.1f; want her inside her base", own.State.X)
			}
			down := downs(t, inst, as, robin.ID, ayla.ID)
			sleep(data.Pvp().Battle.Respawn - time.Second)
			for _, d := range as.Deltas() {
				for _, p := range d.Players {
					if p.ID == ayla.ID && d.Tick >= down && d.Tick < down+ticks(data.Pvp().Battle.Respawn) && p.HP != 0 {
						t.Fatalf("Ayla down in her base at tick %d had %d HP at tick %d; want none till she gets up", down, p.HP, d.Tick)
					}
				}
			}
		})
	})
}
