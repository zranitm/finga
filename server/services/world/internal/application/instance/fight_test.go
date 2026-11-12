package instance_test

import (
	"context"
	"math/rand/v2"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/internal/sim"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

// arena has a floor at 608 and one still slime on a 32 px platform at x 300, 18 px above it.
func arena(respawn time.Duration) *gamedata.Map {
	return &gamedata.Map{
		ID: "arena", Kind: gamedata.Hunting, Town: "town", Width: 1280, Height: 640,
		Geometry: sim.Geometry{
			Solids:    []sim.Rect{{X: 0, Y: 608, W: 1280, H: 32}},
			Platforms: []sim.Rect{{X: 284, Y: 590, W: 32, H: 8}},
		},
		Spawns:      []gamedata.Spawn{{Mob: "green_slime", Max: 1, Respawn: respawn, Area: sim.Rect{X: 284, Y: 562, W: 32, H: 28}}},
		PlayerSpawn: gamedata.Point{X: 100, Y: 608},
	}
}

type transferRecorder struct {
	mu      sync.Mutex
	moved   []model.Player
	to      []string
	dropped []string // "<character> <connection>"
}

func (r *transferRecorder) Dropped(_ *instance.Instance, id kernel.CharacterID, conn string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dropped = append(r.dropped, id.String()+" "+conn)
}

func (r *transferRecorder) Transfer(_ *instance.Instance, p model.Player, _ instance.Sink, mapID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.moved = append(r.moved, p)
	r.to = append(r.to, mapID)
}

func ofType(s *worldtest.Sink, want func(any) bool) []any {
	var out []any
	for _, e := range s.All() {
		if want(e) {
			out = append(out, e)
		}
	}
	return out
}

func damages(s *worldtest.Sink) []model.Damage {
	var out []model.Damage
	for _, e := range ofType(s, func(e any) bool { _, ok := e.(model.Damage); return ok }) {
		out = append(out, e.(model.Damage))
	}
	return out
}

// warriorAt is a new warrior, wearing the starter sword at its lowest roll as every new warrior does: two hits kill a slime.
func warriorAt(x float64) model.Player {
	p := worldtest.NewPlayer("Ayla", x, 608)
	p.Worn = model.Worn{Revision: 1, Items: []model.WornItem{{Template: "wooden_sword", Stats: progression.ItemStats{PhysicalAttack: 10}}}}
	return p
}

func bowmanAt(x float64) model.Player {
	p := model.Player{ID: kernel.NewCharacterID(), Name: "Robin", Class: kernel.Class("bowman"), Level: 1, HP: 105, MP: 33, X: x, Y: 608}
	p.Worn = model.Worn{Revision: 1, Items: []model.WornItem{{Template: "short_bow", Stats: progression.ItemStats{PhysicalAttack: 8}}}}
	return p
}

func join(t *testing.T, inst *instance.Instance, p model.Player) *worldtest.Sink {
	t.Helper()
	s := &worldtest.Sink{}
	if err := inst.Join(context.Background(), p, s); err != nil {
		t.Fatal(err)
	}
	time.Sleep(worldtest.TickInterval)
	synctest.Wait()
	return s
}

func mobsIn(s *worldtest.Sink) map[uint64]model.MobView {
	seen := map[uint64]model.MobView{}
	for _, e := range s.All() {
		switch ev := e.(type) {
		case model.Snapshot:
			for _, m := range ev.Mobs {
				seen[m.ID] = m
			}
		case worldtest.Delta:
			for _, m := range ev.Mobs {
				seen[m.ID] = m
			}
			for _, id := range ev.RemovedMobs {
				delete(seen, id)
			}
		default: // the rest change nothing on the map
		}
	}
	return seen
}

// A map is full from the start, so a player joining in its first tick sees its mobs in the snapshot.
func TestAMapHoldsItsMobsBeforeItsFirstTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), arena(2*time.Second), nil, worldtest.Options{})
		defer stop()
		s := join(t, inst, warriorAt(250))
		if snap, ok := s.All()[0].(model.Snapshot); !ok || len(snap.Mobs) != 1 {
			t.Fatalf("first event %+v, want a snapshot with the one mob", s.All()[0])
		}
	})
}

