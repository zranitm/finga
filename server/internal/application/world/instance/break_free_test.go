package instance_test

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/internal/shared/sim"
)

// heldBy joins the status caster, of class cls, to an arena whose slime's hop puts on hop, and waits out the hop's landing: the player is held, as of input seq.
func heldBy(t *testing.T, hop string, cls kernel.Class) (inst *instance.Instance, p model.Player, s *worldtest.Sink, data *gamedata.Data, m *gamedata.Map, seq uint32, stop func()) {
	t.Helper()
	data, m = statusData(t, hop, false), arena(t, time.Hour)
	inst, stop = worldtest.RunMap(t, data, m, nil, worldtest.Options{})
	tick(1)
	p = statusCaster()
	p.Class = cls
	s = join(t, inst, p)
	mobHitsOn(t, s, p.ID, 1)
	seq = worldtest.Walk(inst, p.ID, 0, sim.Input{}, 8) // landed from the hop
	if v, _ := s.Own(p.ID); len(v.Statuses) != 1 || v.State.Held == 0 {
		t.Fatalf("after the hop %+v; want held by %s", v, hop)
	}
	return inst, p, s, data, m, seq, stop
}

// escapeRoot is the key each class breaks free from: its escape root, whose chain lists its break-free first.
var escapeRoot = map[kernel.Class]string{"warrior": "roll_back", "thief": "smoke_bomb", "bowman": "disengage", "magician": "blink"}

// breakFree uses p's break-free from their escape root's key going way, as pressed during input seq; the answer comes once a tick resolves it.
func breakFree(inst *instance.Instance, p model.Player, skill string, way sim.Way, seq uint32) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- inst.UseSkill(context.Background(), p.ID, "c1", model.SkillUse{Skill: skill, Root: escapeRoot[p.Class], Way: way}, seq)
	}()
	return done
}

// breakFreeNow is breakFree on the next tick, with its answer.
func breakFreeNow(inst *instance.Instance, p model.Player, skill string) error {
	done := breakFree(inst, p, skill, sim.Way{}, 0)
	tick(1)
	return <-done
}

// C19 §13: a Warrior's Unyielding, granted with no points, goes while each hard status holds them, through cant-act; from the press's next input the status is gone and they walk.
func TestABreakFreeEndsEachHardStatus(t *testing.T) {
	for _, hop := range []string{"{id: stun, time: 2s}", "{id: freeze, time: 2s}", "{id: sleep, time: 2s}", "{id: knockdown}", "{id: pin, time: 2s}"} {
		t.Run(hop, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inst, p, s, _, _, seq, stop := heldBy(t, hop, "warrior")
				defer stop()
				held := viewAfter(t, s, p.ID, seq)
				done := breakFree(inst, p, "unyielding", sim.Way{}, seq)
				tick(1)
				if err := <-done; err != nil {
					t.Fatalf("Unyielding while held: %v, want it used", err)
				}
				seq = worldtest.Walk(inst, p.ID, seq, sim.Input{Down: true}, 1)
				v, _ := s.Own(p.ID)
				if got := viewAfter(t, s, p.ID, seq); len(v.Statuses) != 0 || got.Held != 0 || got.Z >= held.Z {
					t.Fatalf("broken free: statuses %+v, %+v from %+v; want none, walking south", v.Statuses, got, held)
				}
			})
		})
	}
}

