package instance_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/regiongen"
	"finga/internal/shared/sim"
)

// townLine is where vale's town ends and its field starts, going north.
const townLine = 12.0

// vale is the arena made a region of two areas (large-regions.md section 6): a town south of townLine, holding a waypoint at (arenaX, 6), and the field round the slime north of it, with more waypoints.
func vale(t *testing.T, more ...gamedata.Waypoint) *gamedata.Map {
	t.Helper()
	m := arena(t, time.Hour)
	m.ID, m.Kind, m.Town = "vale", gamedata.Region, "vale"
	grid := regiongen.AreaGrid{NX: 10, NZ: 10, Of: make([]int16, 100)}
	for s := range grid.Of {
		if float64(s/10*regiongen.AreaCell) >= townLine {
			grid.Of[s] = 1
		}
	}
	m.AreaGrid = grid
	m.Areas = []gamedata.Area{{ID: "town", Name: "Vale Town", Kind: gamedata.Town}, {ID: "field", Name: "Vale Field", Kind: gamedata.Hunting, MinLevel: 1, MaxLevel: 5}}
	m.Waypoints = append([]gamedata.Waypoint{{ID: "town", Name: "Vale Town", At: gamedata.Point{X: arenaX, Z: 6}, Arrive: gamedata.Point{X: arenaX + 2, Z: 6}, Yaw: math.Pi / 2, Main: true}}, more...)
	return m
}

// A region is its own town (large-regions.md section 6): one who dies in it gets up beside its nearest waypoint in a straight line, found or not, facing away from it, in it and in their channel, with no hand-off, having paid the death's EXP as on a hunting map.
func TestAPlayerWhoDiesInARegionGetsUpAtItsNearestWaypoint(t *testing.T) {
	for _, c := range []struct {
		name  string
		field float64 // the field's waypoint's z; the death is at nextToSlime, 12.5 m from the town's
		want  gamedata.Point
	}{
		{"the town's", 34, gamedata.Point{X: arenaX + 2, Z: 6}},
		{"the field's", 28, gamedata.Point{X: arenaX + 2, Z: 28}},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := worldtest.LoadData(t)
				m := vale(t, gamedata.Waypoint{ID: "field", Name: "Vale Field", At: gamedata.Point{X: arenaX, Z: c.field}, Arrive: gamedata.Point{X: arenaX + 2, Z: c.field}, Yaw: math.Pi / 2})
				m.SpawnYaw = 1.5 // the spawn's, which a getting up beside a waypoint doesn't take
				tr := &transferRecorder{}
				inst, stop := worldtest.RunMap(t, data, m, &worldtest.Checkpoints{}, worldtest.Options{Transfer: tr, Channel: 2})
				defer stop()
				tick(1)
				p := warriorAt(nextToSlime)
				p.HP, p.Exp = 1, 10
				s := join(t, inst, p)
				untilDead(t, s, p.ID)
				time.Sleep(3 * time.Second)
				synctest.Wait()
				got := eventsOf[model.Respawned](s)
				if len(got) != 1 || got[0].MapID != "vale" || got[0].X != c.want.X || got[0].Z != c.want.Z || got[0].Yaw != math.Pi/2 {
					t.Fatalf("respawned %+v; want once in vale at %v", got, c.want)
				}
				if v, ok := s.Own(p.ID); !ok || v.Dead || v.State.X != c.want.X || v.State.Z != c.want.Z {
					t.Fatalf("after getting up: %+v", v)
				}
				if lost := eventsOf[model.ExpGained](s); len(lost) != 1 || lost[0].Amount != -1 {
					t.Errorf("EXP %+v; want the death's 1 lost", lost)
				}
				tr.mu.Lock()
				defer tr.mu.Unlock()
				if tel := eventsOf[model.Teleport](s); len(tel) != 0 || len(tr.moved) != 0 {
					t.Errorf("handed off: teleports %+v, moved to %v", tel, tr.to)
				}
			})
		})
	}
}