func TestMobsSpawnAndRespawn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.RunMap(t, data, arena(2*time.Second), nil, worldtest.Options{})
		defer stop()
		s := join(t, inst, warriorAt(250))
		slime, _ := data.Mob("green_slime")
		{ // the snapshot carries the one spawned mob, standing on its platform facing right
			mobs := mobsIn(s)
			if len(mobs) != 1 {
				t.Fatalf("%d mobs, want 1", len(mobs))
			}
			for _, m := range mobs {
				if m.Type != "green_slime" || m.X != 300 || m.Y != 590 || m.HP != slime.HP || m.MaxHP != slime.HP || m.Facing != 1 {
					t.Fatalf("mob %+v", m)
				}
			}
		}
		{ // two hits kill it (24 damage each with the starter sword, against 34 HP); it leaves the map
			_ = inst.Attack(idOf(t, s), 0)
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			time.Sleep(17 * worldtest.TickInterval) // the rest of the 900 ms attack
			synctest.Wait()
			_ = inst.Attack(idOf(t, s), 0)
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			if got := damages(s); len(got) != 2 || got[0].Amount != 24 || got[1].Amount != 24 {
				t.Fatalf("damages %+v", got)
			}
			if len(mobsIn(s)) != 0 {
				t.Fatal("the mob is still on the map after dying")
			}
		}
		{ // nothing comes back before the respawn timer, then exactly one does
			time.Sleep(time.Second)
			synctest.Wait()
			if len(mobsIn(s)) != 0 {
				t.Fatal("a mob returned before the respawn timer")
			}
			time.Sleep(2 * time.Second)
			synctest.Wait()
			mobs := mobsIn(s)
			if len(mobs) != 1 {
				t.Fatalf("%d mobs after the respawn timer, want 1", len(mobs))
			}
			for _, m := range mobs {
				if m.ID != 2 || m.HP != slime.HP {
					t.Fatalf("respawned mob %+v", m)
				}
			}
		}
	})
}

func idOf(t *testing.T, s *worldtest.Sink) kernel.CharacterID {
	t.Helper()
	snap := s.All()[0].(model.Snapshot)
	return snap.Players[len(snap.Players)-1].ID
}

func TestAttacks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.RunMap(t, data, arena(time.Second), nil, worldtest.Options{})
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()

		{ // the Warrior's box reaches a mob in front, not one behind or far away
			far := join(t, inst, warriorAt(100)) // 184 px away: outside the 64 px box
			_ = inst.Attack(idOf(t, far), 0)
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			if len(damages(far)) != 0 {
				t.Fatal("a Warrior hit a mob 184 px away")
			}
			_ = inst.Leave(idOf(t, far), "", nil)
			behind := join(t, inst, warriorAt(340)) // the mob is behind, facing right
			_ = inst.Attack(idOf(t, behind), 0)
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			if len(damages(behind)) != 0 {
				t.Fatal("a Warrior hit a mob behind their back")
			}
			_ = inst.Leave(idOf(t, behind), "", nil)
			near := join(t, inst, warriorAt(250))
			_ = inst.Attack(idOf(t, near), 0)
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			got := damages(near)
			if len(got) != 1 || got[0].Amount != 24 || got[0].Crit || got[0].Miss || got[0].Source.Player != idOf(t, near) || !got[0].Target.IsMob() {
				t.Fatalf("damages %+v", got)
			}
			_ = inst.Leave(idOf(t, near), "", nil)
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
		}

		{ // the Bowman's shot reaches 420 px along the facing line
			s := join(t, inst, bowmanAt(100))
			_ = inst.Attack(idOf(t, s), 0)
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			got := damages(s)
			if len(got) != 1 || got[0].Amount != 22 { // physical attack 14 at level 1 (STR 7 × 1.5 + DEX 7 × 0.5), and the bow's 8
				t.Fatalf("damages %+v", got)
			}
			_ = inst.Leave(idOf(t, s), "", nil)
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
		}

		{ // an attack runs its length even if the client spams Attack, which keeps one swing for its end (after the slime, killed above, has respawned)
			time.Sleep(1200 * time.Millisecond)
			synctest.Wait()
			s := join(t, inst, bowmanAt(100))
			id := idOf(t, s)
			for range 30 {
				_ = inst.Attack(id, 0)
			}
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			for range 5 {
				_ = inst.Attack(id, 0)
				time.Sleep(worldtest.TickInterval)
				synctest.Wait()
			}
			if got := damages(s); len(got) != 1 {
				t.Fatalf("%d hits from 35 attacks inside one attack, want 1", len(got))
			}
			length, _ := attackTicks(t, data, bowmanAt(100))
			time.Sleep(time.Duration(length-5) * worldtest.TickInterval) // the attack ends
			synctest.Wait()
			if got := damages(s); len(got) != 2 {
				t.Fatalf("%d hits once the attack ended, want the one kept", len(got))
			}
		}
	})
}

