package world_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
)

// The World counts the bots its matches' specs place from the open until the match stops, and an open that fails, as a second of the same match does, counts none; a match none of whose players came closes after its prep.
func TestTheWorldCountsItsMatchesBots(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newWorld(t, &fakeSaver{}, worldtest.Options{})
		ctx, cancel := context.WithCancel(context.Background())
		runDone := make(chan error, 1)
		go func() { runDone <- w.Run(ctx) }()
		defer func() {
			cancel()
			if err := <-runDone; err != nil {
				t.Error(err)
			}
		}()
		synctest.Wait() // the World is running
		data := worldtest.LoadData(t)
		build := data.Pvp().Bots.Builds["warrior"][0].Bar
		bot := func(name string) model.Fighter {
			return model.Fighter{Bot: &model.Bot{ID: kernel.NewCharacterID(), Name: name, Class: "warrior", Tier: data.Pvp().Bots.Tiers[0].ID, Build: model.Build{Bar: build}}}
		}
		spec := model.MatchSpec{ID: kernel.NewMatchID(), Mode: "team", Sides: [2][]model.Fighter{
			{{Character: kernel.NewCharacterID()}, bot("Ash"), bot("Birch")},
			{bot("Cedar"), bot("Dune"), bot("Elm")},
		}}
		if err := w.OpenMatch(ctx, spec); err != nil {
			t.Fatal(err)
		}
		if got := w.Bots(); got != 5 {
			t.Fatalf("%d bots with a 3v3 of one player open; want its 5", got)
		}
		if err := w.OpenMatch(ctx, spec); err == nil || w.Bots() != 5 {
			t.Fatalf("opening the match again: %v, %d bots; want refused, and its 5 bots counted once", err, w.Bots())
		}
		time.Sleep(data.Pvp().Rounds.Prep + time.Second)
		synctest.Wait()
		if got := w.Bots(); got != 0 {
			t.Fatalf("%d bots once the match none came to closed; want 0", got)
		}
	})
}
