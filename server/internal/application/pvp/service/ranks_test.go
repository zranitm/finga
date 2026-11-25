package service_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/pvp/service"
	model "finga/internal/model/pvp"
	"finga/internal/shared/kernel"
	"finga/pkg/transient"
)

// items is the wardrobe as pvp grants to it: each account's pieces, a grant of one it has changing nothing; fail fails that many grants.
type items struct {
	mu   sync.Mutex
	has  map[kernel.AccountID]map[string]bool
	asks int
	fail int
}

func (i *items) GrantRankPieces(_ context.Context, account kernel.AccountID, pieces []model.RankPiece) ([]model.RankPiece, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.asks++
	if i.fail > 0 {
		i.fail--
		return nil, fmt.Errorf("%w: the wardrobe is down", transient.ErrUnavailable)
	}
	if i.has[account] == nil {
		i.has[account] = map[string]bool{}
	}
	var added []model.RankPiece
	for _, p := range pieces {
		if !i.has[account][p.Piece] {
			i.has[account][p.Piece] = true
			added = append(added, p)
		}
	}
	return added, nil
}

// placedAt is a placed rating whose shown rating is about shown: 1000 + 40 × (mu − 3 sigma).
func placedAt(shown int) model.Rating {
	return model.Rating{Mu: float64(shown-1000)/40 + 6, Sigma: 2, Shown: shown, Placed: true, Matches: 20}
}

// ranked is a fixture whose Ayla, of an account, is rated rt in duels.
func ranked(t *testing.T, rt model.Rating) (fixture, kernel.CharacterID, kernel.AccountID) {
	f := newFixture(t)
	ayla, account := kernel.NewCharacterID(), kernel.NewAccountID()
	f.chars.of[ayla] = model.Character{Account: account, Class: "warrior"}
	f.store.ratings[ayla] = rt
	return f, ayla, account
}

// duel records a rated 1v1 of Ayla's against a novice bot, rated rt before it.
func duel(f fixture, ayla kernel.CharacterID, rt model.Rating, won bool) {
	f.store.mu.Lock()
	f.store.ratings[ayla] = rt
	f.store.mu.Unlock()
	winner := 2
	if won {
		winner = 1
	}
	f.svc.Record(context.Background(), model.Result{Match: kernel.NewMatchID(), Mode: "duel", Winner: winner, Sides: [2][]model.Outcome{{{Character: ayla}}, {{Tier: "novice"}}}})
}

// added is every rank piece Ayla was told of, in order, and in how many pushes.
func added(f fixture, ayla kernel.CharacterID) ([]model.RankPiece, int) {
	var out []model.RankPiece
	pushes := 0
	for _, d := range f.notes.of(ayla) {
		if p, ok := d.(service.PiecesAdded); ok {
			out = append(out, p.Pieces...)
			pushes++
		}
	}
	return out, pushes
}

// outfits.md section 4: a rated match's end grants the piece of each rank the shown rating reaches, and tells of those new to the account, after its MatchEnded; falling and climbing again finds them there, so each comes once.
func TestARanksPieceComesOnceThroughAFallAndAClimb(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gold := placedAt(1460)
		f, ayla, account := ranked(t, gold)
		duel(f, ayla, gold, true)
		silverGold := []model.RankPiece{{Piece: "helm_silver", Rank: "silver"}, {Piece: "helm_gold", Rank: "gold"}}
		if got, _ := added(f, ayla); !slices.Equal(got, silverGold) {
			t.Fatalf("reaching gold told of %+v, want %+v", got, silverGold)
		}
		if _, ok := f.notes.of(ayla)[0].(service.Ended); !ok {
			t.Fatalf("Ayla heard %+v, the outcome not first", f.notes.of(ayla))
		}
		duel(f, ayla, placedAt(1260), false) // down to silver
		duel(f, ayla, gold, true)            // and back
		if got, pushes := added(f, ayla); !slices.Equal(got, silverGold) || pushes != 1 {
			t.Fatalf("after a fall and a climb Ayla was told of %+v in %d pushes, want gold's piece once", got, pushes)
		}
		duel(f, ayla, placedAt(1660), true)
		if got, _ := added(f, ayla); !slices.Equal(got, append(silverGold, model.RankPiece{Piece: "platinum_plate", Rank: "platinum"})) {
			t.Fatalf("reaching platinum told of %+v", got)
		}
		if len(f.items.has[account]) != 3 {
			t.Fatalf("the account holds %v", f.items.has[account])
		}
	})
}

