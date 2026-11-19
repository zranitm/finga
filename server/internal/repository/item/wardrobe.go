package item

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	model "finga/internal/model/item"
	"finga/internal/repository/item/internal/db"
	"finga/internal/shared/kernel"
	"finga/pkg/dbx"
)

// Pieces lists the account's wardrobe, oldest first.
func (p *Postgres) Pieces(ctx context.Context, account kernel.AccountID) ([]model.Piece, error) {
	rows, err := db.New(p.pool).ListPieces(ctx, uuid.UUID(account))
	if err != nil {
		return nil, failures.Classify(err)
	}
	out := make([]model.Piece, 0, len(rows))
	for _, r := range rows {
		piece := model.Piece{ID: r.PieceID, Cause: model.PieceCause(r.CauseType), AddedAt: r.AddedAt}
		if r.ItemID != nil {
			id := kernel.ItemID(*r.ItemID)
			piece.Item = &id
		}
		out = append(out, piece)
	}
	return out, nil
}

// Look is the character's look, the zero Look when it never set one.
func (p *Postgres) Look(ctx context.Context, character kernel.CharacterID) (model.Look, error) {
	r, err := db.New(p.pool).FindLook(ctx, uuid.UUID(character))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Look{}, nil
	}
	if err != nil {
		return model.Look{}, failures.Classify(err)
	}
	return lookOf(db.LockLookRow(r)), nil
}

// closet is the wardrobe in a transaction.
type closet struct {
	q   *db.Queries
	now dbx.Stamp
}

// Add puts piece in the account's wardrobe, from item; false when it has it.
func (c closet) Add(ctx context.Context, account kernel.AccountID, piece string, item kernel.ItemID) (bool, error) {
	id := uuid.UUID(item)
	n, err := c.q.AddPiece(ctx, db.AddPieceParams{AccountID: uuid.UUID(account), PieceID: piece, CauseType: string(model.PieceFromItem), ItemID: &id, Now: c.now.Time()})
	return n == 1, err
}

// Collected is which of pieces the account has.
func (c closet) Collected(ctx context.Context, account kernel.AccountID, pieces []string) ([]string, error) {
	if len(pieces) == 0 {
		return nil, nil
	}
	return c.q.CollectedPieces(ctx, db.CollectedPiecesParams{AccountID: uuid.UUID(account), PieceIds: pieces})
}

// LockLook reads the character's look for a change, the zero Look when it has none.
func (c closet) LockLook(ctx context.Context, character kernel.CharacterID) (model.Look, error) {
	r, err := c.q.LockLook(ctx, uuid.UUID(character))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Look{}, nil
	}
	return lookOf(r), err
}

// SetLook stores the character's look one revision on.
func (c closet) SetLook(ctx context.Context, character kernel.CharacterID, look model.Look) (model.Look, error) {
	revision, err := c.q.SetLook(ctx, db.SetLookParams{
		CharacterID: uuid.UUID(character), Outfit: pick(look.Outfit), Headwear: pick(look.Headwear), Face: pick(look.Face), Weapon: pick(look.Weapon),
		HideHeadwear: look.HideHeadwear, Now: c.now,
	})
	look.Revision = uint64(revision) //nolint:gosec // a revision is never negative
	return look, err
}

// pick is a slot's column: null for the class's own.
func pick(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

func lookOf(r db.LockLookRow) model.Look {
	of := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	return model.Look{Outfit: of(r.Outfit), Headwear: of(r.Headwear), Face: of(r.Face), Weapon: of(r.Weapon), HideHeadwear: r.HideHeadwear, Revision: uint64(r.Revision)} //nolint:gosec // a revision is never negative
}
