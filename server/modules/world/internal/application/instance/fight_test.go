package instance_test

import (
	"context"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/internal/sim"
	"finga/modules/world/internal/application/instance"
	"finga/modules/world/internal/model"
	"finga/modules/world/worldtest"
)

// arena has a floor at 608 and one still slime on it at x 300, its spawn area 32 px wide.
func arena(respawn time.Duration) *gamedata.Map {
	return &gamedata.Map{
		ID: "arena", Kind: gamedata.Hunting, Town: "town", Width: 1280, Height: 640,
		Geometry: sim.Geometry{
			Solids: []sim.Rect{{X: 0, Y: 608, W: 1280, H: 32}},
		},
		Spawns:      []gamedata.Spawn{{Mob: "green_slime", Max: 1, Respawn: respawn, Area: sim.Rect{X: 284, Y: 580, W: 32, H: 28}}},
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

// nextToSlime is where a player stands on the ground in the arena slime's reach: their box from 258 to 282, its hop landing from 272.
const nextToSlime = 270.0

// mobHitsOn waits, a tick at a time and at most 3 s, until s has n mob attacks on the player, hit or MISS, and returns them.
func mobHitsOn(t *testing.T, s *worldtest.Sink, id kernel.CharacterID, n int) []model.Damage {
	t.Helper()
	for range 60 {
		var got []model.Damage
		for _, d := range damages(s) {
			if d.Source.IsMob() && d.Target.Player == id {
				got = append(got, d)
			}
		}
		if len(got) >= n {
			return got
		}
		tick(1)
	}
	t.Fatalf("fewer than %d mob attacks on the player in 3 s; damages %+v", n, damages(s))
	return nil
}

// windupOn waits, a tick at a time and at most 3 s, for a mob's attack to begin, and returns it.
func windupOn(t *testing.T, s *worldtest.Sink) model.MobAttacked {
	t.Helper()
	for range 60 {
		if w := worldtest.Of[model.MobAttacked](s); len(w) > 0 {
			return w[0]
		}
		tick(1)
	}
	t.Fatal("no mob attacked in 3 s")
	return model.MobAttacked{}
}

// untilDead waits, a tick at a time and at most 3 s, for the player to die.
func untilDead(t *testing.T, s *worldtest.Sink, id kernel.CharacterID) {
	t.Helper()
	for range 60 {
		if v, ok := s.Own(id); ok && v.Dead {
			return
		}
		tick(1)
	}
	t.Fatal("the player didn't die in 3 s")
}

// warriorAt is a new warrior, wearing the starter sword at its lowest roll as every new warrior does: two hits kill a slime.
func warriorAt(x float64) model.Player {
	p := worldtest.NewPlayer("Ayla", x, 608)
	p.Worn = model.Worn{Revision: 1, Items: []model.WornItem{{Template: "wooden_sword", Stats: progression.ItemStats{PhysicalAttack: 10}}}}
	return p
}

func bowmanAt(x float64) model.Player {
	p := model.Player{ID: kernel.NewCharacterID(), Name: "Robin", Class: kernel.Class("bowman"), Level: 1, HP: 105, X: x, Y: 608}
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
		{ // the snapshot carries the one spawned mob, standing on the floor, turned to the player it sees on its left
			mobs := mobsIn(s)
			if len(mobs) != 1 {
				t.Fatalf("%d mobs, want 1", len(mobs))
			}
			for _, m := range mobs {
				if m.Type != "green_slime" || m.X != 300 || m.Y != 608 || m.HP != slime.HP || m.MaxHP != slime.HP || m.Facing != -1 {
					t.Fatalf("mob %+v", m)
				}
			}
		}
		{ // two hits kill it (24 damage each with the starter sword, against 34 HP); it leaves the map
			_ = inst.Attack(idOf(t, s))
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			time.Sleep(17 * worldtest.TickInterval) // the rest of the 900 ms attack
			synctest.Wait()
			_ = inst.Attack(idOf(t, s))
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
			_ = inst.Attack(idOf(t, far))
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			if len(damages(far)) != 0 {
				t.Fatal("a Warrior hit a mob 184 px away")
			}
			_ = inst.Leave(idOf(t, far), "", nil)
			behind := join(t, inst, warriorAt(340)) // the mob is behind, facing right
			_ = inst.Attack(idOf(t, behind))
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			if len(damages(behind)) != 0 {
				t.Fatal("a Warrior hit a mob behind their back")
			}
			_ = inst.Leave(idOf(t, behind), "", nil)
			near := join(t, inst, warriorAt(250))
			_ = inst.Attack(idOf(t, near))
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
			_ = inst.Attack(idOf(t, s))
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
				_ = inst.Attack(id)
			}
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			for range 5 {
				_ = inst.Attack(id)
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
		_ = inst.Attack(id)
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		time.Sleep(10 * worldtest.TickInterval) // inside the attack
		synctest.Wait()
		_ = inst.Attack(id)
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

// C12: a hop hurts, knocks back and blinks for the hit guard, which a second hop can't pass; the next comes at its cooldown.
func TestAMobsAttackHurtsKnocksBackAndGuards(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		m := arena(time.Hour)
		m.Spawns[0].Max = 2 // both at 300, hopping together
		inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{})
		defer stop()
		tick(1)
		s := join(t, inst, warriorAt(296)) // inside the slimes, as after landing on them
		id := idOf(t, s)
		mobHitsOn(t, s, id, 1)
		tick(1)
		got := mobHitsOn(t, s, id, 1)
		if len(got) != 1 {
			t.Fatalf("mob hits %+v; want one, the second slime's inside the guard", got)
		}
		// Slime attack 6 against defense 12: 6·100/112 = 5.4 → 5.
		if got[0].Amount != 5 {
			t.Fatalf("hop damage %d, want 5", got[0].Amount)
		}
		v, _ := s.Own(id)
		if v.HP != 139 || v.State.VY >= 0 || !v.Invulnerable {
			t.Fatalf("after the hit %+v; want 139 HP, the knockback's hop, and the blink", v)
		}
		first := got[0].Tick
		got = mobHitsOn(t, s, id, 2)
		if got[1].Tick != first+50 {
			t.Fatalf("hit again at tick %d; want %d, the hop's 2.5 s cooldown after the first", got[1].Tick, first+50)
		}
		var cleared []uint64
		for _, d := range s.Deltas() {
			for _, pv := range d.Players {
				if pv.ID == id && !pv.Invulnerable && d.Tick > first {
					cleared = append(cleared, d.Tick)
				}
			}
		}
		if len(cleared) == 0 || cleared[0] != first+30 {
			t.Fatalf("the blink cleared in deltas at ticks %v; want %d, 1.5 s after the hit at %d", cleared, first+30, first)
		}
	})
}

// C12: everyone on the map hears a mob's windup, its attack and the way it faces, as it begins.
func TestEveryoneHearsAMobsWindup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		watcher := join(t, inst, warriorAt(100))
		slime := mobsIn(watcher)
		near := join(t, inst, warriorAt(nextToSlime))
		w := windupOn(t, watcher)
		if _, ok := slime[w.Mob]; !ok || w.Attack != 0 || w.Facing != -1 {
			t.Fatalf("heard %+v; want the slime's hop, facing the player on its left", w)
		}
		if got := worldtest.Of[model.MobAttacked](near); len(got) != 1 || got[0] != w {
			t.Fatalf("the player in reach heard %+v; want the same windup", got)
		}
	})
}

