package pvp

import (
	"context"
	"slices"
	"testing"
	"time"

	model "finga/internal/model/pvp"
	"finga/internal/shared/kernel"
)

// A fighter stored as untold is taken once, with their match's mode, end and winner and their own side and leave; one marked told, or never untold, isn't.
func TestAnUntoldOutcomeIsTakenOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := NewPostgres(cluster.DB(t), nil)
	ayla, kai := kernel.NewCharacterID(), kernel.NewCharacterID()
	record := func(winner int, aylaLeft bool) kernel.MatchID {
		m := model.Match{ID: kernel.NewMatchID(), Mode: "duel", Started: t0, Ended: t0.Add(time.Duration(winner) * time.Minute), Winner: winner}
		_, err := store.Record(ctx, m, []kernel.CharacterID{ayla, kai}, func(map[kernel.CharacterID]model.Rating) model.Rated {
			return model.Rated{Fighters: []model.Fighter{
				{Side: 1, Character: ayla, Left: aylaLeft, Untold: true},
				{Side: 2, Character: kai},
			}}
		})
		if err != nil {
			t.Fatal(err)
		}
		return m.ID
	}
	lost, drawn := record(2, true), record(0, false)
	got, err := store.TakeUntold(ctx, ayla)
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(got, func(a, b model.Untold) int { return a.Ended.Compare(b.Ended) })
	want := []model.Untold{
		{Match: drawn, Mode: "duel", Ended: t0, Winner: 0, Side: 1},
		{Match: lost, Mode: "duel", Ended: t0.Add(2 * time.Minute), Winner: 2, Side: 1, Left: true},
	}
	if len(got) != len(want) {
		t.Fatalf("Ayla's untold %+v; want %+v", got, want)
	}
	for k := range want {
		if g := got[k]; g.Match != want[k].Match || g.Mode != want[k].Mode || !g.Ended.Equal(want[k].Ended) || g.Winner != want[k].Winner || g.Side != want[k].Side || g.Left != want[k].Left {
			t.Fatalf("Ayla's untold %d: %+v; want %+v", k, g, want[k])
		}
	}
	for _, id := range []kernel.CharacterID{ayla, kai} {
		if again, err := store.TakeUntold(ctx, id); err != nil || len(again) != 0 {
			t.Fatalf("%v's untold taken again: %+v, %v; want none", id, again, err)
		}
	}
	seen := record(1, false)
	if err := store.MarkTold(ctx, ayla, seen); err != nil {
		t.Fatal(err)
	}
	if again, err := store.TakeUntold(ctx, ayla); err != nil || len(again) != 0 {
		t.Fatalf("Ayla's untold after a live tell: %+v, %v; want none", again, err)
	}
}