// A player is told the region's area they stand in: on joining, in the snapshot, and on each crossing into another, once; a map has none.
func TestAPlayerIsToldTheAreaTheyStandIn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		m := vale(t)
		m.Spawns = nil
		inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{})
		defer stop()
		p := warriorAt(spawnZ)
		s := join(t, inst, p)
		if got := joinedTo(s).Area; got != "town" {
			t.Fatalf("joined in area %q; want the town", got)
		}
		seq := walkTo(inst, s, p.ID, 0, townLine+2, 0.2)
		seq = walkTo(inst, s, p.ID, seq, townLine+0.5, 0.2) // on in the field
		seq = walkTo(inst, s, p.ID, seq, townLine+3, 0.2)
		if got := eventsOf[model.AreaEntered](s); len(got) != 1 || got[0].Area != "field" {
			t.Fatalf("areas entered %+v; want the field, once", got)
		}
		walkTo(inst, s, p.ID, seq, townLine-2, 0.2)
		if got := eventsOf[model.AreaEntered](s); len(got) != 2 || got[1].Area != "town" {
			t.Fatalf("areas entered %+v; want the field, then the town", got)
		}

		hunting, stopHunting := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
		defer stopHunting()
		q := warriorAt(spawnZ)
		h := join(t, hunting, q)
		walkTo(hunting, h, q.ID, 0, townLine+2, 0.2)
		if got := joinedTo(h).Area; got != "" || len(eventsOf[model.AreaEntered](h)) != 0 {
			t.Fatalf("on a map: joined in area %q, entered %+v; want none", got, eventsOf[model.AreaEntered](h))
		}
	})
}

// A region's town is safe (large-regions.md section 6): there a player's skills that harm and their basic attacks are refused, as in a town map; out of it they aren't.
func TestNobodyFightsInARegionsTown(t *testing.T) {
	for _, town := range []bool{true, false} {
		synctest.Test(t, func(t *testing.T) {
			m := vale(t)
			m.Spawns = nil
			inst, stop := worldtest.RunMap(t, skillData(t), m, nil, worldtest.Options{})
			defer stop()
			p := caster(spawnZ)
			if !town {
				p.Z = townLine + 2
			}
			s := join(t, inst, p)
			err := cast(inst, p.ID, "strike")
			tick(40) // past the strike, so an attack starts on its own
			_ = inst.Attack(p.ID, 0, model.EntityRef{})
			tick(2)
			swung := len(eventsOf[model.AttackUsed](s)) > 0
			switch {
			case town && (!errors.Is(err, model.ErrNotInTown) || swung):
				t.Fatalf("in the town: strike %v, attack swung %v; want NOT_IN_TOWN and no swing", err, swung)
			case !town && (err != nil || !swung):
				t.Fatalf("in the field: strike %v, attack swung %v; want both", err, swung)
			}
		})
	}
}

