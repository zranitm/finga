package pvp

import (
	"context"

	"finga/internal/application/item"
	model "finga/internal/model/pvp"
	"finga/internal/shared/kernel"
)

// ItemModule is the item module as the pvp module grants through it.
type ItemModule interface {
	GrantPieces(ctx context.Context, account kernel.AccountID, grants []item.PieceGrant) ([]item.PieceGrant, error)
}

// Items is the item module as pvp's port.
type Items struct{ m ItemModule }

// NewItems adapts m.
func NewItems(m ItemModule) Items { return Items{m: m} }

// GrantRankPieces puts each rank's piece in the account's wardrobe, giving back those new to it.
func (i Items) GrantRankPieces(ctx context.Context, account kernel.AccountID, pieces []model.RankPiece) ([]model.RankPiece, error) {
	grants := make([]item.PieceGrant, 0, len(pieces))
	for _, p := range pieces {
		grants = append(grants, item.PieceGrant{Piece: p.Piece, Cause: item.PieceFromRank, Ref: p.Rank})
	}
	added, err := i.m.GrantPieces(ctx, account, grants)
	out := make([]model.RankPiece, 0, len(added))
	for _, g := range added {
		out = append(out, model.RankPiece{Piece: g.Piece, Rank: g.Ref})
	}
	return out, err
}
