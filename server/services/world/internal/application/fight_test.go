package application_test

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/services/world/internal/application"
	"finga/services/world/internal/model"
)

// fixedRoll always returns the same value: every attack hits for average
// damage, nothing crits, and spawned mobs face right.
type fixedRoll struct{ v float64 }

func (r fixedRoll) Float64() float64 { return r.v }

// arena is a hunting map with a floor at 608 and one green slime standing
// still on a 32 px platform at x 300 (its patrol bounds collapse to a
// point), 18 px above the floor so a shot at chest height crosses it.
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
	mu    sync.Mutex
	moved []model.Player
	to    []string
}

func (r *transferRecorder) Transfer(p model.Player, _ application.Sink, mapID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.moved = append(r.moved, p)
	r.to = append(r.to, mapID)
}

func (s *sink) ofType(want func(model.Event) bool) []model.Event {
	var out []model.Event
	for _, e := range s.all() {
		if want(e) {
			out = append(out, e)
		}
	}
	return out
}

func damages(s *sink) []model.Damage {
	var out []model.Damage
	for _, e := range s.ofType(func(e model.Event) bool { _, ok := e.(model.Damage); return ok }) {
		out = append(out, e.(model.Damage))
	}
	return out
}

// runArena starts an arena instance in the bubble with a fixed roll.
func runArena(t *testing.T, data *gamedata.Data, m *gamedata.Map, persist chan<- model.Job, opts application.Options) (*application.Instance, func()) {
	t.Helper()
	opts.TickInterval = tickInterval
	if opts.Roll == nil {
		opts.Roll = fixedRoll{0.5}
	}
	inst := application.NewInstance(m, data, persist, opts)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = inst.Run(ctx) }()
	var once sync.Once
	return inst, func() { once.Do(func() { cancel(); <-done }) }
}

func warriorAt(x float64) model.Player {
	p := newPlayer("Ayla", x, 608)
	return p
}

func bowmanAt(x float64) model.Player {
	return model.Player{ID: kernel.NewCharacterID(), Name: "Robin", Class: kernel.Bowman, Level: 1, Stats: gamedata.Stats{STR: 5, DEX: 12, INT: 4, AGI: 10, VIT: 5}, HP: 105, MP: 33, X: x, Y: 608}
}

func join(t *testing.T, inst *application.Instance, p model.Player) *sink {
	t.Helper()
	s := &sink{}
	if err := inst.Join(context.Background(), p, s); err != nil {
		t.Fatal(err)
	}
	time.Sleep(tickInterval)
	synctest.Wait()
	return s
}

func mobsIn(s *sink) map[uint64]model.MobView {
	seen := map[uint64]model.MobView{}
	for _, e := range s.all() {
		switch ev := e.(type) {
		case model.Snapshot:
			for _, m := range ev.Mobs {
				seen[m.ID] = m
			}
		case model.Delta:
			for _, m := range ev.Mobs {
				seen[m.ID] = m
			}
			for _, id := range ev.RemovedMobs {
				delete(seen, id)
			}
		}
	}
	return seen
}

func TestMobsSpawnAndRespawn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		inst, stop := runArena(t, data, arena(2*time.Second), nil, application.Options{})
		defer stop()
		time.Sleep(tickInterval) // tick 1: the area fills
		synctest.Wait()
		s := join(t, inst, warriorAt(250))
		{ // the snapshot carries the one spawned mob, standing on its platform facing right
			mobs := mobsIn(s)
			if len(mobs) != 1 {
				t.Fatalf("%d mobs, want 1", len(mobs))
			}
			for _, m := range mobs {
				if m.Type != "green_slime" || m.X != 300 || m.Y != 590 || m.HP != 30 || m.MaxHP != 30 || m.Facing != 1 {
					t.Fatalf("mob %+v", m)
				}
			}
		}
		{ // two hits kill it (21 damage each against 30 HP); it leaves the map
			inst.Attack(s.id(t, inst))
			time.Sleep(tickInterval)
			synctest.Wait()
			time.Sleep(12 * tickInterval) // the 600 ms cooldown
			synctest.Wait()
			inst.Attack(s.id(t, inst))
			time.Sleep(tickInterval)
			synctest.Wait()
			if got := damages(s); len(got) != 2 || got[0].Amount != 21 || got[1].Amount != 21 || !got[0].Source.Player.IsZero() == false && false {
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
				if m.ID != 2 || m.HP != 30 {
					t.Fatalf("respawned mob %+v", m)
				}
			}
		}
	})
}

