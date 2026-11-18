package instance_test

import (
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
	"finga/internal/shared/sim"
)

// blockSkills are the warrior's test blocks: Bulwark for 10 s, so a test can wait for blows in it; Brief for 3 ticks, to hold its edges to the tick; Shell blocking and dodging at once; and Sidestep, a dodge in place alone.
const blockSkills = `  - id: test_bulwark
    icon: lorc/checked-shield
    kind: active
    effect: block
    granted: true
    energy: 0
    cooldown: 1s
    length: 10s
    block: 10s
    stages:
      - {at: 0, name: Bulwark, description: "Blocks for 10 s."}
  - id: test_brief
    icon: lorc/checked-shield
    kind: active
    effect: block
    granted: true
    energy: 0
    cooldown: 1s
    length: 150ms
    block: 150ms
    stages:
      - {at: 0, name: Brief, description: "Blocks for 3 ticks."}
  - id: test_shell
    icon: lorc/checked-shield
    kind: active
    effect: block
    granted: true
    energy: 0
    cooldown: 1s
    length: 10s
    block: 10s
    dodge: 10s
    stages:
      - {at: 0, name: Shell, description: "Blocks and dodges for 10 s."}
  - id: test_sidestep
    icon: lorc/dodging
    kind: active
    effect: move
    energy: 1
    cooldown: 1s
    length: 10s
    move: {kind: leap, distance: 1, time: 300ms, toward: back}
    dodge: 10s
    stages:
      - {at: 0, name: Sidestep, description: "Dodges for 10 s."}
`

// blockData is the shipped data with the arena's slime taking attacks in turn and too tough to fall, and the warrior's test blocks.
func blockData(t *testing.T, attacks string) *gamedata.Data {
	t.Helper()
	return skillDataWith(t, map[string]func(string) string{
		"classes.yaml": edit(t, "bastion, rally]", "bastion, rally, test_bulwark, test_brief, test_shell, test_sidestep]"),
		"skills.yaml":  func(s string) string { return s + blockSkills },
		"mobs.yaml": func(s string) string {
			s = edit(t, "attacks: [{use: hop}]", "attacks: "+attacks)(s)
			return edit(t, "hp: 34", "hp: 1000000")(s)
		},
	})
}

// kingArena is the arena with King Slime where its slime stands: it hops, then slams with a stun no block stops. Its body reaches 1.5 m south of it.
func kingArena(t *testing.T) *gamedata.Map {
	m := arena(t, time.Hour)
	m.Spawns[0].Mob = "king_slime"
	return m
}

// byTheKing is a metre south of King Slime's body: in its hop's and slam's reach.
const byTheKing = slimeZ - 2.5

