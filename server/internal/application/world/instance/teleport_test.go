package instance_test

import (
	"context"
	"errors"
	"reflect"
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

// longChannel is the shipped data with a 10 s teleport channel, long enough for a slime to reach a player in it.
func longChannel(t *testing.T) *gamedata.Data {
	t.Helper()
	return worldtest.LoadDataWith(t, map[string]func(string) string{"world.yaml": func(s string) string {
		if !strings.Contains(s, "channel: 3s") {
			t.Fatal("world.yaml has no 3 s channel to lengthen")
		}
		return strings.Replace(s, "channel: 3s", "channel: 10s", 1)
	}})
}

// channelTicks is the data's teleport channel in ticks.
func channelTicks(data *gamedata.Data) int {
	return int(model.TicksIn(data.TeleportChannel(), worldtest.TickInterval)) //nolint:gosec // a few hundred ticks
}

// teleportAsync asks for a teleport to mapID pressed during input seq, on its own goroutine, and returns where its answer lands.
func teleportAsync(inst *instance.Instance, id kernel.CharacterID, mapID string, seq uint32) <-chan error {
	done := make(chan error, 1)
	go func() { done <- inst.Teleport(context.Background(), id, "c1", mapID, seq) }()
	return done
}

// teleport starts a channel to mapID on the next tick and returns that tick.
func teleport(t *testing.T, inst *instance.Instance, s *worldtest.Sink, id kernel.CharacterID, mapID string, seq uint32) uint64 {
	t.Helper()
	done := teleportAsync(inst, id, mapID, seq)
	tick(1)
	if err := <-done; err != nil {
		t.Fatalf("teleport to %s: %v", mapID, err)
	}
	return s.Last().Tick
}

func cancels(s *worldtest.Sink) []model.TeleportCancelled {
	return eventsOf[model.TeleportCancelled](s)
}

// B9: at the channel's end tick, the player is handed off to the target map's player spawn, at its yaw, checkpointed there.
func TestATeleportTakesThePlayerToTheMapsSpawnAtTheChannelsEnd(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		cps := &worldtest.Checkpoints{}
		tr := &transferRecorder{}
		inst, stop := worldtest.Run(t, data, cps, worldtest.Options{Transfer: tr, CheckpointInterval: time.Hour})
		defer stop()
		p := atSpawn(data, "Ayla")
		p.Connection = "c1"
		s := join(t, inst, p)
		cps.Drain()

		start := teleport(t, inst, s, p.ID, "forest", 0)
		n := channelTicks(data)
		if n != 60 {
			t.Fatalf("the shipped channel is %d ticks, want 60: 3 s at 50 ms", n)
		}
		tick(n - 1)
		if got := worldtest.Of[model.Teleport](s); len(got) != 0 || moved(tr) != 0 {
			t.Fatalf("handed off %+v before the channel's end", got)
		}
		tick(1)
		forest, _ := data.Map("forest")
		got := worldtest.Of[model.Teleport](s)
		if len(got) != 1 || got[0].Tick != start+uint64(n) || got[0].MapID != "forest" ||
			got[0].X != forest.PlayerSpawn.X || got[0].Z != forest.PlayerSpawn.Z || got[0].Yaw != float32(forest.SpawnYaw) {
			t.Fatalf("teleports %+v; want one to forest's spawn %+v at yaw %v on tick %d, %d after the press", got, forest.PlayerSpawn, forest.SpawnYaw, start+uint64(n), n)
		}
		if tr.mu.Lock(); !reflect.DeepEqual(tr.to, []string{"forest"}) {
			t.Errorf("moved to %v, want forest", tr.to)
		}
		tr.mu.Unlock()
		if cp := cps.Drain(); len(cp) != 1 || cp[0].MapID != "forest" || cp[0].X != forest.PlayerSpawn.X || cp[0].Z != forest.PlayerSpawn.Z {
			t.Errorf("checkpoints %+v, want one on forest at its spawn", cp)
		}
		if c := cancels(s); len(c) != 0 {
			t.Errorf("cancelled %+v", c)
		}
	})
}

