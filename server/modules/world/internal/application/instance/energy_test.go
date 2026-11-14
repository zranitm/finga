package instance_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/sim"
	"finga/modules/world/internal/model"
	"finga/modules/world/worldtest"
)

// A basic attack gains 2 energy as it reaches a mob, a MISS too, and the player's next frame tells it.
func TestABasicAttackGainsEnergyAsItReachesAMob(t *testing.T) {
	for _, tc := range []struct {
		name string
		roll float64
		miss bool
	}{
		{"a hit", 0.5, false},
		{"a MISS", 0.999, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := timelineData(t, nil)
				inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{NewRoll: worldtest.Rolls(tc.roll)})
				defer stop()
				tick(1)
				p := warriorAt(250)
				s := join(t, inst, p)
				length, _ := attackTicks(t, data, p)
				_ = inst.Attack(p.ID, 0)
				tick(int(length))
				if got := damages(s); len(got) != 1 || got[0].Miss != tc.miss {
					t.Fatalf("damages %+v, want one with MISS %t", got, tc.miss)
				}
				if got := energiesTold(s); !slices.Equal(got, []int{0, 2}) {
					t.Fatalf("energy told %v, want 0 at the join and 2 after the attack", got)
				}
			})
		})
	}
}

// An attack that reaches no mob gains nothing, and nothing more is told.
func TestAWhiffGainsNoEnergy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := timelineData(t, nil)
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := warriorAt(100) // 184 px from the slime
		s := join(t, inst, p)
		length, _ := attackTicks(t, data, p)
		_ = inst.Attack(p.ID, 0)
		tick(int(length))
		if got := damages(s); len(got) != 0 {
			t.Fatalf("damages %+v, want none", got)
		}
		if got := energiesTold(s); !slices.Equal(got, []int{0}) {
			t.Fatalf("energy told %v, want the join's 0 alone", got)
		}
	})
}

// Energy stops at 10: an attack at 9 gains 1, and one at 10 gains nothing and tells nothing.
func TestEnergyStopsAtItsMost(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := timelineData(t, nil)
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := warriorAt(250)
		p.Energy = 9
		s := join(t, inst, p)
		length, _ := attackTicks(t, data, p)
		for range 2 {
			_ = inst.Attack(p.ID, 0)
			tick(int(length))
		}
		if got := damages(s); len(got) != 2 {
			t.Fatalf("damages %+v, want two", got)
		}
		if got := energiesTold(s); !slices.Equal(got, []int{9, 10}) {
			t.Fatalf("energy told %v, want 9 at the join, then 10 once", got)
		}
	})
}

// A skill spends at its start, so the next press sees what is left: one pressed during Strike waits when the energy covers it, and is refused at once when it doesn't.
func TestTwoSkillsBackToBack(t *testing.T) {
	t.Run("the second covered, waiting for the first's end", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := timelineData(t, nil)
			inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
			defer stop()
			tick(1)
			p := caster(100)
			p.Energy = 4
			s := join(t, inst, p)
			if err := cast(inst, p.ID, "strike"); err != nil {
				t.Fatal(err)
			}
			rally := castAsync(inst, p.ID, "rally")
			length, _ := skillTicks(t, data, "strike")
			tick(int(length))
			if err := <-rally; err != nil {
				t.Fatalf("Rally after Strike: %v", err)
			}
			if got := energiesTold(s); !slices.Equal(got, []int{4, 2, 0}) {
				t.Fatalf("energy told %v, want 4, then 2 after Strike and 0 after Rally", got)
			}
			if err := cast(inst, p.ID, "mend"); !errors.Is(err, model.ErrNotEnoughEnergy) {
				t.Fatalf("Mend with none left: %v, want ErrNotEnoughEnergy", err)
			}
		})
	})
	t.Run("the second short, refused at its press", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			inst, stop := worldtest.RunMap(t, timelineData(t, nil), arena(time.Hour), nil, worldtest.Options{})
			defer stop()
			tick(1)
			p := caster(100)
			p.Energy = 3
			s := join(t, inst, p)
			if err := cast(inst, p.ID, "strike"); err != nil {
				t.Fatal(err)
			}
			if err := cast(inst, p.ID, "rally"); !errors.Is(err, model.ErrNotEnoughEnergy) {
				t.Fatalf("Rally with 1 left: %v, want ErrNotEnoughEnergy", err)
			}
			if got := energiesTold(s); !slices.Equal(got, []int{3, 1}) {
				t.Fatalf("energy told %v, want 3, then 1 after Strike alone", got)
			}
		})
	})
}

// A portal carries the energy to the next map.
func TestAPortalCarriesTheEnergy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		tr := &transferRecorder{}
		inst, stop := worldtest.Run(t, data, nil, worldtest.Options{Transfer: tr})
		defer stop()
		at := data.StartTown().Portals[0].Arrival()
		p := worldtest.NewPlayer("Ayla", at.X, at.Y)
		p.Connection, p.Energy = "c1", 6
		join(t, inst, p)
		_ = inst.EnterPortal(p.ID)
		tick(1)
		tr.mu.Lock()
		defer tr.mu.Unlock()
		if len(tr.moved) != 1 || tr.moved[0].Energy != 6 {
			t.Fatalf("moved %+v; want the portal to carry 6 energy", tr.moved)
		}
	})
}