// mobHitsWithin waits, a tick at a time and at most 10 s, until s has n mob attacks on the player, and returns them.
func mobHitsWithin(t *testing.T, s *worldtest.Sink, id kernel.CharacterID, n int) []model.Damage {
	t.Helper()
	for range 200 {
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
	t.Fatalf("fewer than %d mob attacks on the player in 10 s; damages %+v", n, damages(s))
	return nil
}

// blocker is a warrior at z with 3 energy and a point in Ground Slam, Bastion's counter, and in Sidestep.
func blocker(z float64) model.Player {
	p := warriorAt(z)
	p.Connection, p.Energy = "c1", 3
	p.Skills = model.Skills{Points: map[string]int{"ground_slam": 1, "test_sidestep": 1}}
	return p
}

// north is the yaw facing the slime from the players' line.
const north float32 = 0

// guarded uses skill aimed along aim and waits for its tick.
func guarded(t *testing.T, inst *instance.Instance, id kernel.CharacterID, skill string, aim float32) {
	t.Helper()
	done := useAt(inst, id, skill, aim, 0)
	tick(1)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// C21 §3: a bite, a hop, a charge and a spit from the front are blocked: BLOCK, with no damage, knockback, status or hit guard, and the block's 2 energy.
func TestABlockStopsEachKindOfMobAttackFromTheFront(t *testing.T) {
	for _, c := range []struct {
		use string
		z   float64
	}{{"bite", nextToSlime}, {"hop", nextToSlime}, {"charge", nextToSlime}, {"spit", farFromSlime}} {
		t.Run(c.use, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inst, stop := worldtest.RunMap(t, blockData(t, "[{use: "+c.use+", status: {id: slow, time: 2s}}]"), arena(t, time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := blocker(c.z)
				s := join(t, inst, p)
				before, _ := s.Own(p.ID)
				guarded(t, inst, p.ID, "test_bulwark", north)
				hit := mobHitsOn(t, s, p.ID, 1)[0]
				if !hit.Block || hit.Amount != 0 || hit.Miss || hit.Dodge {
					t.Fatalf("the %s on the block: %+v, want a BLOCK with no damage", c.use, hit)
				}
				tick(2)
				v, _ := s.Own(p.ID)
				if v.HP != before.HP || v.State.X != before.State.X || v.State.Z != before.State.Z || v.Invulnerable || len(v.Statuses) != 0 {
					t.Fatalf("after the blocked %s %+v, want the HP and place of %+v, no guard and no slow", c.use, v, before)
				}
				if got := energiesTold(s); !slices.Equal(got, []int{3, 5}) {
					t.Fatalf("energy told %v, want 3, then the block's 2", got)
				}
			})
		})
	}
}

// C21 §7: a 3-tick block stops a bite landing on its first tick and on its last, and not one a tick past either end. A bite lands 12 ticks into its windup.
func TestABlockHoldsFromItsFirstTickToItsLast(t *testing.T) {
	for _, c := range []struct {
		name    string
		from    int // the block's first tick, from the bite's landing
		blocked bool
	}{
		{"starting as it lands", 0, true},
		{"starting a tick after", 1, false},
		{"its last tick as it lands", -2, true},
		{"ended a tick before", -3, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inst, stop := worldtest.RunMap(t, blockData(t, "[{use: bite}]"), arena(t, time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := blocker(nextToSlime)
				s := join(t, inst, p)
				w := windupOn(t, s) // the world has stepped its tick, and the next press goes on the one after
				lands := w.Tick + 12
				tick(int(int64(lands)+int64(c.from)-int64(w.Tick)) - 1)
				guarded(t, inst, p.ID, "test_brief", north)
				hit := mobHitsOn(t, s, p.ID, 1)[0]
				if hit.Tick != lands || hit.Block != c.blocked {
					t.Fatalf("the bite %+v, want it at tick %d, blocked %v", hit, lands, c.blocked)
				}
			})
		})
	}
}

// C21 §3: a block covers the half in front of its aim, its edges in: a bite from the north is blocked facing north, east or west, and lands facing a little past east, or south.
func TestABlockCoversTheFrontHalf(t *testing.T) {
	for _, c := range []struct {
		name    string
		aim     float32
		blocked bool
	}{
		{"facing it", north, true},
		{"its east edge", math.Pi / 2, true},
		{"its west edge", -math.Pi / 2, true},
		{"just past the edge", math.Pi/2 + 0.05, false},
		{"from behind", math.Pi, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inst, stop := worldtest.RunMap(t, blockData(t, "[{use: bite}]"), arena(t, time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := blocker(nextToSlime)
				s := join(t, inst, p)
				guarded(t, inst, p.ID, "test_bulwark", c.aim)
				if hit := mobHitsOn(t, s, p.ID, 1)[0]; hit.Block != c.blocked || !c.blocked && hit.Amount == 0 && !hit.Miss {
					t.Fatalf("a bite from the north on a block at yaw %v: %+v, want blocked %v", c.aim, hit, c.blocked)
				}
			})
		})
	}
}

// C21 §3: a use's block gives energy once, at its first blocked attack, however many it stops; the next use gives it again.
func TestABlockGivesItsEnergyOnceAUse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, blockData(t, "[{use: bite}]"), arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := blocker(nextToSlime)
		s := join(t, inst, p)
		guarded(t, inst, p.ID, "test_bulwark", north)
		for _, d := range mobHitsWithin(t, s, p.ID, 3) {
			if !d.Block {
				t.Fatalf("mob attacks %+v, want three blocked", mobHitsWithin(t, s, p.ID, 3))
			}
		}
		if got := energiesTold(s); !slices.Equal(got, []int{3, 5}) {
			t.Fatalf("energy told %v after three blocked bites, want 3, then 2 once", got)
		}
		tick(int(10 * time.Second / worldtest.TickInterval)) // past the use's 10 s
		guarded(t, inst, p.ID, "test_bulwark", north)
		next := len(mobHitsWithin(t, s, p.ID, 1)) + 1
		if hit := mobHitsWithin(t, s, p.ID, next)[next-1]; !hit.Block {
			t.Fatalf("the bite on the second use %+v, want it blocked", hit)
		}
		if got := energiesTold(s); !slices.Equal(got, []int{3, 5, 7}) {
			t.Fatalf("energy told %v after a second use's block, want 2 more", got)
		}
	})
}

