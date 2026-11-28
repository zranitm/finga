package instance_test

import (
	"context"
	"errors"
	"reflect"
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

// ahead sends the inputs of seqs 1 to n, each walking up, before any tick takes them: the client's, ahead of a press it makes during input n.
func ahead(inst *instance.Instance, id kernel.CharacterID, n uint32) {
	for seq := uint32(1); seq <= n; seq++ {
		_ = inst.Input(id, seq, sim.Input{Up: true}, 0)
	}
}

// answered runs press on its own goroutine, as the world's command does, and gives where its answer lands.
func answered(press func() error) <-chan error {
	done := make(chan error, 1)
	go func() { done <- press() }()
	return done
}

// pending is whether done has no answer yet.
func pending(done <-chan error) bool { return len(done) == 0 }

// gateArena is the arena with no mob and a portal ring of 1.6 m at (arenaX, 22): a player at z 20 stands outside it, and three inputs up put them 1.24 m from its middle.
func gateArena(t *testing.T) *gamedata.Map {
	t.Helper()
	m := arena(t, time.Hour)
	m.Spawns = nil
	m.Portals = []gamedata.Portal{{Name: "out", At: gamedata.Point{X: arenaX, Z: 22}, Radius: 1.6, TargetMap: "sunreach", TargetPortal: "south_gate"}}
	return m
}

// A portal press the client made during input 3 is judged where input 3 left the player, not where the three inputs ahead of it found them: a player the third input walks into the ring goes through on the tick it is applied, and not before.
func TestAPortalPressIsJudgedAtTheInputItCameDuring(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &transferRecorder{}
		inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), gateArena(t), nil, worldtest.Options{Transfer: tr})
		defer stop()
		tick(1)
		p := worldtest.NewPlayer("Ayla", arenaX, 20)
		s := join(t, inst, p)
		tick(25) // landed
		ahead(inst, p.ID, 3)
		done := answered(func() error { return inst.EnterPortal(context.Background(), p.ID, 3) })
		tick(2)
		if !pending(done) || moved(tr) != 0 {
			t.Fatalf("answered, or moved %d, before input 3 was applied", moved(tr))
		}
		tick(1)
		if err := <-done; err != nil {
			t.Fatalf("a press made from inside the ring: %v", err)
		}
		if got := eventsOf[model.Teleport](s); moved(tr) != 1 || len(got) != 1 || got[0].Tick != s.Last().Tick+1 || s.Last().AckSeq != 2 {
			t.Fatalf("moved %d, teleports %+v after a last frame of tick %d, ack %d; want one teleport on the tick after the frame that acked input 2: the tick input 3 was applied", moved(tr), got, s.Last().Tick, s.Last().AckSeq)
		}
	})
}

// A portal press from outside every ring is answered with why, on the tick after, and one for a player who isn't there says so too.
func TestAPortalPressFromOutsideIsAnswered(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &transferRecorder{}
		inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), gateArena(t), nil, worldtest.Options{Transfer: tr})
		defer stop()
		tick(1)
		p := worldtest.NewPlayer("Ayla", arenaX, 20)
		join(t, inst, p)
		tick(25)
		done := answered(func() error { return inst.EnterPortal(context.Background(), p.ID, 0) })
		tick(1)
		if err := <-done; !errors.Is(err, model.ErrNotAtAGate) || moved(tr) != 0 {
			t.Fatalf("a press from outside the ring: %v, moved %d; want ErrNotAtAGate", err, moved(tr))
		}
		done = answered(func() error { return inst.EnterPortal(context.Background(), kernel.NewCharacterID(), 0) })
		tick(1)
		if err := <-done; err == nil {
			t.Fatal("a press for a player who isn't on the map was taken")
		}
	})
}

// A portal press that waits for an input and then for an item call in flight takes both holds in turn: judged at its input, deferred while the equip is out, and taken once it settles, with the equip's result.
func TestAPortalPressHeldToItsInputStillWaitsForAnItemCall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		eq := &fakeEquipment{answer: sword(), gate: make(chan struct{})}
		tr := &transferRecorder{}
		inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), gateArena(t), nil, worldtest.Options{Equipment: eq, ItemCalls: runningPool(t), Transfer: tr})
		defer stop()
		tick(1)
		p := worldtest.NewPlayer("Ayla", arenaX, 20)
		p.Connection = "c1"
		join(t, inst, p)
		tick(25)
		equipped := equipAsync(inst, p.ID, "c1")
		tick(1)
		ahead(inst, p.ID, 3)
		done := answered(func() error { return inst.EnterPortal(context.Background(), p.ID, 3) })
		tick(3)
		if !pending(done) || moved(tr) != 0 {
			t.Fatalf("answered, or moved %d, while the equip is out; want the press waiting for it", moved(tr))
		}
		close(eq.gate)
		tick(1)
		if err := <-equipped; err != nil {
			t.Fatal(err)
		}
		tick(2)
		if err := <-done; err != nil {
			t.Fatalf("the press, once the equip settled: %v", err)
		}
		tr.mu.Lock()
		defer tr.mu.Unlock()
		if len(tr.moved) != 1 || !reflect.DeepEqual(tr.moved[0].Worn, sword().Worn) {
			t.Fatalf("moved %+v; want the press taken once the equip settled, carrying the sword", tr.moved)
		}
	})
}