// No rank piece before placement, in an unrated battle, or for a bronze rating, whose rank gives none: the wardrobe isn't asked.
func TestNoRankPieceWithoutARank(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		placing := placedAt(1460)
		placing.Placed, placing.Matches = false, 2
		f, ayla, _ := ranked(t, placing)
		duel(f, ayla, placing, true)
		duel(f, ayla, placedAt(1100), true)
		f.store.mu.Lock()
		f.store.ratings[ayla] = placedAt(1460)
		f.store.mu.Unlock()
		f.svc.Record(context.Background(), model.Result{Match: kernel.NewMatchID(), Mode: "battle", Winner: 1, Score: [2]int{1000, 640}, Sides: [2][]model.Outcome{{{Character: ayla}}, {{Tier: "novice"}}}})
		if got, pushes := added(f, ayla); f.items.asks != 0 || pushes != 0 {
			t.Fatalf("the wardrobe was asked %d times; Ayla told of %+v", f.items.asks, got)
		}
	})
}

// A grant the wardrobe can't take is tried again; one that never lands tells of nothing, and the next rated match grants it.
func TestALostGrantComesWithTheNextMatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		silver := placedAt(1260)
		f, ayla, _ := ranked(t, silver)
		f.items.fail = 2
		duel(f, ayla, silver, true)
		if got, _ := added(f, ayla); !slices.Equal(got, []model.RankPiece{{Piece: "helm_silver", Rank: "silver"}}) || f.items.asks != 3 {
			t.Fatalf("after two failures, told of %+v in %d asks", got, f.items.asks)
		}
		f.items.fail = 1 << 20
		gold := placedAt(1460)
		duel(f, ayla, gold, true)
		if got, pushes := added(f, ayla); len(got) != 1 || pushes != 1 {
			t.Fatalf("a grant that never landed told of %+v", got)
		}
		f.items.fail = 0
		duel(f, ayla, gold, true)
		if got, _ := added(f, ayla); !slices.Equal(got, []model.RankPiece{{Piece: "helm_silver", Rank: "silver"}, {Piece: "helm_gold", Rank: "gold"}}) {
			t.Fatalf("the next match told of %+v, want gold's piece", got)
		}
	})
}

// A match's grants share one budget: with the wardrobe down, a 3v3's players give up together within a minute, not a minute each, so Outcomes never waits long on them.
func TestAMatchsGrantsShareOneBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		f.items.fail = 1 << 20
		var side []model.Outcome
		for range 3 {
			id := kernel.NewCharacterID()
			f.chars.of[id] = model.Character{Account: kernel.NewAccountID(), Class: "warrior"}
			f.store.ratings[id] = placedAt(1260)
			side = append(side, model.Outcome{Character: id})
		}
		start := time.Now()
		f.svc.Record(context.Background(), model.Result{Match: kernel.NewMatchID(), Mode: "team", Winner: 1, Sides: [2][]model.Outcome{side, {{Tier: "novice"}, {Tier: "novice"}, {Tier: "novice"}}}})
		if took := time.Since(start); took > time.Minute {
			t.Fatalf("the grants took %v with the wardrobe down, want one minute's budget for the match", took)
		}
		for _, o := range side {
			if got, _ := added(f, o.Character); len(got) != 0 {
				t.Fatalf("told of %+v with the wardrobe down", got)
			}
		}
	})
}