// A frame tells the energy as of its ack, as the client counts it: a spend for a press past the inputs applied shows once they are.
func TestAFrameTellsTheEnergyAsOfItsAck(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, timelineData(t, nil), arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(100)
		s := join(t, inst, p)
		done := make(chan error, 1)
		go func() { done <- inst.UseSkill(context.Background(), p.ID, "c1", "strike", 2) }() // pressed during input 2, which the world hasn't had
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if got := energiesTold(s); !slices.Equal(got, []int{10}) {
			t.Fatalf("energy told %v before input 2, want 10 alone: the client takes Strike's 2 off itself", got)
		}
		for seq := uint32(1); seq <= 2; seq++ {
			_ = inst.Input(p.ID, seq, sim.Input{}, 0)
		}
		tick(2) // one input a tick
		var acks []uint32
		for _, d := range eventsOf[worldtest.Delta](s) {
			if d.Energy != nil && *d.Energy == 8 {
				acks = append(acks, d.AckSeq)
			}
		}
		if got := energiesTold(s); !slices.Equal(got, []int{10, 8}) || !slices.Equal(acks, []uint32{2}) {
			t.Fatalf("energy told %v, 8 with acks %v; want 8 once, with input 2", got, acks)
		}
	})
}

// A press that says it came during an input far ahead counts as pressed just past the newest input received, so its spend is told once that input steps.
func TestAPressFarAheadIsToldOnceTheInputsCatchUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, timelineData(t, nil), arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(100)
		s := join(t, inst, p)
		done := make(chan error, 1)
		go func() { done <- inst.UseSkill(context.Background(), p.ID, "c1", "strike", 1000) }() // no input received: held to input 2
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		for seq := uint32(1); seq <= 2; seq++ {
			_ = inst.Input(p.ID, seq, sim.Input{}, 0)
		}
		tick(2)
		if got := energiesTold(s); !slices.Equal(got, []int{10, 8}) {
			t.Fatalf("energy told %v, want 10, then 8 once input 2 stepped", got)
		}
	})
}

// A press that says it came during an input long stepped counts as pressed during the last one: told as spent while it waits, and once.
func TestAPressBehindTheInputsIsToldOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := timelineData(t, nil)
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(100) // no mob in reach: the attack gains nothing
		s := join(t, inst, p)
		for seq := uint32(1); seq <= 3; seq++ {
			_ = inst.Input(p.ID, seq, sim.Input{}, 0)
		}
		tick(3)
		_ = inst.Attack(p.ID, 3)
		tick(1)
		done := make(chan error, 1)
		go func() { done <- inst.UseSkill(context.Background(), p.ID, "c1", "strike", 1) }() // waits for the attack's cancel point
		tick(1)
		if got := energiesTold(s); !slices.Equal(got, []int{10, 8}) {
			t.Fatalf("energy told %v while Strike waits, want 10, then 8", got)
		}
		length, _ := attackTicks(t, data, p)
		tick(int(length))
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if got := energiesTold(s); !slices.Equal(got, []int{10, 8}) {
			t.Fatalf("energy told %v after Strike started, want 10, then 8: its cost counted once", got)
		}
	})
}

// A skill waiting for its turn when its player dies never starts: its turn finds the player dead, and the energy the death took stays at none.
func TestAWaitingSkillDoesNotStartAfterADeath(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		slow := map[string]func(string) string{"classes.yaml": edit(t, "width: 64, height: 56, length: 900ms", "width: 64, height: 56, length: 9s")}
		inst, stop := worldtest.RunMap(t, timelineData(t, slow), arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(nextToSlime)
		p.HP = 1
		s := join(t, inst, p)
		_ = inst.Attack(p.ID, 0)
		tick(1)
		strike := castAsync(inst, p.ID, "strike") // waits for the 9 s attack's cancel point, long after the slime's touch
		untilDead(t, s, p.ID)
		tick(1)
		if err := <-strike; !errors.Is(err, model.ErrCharacterDead) {
			t.Fatalf("Strike answered %v, want refused as dead", err)
		}
		if used := eventsOf[model.SkillUsed](s); len(used) != 0 {
			t.Fatalf("skills used %+v, want none", used)
		}
		if got := energiesTold(s); got[len(got)-1] != 0 {
			t.Fatalf("energy told %v, want none last", got)
		}
	})
}

// A skill pressed by the inputs applied and waiting for its turn is told as spent while it waits, and its start tells nothing more.
func TestAWaitingSkillIsToldAsSpent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := timelineData(t, nil)
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(100) // no mob in reach: the attack gains nothing
		s := join(t, inst, p)
		_ = inst.Attack(p.ID, 0)
		tick(1)
		strike := castAsync(inst, p.ID, "strike") // waits for the attack's cancel point
		tick(1)
		if len(eventsOf[model.SkillUsed](s)) != 0 {
			t.Fatal("Strike went before the attack's cancel point")
		}
		if got := energiesTold(s); !slices.Equal(got, []int{10, 8}) {
			t.Fatalf("energy told %v while Strike waits, want 10, then 8", got)
		}
		length, _ := attackTicks(t, data, p)
		tick(int(length))
		if err := <-strike; err != nil {
			t.Fatal(err)
		}
		if used := eventsOf[model.SkillUsed](s); len(used) != 1 {
			t.Fatalf("skills used %+v, want Strike", used)
		}
		if got := energiesTold(s); !slices.Equal(got, []int{10, 8}) {
			t.Fatalf("energy told %v after Strike started, want nothing more than 10, then 8", got)
		}
	})
}
