package pvp

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"finga/internal/migration"
	model "finga/internal/model/pvp"
	"finga/internal/shared/kernel"
	"finga/pkg/pgtest"
)

var cluster *pgtest.Cluster

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	cluster, err = pgtest.Start(ctx, pgtest.Migrations(migration.FS, migration.Table)) // the whole sequence, as the server runs on it
	if err != nil {
		panic(err)
	}
	code := m.Run()
	_ = cluster.Close(ctx)
	os.Exit(code)
}

var t0 = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

// A match's outcome is stored once: its ratings rows are written from what rate gives, read back by Rating, and each fighter's row kept; told again, rate isn't called, nothing changes, and the characters' ratings come back as stored.
func TestAnOutcomeIsStoredOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := cluster.DB(t)
	store := NewPostgres(pool, nil)
	ayla := kernel.NewCharacterID()
	if _, ok, err := store.Rating(ctx, ayla, "duel"); err != nil || ok {
		t.Fatalf("a new character's rating: found %v, %v", ok, err)
	}
	match := model.Match{ID: kernel.NewMatchID(), Mode: "duel", Started: t0, Ended: t0.Add(3 * time.Minute), Winner: 1}
	after := model.Rating{Mu: 27, Sigma: 7.9, Shown: 1132, Matches: 1, Wins: 1}
	rated := model.Rated{
		After: map[kernel.CharacterID]model.Rating{ayla: after},
		Fighters: []model.Fighter{
			{Side: 1, Slot: 0, Character: ayla, Before: model.Rating{Mu: 25, Sigma: 8.333}, After: after},
			{Side: 2, Slot: 0, Tier: "novice", Before: model.Rating{Mu: 15, Sigma: 1}, After: model.Rating{Mu: 15, Sigma: 1}},
		},
	}
	calls := 0
	rate := func(before map[kernel.CharacterID]model.Rating) model.Rated {
		calls++
		if len(before) != 0 {
			t.Errorf("rows before the first match: %+v", before)
		}
		return rated
	}
	stored, err := store.Record(ctx, match, []kernel.CharacterID{ayla}, rate)
	if err != nil || len(stored) != 2 || stored[0].After != after {
		t.Fatalf("Record gave %+v, %v; want rate's fighters", stored, err)
	}
	got, ok, err := store.Rating(ctx, ayla, "duel")
	if err != nil || !ok || got.Mu != 27 || got.Sigma != 7.9 || got.Matches != 1 || got.Wins != 1 {
		t.Fatalf("Ayla's rating %+v, %v, %v", got, ok, err)
	}
	var fighters, bots int
	if err := pool.QueryRow(ctx, "SELECT count(*), count(*) FILTER (WHERE bot_tier = 'novice') FROM pvp_match_fighters WHERE match_id = $1", uuid.UUID(match.ID)).Scan(&fighters, &bots); err != nil {
		t.Fatal(err)
	}
	if fighters != 2 || bots != 1 {
		t.Fatalf("%d fighters, %d bots stored", fighters, bots)
	}
	var shown int
	if err := pool.QueryRow(ctx, "SELECT shown FROM pvp_ratings WHERE character_id = $1", uuid.UUID(ayla)).Scan(&shown); err != nil || shown != 1132 {
		t.Fatalf("shown %d, %v", shown, err)
	}
	stored, err = store.Record(ctx, match, []kernel.CharacterID{ayla}, func(map[kernel.CharacterID]model.Rating) model.Rated {
		t.Error("a repeated outcome was rated again")
		return model.Rated{}
	})
	want := model.Fighter{Side: 1, Slot: 0, Character: ayla, Before: model.Rating{Mu: 25, Sigma: 8.333}, After: model.Rating{Mu: 27, Sigma: 7.9, Matches: 1, Wins: 1}}
	if err != nil || len(stored) != 1 || stored[0] != want {
		t.Fatalf("a repeat gave %+v, %v; want Ayla's rows as stored, %+v", stored, err, want)
	}
	if calls != 1 {
		t.Fatalf("rated %d times", calls)
	}
}

// A second match hands rate the row the first one wrote.
func TestTheNextMatchReadsTheRowsOfTheLast(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := NewPostgres(cluster.DB(t), nil)
	ayla := kernel.NewCharacterID()
	first := model.Rating{Mu: 27, Sigma: 7.9, Shown: 1132, Matches: 1, Wins: 1}
	record := func(r model.Rating) map[kernel.CharacterID]model.Rating {
		var saw map[kernel.CharacterID]model.Rating
		_, err := store.Record(ctx, model.Match{ID: kernel.NewMatchID(), Mode: "duel", Started: t0, Ended: t0, Winner: 1}, []kernel.CharacterID{ayla}, func(before map[kernel.CharacterID]model.Rating) model.Rated {
			saw = before
			return model.Rated{After: map[kernel.CharacterID]model.Rating{ayla: r}, Fighters: []model.Fighter{{Side: 1, Character: ayla, Before: before[ayla], After: r}}}
		})
		if err != nil {
			t.Fatal(err)
		}
		return saw
	}
	record(first)
	saw := record(model.Rating{Mu: 28, Sigma: 7.5, Shown: 1180, Matches: 2, Wins: 2})
	if got := saw[ayla]; got.Mu != 27 || got.Matches != 1 || got.Wins != 1 {
		t.Fatalf("the second match saw %+v, want the first's row", got)
	}
}