// An attack that comes before the last one ends swings as it ends: a client timing the attack in milliseconds sends it a little early at a rate where it isn't whole ticks.
func TestAnEarlyAttackSwingsAsTheLastEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		s := join(t, inst, bowmanAt(100))
		id := idOf(t, s)
		_ = inst.Attack(id, 0)
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		time.Sleep(10 * worldtest.TickInterval) // inside the attack
		synctest.Wait()
		_ = inst.Attack(id, 0)
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		if got := damages(s); len(got) != 1 {
			t.Fatalf("%d hits inside the attack, want 1", len(got))
		}
		length, _ := attackTicks(t, data, bowmanAt(100))
		time.Sleep(time.Duration(length-11) * worldtest.TickInterval) // its end
		synctest.Wait()
		if got := damages(s); len(got) != 2 {
			t.Fatalf("%d hits after the attack, want the one sent during it", len(got))
		}
	})
}

func TestContactDamageAndInvulnerability(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		s := join(t, inst, warriorAt(260)) // 24 px from the mob's box; one tick of walking touches it
		id := idOf(t, s)
		seq := worldtest.Walk(inst, id, 0, sim.Input{Right: true}, 2)
		got := damages(s)
		if len(got) != 1 || !got[0].Source.IsMob() || got[0].Target.Player != id {
			t.Fatalf("damages %+v", got)
		}
		// Slime attack 6 against defense 12: 6·100/112 = 5.4 → 5.
		if got[0].Amount != 5 {
			t.Fatalf("contact damage %d, want 5", got[0].Amount)
		}
		v, _ := s.Own(id)
		if v.HP != 139 {
			t.Fatalf("HP %d, want 139", v.HP)
		}
		if v.State.VY >= 0 {
			t.Fatalf("no knockback hop: %+v", v.State)
		}
		if !v.Invulnerable {
			t.Fatalf("not invulnerable after contact damage: %+v", v)
		}
		// Wait out the invulnerability standing still, then walk back in for a second hit.
		seq = worldtest.Walk(inst, id, seq, sim.Input{}, 32) // past the 1.5 s touch guard, standing still
		{                                                    // the Delta carries the cleared flag while standing still
			var ticks []uint64
			for _, d := range s.Deltas() {
				for _, pv := range d.Players {
					if pv.ID == id && !pv.Invulnerable && pv.HP == 139 {
						ticks = append(ticks, d.Tick)
					}
				}
			}
			hitTick := damages(s)[0].Tick
			if len(ticks) == 0 || ticks[0] != hitTick+30 {
				t.Fatalf("the invulnerability edge: cleared in deltas at ticks %v, want %d (1.5 s after the hit at %d)", ticks, hitTick+30, hitTick)
			}
		}
		if len(damages(s)) != 1 {
			t.Fatalf("%d hits inside the invulnerability window", len(damages(s)))
		}
		worldtest.Walk(inst, id, seq, sim.Input{Right: true}, 8)
		if len(damages(s)) < 2 {
			t.Fatal("no second hit after the invulnerability window")
		}
	})
}