// C12: three blows in a tick knock a mob down, and a player arriving then sees it down in their snapshot.
func TestAPlayerArrivingSeesAMobDown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := arena(time.Hour)
		m.Spawns[0].Mob = "orange_mushroom" // three bowmen's blows leave it standing
		inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), m, nil, worldtest.Options{})
		defer stop()
		tick(1)
		var bowmen []*worldtest.Sink
		for range 3 {
			bowmen = append(bowmen, join(t, inst, bowmanAt(100)))
		}
		for _, s := range bowmen {
			_ = inst.Attack(idOf(t, s))
		}
		landed := func() (n int) {
			for _, d := range damages(bowmen[0]) {
				if d.Target.IsMob() && !d.Miss {
					n++
				}
			}
			return n
		}
		for range 30 {
			if landed() >= 3 {
				break
			}
			tick(1)
		}
		if n := landed(); n != 3 {
			t.Fatalf("%d blows landed; want the three bowmen's", n)
		}
		snap := join(t, inst, warriorAt(600)).All()[0].(model.Snapshot)
		if len(snap.Mobs) != 1 || snap.Mobs[0].Pose != model.Down {
			t.Fatalf("the arriving player saw %+v; want the mushroom down", snap.Mobs)
		}
	})
}

// A dodged attack neither knocks back nor blinks, but starts the hit guard: the second slime's hop landing with it is skipped.
func TestADodgedAttackNeitherKnocksBackNorBlinks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		m := arena(time.Hour)
		m.Spawns[0].Max = 2
		inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{NewRoll: worldtest.Rolls(0.999)})
		defer stop()
		tick(1)
		s := join(t, inst, warriorAt(nextToSlime))
		id := idOf(t, s)
		mobHitsOn(t, s, id, 1)
		tick(1)
		got := mobHitsOn(t, s, id, 1)
		if len(got) != 1 || !got[0].Miss {
			t.Fatalf("mob attacks %+v, want one MISS", got)
		}
		v, _ := s.Own(id)
		if v.HP != 144 || !v.State.OnGround || v.State.VY != 0 || v.State.X != nextToSlime || v.Invulnerable {
			t.Fatalf("after the MISS %+v, want full HP, on the ground where they stood, not blinking", v)
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
		p := warriorAt(nextToSlime)
		p.HP = 1
		p.Exp = 10 // level 1 needs 20; the penalty is 5% of 20, floored to 1
		p.Energy = 7
		s := join(t, inst, p)
		untilDead(t, s, p.ID)
		{ // the slime's hop kills: Died to everyone, the player is marked dead and frozen, the energy gone
			died := ofType(s, func(e any) bool { _, ok := e.(model.Died); return ok })
			if len(died) != 1 || died[0].(model.Died).ID != p.ID {
				t.Fatalf("died events %+v", died)
			}
			v, _ := s.Own(p.ID)
			if !v.Dead || v.HP != 0 {
				t.Fatalf("view after death %+v", v)
			}
			if got := energiesTold(s); !slices.Equal(got, []int{7, 0}) {
				t.Fatalf("energy told %v, want 7 at the join and 0 at the death", got)
			}
			lost := ofType(s, func(e any) bool { _, ok := e.(model.ExpGained); return ok })
			if len(lost) != 1 || lost[0].(model.ExpGained).Amount != -1 || lost[0].(model.ExpGained).Exp != 9 {
				t.Fatalf("the death penalty as ExpGained: %+v, want amount -1 and exp 9", lost)
			}
			x := v.State.X
			_ = inst.Input(p.ID, 1, sim.Input{Right: true}, 0)
			_ = inst.Attack(p.ID)
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
			if len(tr.moved) != 1 || tr.to[0] != town.ID || tr.moved[0].HP != 72 || tr.moved[0].Exp != 9 || tr.moved[0].X != town.PlayerSpawn.X || tr.moved[0].Energy != 0 {
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
		_ = inst.Attack(p.ID)
		time.Sleep(18 * worldtest.TickInterval) // the 900 ms attack
		synctest.Wait()
		_ = inst.Attack(p.ID)
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
		// The level's 5 points wait unspent, so the stats stay the class's; max HP 60+18+2×7+10×7 = 162.
		if lu.Level != 2 || lu.Exp != 6 || lu.Stats != (gamedata.Stats{STR: 7, DEX: 7, INT: 7, AGI: 7, VIT: 7}) || lu.HP != 162 || lu.MaxHP != 162 ||
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
		if got := energiesTold(s); !slices.Equal(got, []int{0, 2, 4}) {
			t.Fatalf("energy told %v, want 0, 2, 4: each attack's 2, which the level-up leaves", got)
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
				_ = inst.Attack(idOf(t, s))
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
		p := warriorAt(296) // inside the slime, as after landing on it
		p.HP = 1
		s := join(t, inst, p)
		seq := worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, 10)
		seq = worldtest.Walk(inst, p.ID, seq, sim.Input{Left: true}, 4)
		_ = inst.Input(p.ID, seq+1, sim.Input{}, 0)
		_ = inst.Attack(p.ID)
		time.Sleep(2 * worldtest.TickInterval)
		synctest.Wait()
		if n := len(damages(s)); n != 0 {
			t.Fatalf("%d damage events in a town", n)
		}
		v, _ := s.Own(p.ID)
		if v.Dead || v.HP != 1 {
			t.Fatalf("after standing in a mob in a town: %+v", v)
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
		p := warriorAt(nextToSlime)
		p.HP, p.Exp = 1, 0 // at the start of the level: the penalty takes nothing
		s := join(t, inst, p)
		untilDead(t, s, p.ID)
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
		p := warriorAt(nextToSlime)
		p.HP, p.Exp = 1, 10
		s := join(t, inst, p)
		untilDead(t, s, p.ID)
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
		p := warriorAt(nextToSlime)
		p.HP, p.Exp, p.Energy = 1, 10, 7
		s := join(t, inst, p)
		untilDead(t, s, p.ID)
		if v, _ := s.Own(p.ID); !v.Dead {
			t.Fatalf("not dead: %+v", v)
		}
		cps.Drain()
		_ = inst.Leave(p.ID, "", nil)
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		town := data.StartTown()
		saved := cps.Drain()
		if len(saved) != 1 || saved[0].MapID != town.ID || saved[0].X != town.PlayerSpawn.X || saved[0].Y != town.PlayerSpawn.Y || saved[0].HP != 72 || saved[0].Exp != 9 || saved[0].Energy != 0 {
			t.Fatalf("checkpoint of a dead player %+v, want town's spawn with 72 HP, EXP 9 and no energy", saved)
		}
	})
}