// A hit started out of a region's town that comes due after its player stepped in lands nothing, as nothing is dealt from a town, a strike's damage or a glare's taunt; one that comes due outside lands.
func TestAHitDueInARegionsTownLandsNothing(t *testing.T) {
	data := func(t *testing.T) *gamedata.Data {
		return skillDataWith(t, map[string]func(string) string{
			"mobs.yaml": func(m string) string { // the slime outlasts the strike, and its first blow comes long after
				return edit(t, "hp: 34", "hp: 1000000")(edit(t, "attacks: [{use: hop}]", "attacks: [{use: bite, windup: 3s, cooldown: 4s}]")(m))
			},
			"classes.yaml": edit(t, "bastion, rally]", "bastion, rally, test_glare]"),
			"skills.yaml": func(s string) string { // each lands 12 ticks in
				return edit(t, "    cancel: 0.45\n    power: {base: 1.5, per_point: 0.05}", "    cancel: 0.6\n    hit: 0.6\n    power: {base: 1.5, per_point: 0.05}")(s) + `
  - id: test_glare
    icon: lorc/shouting
    kind: active
    effect: status
    shape: {kind: area, radius: 6}
    energy: 2
    cooldown: 1s
    length: 1s
    hit: 0.6
    cancel: 0.6
    status: {id: taunt, time: 5s}
    stages:
      - {at: 0, name: Glare, description: "Taunts."}
`
			},
		})
	}
	for _, skill := range []string{"strike", "test_glare"} {
		for _, in := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s, in the town %v", skill, in), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					m := vale(t)
					m.Spawns[0].At.Z = townLine + 1.1
					inst, stop := worldtest.RunMap(t, data(t), m, nil, worldtest.Options{})
					defer stop()
					tick(1)
					p := caster(townLine + 0.1) // a metre south of the slime, facing it
					p.Skills.Points["test_glare"] = 1
					s := join(t, inst, p)
					if err := cast(inst, p.ID, skill); err != nil {
						t.Fatal(err)
					}
					if in {
						walkTo(inst, s, p.ID, 0, townLine-0.4, 0.15)
					}
					tick(20)
					own, _ := s.Own(p.ID)
					landed := 0
					for _, d := range damages(s) {
						if d.Source.Player == p.ID {
							landed++
						}
					}
					for _, v := range mobsIn(s) {
						if d := math.Hypot(v.X-own.State.X, v.Z-own.State.Z); d > 2.375+0.5 {
							t.Fatalf("the slime %.2f m off: out of the strike's reach, so this shows nothing", d)
						}
						landed += len(v.Statuses)
					}
					switch {
					case in != (own.State.Z < townLine):
						t.Fatalf("the player at z %.2f; want them in the town %v", own.State.Z, in)
					case in && landed != 0:
						t.Fatalf("damage %+v, the slime %+v: a hit that came due in the town landed", damages(s), mobsIn(s))
					case !in && landed != 1:
						t.Fatalf("damage %+v, the slime %+v; want the hit landed, out of the town", damages(s), mobsIn(s))
					}
				})
			})
		}
	}
}

// A mob's blow that reaches into a region's town lands nothing on a player there; poison it put on outside does them no harm in the town, and runs out.
func TestARegionsTownIsSafeFromBlowsAndPoison(t *testing.T) {
	// The slime's hop goes up to 8 m, landing touching its target, lands 3 m wide, and poisons for 4 s.
	data := func(t *testing.T) *gamedata.Data {
		return slimeAttacking(t, "[{use: hop, distance: 8, spread: 3, status: {id: poison, time: 4s, power: 0.5}}]", false)
	}
	t.Run("a blow reaching in", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			inst, stop := worldtest.RunMap(t, data(t), vale(t), nil, worldtest.Options{})
			defer stop()
			tick(1)
			out, in := warriorAt(townLine+1.5), warriorAt(townLine-0.5) // the slime comes for the one out, side by side with one in
			in.X += 1
			so, si := join(t, inst, out), join(t, inst, in)
			before, _ := si.Own(in.ID)
			at := windupOn(t, so)
			mobHitsOn(t, so, out.ID, 1)
			if d := math.Hypot(in.X-at.ToX, in.Z-at.ToZ); d > 3+0.875 {
				t.Fatalf("the player in the town %.2f m from the hop's landing: out of its reach, so this shows nothing", d)
			}
			own, _ := si.Own(in.ID)
			for _, d := range damages(si) {
				if d.Target.Player == in.ID {
					t.Fatalf("damage %+v on the player in the town", d)
				}
			}
			if own.HP != before.HP || len(own.Statuses) != 0 {
				t.Fatalf("the player in the town at HP %d with %+v; want them untouched", own.HP, own.Statuses)
			}
		})
	})
	t.Run("poison put on outside", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			inst, stop := worldtest.RunMap(t, data(t), vale(t), nil, worldtest.Options{})
			defer stop()
			tick(1)
			p := warriorAt(townLine + 1)
			s := join(t, inst, p)
			hit := mobHitsOn(t, s, p.ID, 1)[0]
			walkTo(inst, s, p.ID, 0, townLine-2, 0.2) // in before its first tick, a second after the hop
			tick(100)
			for _, d := range damages(s) {
				if d.Status == "poison" {
					t.Fatalf("poison %+v on a player in the town, after the hop %+v", d, hit)
				}
			}
			if own, _ := s.Own(p.ID); len(own.Statuses) != 0 {
				t.Fatalf("statuses %+v 5 s on; want the poison run out", own.Statuses)
			}
		})
	})
}