// A dodged touch shows MISS and leaves the player where they stand, without the hurt blink; the mob still rolls its next touch a touch guard later.
func TestADodgedTouchNeitherKnocksBackNorBlinks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{NewRoll: worldtest.Rolls(0.999)})
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		s := join(t, inst, warriorAt(260))
		id := idOf(t, s)
		seq := worldtest.Walk(inst, id, 0, sim.Input{Right: true}, 2)
		got := damages(s)
		if len(got) != 1 || !got[0].Miss || got[0].Target.Player != id {
			t.Fatalf("damages %+v, want one MISS on the player", got)
		}
		v, _ := s.Own(id)
		if v.HP != 144 || !v.State.OnGround || v.State.VY != 0 || v.State.X <= 260 {
			t.Fatalf("after the MISS %+v, want full HP, on the ground, not pushed back", v)
		}
		if v.Invulnerable {
			t.Fatal("blinking after a MISS")
		}
		for range 35 { // standing in the slime
			if len(damages(s)) > 1 {
				break
			}
			seq = worldtest.Walk(inst, id, seq, sim.Input{}, 1)
		}
		if got := damages(s); len(got) != 2 || got[1].Tick != got[0].Tick+30 {
			t.Fatalf("touches %+v, want the next roll 1.5 s after the MISS", got)
		}
	})
}

func TestDeathAndRespawn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		cps := &worldtest.Checkpoints{}
		tr := &transferRecorder{}
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), cps, worldtest.Options{Transfer: tr})
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		p := warriorAt(260)
		p.HP = 1
		p.Exp = 10 // level 1 needs 20; the penalty is 5% of 20, floored to 1
		s := join(t, inst, p)
		seq := worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, 2)
		{ // the touch kills: Died to everyone, the player is marked dead and frozen
			died := ofType(s, func(e any) bool { _, ok := e.(model.Died); return ok })
			if len(died) != 1 || died[0].(model.Died).ID != p.ID {
				t.Fatalf("died events %+v", died)
			}
			v, _ := s.Own(p.ID)
			if !v.Dead || v.HP != 0 {
				t.Fatalf("view after death %+v", v)
			}
			lost := ofType(s, func(e any) bool { _, ok := e.(model.ExpGained); return ok })
			if len(lost) != 1 || lost[0].(model.ExpGained).Amount != -1 || lost[0].(model.ExpGained).Exp != 9 {
				t.Fatalf("the death penalty as ExpGained: %+v, want amount -1 and exp 9", lost)
			}
			x := v.State.X
			_ = inst.Input(p.ID, seq+1, sim.Input{Right: true})
			_ = inst.Attack(p.ID, 0)
			time.Sleep(5 * worldtest.TickInterval)
			synctest.Wait()
			if v2, ok := s.Own(p.ID); ok && v2.State.X != x {
				t.Fatal("a dead character moved")
			}
			if n := len(damages(s)); n != 1 {
				t.Fatalf("%d damage events; a dead character attacked or was hit again", n)
			}
		}
		{ // after 3 seconds: Respawned at the town spawn with 50% HP, the penalty applied, handed to the town
			time.Sleep(3 * time.Second)
			synctest.Wait()
			resp := ofType(s, func(e any) bool { _, ok := e.(model.Respawned); return ok })
			if len(resp) != 1 {
				t.Fatalf("respawned events %+v", resp)
			}
			r := resp[0].(model.Respawned)
			town := data.StartTown()
			if r.MapID != town.ID || r.X != town.PlayerSpawn.X || r.Y != town.PlayerSpawn.Y || r.HP != 72 {
				t.Fatalf("respawned %+v", r)
			}
			tel := ofType(s, func(e any) bool { _, ok := e.(model.Teleport); return ok })
			if len(tel) != 1 || tel[0].(model.Teleport).MapID != town.ID {
				t.Fatalf("teleport events %+v", tel)
			}
			tr.mu.Lock()
			defer tr.mu.Unlock()
			if len(tr.moved) != 1 || tr.to[0] != town.ID || tr.moved[0].HP != 72 || tr.moved[0].Exp != 9 || tr.moved[0].X != town.PlayerSpawn.X {
				t.Fatalf("transfer %+v to %v", tr.moved, tr.to)
			}
		}
		{ // the arena checkpointed the player on the way out, with the penalized EXP
			saved := cps.Drain()
			found := false
			for _, cp := range saved {
				if cp.ID == p.ID && cp.Exp == 9 && cp.MapID == data.StartTown().ID && cp.X == data.StartTown().PlayerSpawn.X {
					found = true
				}
			}
			if !found {
				t.Fatalf("no checkpoint with the penalty among %+v", saved)
			}
		}
	})
}

func TestKillGivesExpAndLevelsUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		cps := &worldtest.Checkpoints{}
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), cps, worldtest.Options{})
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		p := warriorAt(250)
		p.Exp = 18 // level 1 needs 20; a slime gives 8
		s := join(t, inst, p)
		_ = inst.Attack(p.ID, 0)
		time.Sleep(18 * worldtest.TickInterval) // the 900 ms attack
		synctest.Wait()
		_ = inst.Attack(p.ID, 0)
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		exp := ofType(s, func(e any) bool { _, ok := e.(model.ExpGained); return ok })
		if len(exp) != 1 || exp[0].(model.ExpGained).Amount != 8 || exp[0].(model.ExpGained).Exp != 6 {
			t.Fatalf("exp events %+v", exp)
		}
		lus := ofType(s, func(e any) bool { _, ok := e.(model.LevelUp); return ok })
		if len(lus) != 1 {
			t.Fatalf("level-up events %+v", lus)
		}
		lu := lus[0].(model.LevelUp)
		// The level's 5 points wait unspent, so the stats stay the class's; max HP 60+18+2×7+10×7 = 162, max MP 20+4+3×7 = 45.
		if lu.Level != 2 || lu.Exp != 6 || lu.Stats != (gamedata.Stats{STR: 7, DEX: 7, INT: 7, AGI: 7, VIT: 7}) || lu.HP != 162 || lu.MaxHP != 162 || lu.MP != 45 || lu.MaxMP != 45 ||
			lu.Sheet.Points != (model.Points{Unspent: 5}) || lu.Sheet.Skills.Unspent != 5 {
			t.Fatalf("level-up %+v; want 5 stat and 5 skill points unspent", lu)
		}
		v, _ := s.Own(p.ID)
		if v.Level != 2 || v.HP != 162 || v.MaxHP != 162 {
			t.Fatalf("view after level-up %+v", v)
		}
		// The level-up is checkpointed right away, with the whole progression.
		if saved := cps.Drain(); len(saved) != 1 || saved[0].ID != p.ID || saved[0].Level != 2 || saved[0].Exp != 6 || saved[0].HP != 162 || saved[0].Points != (model.Points{Unspent: 5}) || saved[0].Skills.Unspent != 5 {
			t.Fatalf("checkpoints after the level-up %+v", saved)
		}
	})
}

// The tick draws on nothing random but its roll, so the same rolls replay a fight.
func TestTheSameRollsGiveTheSameFight(t *testing.T) {
	fight := func() []model.Damage {
		var out []model.Damage
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			inst, stop := worldtest.RunMap(t, data, arena(time.Second), nil, worldtest.Options{NewRoll: func() model.Roll { return rand.New(rand.NewPCG(42, 42)) }})
			defer stop()
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			s := join(t, inst, bowmanAt(100))
			for range 40 {
				_ = inst.Attack(idOf(t, s), 0)
				time.Sleep(4 * worldtest.TickInterval)
				synctest.Wait()
			}
			out = damages(s)
		})
		return out
	}
	a, b := fight(), fight()
	if len(a) == 0 || len(a) != len(b) {
		t.Fatalf("%d and %d damage events", len(a), len(b))
	}
	for i := range a {
		a[i].Source, b[i].Source = model.EntityRef{}, model.EntityRef{} // the players' random IDs differ between runs
		if a[i] != b[i] {
			t.Fatalf("event %d differs: %+v vs %+v", i, a[i], b[i])
		}
	}
	crits, misses := 0, 0
	for _, d := range a {
		if d.Crit {
			crits++
		}
		if d.Miss {
			misses++
		}
	}
	t.Logf("%d hits, %d crits, %d misses with seed 42", len(a), crits, misses)
}

