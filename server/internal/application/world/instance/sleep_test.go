package instance_test

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/quant"
	"finga/internal/shared/sim"
)

// mobIn is mob id as snap holds it.
func mobIn(t *testing.T, snap model.Snapshot, id uint64) model.MobView {
	t.Helper()
	k := slices.IndexFunc(snap.Mobs, func(m model.MobView) bool { return m.ID == id })
	if k < 0 {
		t.Fatalf("snapshot mobs %+v; want mob %d", snap.Mobs, id)
	}
	return snap.Mobs[k]
}

// A camp no player's block holds sleeps: its mob stands where it fell asleep, and a killed one comes back meanwhile, so a player walking up finds it full; their coming wakes it.
func TestAFarCampSleepsFullAndWakesAsAPlayerComes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		second := int(time.Second / worldtest.TickInterval)
		killed, wandering := slimeAt(160, 160), slimeAt(100, 160)
		killed.Respawn, wandering.Radius = time.Second, 6
		rolls := func() model.Roll { return rand.New(rand.NewPCG(1, 2)) } // so a mob awake wanders, each spot a new one
		inst, stop := worldtest.RunMap(t, data, plain(t, []gamedata.Spawn{killed, wandering}, nil), nil, worldtest.Options{NewRoll: rolls})
		defer stop()
		join(t, inst, worldtest.NewPlayer("Ayla", 160, 300)) // row 9: her block reaches back to row 8, keeping the map up but not the camps' row 5
		bruno := warriorAt(158.5)                            // in a swing's reach of the first camp, facing it
		bruno.X = 160
		bruno.Worn.Items[0].Stats.PhysicalAttack = 200 // a swing kills
		sb := join(t, inst, bruno)
		_ = inst.Attack(bruno.ID, 0, model.EntityRef{})
		for k := 0; !slices.ContainsFunc(sb.Deltas(), func(d worldtest.Delta) bool { return slices.Contains(d.RemovedMobs, 1) }); k++ {
			if k == 100 {
				t.Fatal("the camp's mob never died")
			}
			tick(1)
		}
		asleep := sightOf(t, sb).mobs[2]
		if err := inst.Leave(bruno.ID, "", nil); err != nil {
			t.Fatal(err)
		}
		tick(30 * second)
		sr := join(t, inst, worldtest.NewPlayer("Robin", 130, 150)) // 30 m from each camp: out of their sight
		snap := eventsOf[model.Snapshot](sr)[0]
		if m := mobIn(t, snap, 2); quant.Position(m.X) != quant.Position(asleep.X) || quant.Position(m.Z) != quant.Position(asleep.Z) { // the mob as a client holds it, to the centimetre
			t.Fatalf("the second camp's mob at (%.2f, %.2f) after 30 s nobody watched it; want where it fell asleep, (%.2f, %.2f)", m.X, m.Z, asleep.X, asleep.Z)
		}
		mobIn(t, snap, 3) // the first camp's next, come while it slept
		for k := 0; ; k++ {
			if m := sightOf(t, sr).mobs[2]; quant.Position(m.X) != quant.Position(asleep.X) || quant.Position(m.Z) != quant.Position(asleep.Z) { // the mob as a client holds it, to the centimetre
				break
			}
			if k == 10*second {
				t.Fatal("the second camp's mob never stirred in 10 s with Robin watching it")
			}
			tick(1)
		}
	})
}

// A mob coming after a player gives up once no player's block holds it, and walks home asleep, as it would at its leash.
func TestAMobNobodyWatchesMidChaseWalksHome(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		second := int(time.Second / worldtest.TickInterval)
		camp := gamedata.Spawn{Mob: "wild_boar", Max: 1, Respawn: time.Hour, At: gamedata.Point{X: 160, Z: 160}, Radius: 2} // it bites, so it walks up to its target
		inst, stop := worldtest.RunMap(t, data, plain(t, []gamedata.Spawn{camp}, nil), nil, worldtest.Options{})
		defer stop()
		join(t, inst, worldtest.NewPlayer("Ayla", 160, 300))
		bruno := worldtest.NewPlayer("Bruno", 160, 167) // 7 m north of the camp's middle: in its sight from anywhere in it, well inside its leash
		sb := join(t, inst, bruno)
		worldtest.Walk(inst, bruno.ID, 0, sim.Input{}, 7*second) // it comes at him
		led := sightOf(t, sb).mobs[1]
		if d := math.Hypot(led.X-160, led.Z-160); d < 5 {
			t.Fatalf("the boar %.2f m from its camp's middle; want it led off", d)
		}
		if err := inst.Leave(bruno.ID, "", nil); err != nil {
			t.Fatal(err)
		}
		tick(20 * second)
		m := mobIn(t, eventsOf[model.Snapshot](join(t, inst, worldtest.NewPlayer("Robin", 160, 130)))[0], 1) // 30 m south: out of its sight
		if d := math.Hypot(m.X-160, m.Z-160); d > camp.Radius+0.5 {
			t.Fatalf("the boar %.2f m from its camp's middle 20 s after nobody watched it; want it home, within %.1f m", d, camp.Radius)
		}
	})
}

// Every mob a player is sent is awake: camps in the four corner cells of the block round them all wander.
func TestTheMobsAtTheBlocksCornersAreAwake(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		second := int(time.Second / worldtest.TickInterval)
		corner := func(x, z float64) gamedata.Spawn {
			c := slimeAt(x, z)
			c.Radius = 4
			return c
		}
		camps := []gamedata.Spawn{corner(36, 100), corner(252, 100), corner(36, 252), corner(252, 252)} // the block round cell (4, 4): columns 1 to 7, rows 3 to 7
		rolls := func() model.Roll { return rand.New(rand.NewPCG(3, 4)) }
		inst, stop := worldtest.RunMap(t, data, plain(t, camps, nil), nil, worldtest.Options{NewRoll: rolls})
		defer stop()
		s := join(t, inst, worldtest.NewPlayer("Ayla", 144, 144))
		start := sightOf(t, s).mobs
		if len(start) != len(camps) {
			t.Fatalf("Ayla sent %d mobs; want the %d at the block's corners", len(start), len(camps))
		}
		tick(10 * second)
		for id, m := range sightOf(t, s).mobs {
			if m.X == start[id].X && m.Z == start[id].Z {
				t.Errorf("mob %d at (%.0f, %.0f) never stirred in 10 s", id, m.X, m.Z)
			}
		}
	})
}