// The test region's boars follow a player to its town's line at z 60 and no further, as 4b-1's playtest saw one cross it by 2.7 m: their camp's leash reaches 6 m into the town. The player waits for them a metre short of the line, then steps in.
func TestTheTestRegionsBoarsStopAtItsTownsLine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		region, _ := data.Map("test_region")
		inst, stop := worldtest.RunMap(t, data, region, nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := worldtest.NewPlayer("Ayla", 102, 50)
		p.Level, p.HP = 20, 100_000 // the boars' bites don't end the walk
		s := join(t, inst, p)
		boars := map[uint64]bool{}
		for id, v := range mobsIn(s) {
			if v.Type == "wild_boar" {
				boars[id] = true
			}
		}
		nearest, seq := 0.0, uint32(0)
		check := func() {
			for id, v := range mobsIn(s) {
				if !boars[id] {
					continue
				}
				if v.Z >= 60 {
					t.Fatalf("boar %d at (%.2f, %.2f), in the town", id, v.X, v.Z)
				}
				nearest = max(nearest, v.Z)
			}
		}
		wait := func(seconds int) {
			for range seconds * 20 {
				seq = worldtest.Walk(inst, p.ID, seq, sim.Input{}, 1)
				check()
			}
		}
		walk := func(from, to float64) {
			for z := from; z <= to; z++ {
				seq = walkTo(inst, s, p.ID, seq, z, 0.2)
				check()
			}
		}
		wait(3) // by the camp, so the boars come
		walk(51, 59)
		wait(5)
		walk(60, 64)
		wait(5)
		if own, _ := s.Own(p.ID); len(boars) == 0 || own.State.Z < 63 || nearest < 58.5 {
			t.Fatalf("%d boars, the player at z %.2f, the boars' nearest z %.2f; want boars following the player into the town to its line", len(boars), own.State.Z, nearest)
		}
	})
}

// The test region is reached by a teleport to its id, which lands at its start (large-regions.md section 12).
func TestATeleportToTheTestRegionLandsAtItsStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		tr := &transferRecorder{}
		inst, stop := worldtest.Run(t, data, &worldtest.Checkpoints{}, worldtest.Options{Transfer: tr, CheckpointInterval: time.Hour})
		defer stop()
		p := atSpawn(data, "Ayla")
		p.Connection = "c1"
		s := join(t, inst, p)
		teleport(t, inst, s, p.ID, "test_region", 0)
		tick(channelTicks(data))
		region, _ := data.Map("test_region")
		got := eventsOf[model.Teleport](s)
		if len(got) != 1 || got[0].MapID != "test_region" || got[0].X != region.PlayerSpawn.X || got[0].Z != region.PlayerSpawn.Z || region.Kind != gamedata.Region {
			t.Fatalf("teleports %+v; want one to the test region's start %+v", got, region.PlayerSpawn)
		}
		tr.mu.Lock()
		defer tr.mu.Unlock()
		if len(tr.to) != 1 || tr.to[0] != "test_region" {
			t.Errorf("moved to %v", tr.to)
		}
	})
}

