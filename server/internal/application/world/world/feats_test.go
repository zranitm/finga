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

// fakeFeats is the wardrobe as feats grant to it, unavailable while down; a piece the account has changes nothing.
type fakeFeats struct {
	mu    sync.Mutex
	down  bool
	has   map[string]bool
	calls [][]model.FeatPiece
}

func (f *fakeFeats) GrantFeatPieces(_ context.Context, _ kernel.AccountID, pieces []model.FeatPiece) ([]model.FeatPiece, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, slices.Clone(pieces))
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

// outfits.md section 4: a join asks for the feats the character meets that it can see, so a promotion's grant lost while the item module was down comes at the next join, told once; a join with nothing new tells of nothing.
func TestAJoinGrantsAFeatLostWhenItWasMet(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		trainer, _ := data.NPC("warrior_trainer")
		sin, cos := math.Sincos(trainer.Yaw)
		account := kernel.NewAccountID()
		ayla := model.Character{
			ID: kernel.NewCharacterID(), Account: account, Name: "Ayla", Class: "beginner", Level: 10, HP: 50, Energy: 5, MapID: "town", X: trainer.At.X + sin, Z: trainer.At.Z + cos,
			Points: model.Points{Spent: gamedata.Stats{STR: 23}},
			Skills: model.Skills{Points: map[string]int{"strike": 10}},
			Quests: model.Quests{"warriors_trial": {Status: model.QuestFinished, TimesFinished: 1}},
		}
		feats := &fakeFeats{down: true, has: map[string]bool{"wanderer_hat": true}}
		chars := &fakeCharacters{chars: map[kernel.CharacterID]model.Character{ayla.ID: ayla}}
		w := worldtest.NewWorld(data, &fakeSaver{hangs: 1000}, worldtest.Options{Characters: chars, Feats: feats, Gifts: gifted{}})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); _ = w.Run(ctx) }()
		defer func() { cancel(); <-done }()
		join := func(conn string) *worldtest.Sink {
			t.Helper()
			s := &worldtest.Sink{}
			if _, err := w.Join(context.Background(), account, ayla.ID, conn, s); err != nil {
				t.Fatal(err)
			}
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			return s
		}

		join("c1") // the wardrobe is down: the join goes on
		if err := w.Promote(context.Background(), ayla.ID, "c1", "warrior_trainer", "warrior"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Second) // past every try
		synctest.Wait()
		if got := feats.last(); !slices.Equal(got, []model.FeatPiece{{Piece: "veteran_plate", Feat: "promoted"}}) {
			t.Fatalf("the promotion asked for %+v", got)
		}

		feats.setDown(false)
		s := join("c2")
		if got := feats.last(); !slices.Equal(got, []model.FeatPiece{{Piece: "wanderer_hat", Feat: "level_10"}, {Piece: "veteran_plate", Feat: "promoted"}}) {
			t.Fatalf("the join asked for %+v, want every feat Ayla meets", got)
		}
		if told, notices := featsTold(s); !slices.Equal(told, []string{"veteran_plate"}) || notices != 1 {
			t.Fatalf("the join told of %v in %d notices, want the plate once", told, notices)
		}
		if told, notices := featsTold(join("c3")); len(told) != 0 || notices != 0 {
			t.Fatalf("a join with nothing new told of %v in %d notices", told, notices)
		}
	})
}