// C21 §3: a hit landing in both a dodge and a block passes through as a dodge, and gives no block energy.
func TestADodgeWinsOverABlock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, blockData(t, "[{use: bite}]"), arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := blocker(nextToSlime)
		s := join(t, inst, p)
		guarded(t, inst, p.ID, "test_shell", north)
		if hit := mobHitsOn(t, s, p.ID, 1)[0]; !hit.Dodge || hit.Block {
			t.Fatalf("a bite on a dodge and a block %+v, want a DODGE", hit)
		}
		if got := energiesTold(s); !slices.Equal(got, []int{3}) {
			t.Fatalf("energy told %v, want no block's 2", got)
		}
	})
}

// C21 §3: King Slime's slam goes through a block, its stun ending the block, so the hop after it lands too; a dodge takes the slam.
func TestABossSlamGoesThroughABlock(t *testing.T) {
	t.Run("blocked", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			inst, stop := worldtest.RunMap(t, blockData(t, "[{use: hop}]"), kingArena(t), nil, worldtest.Options{})
			defer stop()
			tick(1)
			p := blocker(byTheKing)
			s := join(t, inst, p)
			guarded(t, inst, p.ID, "test_bulwark", north)
			hits := mobHitsWithin(t, s, p.ID, 3) // the hop, the slam, the next hop
			hop, slam, next := hits[0], hits[1], hits[2]
			if !hop.Block || slam.Block || slam.Dodge || next.Block {
				t.Fatalf("King Slime's hop, slam and hop on a block: %+v, want the first hop blocked, the slam through it, and its stun ending the block", hits)
			}
			if next.Tick > hop.Tick+uint64(10*time.Second/worldtest.TickInterval) {
				t.Fatalf("the next hop at tick %d, past the block's 10 s from %d: it shows nothing", next.Tick, hop.Tick)
			}
		})
	})
	t.Run("dodged", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			inst, stop := worldtest.RunMap(t, blockData(t, "[{use: hop}]"), kingArena(t), nil, worldtest.Options{})
			defer stop()
			tick(1)
			p := blocker(byTheKing)
			s := join(t, inst, p)
			done := useWay(inst, p.ID, "test_sidestep", sim.Way{Z: -1}, 0) // in place
			tick(1)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if hits := mobHitsWithin(t, s, p.ID, 2); !hits[0].Dodge || !hits[1].Dodge {
				t.Fatalf("King Slime's hop and slam on a dodge %+v, want both dodged", hits)
			}
		})
	})
}

// C21 §3: Ground Slam follows Bastion within 1.5 s and 150 ms of the last block, counted in inputs from the one its frame acked; with no block it isn't offered.
func TestABlockedFollowUpHoldsWithinItsWindow(t *testing.T) {
	for _, c := range []struct {
		name  string
		block bool
		after uint32 // inputs from the block's frame's ack to the press
		ok    bool
	}{
		{"no block", false, 0, false},
		{"1.5 s and 3 inputs after", true, 33, true},
		{"1.5 s and 4 inputs after", true, 34, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inst, stop := worldtest.RunMap(t, blockData(t, "[{use: bite}]"), arena(t, time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := blocker(nextToSlime)
				p.Energy = 10
				s := join(t, inst, p)
				seq := worldtest.Walk(inst, p.ID, 0, sim.Input{}, 1)
				if !c.block {
					if err := press(t, inst, p.ID, "ground_slam", "bastion", 0, seq); !errors.Is(err, model.ErrChainNotReady) {
						t.Fatalf("Ground Slam with no block: %v, want ErrChainNotReady", err)
					}
					return
				}
				for len(worldtest.Of[model.MobAttacked](s)) == 0 {
					seq = worldtest.Walk(inst, p.ID, seq, sim.Input{}, 1)
				}
				done := useAt(inst, p.ID, "bastion", north, 0) // its 12 ticks hold the bite, 12 into its windup
				seq = worldtest.Walk(inst, p.ID, seq, sim.Input{}, 1)
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				for len(damages(s)) == 0 {
					seq = worldtest.Walk(inst, p.ID, seq, sim.Input{}, 1)
				}
				blocked := damages(s)[0]
				if !blocked.Block {
					t.Fatalf("the bite on Bastion %+v, want it blocked", blocked)
				}
				var acked uint32
				for _, d := range s.Deltas() {
					if d.Tick == blocked.Tick {
						acked = d.AckSeq
					}
				}
				at := acked + c.after
				if at < seq {
					t.Fatalf("the press at input %d is behind the world's %d", at, seq)
				}
				worldtest.Walk(inst, p.ID, seq, sim.Input{}, int(at-seq))
				err := press(t, inst, p.ID, "ground_slam", "bastion", 0, at)
				if (err == nil) != c.ok || err != nil && !errors.Is(err, model.ErrChainNotReady) {
					t.Fatalf("Ground Slam %d inputs after the block's frame: %v, want ok %v", c.after, err, c.ok)
				}
			})
		})
	}
}