// id finds the joining player's ID from the snapshot.
func (s *sink) id(t *testing.T, _ *application.Instance) kernel.CharacterID {
	t.Helper()
	snap := s.all()[0].(model.Snapshot)
	return snap.Players[len(snap.Players)-1].ID
}

func TestAttacks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		inst, stop := runArena(t, data, arena(time.Second), nil, application.Options{})
		defer stop()
		time.Sleep(tickInterval)
		synctest.Wait()

		{ // the Warrior's box reaches a mob in front, not one behind or far away
			far := join(t, inst, warriorAt(100)) // 184 px away: outside the 64 px box
			inst.Attack(far.id(t, inst))
			time.Sleep(tickInterval)
			synctest.Wait()
			if len(damages(far)) != 0 {
				t.Fatal("a Warrior hit a mob 184 px away")
			}
			inst.Leave(far.id(t, inst))
			behind := join(t, inst, warriorAt(340)) // the mob is behind, facing right
			inst.Attack(behind.id(t, inst))
			time.Sleep(tickInterval)
			synctest.Wait()
			if len(damages(behind)) != 0 {
				t.Fatal("a Warrior hit a mob behind their back")
			}
			inst.Leave(behind.id(t, inst))
			near := join(t, inst, warriorAt(250))
			inst.Attack(near.id(t, inst))
			time.Sleep(tickInterval)
			synctest.Wait()
			got := damages(near)
			if len(got) != 1 || got[0].Amount != 21 || got[0].Crit || got[0].Miss || got[0].Source.Player != near.id(t, inst) || !got[0].Target.IsMob() {
				t.Fatalf("damages %+v", got)
			}
			inst.Leave(near.id(t, inst))
			time.Sleep(tickInterval)
			synctest.Wait()
		}

		{ // the Bowman's shot reaches 420 px along the facing line
			s := join(t, inst, bowmanAt(100))
			inst.Attack(s.id(t, inst))
			time.Sleep(tickInterval)
			synctest.Wait()
			got := damages(s)
			if len(got) != 1 || got[0].Amount != 21 {
				t.Fatalf("damages %+v", got)
			}
			inst.Leave(s.id(t, inst))
			time.Sleep(tickInterval)
			synctest.Wait()
		}

		{ // cooldowns hold even if the client spams Attack (after the slime, killed above, has respawned)
			time.Sleep(1200 * time.Millisecond)
			synctest.Wait()
			s := join(t, inst, bowmanAt(100))
			id := s.id(t, inst)
			for range 30 {
				inst.Attack(id)
			}
			time.Sleep(tickInterval)
			synctest.Wait()
			for range 5 {
				inst.Attack(id)
				time.Sleep(tickInterval)
				synctest.Wait()
			}
			if got := damages(s); len(got) != 1 {
				t.Fatalf("%d hits from 35 attacks inside one 800 ms cooldown, want 1", len(got))
			}
			time.Sleep(16 * tickInterval) // the cooldown runs out
			synctest.Wait()
			inst.Attack(id)
			time.Sleep(tickInterval)
			synctest.Wait()
			if got := damages(s); len(got) != 2 {
				t.Fatalf("%d hits after the cooldown, want 2", len(got))
			}
		}
	})
}