// One going into a match from a region's channel keeps it as their way back, and goes back to it (large-regions.md section 4): the World then lands them there while it has room.
func TestAMatchFromARegionsChannelGoesBackToIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		m := arena(t, time.Hour)
		m.ID, m.Kind, m.Town = "vale", gamedata.Region, "vale"
		tr := &transferRecorder{}
		inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{Transfer: tr, Channel: 2})
		defer stop()
		p := warriorAt(outOfSight)
		p.Connection = "c1"
		join(t, inst, p)
		ring, _ := data.Map("ring")
		if err := inst.ToMatch(context.Background(), p.ID, "m-1", ring.ID, ring.Sides[0][0]); err != nil {
			t.Fatal(err)
		}
		tr.mu.Lock()
		if len(tr.moved) != 1 || tr.dests[0] != (instance.Dest{ID: "m-1"}) || tr.moved[0].Back == nil || tr.moved[0].Back.MapID != "vale" || tr.moved[0].Back.Channel != 2 {
			t.Fatalf("hand-offs to %+v, back %+v; want the match, back to vale's channel 2", tr.dests, tr.moved)
		}
		tr.mu.Unlock()

		results, moves := &worldtest.Results{}, &worldtest.Moves{}
		ayla := fighterFrom(t, data, "Ayla")
		ayla.Back.MapID, ayla.Back.Channel = "test_region", 2
		match, stopMatch := worldtest.RunMatch(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Bot: botOf(data, "Ash")}), nil, worldtest.Options{Results: results, Transfer: moves})
		defer stopMatch()
		if err := match.Join(context.Background(), ayla, &worldtest.Sink{}); err != nil {
			t.Fatal(err)
		}
		sleep(worldtest.TickInterval)
		if err := match.Forfeit(context.Background(), ayla.ID, ayla.Connection); err != nil {
			t.Fatal(err)
		}
		sleep(worldtest.TickInterval)
		if mv := moves.All(); len(mv) != 1 || mv[0].Dest != (instance.Dest{ID: "test_region", Channel: 2}) {
			t.Fatalf("hand-offs %+v, want Ayla back to the test region's channel 2", mv)
		}
	})
}

// A region's channel reaches the party frame and the tick metrics: a join tells the party the channel, and a hand-off off the region tells none, as the map it lands on tells its own.
func TestARegionsChannelReachesThePartyAndTheTicks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		m := arena(t, time.Hour)
		m.ID, m.Kind, m.Town = "vale", gamedata.Region, "vale"
		parties, ticks := &fakeParties{}, &worldtest.Observer{}
		inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{Parties: parties, Observer: ticks, Transfer: &transferRecorder{}, Channel: 2})
		defer stop()
		p := warriorAt(outOfSight)
		p.Connection = "c1"
		s := join(t, inst, p)
		tick(1)
		if got := parties.updated(); len(got) == 0 || got[0].MapID != "vale" || got[0].Channel != 2 || ticks.Channel() != 2 {
			t.Fatalf("party updates %+v, ticks of channel %d; want vale's channel 2 for both", got, ticks.Channel())
		}
		teleport(t, inst, s, p.ID, "forest", 0)
		tick(channelTicks(data) + 1)
		if got := parties.updated(); got[len(got)-1].MapID != "forest" || got[len(got)-1].Channel != 0 {
			t.Fatalf("the hand-off told %+v; want the forest, with no channel", got[len(got)-1])
		}
	})
}

// Step 6a (large-regions.md section 8): one who comes within 12 m of a region's stone finds it, told once; the checkpoint keeps it, and a hand-off carries it to the next map.
func TestAWaypointFoundIsToldOnceKeptAndCarried(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		cps := &worldtest.Checkpoints{}
		tr := &transferRecorder{}
		inst, stop := worldtest.RunMap(t, data, vale(t), cps, worldtest.Options{Transfer: tr, CheckpointInterval: time.Hour})
		defer stop()
		tick(1)
		p := warriorAt(8) // 2 m north of the town's stone
		p.Connection = "c1"
		s := join(t, inst, p)
		tick(5)
		if got := eventsOf[model.WaypointFound](s); len(got) != 1 || got[0].Waypoint != "town" {
			t.Fatalf("found %+v; want the town's once", got)
		}
		teleport(t, inst, s, p.ID, "forest", 0)
		tick(channelTicks(data) + 1)
		tr.mu.Lock()
		defer tr.mu.Unlock()
		if len(tr.moved) != 1 || len(tr.moved[0].Waypoints) != 1 || tr.moved[0].Waypoints["town"].IsZero() {
			t.Fatalf("handed off %+v; want one carrying the town's", tr.moved)
		}
		if cp := cps.Drain(); len(cp) != 1 || len(cp[0].Waypoints) != 1 || !cp[0].Waypoints["town"].Equal(tr.moved[0].Waypoints["town"]) {
			t.Fatalf("checkpoints %+v; want the hand-off's, keeping the town's as found", cp)
		}
	})
}
