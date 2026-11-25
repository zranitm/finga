package world_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

// tick runs the World's ticks for d.
func tick(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

// running runs a new World till stop.
func running(t *testing.T) (w *testWorld, ctx context.Context, stop func()) {
	t.Helper()
	w = newWorld(t, &fakeSaver{}, worldtest.Options{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	synctest.Wait()
	return w, ctx, func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
}

// inDuel runs a World with Ayla, of an account of her own, joined in town through c-1 and handed to a duel with a bot, to its first round.
func inDuel(t *testing.T) (w *testWorld, ayla model.Player, as *worldtest.Sink, stop func()) {
	t.Helper()
	w, ctx, stop := running(t)
	data := worldtest.LoadData(t)
	ayla = at(t, "Ayla", "town")
	ayla.Account = kernel.NewAccountID()
	as = &worldtest.Sink{}
	w.enter(t, ayla, "town", "c-1", as)
	bot := &model.Bot{ID: kernel.NewCharacterID(), Name: "Ash", Class: "warrior", Tier: data.Pvp().Bots.Tiers[0].ID, Build: model.Build{Bar: data.Pvp().Bots.Builds["warrior"][0].Bar}}
	if err := w.OpenMatch(ctx, model.MatchSpec{ID: kernel.NewMatchID(), Mode: "duel", Sides: [2][]model.Fighter{{{Character: ayla.ID}}, {{Bot: bot}}}}); err != nil {
		t.Fatal(err)
	}
	tick(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + worldtest.TickInterval)
	if snaps := worldtest.Of[model.Snapshot](as); snaps[len(snaps)-1].MapID != "ring" {
		t.Fatalf("Ayla's last snapshot %+v, want her on the ring", snaps[len(snaps)-1])
	}
	return w, ayla, as, stop
}

// A connection dropped in a match holds the place: the next join of the character's account, through a new connection, lands on the body in the match, which that connection steers, with a sequence for social to order the login by. Another account's join for the character takes nothing.
func TestADropInAMatchHoldsThePlaceForTheNextJoin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w, ayla, _, stop := inDuel(t)
		defer stop()
		ctx := context.Background()
		if err := w.Drop(ctx, ayla.ID, "c-1"); err != nil {
			t.Fatal(err)
		}
		back := &worldtest.Sink{}
		v, err := w.Join(ctx, ayla.Account, ayla.ID, "c-2", back)
		if err != nil || v.MapID != "ring" || v.JoinSeq == 0 {
			t.Fatalf("the join after the drop: %+v, %v; want her on the ring, with a sequence", v, err)
		}
		tick(worldtest.TickInterval)
		if snaps := worldtest.Of[model.Snapshot](back); len(snaps) != 1 || snaps[0].MapID != "ring" {
			t.Fatalf("the new connection's snapshots %+v, want the ring's", snaps)
		}
		from, _ := back.Own(ayla.ID)
		for seq := range uint32(10) {
			if err := w.Input(ayla.ID, "c-2", seq+1, sim.Input{Up: true}, 0); err != nil {
				t.Fatal(err)
			}
			tick(worldtest.TickInterval)
		}
		if to, _ := back.Own(ayla.ID); to.State.Z < from.State.Z+0.5 {
			t.Fatalf("Ayla at z %.2f from %.2f, want the new connection walking her north", to.State.Z, from.State.Z)
		}
		if _, err := w.Join(ctx, kernel.NewAccountID(), ayla.ID, "c-3", &worldtest.Sink{}); err == nil {
			t.Fatal("another account's join took Ayla's place")
		}
		heard := len(back.Deltas())
		tick(2 * worldtest.TickInterval)
		if len(back.Deltas()) == heard {
			t.Fatal("Ayla's connection was sent nothing after another account's join")
		}
	})
}

// A return that finds the match's map stopped since it read it is a plain join: the character lands where they came from.
func TestAReturnToAStoppedMatchLandsWhereTheyCameFrom(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w, ayla, _, stop := inDuel(t)
		defer stop()
		ctx := context.Background()
		if err := w.Drop(ctx, ayla.ID, "c-1"); err != nil {
			t.Fatal(err)
		}
		w.StopMatchOf(ayla.ID)
		if v, err := w.Join(ctx, ayla.Account, ayla.ID, "c-2", &worldtest.Sink{}); err != nil || v.MapID != "town" {
			t.Fatalf("the return to a stopped match: %+v, %v; want her in town", v, err)
		}
	})
}

// A join whose connection closes before it's on the body is busy, for the next to try, and the place stays held for it.
func TestAReturnWhoseConnectionClosesKeepsThePlaceHeld(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w, ayla, _, stop := inDuel(t)
		defer stop()
		ctx := context.Background()
		if err := w.Drop(ctx, ayla.ID, "c-1"); err != nil {
			t.Fatal(err)
		}
		closed := &worldtest.Sink{}
		closed.Refuse()
		if _, err := w.Join(ctx, ayla.Account, ayla.ID, "c-2", closed); !errors.Is(err, model.ErrBusy) {
			t.Fatalf("a join on a closed connection: %v, want ErrBusy", err)
		}
		if v, err := w.Join(ctx, ayla.Account, ayla.ID, "c-3", &worldtest.Sink{}); err != nil || v.MapID != "ring" {
			t.Fatalf("the next join: %+v, %v; want her on the ring", v, err)
		}
	})
}

// A connection dropped off a match is a leave: the map takes the character off.
func TestADropOffAMatchIsALeave(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w, ctx, stop := running(t)
		defer stop()
		ayla := at(t, "Ayla", "town")
		as := &worldtest.Sink{}
		w.enter(t, ayla, "town", "c-1", as)
		if err := w.Drop(ctx, ayla.ID, "c-1"); err != nil {
			t.Fatal(err)
		}
		tick(worldtest.TickInterval)
		heard := len(as.Deltas())
		tick(2 * worldtest.TickInterval)
		if len(as.Deltas()) != heard {
			t.Fatal("Ayla is still on the map after her connection dropped")
		}
	})
}

// A join with the match's outcome showing is a plain one: the character's body leaves the match's map, and they land where they came from.
func TestAJoinWithTheOutcomeShowingLandsWhereTheyCameFrom(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w, ctx, stop := running(t)
		defer stop()
		data := worldtest.LoadData(t)
		ayla, robin := at(t, "Ayla", "town"), at(t, "Robin", "town")
		ayla.Account, robin.Account = kernel.NewAccountID(), kernel.NewAccountID()
		as := &worldtest.Sink{}
		w.enter(t, ayla, "town", "c-1", as)
		w.enter(t, robin, "town", "c-r", &worldtest.Sink{})
		if err := w.OpenMatch(ctx, model.MatchSpec{ID: kernel.NewMatchID(), Mode: "duel", Sides: [2][]model.Fighter{{{Character: ayla.ID}}, {{Character: robin.ID}}}}); err != nil {
			t.Fatal(err)
		}
		tick(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + worldtest.TickInterval)
		if err := w.Forfeit(ctx, robin.ID, "c-r"); err != nil {
			t.Fatal(err)
		}
		tick(worldtest.TickInterval)
		if v, err := w.Join(ctx, ayla.Account, ayla.ID, "c-2", &worldtest.Sink{}); err != nil || v.MapID != "town" {
			t.Fatalf("the join with the outcome showing: %+v, %v; want her in town", v, err)
		}
		tick(worldtest.TickInterval)
		heard := len(as.Deltas())
		tick(2 * worldtest.TickInterval)
		if len(as.Deltas()) != heard {
			t.Fatal("Ayla's body is still on the ring after her join elsewhere")
		}
	})
}