func TestNoCombatInTown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		m := arena(time.Hour)
		m.ID, m.Kind, m.Town = "town", gamedata.Town, "town" // a town with a mob, for the test only
		tr := &transferRecorder{}
		inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{Transfer: tr})
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		p := warriorAt(260)
		p.HP = 1
		s := join(t, inst, p)
		seq := worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, 10) // walk straight through the slime
		seq = worldtest.Walk(inst, p.ID, seq, sim.Input{Left: true}, 4)
		_ = inst.Input(p.ID, seq+1, sim.Input{})
		_ = inst.Attack(p.ID, 0)
		time.Sleep(2 * worldtest.TickInterval)
		synctest.Wait()
		if n := len(damages(s)); n != 0 {
			t.Fatalf("%d damage events in a town", n)
		}
		v, _ := s.Own(p.ID)
		if v.Dead || v.HP != 1 {
			t.Fatalf("after walking through a mob in a town: %+v", v)
		}
		for _, mb := range mobsIn(s) {
			if mb.HP != mb.MaxHP {
				t.Fatalf("a mob was hurt in a town: %+v", mb)
			}
		}
		if len(ofType(s, func(e any) bool { _, ok := e.(model.Died); return ok })) != 0 {
			t.Fatal("died in a town")
		}
	})
}

func TestDeathWithNothingToLoseSendsNoExpGained(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		p := warriorAt(260)
		p.HP, p.Exp = 1, 0 // at the start of the level: the penalty takes nothing
		s := join(t, inst, p)
		worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, 2)
		if died := ofType(s, func(e any) bool { _, ok := e.(model.Died); return ok }); len(died) != 1 {
			t.Fatalf("died events %+v", died)
		}
		if lost := ofType(s, func(e any) bool { _, ok := e.(model.ExpGained); return ok }); len(lost) != 0 {
			t.Fatalf("an ExpGained with nothing lost: %+v", lost)
		}
	})
}

func TestATownRevivesItsOwnDead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		m := arena(time.Hour)
		m.Town = m.ID
		tr := &transferRecorder{}
		inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{Transfer: tr})
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		p := warriorAt(260)
		p.HP, p.Exp = 1, 10
		s := join(t, inst, p)
		worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, 2)
		time.Sleep(3 * time.Second)
		synctest.Wait()
		resp := ofType(s, func(e any) bool { _, ok := e.(model.Respawned); return ok })
		if len(resp) != 1 || resp[0].(model.Respawned).MapID != "arena" {
			t.Fatalf("respawned %+v", resp)
		}
		v, _ := s.Own(p.ID)
		if v.Dead || v.HP != 72 || v.State.X != 100 || v.State.Y != 608 {
			t.Fatalf("after respawn in place: %+v", v)
		}
		if len(ofType(s, func(e any) bool { _, ok := e.(model.Teleport); return ok })) != 0 || len(tr.moved) != 0 {
			t.Fatal("a town's dead were moved")
		}
	})
}

// Leaving in the respawn delay saves the respawn the death is owed: alive in town, at half HP, the penalty taken once.
func TestADeadPlayerIsCheckpointedAsRespawned(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		cps := &worldtest.Checkpoints{}
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), cps, worldtest.Options{})
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		p := warriorAt(260)
		p.HP, p.Exp = 1, 10
		s := join(t, inst, p)
		worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, 2)
		if v, _ := s.Own(p.ID); !v.Dead {
			t.Fatalf("not dead: %+v", v)
		}
		cps.Drain()
		_ = inst.Leave(p.ID, "", nil)
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		town := data.StartTown()
		saved := cps.Drain()
		if len(saved) != 1 || saved[0].MapID != town.ID || saved[0].X != town.PlayerSpawn.X || saved[0].Y != town.PlayerSpawn.Y || saved[0].HP != 72 || saved[0].Exp != 9 {
			t.Fatalf("checkpoint of a dead player %+v, want town's spawn with 72 HP and EXP 9", saved)
		}
	})
}