func TestContactDamageAndInvulnerability(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		inst, stop := runArena(t, data, arena(time.Hour), nil, application.Options{})
		defer stop()
		time.Sleep(tickInterval)
		synctest.Wait()
		s := join(t, inst, warriorAt(260)) // 24 px from the mob's box; one tick of walking touches it
		id := s.id(t, inst)
		seq := walk(inst, id, 0, sim.Input{Right: true}, 2)
		got := damages(s)
		if len(got) != 1 || !got[0].Source.IsMob() || got[0].Target.Player != id {
			t.Fatalf("damages %+v", got)
		}
		// Slime attack 6 against defense 15: 6·100/115 = 5.2 → 5.
		if got[0].Amount != 5 {
			t.Fatalf("contact damage %d, want 5", got[0].Amount)
		}
		v, _ := s.own(id)
		if v.HP != 199 {
			t.Fatalf("HP %d, want 199", v.HP)
		}
		if v.State.VY >= 0 {
			t.Fatalf("no knockback hop: %+v", v.State)
		}
		if !v.Invulnerable {
			t.Fatalf("not invulnerable after contact damage: %+v", v)
		}
		// Stop, wait inside the invulnerability window: no second hit. Then
		// walk back in as it ends: a second hit.
		seq = walk(inst, id, seq, sim.Input{}, 22) // past the second of invulnerability, standing still
		{                                          // the second is over: the Delta carries the cleared flag although the player stood still
			var ticks []uint64
			for _, d := range s.deltas() {
				for _, pv := range d.Players {
					if pv.ID == id && !pv.Invulnerable && pv.HP == 199 {
						ticks = append(ticks, d.Tick)
					}
				}
			}
			hitTick := damages(s)[0].Tick
			if len(ticks) == 0 || ticks[0] != hitTick+20 {
				t.Fatalf("the invulnerability edge: cleared in deltas at ticks %v, want %d (one second after the hit at %d)", ticks, hitTick+20, hitTick)
			}
		}
		if len(damages(s)) != 1 {
			t.Fatalf("%d hits inside the invulnerability window", len(damages(s)))
		}
		walk(inst, id, seq, sim.Input{Right: true}, 8)
		if len(damages(s)) < 2 {
			t.Fatal("no second hit after the invulnerability window")
		}
	})
}

func TestDeathAndRespawn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		persist := make(chan model.Job, 100)
		tr := &transferRecorder{}
		inst, stop := runArena(t, data, arena(time.Hour), persist, application.Options{Transfer: tr})
		defer stop()
		time.Sleep(tickInterval)
		synctest.Wait()
		p := warriorAt(260)
		p.HP = 1
		p.Exp = 10 // level 1 needs 20; the penalty is 5% of 20, floored to 1
		s := join(t, inst, p)
		seq := walk(inst, p.ID, 0, sim.Input{Right: true}, 2)
		{ // the touch kills: Died to everyone, the player is marked dead and frozen
			died := s.ofType(func(e model.Event) bool { _, ok := e.(model.Died); return ok })
			if len(died) != 1 || died[0].(model.Died).ID != p.ID {
				t.Fatalf("died events %+v", died)
			}
			v, _ := s.own(p.ID)
			if !v.Dead || v.HP != 0 {
				t.Fatalf("view after death %+v", v)
			}
			lost := s.ofType(func(e model.Event) bool { _, ok := e.(model.ExpGained); return ok })
			if len(lost) != 1 || lost[0].(model.ExpGained).Amount != -1 || lost[0].(model.ExpGained).Exp != 9 {
				t.Fatalf("the death penalty as ExpGained: %+v, want amount -1 and exp 9", lost)
			}
			x := v.State.X
			inst.Input(p.ID, seq+1, sim.Input{Right: true})
			inst.Attack(p.ID)
			time.Sleep(5 * tickInterval)
			synctest.Wait()
			if v2, ok := s.own(p.ID); ok && v2.State.X != x {
				t.Fatal("a dead character moved")
			}
			if n := len(damages(s)); n != 1 {
				t.Fatalf("%d damage events; a dead character attacked or was hit again", n)
			}
		}
		{ // after 3 seconds: Respawned at the town spawn with 50% HP, the penalty applied, handed to the town
			time.Sleep(3 * time.Second)
			synctest.Wait()
			resp := s.ofType(func(e model.Event) bool { _, ok := e.(model.Respawned); return ok })
			if len(resp) != 1 {
				t.Fatalf("respawned events %+v", resp)
			}
			r := resp[0].(model.Respawned)
			town := data.Town()
			if r.MapID != town.ID || r.X != town.PlayerSpawn.X || r.Y != town.PlayerSpawn.Y || r.HP != 102 {
				t.Fatalf("respawned %+v", r)
			}
			tel := s.ofType(func(e model.Event) bool { _, ok := e.(model.Teleport); return ok })
			if len(tel) != 1 || tel[0].(model.Teleport).MapID != town.ID {
				t.Fatalf("teleport events %+v", tel)
			}
			tr.mu.Lock()
			defer tr.mu.Unlock()
			if len(tr.moved) != 1 || tr.to[0] != town.ID || tr.moved[0].HP != 102 || tr.moved[0].Exp != 9 || tr.moved[0].X != town.PlayerSpawn.X {
				t.Fatalf("transfer %+v to %v", tr.moved, tr.to)
			}
		}
		{ // the arena checkpointed the player on the way out, with the penalized EXP
			var cps []model.Job
			for len(persist) > 0 {
				cps = append(cps, <-persist)
			}
			found := false
			for _, j := range cps {
				if j.Checkpoint != nil && j.Checkpoint.ID == p.ID && j.Checkpoint.Exp == 9 && j.Checkpoint.MapID == data.Town().ID && j.Checkpoint.X == data.Town().PlayerSpawn.X {
					found = true
				}
			}
			if !found {
				t.Fatalf("no checkpoint with the penalty among %+v", cps)
			}
		}
	})
}

func TestKillGivesExpAndLevelsUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		persist := make(chan model.Job, 100)
		inst, stop := runArena(t, data, arena(time.Hour), persist, application.Options{})
		defer stop()
		time.Sleep(tickInterval)
		synctest.Wait()
		p := warriorAt(250)
		p.Exp = 18 // level 1 needs 20; a slime gives 5
		s := join(t, inst, p)
		inst.Attack(p.ID)
		time.Sleep(13 * tickInterval)
		synctest.Wait()
		inst.Attack(p.ID)
		time.Sleep(tickInterval)
		synctest.Wait()
		exp := s.ofType(func(e model.Event) bool { _, ok := e.(model.ExpGained); return ok })
		if len(exp) != 1 || exp[0].(model.ExpGained).Amount != 5 || exp[0].(model.ExpGained).Exp != 3 {
			t.Fatalf("exp events %+v", exp)
		}
		lus := s.ofType(func(e model.Event) bool { _, ok := e.(model.LevelUp); return ok })
		if len(lus) != 1 {
			t.Fatalf("level-up events %+v", lus)
		}
		lu := lus[0].(model.LevelUp)
		// STR 15, VIT 12 after the preset; max HP 60+144+30+18 = 252, max MP 20+8+4 = 32.
		if lu.Level != 2 || lu.Exp != 3 || lu.Stats != (gamedata.Stats{STR: 15, DEX: 5, INT: 4, AGI: 5, VIT: 12}) || lu.HP != 252 || lu.MaxHP != 252 || lu.MP != 32 || lu.MaxMP != 32 {
			t.Fatalf("level-up %+v", lu)
		}
		v, _ := s.own(p.ID)
		if v.Level != 2 || v.HP != 252 || v.MaxHP != 252 {
			t.Fatalf("view after level-up %+v", v)
		}
		var saved []model.Job
		for len(persist) > 0 {
			saved = append(saved, <-persist)
		}
		found := false
		for _, j := range saved {
			if j.LevelUp != nil && j.LevelUp.ID == p.ID && j.LevelUp.Level == 2 && j.LevelUp.HP == 252 && j.LevelUp.Stats.STR == 15 {
				found = true
			}
		}
		if !found {
			t.Fatalf("no level-up job among %+v", saved)
		}
	})
}