func TestATeleportIsRefused(t *testing.T) {
	for _, tt := range []struct {
		name  string
		mapID string
		want  error
	}{
		{"to a map that isn't one", "atlantis", model.ErrTeleportUnknownMap},
		{"to the player's own map", "town", model.ErrTeleportSameMap},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := worldtest.LoadData(t)
				inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Transfer: &transferRecorder{}})
				defer stop()
				p := atSpawn(data, "Ayla")
				p.Connection = "c1"
				join(t, inst, p)
				done := teleportAsync(inst, p.ID, tt.mapID, 0)
				tick(1)
				if err := <-done; !errors.Is(err, tt.want) {
					t.Fatalf("err = %v, want %v", err, tt.want)
				}
			})
		})
	}
	t.Run("while one channels", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			tr := &transferRecorder{}
			inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Transfer: tr})
			defer stop()
			p := atSpawn(data, "Ayla")
			p.Connection = "c1"
			s := join(t, inst, p)
			start := teleport(t, inst, s, p.ID, "forest", 0)
			done := teleportAsync(inst, p.ID, "cave", 0)
			tick(1)
			if err := <-done; !errors.Is(err, model.ErrTeleportBusy) {
				t.Fatalf("err = %v, want ErrTeleportBusy", err)
			}
			tick(channelTicks(data))
			if got := worldtest.Of[model.Teleport](s); len(got) != 1 || got[0].MapID != "forest" || got[0].Tick != start+uint64(channelTicks(data)) {
				t.Fatalf("teleports %+v; want the first channel's, to forest, on time", got)
			}
		})
	})
	t.Run("while dead", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), arena(t, time.Hour), nil, worldtest.Options{Transfer: &transferRecorder{}})
			defer stop()
			tick(1)
			p := warriorAt(nextToSlime)
			p.HP, p.Connection = 1, "c1"
			untilDead(t, join(t, inst, p), p.ID)
			done := teleportAsync(inst, p.ID, "forest", 0)
			tick(1)
			if err := <-done; !errors.Is(err, model.ErrTeleportDead) {
				t.Fatalf("err = %v, want ErrTeleportDead", err)
			}
		})
	})
}

// cancelled checks the player heard one cancel, for why, on tick at, and that the channel's end took them nowhere.
func cancelled(t *testing.T, s *worldtest.Sink, tr *transferRecorder, why model.TeleportCancel, at uint64, rest int) {
	t.Helper()
	if got := cancels(s); len(got) != 1 || got[0].Reason != why || got[0].Tick != at {
		t.Fatalf("cancels %+v; want one, reason %d, on tick %d", got, why, at)
	}
	tick(rest)
	if got := worldtest.Of[model.Teleport](s); len(got) != 0 || moved(tr) != 0 {
		t.Fatalf("handed off %+v after a cancel", got)
	}
}

func TestATeleportIsCancelled(t *testing.T) {
	keys := map[string]sim.Input{"left": {Left: true}, "right": {Right: true}, "up": {Up: true}, "down": {Down: true}, "jump": {Jump: true}}
	for name, in := range keys {
		t.Run("by "+name+" in an input pressed after it", func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := worldtest.LoadData(t)
				tr := &transferRecorder{}
				inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Transfer: tr})
				defer stop()
				p := atSpawn(data, "Ayla")
				p.Connection = "c1"
				s := join(t, inst, p)
				stepInput(inst, p.ID, 1)
				teleport(t, inst, s, p.ID, "forest", 1)
				stepInput(inst, p.ID, 2) // no key held
				if c := cancels(s); len(c) != 0 {
					t.Fatalf("cancelled %+v by an input with no key held", c)
				}
				_ = inst.Input(p.ID, 3, in, 0)
				tick(1)
				cancelled(t, s, tr, model.TeleportMoved, s.Last().Tick, channelTicks(data))
			})
		})
	}
	t.Run("by a key held on through a gap in the inputs, as the gap steps the player on", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			tr := &transferRecorder{}
			inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Transfer: tr})
			defer stop()
			p := atSpawn(data, "Ayla")
			p.Connection = "c1"
			s := join(t, inst, p)
			_ = inst.Input(p.ID, 1, sim.Input{Right: true}, 0)
			tick(1)
			start := teleport(t, inst, s, p.ID, "forest", 1) // and no input after it
			// Her x as of the last frame by tick that has her: a frame names only who changed.
			at := func(tick uint64) float64 {
				x := p.X
				for _, d := range s.Deltas() {
					for _, v := range d.Players {
						if d.Tick <= tick && v.ID == p.ID {
							x = v.State.X
						}
					}
				}
				return x
			}
			for range 20 {
				if len(cancels(s)) > 0 {
					break
				}
				tick(1)
			}
			c := cancels(s)
			if len(c) != 1 || c[0].Reason != model.TeleportMoved || c[0].Tick <= start {
				t.Fatalf("cancels %+v; want one, as a move, once the gap stepped the held key on", c)
			}
			if at(c[0].Tick-1) != at(start) || at(c[0].Tick) <= at(start) {
				t.Fatalf("x %v at the press, %v the tick before the cancel, %v at it; want still through the hold, then a step east cancelling", at(start), at(c[0].Tick-1), at(c[0].Tick))
			}
			tick(channelTicks(data))
			if moved(tr) != 0 {
				t.Fatal("handed off after a cancel")
			}
		})
	})
	t.Run("by an attack starting", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			tr := &transferRecorder{}
			inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{Transfer: tr})
			defer stop()
			tick(1)
			p := warriorAt(outOfSight)
			p.Connection = "c1"
			s := join(t, inst, p)
			teleport(t, inst, s, p.ID, "forest", 0)
			_ = inst.Attack(p.ID, 0, 0)
			tick(1)
			if len(eventsOf[model.AttackUsed](s)) != 1 {
				t.Fatal("the attack didn't start")
			}
			cancelled(t, s, tr, model.TeleportActed, s.Last().Tick, channelTicks(data))
		})
	})
	t.Run("by a skill starting", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			tr := &transferRecorder{}
			inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{Transfer: tr})
			defer stop()
			tick(1)
			p := caster(outOfSight)
			s := join(t, inst, p)
			teleport(t, inst, s, p.ID, "forest", 0)
			if err := cast(inst, p.ID, "strike"); err != nil {
				t.Fatal(err)
			}
			cancelled(t, s, tr, model.TeleportActed, s.Last().Tick, channelTicks(data))
		})
	})
	t.Run("by a mob's hit landing", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := longChannel(t)
			tr := &transferRecorder{}
			inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{Transfer: tr})
			defer stop()
			tick(1)
			p := warriorAt(nextToSlime)
			p.Connection = "c1"
			s := join(t, inst, p)
			teleport(t, inst, s, p.ID, "forest", 0)
			hit := mobHitsOn(t, s, p.ID, 1)[0]
			if hit.Miss {
				t.Fatal("the slime missed")
			}
			cancelled(t, s, tr, model.TeleportHit, hit.Tick, channelTicks(data))
		})
	})
	t.Run("by death, as a death and not a hit", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := longChannel(t)
			tr := &transferRecorder{}
			inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{Transfer: tr})
			defer stop()
			tick(1)
			p := warriorAt(nextToSlime)
			p.HP, p.Connection = 1, "c1"
			s := join(t, inst, p)
			teleport(t, inst, s, p.ID, "forest", 0)
			untilDead(t, s, p.ID)
			died := eventsOf[model.Died](s)
			if got := cancels(s); len(got) != 1 || got[0].Reason != model.TeleportDied || got[0].Tick != died[0].Tick {
				t.Fatalf("cancels %+v; want one, as a death, on the death's tick %d", got, died[0].Tick)
			}
		})
	})
}