// C21, from the live pass: Deflect blocks, then springs back. Leaping from the press, it cleared the bite and missed its energy and counter; now a front blow in its guard is blocked, with both, and the leap starts on the input after the guard's last, exactly as one from that input would.
func TestDeflectBlocksThenLeaps(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data, m := blockData(t, "[{use: bite}]"), arena(t, time.Hour)
		inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := blocker(nextToSlime)
		p.Class, p.Skills = "bowman", model.Skills{Points: map[string]int{"power_shot": 1}}
		s := join(t, inst, p)
		seq := worldtest.Walk(inst, p.ID, 0, sim.Input{}, 1)
		for len(worldtest.Of[model.MobAttacked](s)) == 0 {
			seq = worldtest.Walk(inst, p.ID, seq, sim.Input{}, 1)
		}
		way := sim.Way{Z: -1, Length: 2} // back, from the slime north
		pressedAt := seq
		pressed, _ := s.Own(p.ID)
		done := useWay(inst, p.ID, "deflect", way, seq) // aimed north, its 12 ticks of guard hold the bite, 12 into its windup
		tick(1)                                         // cast before the next input, so it counts from this one
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		guardDown := pressedAt + 12 // 600 ms
		for seq < guardDown {
			seq = worldtest.Walk(inst, p.ID, seq, sim.Input{}, 1)
		}
		hits := damages(s)
		if len(hits) != 1 || !hits[0].Block {
			t.Fatalf("mob attacks in Deflect's guard %+v, want the bite blocked", hits)
		}
		if got := energiesTold(s); !slices.Equal(got, []int{3, 5}) {
			t.Fatalf("energy told %v, want the block's 2", got)
		}
		own, _ := s.Own(p.ID)
		still := own.State // standing, so no frame since the press has carried it
		if still.Z != pressed.State.Z || still.Move != (sim.Move{}) {
			t.Fatalf("as the guard ends %+v, want standing where it was pressed %+v", still, pressed.State)
		}
		seq = worldtest.Walk(inst, p.ID, seq, sim.Input{}, 1)
		leap := sim.Dash(still, way, 6*3, sim.StopAtMob, true, data.Movement()) // 300 ms, three sub-steps a tick
		got, want := viewAfter(t, s, p.ID, seq), replayed(leap, 1, m.Geometry, data.Movement())
		if got.X != want.X || got.Z != want.Z || got.Move != want.Move || got.OnGround { // its hop's arc goes by the player's own move speed
			t.Fatalf("on the input after the guard %+v, want the leap's first three sub-steps %+v, off the ground", got, want)
		}
		seq = worldtest.Walk(inst, p.ID, seq, sim.Input{}, 5)
		if got := viewAfter(t, s, p.ID, seq); math.Abs(still.Z-got.Z-2) > 1e-9 || got.Move != (sim.Move{}) {
			t.Fatalf("as the leap ends %+v, want 2 m back of %v", got, still.Z)
		}
		if err := press(t, inst, p.ID, "power_shot", "deflect", 0, seq); err != nil {
			t.Fatalf("Power Shot from Deflect's key after the leap: %v, want the counter taken", err)
		}
	})
}
