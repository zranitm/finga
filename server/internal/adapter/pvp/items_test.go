package pvp_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	adapter "finga/internal/adapter/pvp"
	"finga/internal/application/item"
	model "finga/internal/model/pvp"
	"finga/internal/shared/kernel"
)

// wardrobe is the item module as pvp grants through it: what it was asked, and what it answers.
type wardrobe struct {
	account kernel.AccountID
	asked   []item.PieceGrant
	added   []item.PieceGrant
	err     error
}

func (w *wardrobe) GrantPieces(_ context.Context, account kernel.AccountID, grants []item.PieceGrant) ([]item.PieceGrant, error) {
	w.account, w.asked = account, grants
	return w.added, w.err
}

// Each rank's piece goes to the item module as a grant for its rank, and comes back as the rank's piece; an error passes as it is.
func TestARanksPieceIsGrantedForItsRank(t *testing.T) {
	account := kernel.NewAccountID()
	w := &wardrobe{added: []item.PieceGrant{{Piece: "helm_gold", Cause: item.PieceFromRank, Ref: "gold"}}}
	got, err := adapter.NewItems(w).GrantRankPieces(context.Background(), account, []model.RankPiece{{Piece: "helm_silver", Rank: "silver"}, {Piece: "helm_gold", Rank: "gold"}})
	if err != nil || w.account != account || !slices.Equal(w.asked, []item.PieceGrant{{Piece: "helm_silver", Cause: "rank", Ref: "silver"}, {Piece: "helm_gold", Cause: "rank", Ref: "gold"}}) {
		t.Fatalf("asked %+v for %v, err %v", w.asked, w.account, err)
	}
	if !slices.Equal(got, []model.RankPiece{{Piece: "helm_gold", Rank: "gold"}}) {
		t.Fatalf("added %+v", got)
	}
	w.err = errors.New("down")
	if _, err := adapter.NewItems(w).GrantRankPieces(context.Background(), account, nil); !errors.Is(err, w.err) {
		t.Fatalf("err %v", err)
	}
}

// Each win feat's piece goes to the item module as a grant for its feat, and comes back as the feat's piece; an error passes as it is.
func TestAWinFeatsPieceIsGrantedForItsFeat(t *testing.T) {
	account := kernel.NewAccountID()
	w := &wardrobe{added: []item.PieceGrant{{Piece: "victors_horns", Cause: item.PieceFromFeat, Ref: "first_pvp_win"}}}
	got, err := adapter.NewItems(w).GrantFeatPieces(context.Background(), account, []model.FeatPiece{{Piece: "victors_horns", Feat: "first_pvp_win"}})
	if err != nil || w.account != account || !slices.Equal(w.asked, []item.PieceGrant{{Piece: "victors_horns", Cause: "feat", Ref: "first_pvp_win"}}) {
		t.Fatalf("asked %+v for %v, err %v", w.asked, w.account, err)
	}
	if !slices.Equal(got, []model.FeatPiece{{Piece: "victors_horns", Feat: "first_pvp_win"}}) {
		t.Fatalf("added %+v", got)
	}
	w.err = errors.New("down")
	if _, err := adapter.NewItems(w).GrantFeatPieces(context.Background(), account, nil); !errors.Is(err, w.err) {
		t.Fatalf("err %v", err)
	}
}