// C19 §13: unheld, a break-free is refused with NOT_CONTROLLED, its cooldown not started, silence alone holding no one.
func TestABreakFreeIsRefusedUnheld(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, statusData(t, "{id: silence, time: 2s}", false), arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := statusCaster()
		s := join(t, inst, p)
		if err := breakFreeNow(inst, p, "unyielding"); !errors.Is(err, model.ErrNotControlled) {
			t.Fatalf("Unyielding free: %v, want NOT_CONTROLLED", err)
		}
		mobHitsOn(t, s, p.ID, 1)
		tick(1)
		if err := breakFreeNow(inst, p, "unyielding"); !errors.Is(err, model.ErrNotControlled) {
			t.Fatalf("Unyielding silenced: %v, want NOT_CONTROLLED", err)
		}
	})
	synctest.Test(t, func(t *testing.T) { // refused, it didn't cool down: held, it goes from Roll Back's key alone
		inst, stop := worldtest.RunMap(t, statusData(t, "{id: stun, time: 2s}", false), arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := statusCaster()
		s := join(t, inst, p)
		if err := breakFreeNow(inst, p, "unyielding"); !errors.Is(err, model.ErrNotControlled) {
			t.Fatalf("Unyielding free: %v, want NOT_CONTROLLED", err)
		}
		mobHitsOn(t, s, p.ID, 1)
		tick(1)
		if err := cast(inst, p.ID, "unyielding"); !errors.Is(err, model.ErrChainNotReady) {
			t.Fatalf("Unyielding stunned, from a key of its own: %v, want SKILL_CHAIN_NOT_READY", err)
		}
		if err := breakFreeNow(inst, p, "unyielding"); err != nil {
			t.Fatalf("Unyielding stunned after a refusal: %v, want it used", err)
		}
	})
}

// C19 §13: stunned and silenced, a break-free still goes; it ends the stun alone, so the silence still refuses skills.
func TestAHeldAndSilencedPlayerBreaksFree(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := skillDataWith(t, map[string]func(string) string{"mobs.yaml": func(s string) string {
			s = edit(t, "attacks: [{use: hop}]", "attacks: [{use: hop, status: {id: stun, time: 2s}}]")(s)       // the green slime's
			s = edit(t, "attacks: [{use: bite}]", "attacks: [{use: bite, status: {id: silence, time: 20s}}]")(s) // the wild boar's
			return edit(t, "hp: 34", "hp: 1000000")(s)
		}})
		m := worldtest.Gridded(t, &gamedata.Map{
			ID: "arena", Kind: gamedata.Hunting, Town: "town", Geometry: worldtest.Flat(40),
			Spawns: []gamedata.Spawn{
				{Mob: "green_slime", Max: 1, Respawn: time.Hour, At: gamedata.Point{X: arenaX, Z: slimeZ}, Radius: 0.01},
				{Mob: "wild_boar", Max: 1, Respawn: time.Hour, At: gamedata.Point{X: arenaX, Z: nextToSlime - 1.5}, Radius: 0.01},
			},
			PlayerSpawn: gamedata.Point{X: arenaX, Z: spawnZ},
		})
		inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := statusCaster()
		s := join(t, inst, p)
		both := func() bool {
			v, _ := s.Own(p.ID)
			ids := make([]string, 0, len(v.Statuses))
			for _, st := range v.Statuses {
				ids = append(ids, st.ID)
			}
			return slices.Contains(ids, "stun") && slices.Contains(ids, "silence")
		}
		for n := 0; !both(); n++ {
			if n == 200 {
				t.Fatal("never stunned and silenced at once")
			}
			tick(1)
		}
		if err := breakFreeNow(inst, p, "unyielding"); err != nil {
			t.Fatalf("Unyielding stunned and silenced: %v, want it used", err)
		}
		stepInput(inst, p.ID, 1)
		v, _ := s.Own(p.ID)
		if len(v.Statuses) != 1 || v.Statuses[0].ID != "silence" {
			t.Fatalf("statuses %+v; want the silence alone", v.Statuses)
		}
		tick(12) // Unyielding's 0.4 s over
		if err := cast(inst, p.ID, "rally"); !errors.Is(err, model.ErrSilenced) {
			t.Fatalf("a skill still silenced: %v, want SILENCED", err)
		}
	})
}