// handedOffOnTime checks the channel that started on tick start took the player to forest at its end, uncancelled.
func handedOffOnTime(t *testing.T, s *worldtest.Sink, data *gamedata.Data, start uint64) {
	t.Helper()
	if c := cancels(s); len(c) != 0 {
		t.Fatalf("cancelled %+v", c)
	}
	end := start + uint64(channelTicks(data)) //nolint:gosec // a few hundred ticks
	if left := int(end - s.Last().Tick); left > 0 {
		tick(left)
	}
	if got := worldtest.Of[model.Teleport](s); len(got) != 1 || got[0].MapID != "forest" || got[0].Tick != end {
		t.Fatalf("teleports %+v; want one to forest on tick %d", got, end)
	}
}

func TestATeleportRunsOnThrough(t *testing.T) {
	t.Run("inputs pressed before it, keys held", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Transfer: &transferRecorder{}})
			defer stop()
			p := atSpawn(data, "Ayla")
			p.Connection = "c1"
			s := join(t, inst, p)
			for seq := range uint32(3) {
				_ = inst.Input(p.ID, seq+1, sim.Input{Right: true}, 0)
			}
			start := teleport(t, inst, s, p.ID, "forest", 3) // queued behind the three
			_ = inst.Input(p.ID, 4, sim.Input{}, 0)          // the key let go after the press; the gap after it steps nothing on
			tick(4)
			if v, _ := s.Own(p.ID); v.State.X <= p.X {
				t.Fatalf("at x %v; want the three inputs stepped east of %v", v.State.X, p.X)
			}
			handedOffOnTime(t, s, data, start)
		})
	})
	t.Run("a mob's MISS", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := longChannel(t)
			inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{Transfer: &transferRecorder{}, NewRoll: worldtest.Rolls(0.999)})
			defer stop()
			tick(1)
			p := warriorAt(nextToSlime)
			p.Connection = "c1"
			s := join(t, inst, p)
			start := teleport(t, inst, s, p.ID, "forest", 0)
			if got := mobHitsOn(t, s, p.ID, 1); !got[0].Miss {
				t.Fatalf("hit %+v, want a MISS", got[0])
			}
			handedOffOnTime(t, s, data, start)
		})
	})
	t.Run("a potion", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			potions := &fakePotions{answer: redPotionsLeft(4)}
			inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Potions: potions, ItemCalls: runningPool(t), Transfer: &transferRecorder{}})
			defer stop()
			p := atSpawn(data, "Ayla")
			p.HP, p.Connection = 10, "c1"
			s := join(t, inst, p)
			start := teleport(t, inst, s, p.ID, "forest", 0)
			done := useAsync(inst, p.ID, "red_potion")
			tick(2)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if v, _ := s.Own(p.ID); v.HP != 60 {
				t.Fatalf("HP %d, want the potion's 50 more", v.HP)
			}
			handedOffOnTime(t, s, data, start)
		})
	})
	t.Run("talking to an NPC", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Transfer: &transferRecorder{}})
			defer stop()
			p := besideNPC(t, data, "Ayla", "town_guide")
			p.Connection = "c1"
			s := join(t, inst, p)
			start := teleport(t, inst, s, p.ID, "forest", 0)
			if err := inst.Talk(context.Background(), p.ID, "c1", "town_guide"); err != nil {
				t.Fatal(err)
			}
			tick(1)
			if len(eventsOf[model.NpcDialog](s)) != 1 {
				t.Fatal("no dialog")
			}
			handedOffOnTime(t, s, data, start)
		})
	})
}

