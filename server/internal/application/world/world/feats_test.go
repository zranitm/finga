package world_test

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/pkg/transient"
)

// fakeFeats is the wardrobe as feats grant to it, unavailable while down, answering no call till its end while hung; a piece the account has changes nothing.
type fakeFeats struct {
	mu    sync.Mutex
	down  bool
	hung  bool
	has   map[string]bool
	calls [][]model.FeatPiece
}

func (f *fakeFeats) GrantFeatPieces(ctx context.Context, _ kernel.AccountID, pieces []model.FeatPiece) ([]model.FeatPiece, error) {
	f.mu.Lock()
	f.calls = append(f.calls, slices.Clone(pieces))
	if f.hung {
		f.mu.Unlock()
		<-ctx.Done()
		return nil, fmt.Errorf("%w: %w", transient.ErrUnavailable, ctx.Err())
	}
	defer f.mu.Unlock()
	if f.down {
		return nil, fmt.Errorf("%w: the wardrobe is down", transient.ErrUnavailable)
	}
	var added []model.FeatPiece
	for _, p := range pieces {
		if !f.has[p.Piece] {
			f.has[p.Piece] = true
			added = append(added, p)
		}
	}
	return added, nil
}

func (f *fakeFeats) setDown(down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.down = down
}

func (f *fakeFeats) last() []model.FeatPiece {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
}

// gifted hands over every trainer's gift.
type gifted struct{}

func (gifted) Give(context.Context, model.GiftCall) (model.Gift, error) { return model.Gift{}, nil }

// featsTold is the pieces s heard a feat added, and in how many notices.
func featsTold(s *worldtest.Sink) ([]string, int) {
	var out []string
	notices := 0
	for _, e := range s.All() {
		if a, ok := e.(model.PiecesAdded); ok && a.Cause == model.PieceFromFeat {
			out = append(out, a.Pieces...)
			notices++
		}
	}
	return out, notices
}

// featWorld runs a world over ayla's save and its wardrobe feats, and gives a join through a new connection that waits for the map and the feat worker after it, and how long the join itself took.
func featWorld(t *testing.T, data *gamedata.Data, ayla model.Character, feats *fakeFeats) (*worldtest.World, func(conn string) (*worldtest.Sink, time.Duration)) {
	t.Helper()
	chars := &fakeCharacters{chars: map[kernel.CharacterID]model.Character{ayla.ID: ayla}}
	w := worldtest.NewWorld(data, &fakeSaver{hangs: 1000}, worldtest.Options{Characters: chars, Feats: feats, Gifts: gifted{}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = w.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	return w, func(conn string) (*worldtest.Sink, time.Duration) {
		t.Helper()
		s := &worldtest.Sink{}
		start := time.Now()
		if _, err := w.Join(context.Background(), ayla.Account, ayla.ID, conn, s); err != nil {
			t.Fatal(err)
		}
		took := time.Since(start)
		time.Sleep(3 * worldtest.TickInterval)
		synctest.Wait()
		return s, took
	}
}

// trainee is Ayla, a beginner beside the warrior trainer who meets every condition of the warrior's promotion.
func trainee(data *gamedata.Data) model.Character {
	trainer, _ := data.NPC("warrior_trainer")
	sin, cos := math.Sincos(trainer.Yaw)
	return model.Character{
		ID: kernel.NewCharacterID(), Account: kernel.NewAccountID(), Name: "Ayla", Class: "beginner", Level: 10, HP: 50, Energy: 5, MapID: "willowmere", X: trainer.At.X + sin, Z: trainer.At.Z + cos,
		Points: model.Points{Spent: gamedata.Stats{STR: 23}},
		Skills: model.Skills{Points: map[string]int{"strike": 10}},
		Quests: model.Quests{"warriors_trial": {Status: model.QuestFinished, TimesFinished: 1}},
	}
}

// outfits.md section 4: once a login or a reconnect has joined, the map's feat worker asks for the feats the save shows met, so a promotion's grant lost while the item module was down comes then, told once; a join with nothing new tells of nothing.
func TestAJoinGrantsAFeatLostWhenItWasMet(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		ayla := trainee(data)
		feats := &fakeFeats{down: true, has: map[string]bool{"wanderer_hat": true}}
		w, join := featWorld(t, data, ayla, feats)

		join("c1") // the wardrobe is down: the join goes on, its ask given up
		time.Sleep(10 * time.Second)
		synctest.Wait()
		if err := w.Promote(context.Background(), ayla.ID, "c1", "warrior_trainer", "warrior", 0); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Second) // past every try
		synctest.Wait()
		if got := feats.last(); !slices.Equal(got, []model.FeatPiece{{Piece: "veteran_plate", Feat: "promoted"}}) {
			t.Fatalf("the promotion asked for %+v", got)
		}

		feats.setDown(false)
		s, _ := join("c2")
		if got := feats.last(); !slices.Equal(got, []model.FeatPiece{{Piece: "wanderer_hat", Feat: "level_10"}, {Piece: "veteran_plate", Feat: "promoted"}}) {
			t.Fatalf("the join asked for %+v, want every feat Ayla's save shows met", got)
		}
		if told, notices := featsTold(s); !slices.Equal(told, []string{"veteran_plate"}) || notices != 1 {
			t.Fatalf("the join told of %v in %d notices, want the plate once", told, notices)
		}
		s, _ = join("c3")
		if told, notices := featsTold(s); len(told) != 0 || notices != 0 {
			t.Fatalf("a join with nothing new told of %v in %d notices", told, notices)
		}
	})
}

// A join answers on time while the item module hangs: its ask for the feats met waits on a worker, not on the join.
func TestAJoinDoesntWaitOnItsFeats(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		ayla := trainee(data)
		feats := &fakeFeats{hung: true, has: map[string]bool{}}
		_, join := featWorld(t, data, ayla, feats)
		if _, took := join("c1"); took > 2*worldtest.TickInterval { // the snapshot waits a tick
			t.Fatalf("the join took %v with the item module hung", took)
		}
		if got := feats.last(); !slices.Equal(got, []model.FeatPiece{{Piece: "wanderer_hat", Feat: "level_10"}}) {
			t.Fatalf("the join asked for %+v", got)
		}
	})
}