// A talk made during input 3 is judged where input 3 left the player: out of reach of the town guide at the three inputs' start, and in reach after them, it is answered on that tick; with no input to wait for it is judged where they stand.
func TestATalkIsJudgedAtTheInputItCameDuring(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.RunMap(t, data, worldtest.Town(data), nil, worldtest.Options{CheckpointInterval: time.Hour})
		defer stop()
		npc, _ := data.NPC("town_guide")
		p := worldtest.NewPlayer("Ayla", npc.At.X, npc.At.Z-2.2) // 2.2 m south of the guide, beyond reach, and 1.44 m after three inputs up
		p.Connection = "c1"
		s := join(t, inst, p)
		tick(25)
		if err := inst.Talk(context.Background(), p.ID, "c1", "town_guide", 0); !errors.Is(err, model.ErrNpcOutOfReach) {
			t.Fatalf("a talk from 2.2 m away: %v; want out of reach", err)
		}
		ahead(inst, p.ID, 3)
		done := answered(func() error { return inst.Talk(context.Background(), p.ID, "c1", "town_guide", 3) })
		tick(2)
		if !pending(done) {
			t.Fatal("answered before input 3 was applied")
		}
		tick(1)
		if err := <-done; err != nil {
			t.Fatalf("a talk made from within reach: %v", err)
		}
		if got := eventsOf[model.NpcDialog](s); len(got) != 1 || got[0].Tick != s.Last().Tick {
			t.Fatalf("dialogs %+v on a last tick of %d; want one, on the tick input 3 was applied", got, s.Last().Tick)
		}
	})
}

// A press for an input that never comes is judged where the player stands once its second is up, and one for a player who leaves first is answered.
func TestAPressForAnInputThatNeverComesIsJudgedAnyway(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), gateArena(t), nil, worldtest.Options{Transfer: &transferRecorder{}})
		defer stop()
		tick(1)
		p := worldtest.NewPlayer("Ayla", arenaX, 20)
		p.Connection = "c1"
		join(t, inst, p)
		tick(25)
		done := answered(func() error { return inst.EnterPortal(context.Background(), p.ID, 2) }) // the input it names was never sent
		tick(10)
		if !pending(done) {
			t.Fatal("answered before its second was up")
		}
		tick(12)
		if err := <-done; !errors.Is(err, model.ErrNotAtAGate) {
			t.Fatalf("the press: %v; want it judged where the player stands, out of the ring", err)
		}
		left := answered(func() error { return inst.EnterPortal(context.Background(), p.ID, 5) })
		tick(1)
		_ = inst.Leave(p.ID, "c1", nil)
		tick(1)
		if err := <-left; err == nil {
			t.Fatal("a press held for a player who left was taken")
		}
	})
}

// A portal press that waits for an item call in flight is answered when it is judged once the call settles: a player who has walked out of the ring meanwhile is told there is no gate, not given an ok that comes to nothing; and one who leaves the map first is answered too.
func TestAPortalPressHeldForAnItemCallIsAnsweredWhenJudged(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		eq := &fakeEquipment{answer: sword(), gate: make(chan struct{})}
		tr := &transferRecorder{}
		inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), gateArena(t), nil, worldtest.Options{Equipment: eq, ItemCalls: runningPool(t), Transfer: tr})
		defer stop()
		tick(1)
		p := worldtest.NewPlayer("Ayla", arenaX, 21) // inside the 1.6 m ring
		p.Connection = "c1"
		s := join(t, inst, p)
		tick(25)
		equipped := equipAsync(inst, p.ID, "c1")
		tick(1)
		done := answered(func() error { return inst.EnterPortal(context.Background(), p.ID, 0) })
		tick(2)
		if !pending(done) {
			t.Fatal("answered with the equip still out")
		}
		walked := worldtest.Walk(inst, p.ID, 0, sim.Input{Down: true}, 12) // out of the ring, backwards from the portal
		_ = worldtest.Walk(inst, p.ID, walked, sim.Input{}, 1)
		if v, _ := s.Own(p.ID); v.State.Z > 21-1.5+1e-9 && v.State.Z > 19.5 {
			t.Fatalf("still in the ring at %+v", v.State)
		}
		close(eq.gate)
		tick(1)
		if err := <-equipped; err != nil {
			t.Fatal(err)
		}
		tick(2)
		if err := <-done; !errors.Is(err, model.ErrNotAtAGate) || moved(tr) != 0 {
			t.Fatalf("the press once the equip settled: %v, moved %d; want ErrNotAtAGate", err, moved(tr))
		}

		eq.gate = make(chan struct{})
		equipped = equipAsync(inst, p.ID, "c1")
		tick(1)
		left := answered(func() error { return inst.EnterPortal(context.Background(), p.ID, 0) })
		tick(1)
		_ = inst.Leave(p.ID, "c1", nil)
		tick(1)
		if err := <-left; err == nil {
			t.Fatal("a press held for a player who left was answered ok")
		}
		close(eq.gate)
		tick(1)
		if err := <-equipped; err != nil {
			t.Fatal(err)
		}
	})
}