// A portal, a leave or a rejoin ends the channel without a word, and its end takes the player nowhere.
func TestATeleportIsDroppedSilently(t *testing.T) {
	t.Run("by a portal taken during it", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			tr := &transferRecorder{}
			inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Transfer: tr})
			defer stop()
			portal := data.StartTown().Portals[0]
			p := worldtest.NewPlayer("Ayla", portal.At.X, portal.At.Z)
			p.Connection = "c1"
			s := join(t, inst, p)
			teleport(t, inst, s, p.ID, "cave", 0)
			_ = inst.EnterPortal(p.ID)
			tick(channelTicks(data) + 1)
			if got := worldtest.Of[model.Teleport](s); len(got) != 1 || got[0].MapID != portal.TargetMap {
				t.Fatalf("teleports %+v; want the portal's alone, to %s", got, portal.TargetMap)
			}
			if c := cancels(s); len(c) != 0 {
				t.Fatalf("cancelled %+v", c)
			}
		})
	})
	t.Run("by a rejoin", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			tr := &transferRecorder{}
			inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Transfer: tr})
			defer stop()
			p := atSpawn(data, "Ayla")
			p.Connection = "c1"
			first := join(t, inst, p)
			teleport(t, inst, first, p.ID, "forest", 0)
			again := join(t, inst, p)
			tick(channelTicks(data))
			for _, s := range []*worldtest.Sink{first, again} {
				if got, c := worldtest.Of[model.Teleport](s), cancels(s); len(got) != 0 || len(c) != 0 {
					t.Fatalf("teleports %+v and cancels %+v after a rejoin", got, c)
				}
			}
			if moved(tr) != 0 {
				t.Fatal("handed off after a rejoin")
			}
		})
	})
	t.Run("by a leave", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := worldtest.LoadData(t)
			tr := &transferRecorder{}
			inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Transfer: tr})
			defer stop()
			p := atSpawn(data, "Ayla")
			p.Connection = "c1"
			s := join(t, inst, p)
			teleport(t, inst, s, p.ID, "forest", 0)
			if err := inst.Leave(p.ID, "c1", nil); err != nil {
				t.Fatal(err)
			}
			tick(channelTicks(data))
			if got, c := worldtest.Of[model.Teleport](s), cancels(s); len(got) != 0 || len(c) != 0 || moved(tr) != 0 {
				t.Fatalf("teleports %+v and cancels %+v after a leave", got, c)
			}
		})
	})
}

// An item call in flight at the channel's end holds the hand-off until it settles, as at a portal, so the item goes along.
func TestATeleportWaitsForAnItemCallInFlight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		eq := &fakeEquipment{answer: sword(), gate: make(chan struct{})}
		tr := &transferRecorder{}
		inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Equipment: eq, ItemCalls: runningPool(t), Transfer: tr})
		defer stop()
		p := atSpawn(data, "Ayla")
		p.Connection = "c1"
		s := join(t, inst, p)
		start := teleport(t, inst, s, p.ID, "forest", 0)
		done := equipAsync(inst, p.ID, "c1")
		tick(channelTicks(data) + 2)
		if moved(tr) != 0 {
			t.Fatal("handed off with an equip in flight")
		}
		close(eq.gate)
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		tick(1)
		got := worldtest.Of[model.Teleport](s)
		if len(got) != 1 || got[0].MapID != "forest" || got[0].Tick <= start+uint64(channelTicks(data)) {
			t.Fatalf("teleports %+v; want one to forest once the equip settled", got)
		}
		tr.mu.Lock()
		defer tr.mu.Unlock()
		if len(tr.moved) != 1 || !reflect.DeepEqual(tr.moved[0].Worn, sword().Worn) {
			t.Fatalf("moved %+v; want the teleport to carry the sword", tr.moved)
		}
	})
}