// C19 §13: a break-free cools down, and gives no immunity: the next hop stuns again at once, and the break-free is still cooling.
func TestABreakFreeCoolsDownAndTheNextStunTakes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, p, s, _, _, seq, stop := heldBy(t, "{id: stun, time: 1s}", "warrior")
		defer stop()
		done := breakFree(inst, p, "unyielding", sim.Way{}, seq)
		tick(1)
		if err := <-done; err != nil {
			t.Fatalf("Unyielding stunned: %v, want it used", err)
		}
		if used := eventsOf[model.SkillUsed](s); len(used) != 1 || used[0].Skill != "unyielding" {
			t.Fatalf("used %+v; want Unyielding", used)
		}
		mobHitsOn(t, s, p.ID, 2) // 2.5 s on, the dodge long over
		tick(1)
		if v, _ := s.Own(p.ID); len(v.Statuses) != 1 || v.Statuses[0].ID != "stun" {
			t.Fatalf("statuses %+v after the next hop; want stunned again", v.Statuses)
		}
		if err := breakFreeNow(inst, p, "unyielding"); !errors.Is(err, model.ErrSkillCoolingDown) {
			t.Fatalf("Unyielding again within 32 s: %v, want SKILL_COOLING_DOWN", err)
		}
	})
}

// C19 §13: a break-free's move starts at the press's next input from the body the world held that input, as the sim does it and the client predicts it, bit for bit: Vanish's leap back and Arcane Release's blink.
func TestABreakFreesMoveStartsFromTheHeldBody(t *testing.T) {
	for _, tc := range []struct {
		cls   kernel.Class
		skill string
		start func(sim.State, sim.Way, sim.Geometry, sim.Params) sim.State
	}{
		{"thief", "vanish", func(s sim.State, w sim.Way, _ sim.Geometry, p sim.Params) sim.State {
			return sim.Dash(s, w, 18, sim.ThroughMobs, true, p)
		}},
		{"magician", "arcane_release", sim.Blink},
	} {
		t.Run(tc.skill, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inst, p, s, data, m, seq, stop := heldBy(t, "{id: stun, time: 2s}", tc.cls)
				defer stop()
				cls, _ := data.Class(tc.cls)
				derived := instance.StatsOf(data, cls, 1, p.Points, p.Skills, p.Worn, progression.ItemStats{}, worldtest.TickInterval).Derived
				params := data.Movement().Faster(sim.SpeedFactor(derived.MoveSpeed))
				held := viewAfter(t, s, p.ID, seq)
				way := sim.Way{Z: -1, Length: 4} // south, away from the slime
				done := breakFree(inst, p, tc.skill, way, seq)
				tick(1)
				if err := <-done; err != nil {
					t.Fatalf("%s stunned: %v, want it used", tc.skill, err)
				}
				seq = worldtest.Walk(inst, p.ID, seq, sim.Input{}, 1)
				free := held
				free.Held = 0
				want := stepped(tc.start(free, way, m.Geometry, params), sim.Input{}, m.Geometry, params)
				if got := viewAfter(t, s, p.ID, seq); got != want || held.Held == 0 || got.Z >= held.Z {
					t.Fatalf("%s from %+v: %+v; want %+v", tc.skill, held, got, want)
				}
			})
		})
	}
}