func TestReplayingASeedGivesTheSameFight(t *testing.T) {
	fight := func() []model.Damage {
		var out []model.Damage
		synctest.Test(t, func(t *testing.T) {
			data := loadData(t)
			inst, stop := runArena(t, data, arena(time.Second), nil, application.Options{Seed: 42, Roll: nil})
			defer stop()
			time.Sleep(tickInterval)
			synctest.Wait()
			s := join(t, inst, bowmanAt(100))
			for range 40 {
				inst.Attack(s.id(t, inst))
				time.Sleep(4 * tickInterval)
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
		data := loadData(t)
		m := arena(time.Hour)
		m.ID, m.Kind, m.Town = "town", gamedata.Town, "town" // a town with a mob, for the test only
		tr := &transferRecorder{}
		inst, stop := runArena(t, data, m, nil, application.Options{Transfer: tr})
		defer stop()
		time.Sleep(tickInterval)
		synctest.Wait()
		p := warriorAt(260)
		p.HP = 1
		s := join(t, inst, p)
		seq := walk(inst, p.ID, 0, sim.Input{Right: true}, 10) // walk straight through the slime
		seq = walk(inst, p.ID, seq, sim.Input{Left: true}, 4)
		inst.Input(p.ID, seq+1, sim.Input{})
		inst.Attack(p.ID)
		time.Sleep(2 * tickInterval)
		synctest.Wait()
		if n := len(damages(s)); n != 0 {
			t.Fatalf("%d damage events in a town", n)
		}
		v, _ := s.own(p.ID)
		if v.Dead || v.HP != 1 {
			t.Fatalf("after walking through a mob in a town: %+v", v)
		}
		for _, mb := range mobsIn(s) {
			if mb.HP != mb.MaxHP {
				t.Fatalf("a mob was hurt in a town: %+v", mb)
			}
		}
		if len(s.ofType(func(e model.Event) bool { _, ok := e.(model.Died); return ok })) != 0 {
			t.Fatal("died in a town")
		}
	})
}

func TestDeathWithNothingToLoseSendsNoExpGained(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		inst, stop := runArena(t, data, arena(time.Hour), nil, application.Options{})
		defer stop()
		time.Sleep(tickInterval)
		synctest.Wait()
		p := warriorAt(260)
		p.HP, p.Exp = 1, 0 // at the start of the level: the penalty takes nothing
		s := join(t, inst, p)
		walk(inst, p.ID, 0, sim.Input{Right: true}, 2)
		if died := s.ofType(func(e model.Event) bool { _, ok := e.(model.Died); return ok }); len(died) != 1 {
			t.Fatalf("died events %+v", died)
		}
		if lost := s.ofType(func(e model.Event) bool { _, ok := e.(model.ExpGained); return ok }); len(lost) != 0 {
			t.Fatalf("an ExpGained with nothing lost: %+v", lost)
		}
	})
}

func TestRespawnInPlaceWithoutAMover(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		inst, stop := runArena(t, data, arena(time.Hour), nil, application.Options{})
		defer stop()
		time.Sleep(tickInterval)
		synctest.Wait()
		p := warriorAt(260)
		p.HP, p.Exp = 1, 10
		s := join(t, inst, p)
		walk(inst, p.ID, 0, sim.Input{Right: true}, 2)
		time.Sleep(3 * time.Second)
		synctest.Wait()
		resp := s.ofType(func(e model.Event) bool { _, ok := e.(model.Respawned); return ok })
		if len(resp) != 1 || resp[0].(model.Respawned).MapID != "arena" {
			t.Fatalf("respawned %+v", resp)
		}
		v, _ := s.own(p.ID)
		if v.Dead || v.HP != 102 || v.State.X != 100 || v.State.Y != 608 {
			t.Fatalf("after respawn in place: %+v", v)
		}
		if len(s.ofType(func(e model.Event) bool { _, ok := e.(model.Teleport); return ok })) != 0 {
			t.Fatal("a respawn without a mover teleported")
		}
	})
}