// A break-free's leap, Vanish's or Recoil's, passes through the slime in its way and lands 4 m on, clear of its body, where an escape's leap would stop at it.
func TestABreakFreesLeapPassesThroughAMobInItsWay(t *testing.T) {
	for _, tc := range []struct {
		cls   kernel.Class
		skill string
	}{{"thief", "vanish"}, {"bowman", "recoil"}} {
		t.Run(tc.skill, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inst, p, s, data, _, seq, stop := heldBy(t, "{id: stun, time: 2s}", tc.cls)
				defer stop()
				held, slime := viewAfter(t, s, p.ID, seq), slimeOf(t, s)
				kind, _ := data.Mob("green_slime")
				ring := kind.Radius + data.Movement().Radius
				dx, dz := slime.X-held.X, slime.Z-held.Z
				d := math.Hypot(dx, dz)
				if d+ring > 4 {
					t.Fatalf("the slime %v m away; want its far side within the leap's 4 m", d)
				}
				way := sim.Way{X: dx / d, Z: dz / d, Length: 4} // straight at the slime and through it
				done := breakFree(inst, p, tc.skill, way, seq)
				tick(1)
				if err := <-done; err != nil {
					t.Fatalf("%s stunned: %v, want it used", tc.skill, err)
				}
				seq = worldtest.Walk(inst, p.ID, seq, sim.Input{}, 7) // the leap's 18 sub-steps from the input after the press
				got, now := viewAfter(t, s, p.ID, seq), slimeOf(t, s)
				if gone := math.Hypot(got.X-held.X, got.Z-held.Z); math.Abs(gone-4) > 1e-9 {
					t.Fatalf("%s from %+v through the slime at (%v, %v): went %v m to %+v; want 4", tc.skill, held, slime.X, slime.Z, gone, got)
				}
				if apart := math.Hypot(got.X-now.X, got.Z-now.Z); apart < ring-1e-9 { // the slime may have walked up to touch since
					t.Fatalf("%s landed %v m from the slime's centre; want clear of its body, %v", tc.skill, apart, ring)
				}
			})
		})
	}
}

// C19 §13: Unyielding shoves the slime beside it back 1.5 m; Vanish leaves it unable to see its user for 2 s.
func TestBreakFreesRunTheirEffects(t *testing.T) {
	for _, tc := range []struct {
		cls   kernel.Class
		skill string
		check func(t *testing.T, before, after model.MobView)
	}{
		{"warrior", "unyielding", func(t *testing.T, before, after model.MobView) {
			if d := after.Z - before.Z; d < 1.4 || d > 1.6 {
				t.Fatalf("the slime moved %.3f m north, from %+v to %+v; want 1.5", d, before, after)
			}
		}},
		{"thief", "vanish", func(t *testing.T, _, after model.MobView) {
			if !slices.ContainsFunc(after.Statuses, func(st model.StatusView) bool { return st.ID == "lose_sight" }) {
				t.Fatalf("the slime's statuses %+v; want lose_sight", after.Statuses)
			}
		}},
	} {
		t.Run(tc.skill, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inst, p, s, _, _, seq, stop := heldBy(t, "{id: stun, time: 2s}", tc.cls)
				defer stop()
				before := lastMob(s)
				done := breakFree(inst, p, tc.skill, sim.Way{Z: -1, Length: 4}, seq)
				tick(1)
				if err := <-done; err != nil {
					t.Fatalf("%s stunned: %v, want it used", tc.skill, err)
				}
				worldtest.Walk(inst, p.ID, seq, sim.Input{}, 10)
				tc.check(t, before, lastMob(s))
			})
		})
	}
}

// C19 §13: a stun landing between the press and its next input, where the break waits, ends no part of the break-free: the break ends that stun too.
func TestAStunLandingBeforeTheBreakIsBrokenToo(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, p, s, _, _, seq, stop := heldBy(t, "{id: stun, time: 2s}", "warrior")
		defer stop()
		done := breakFree(inst, p, "unyielding", sim.Way{}, seq)
		tick(1)
		if err := <-done; err != nil {
			t.Fatalf("Unyielding stunned: %v, want it used", err)
		}
		mobHitsOn(t, s, p.ID, 2) // no input yet: the break waits for one
		seq = worldtest.Walk(inst, p.ID, seq, sim.Input{Down: true}, 1)
		v, _ := s.Own(p.ID)
		if got := viewAfter(t, s, p.ID, seq); len(v.Statuses) != 0 || got.Held != 0 {
			t.Fatalf("broken free after a second stun: statuses %+v, %+v; want none", v.Statuses, got)
		}
	})
}
